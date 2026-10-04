package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/enbility/eebus-go/features/client"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Messwerte der Bruecke lesen (MPC und MGCP).
//
// ma/mpc aus eebus-go akzeptiert nur bestimmte Geraete-Entitaeten (Waermepumpe,
// Wallbox, Unterzaehler, ...), nicht die CEM-Entitaet eines Energiemanagers,
// auf der die Bruecke MPC anbietet. Deshalb liest die Steuerbox die Messwerte
// hier selbst: Entitaet aus der Use-Case-Liste der Bruecke suchen, Measurement
// und ElectricalConnection abonnieren und alle Messwerte anzeigen.

type MesswertDaten struct {
	Name      string   `json:"name"`
	Wert      *float64 `json:"wert"` // nil = nicht vorhanden
	Einheit   string   `json:"einheit"`
	Ungueltig bool     `json:"ungueltig"` // ValueState nicht "normal"
}

var scopeNamen = map[model.ScopeTypeType]string{
	model.ScopeTypeTypeACPowerTotal:     "Leistung",
	model.ScopeTypeTypeACPower:          "Leistung",
	model.ScopeTypeTypeACEnergyConsumed: "Energie Bezug",
	model.ScopeTypeTypeACEnergyProduced: "Energie Erzeugung",
	model.ScopeTypeTypeACCurrent:        "Strom",
	model.ScopeTypeTypeACVoltage:        "Spannung",
	model.ScopeTypeTypeACFrequency:      "Frequenz",
	model.ScopeTypeTypeGridFeedIn:       "Energie Einspeisung",
	model.ScopeTypeTypeGridConsumption:  "Energie Bezug",
}

var phasenNamen = map[model.ElectricalConnectionPhaseNameType]string{"a": "L1", "b": "L2", "c": "L3"}

// entitaetFuerUseCase sucht die Entitaet der Bruecke, auf der sie den Use Case
// mit diesem Akteur anbietet.
func (s *Steuerbox) entitaetFuerUseCase(akteur model.UseCaseActorType, name model.UseCaseNameType) spineapi.EntityRemoteInterface {
	s.mu.Lock()
	ski, verbunden := s.partner.SKI, s.verbunden
	s.mu.Unlock()
	if !verbunden || ski == "" {
		return nil
	}
	geraet := s.dienst.LocalDevice().RemoteDeviceForSki(ski)
	if geraet == nil {
		return nil
	}
	for _, uc := range geraet.UseCases() {
		if uc.Actor == nil || *uc.Actor != akteur || uc.Address == nil {
			continue
		}
		for _, u := range uc.UseCaseSupport {
			if u.UseCaseName != nil && *u.UseCaseName == name {
				return geraet.Entity(uc.Address.Entity)
			}
		}
	}
	return nil
}

func (s *Steuerbox) mpcEntitaet() spineapi.EntityRemoteInterface {
	return s.entitaetFuerUseCase(model.UseCaseActorTypeMonitoredUnit, model.UseCaseNameTypeMonitoringOfPowerConsumption)
}

// PflegeMesswerte abonniert die Messwerte der MPC-Entitaet und fordert fehlende
// Daten an. Zyklisch ausserhalb von mu aufrufen.
func (s *Steuerbox) PflegeMesswerte() {
	entitaet := s.mpcEntitaet()
	if entitaet == nil || s.mpc == nil {
		return
	}
	schluessel := fmt.Sprintf("%s/%v", entitaet.Device().Ski(), entitaet.Address().Entity)
	s.mu.Lock()
	neu := s.abonniert != schluessel
	s.abonniert = schluessel
	s.mu.Unlock()

	messung, err := client.NewMeasurement(s.entitaet, entitaet)
	if err != nil {
		return
	}
	if verbindung, err := client.NewElectricalConnection(s.entitaet, entitaet); err == nil && neu {
		if !verbindung.HasSubscription() {
			verbindung.Subscribe()
		}
		verbindung.RequestDescriptions(nil, nil)
		verbindung.RequestParameterDescriptions(nil, nil)
	}
	if neu {
		if !messung.HasSubscription() {
			messung.Subscribe()
		}
		messung.RequestDescriptions(nil, nil)
		messung.RequestConstraints(nil, nil)
		return
	}
	// Beschreibungen da, aber noch keine Werte: einmal anfordern, danach
	// kommen Aenderungen ueber das Abonnement.
	if beschreibungen, err := messung.GetDescriptionsForFilter(model.MeasurementDescriptionDataType{}); err == nil && len(beschreibungen) > 0 {
		if daten, err := messung.GetDataForFilter(model.MeasurementDescriptionDataType{}); err != nil || len(daten) == 0 {
			messung.RequestData(nil, nil)
		}
	}
}

// leseMesswerte liefert alle Messwerte einer Entitaet der Bruecke, nil = keine.
func (s *Steuerbox) leseMesswerte(entitaet spineapi.EntityRemoteInterface) []MesswertDaten {
	if entitaet == nil {
		return nil
	}
	messung, err := client.NewMeasurement(s.entitaet, entitaet)
	if err != nil {
		return nil
	}
	beschreibungen, err := messung.GetDescriptionsForFilter(model.MeasurementDescriptionDataType{})
	if err != nil {
		return nil
	}
	verbindung, _ := client.NewElectricalConnection(s.entitaet, entitaet)

	werte := []MesswertDaten{}
	reihenfolge := map[string]int{}
	for _, d := range beschreibungen {
		if d.MeasurementId == nil || d.ScopeType == nil {
			continue
		}
		name, ok := scopeNamen[*d.ScopeType]
		if !ok {
			name = string(*d.ScopeType)
		}
		if verbindung != nil && *d.ScopeType != model.ScopeTypeTypeACPowerTotal {
			if parameter, err := verbindung.GetParameterDescriptionsForFilter(model.ElectricalConnectionParameterDescriptionDataType{MeasurementId: d.MeasurementId}); err == nil && len(parameter) > 0 && parameter[0].AcMeasuredPhases != nil {
				if phase, ok := phasenNamen[*parameter[0].AcMeasuredPhases]; ok {
					name += " " + phase
				}
			}
		}
		w := MesswertDaten{Name: name}
		if d.Unit != nil {
			w.Einheit = string(*d.Unit)
		}
		if daten, err := messung.GetDataForId(*d.MeasurementId); err == nil && daten.Value != nil {
			wert := daten.Value.GetValue()
			w.Wert = &wert
			w.Ungueltig = daten.ValueState != nil && *daten.ValueState != model.MeasurementValueStateTypeNormal
		}
		reihenfolge[name] = slices.Index(scopeReihenfolge, *d.ScopeType)
		werte = append(werte, w)
	}
	// eebus-go vergibt die IDs der Phasen in zufaelliger Reihenfolge
	slices.SortStableFunc(werte, func(a, b MesswertDaten) int {
		if d := reihenfolge[a.Name] - reihenfolge[b.Name]; d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	return werte
}

var scopeReihenfolge = []model.ScopeTypeType{
	model.ScopeTypeTypeACPowerTotal, model.ScopeTypeTypeACPower,
	model.ScopeTypeTypeACEnergyConsumed, model.ScopeTypeTypeACEnergyProduced,
	model.ScopeTypeTypeGridFeedIn, model.ScopeTypeTypeGridConsumption,
	model.ScopeTypeTypeACCurrent, model.ScopeTypeTypeACVoltage, model.ScopeTypeTypeACFrequency,
}

// leseAnlagenstatus liefert den Betriebszustand der Bruecke (DeviceDiagnosis).
// eg/lpc abonniert die DeviceDiagnosis bereits, der Zustand wird einmal angefordert.
func (s *Steuerbox) leseAnlagenstatus(entitaet spineapi.EntityRemoteInterface) string {
	diagnose, err := client.NewDeviceDiagnosis(s.entitaet, entitaet)
	if err != nil {
		return ""
	}
	if zustand, err := diagnose.GetState(); err == nil && zustand != nil && zustand.OperatingState != nil {
		return string(*zustand.OperatingState)
	}
	schluessel := fmt.Sprintf("%s/%v", entitaet.Device().Ski(), entitaet.Address().Entity)
	s.mu.Lock()
	anfordern := s.zustandAngefordert != schluessel
	s.zustandAngefordert = schluessel
	s.mu.Unlock()
	if anfordern {
		diagnose.RequestState()
	}
	return ""
}

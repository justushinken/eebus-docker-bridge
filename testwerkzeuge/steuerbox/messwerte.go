package main

import (
	"fmt"

	"github.com/enbility/eebus-go/features/client"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
	"github.com/enbility/spine-go/util"
)

// Messwerte der Bruecke lesen (MPC und MGCP).
//
// ma/mpc aus eebus-go akzeptiert nur bestimmte Geraete-Entitaeten (Waermepumpe,
// Wallbox, Unterzaehler, ...), nicht die CEM-Entitaet eines Energiemanagers,
// auf der die Bruecke MPC anbietet. Deshalb liest die Steuerbox die Messwerte
// hier selbst: Entitaet aus der Use-Case-Liste der Bruecke suchen, Measurement
// abonnieren und die Messwerte anzeigen.

type MesswertDaten struct {
	Name      string   `json:"name"`
	Wert      *float64 `json:"wert"` // nil = nicht vorhanden
	Einheit   string   `json:"einheit"`
	Ungueltig bool     `json:"ungueltig"` // ValueState nicht "normal"
}

// Anzeigenamen in dieser Reihenfolge
var scopes = []struct {
	scope model.ScopeTypeType
	name  string
}{
	{model.ScopeTypeTypeACPowerTotal, "Leistung"},
	{model.ScopeTypeTypeACEnergyConsumed, "Energie Bezug"},
	{model.ScopeTypeTypeACEnergyProduced, "Energie Erzeugung"},
	{model.ScopeTypeTypeGridFeedIn, "Energie Einspeisung"},
	{model.ScopeTypeTypeGridConsumption, "Energie Bezug"},
}

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
	werte := []MesswertDaten{}
	for _, sc := range scopes {
		beschreibungen, err := messung.GetDescriptionsForFilter(model.MeasurementDescriptionDataType{ScopeType: util.Ptr(sc.scope)})
		if err != nil || len(beschreibungen) == 0 || beschreibungen[0].MeasurementId == nil {
			continue
		}
		d := beschreibungen[0]
		w := MesswertDaten{Name: sc.name}
		if d.Unit != nil {
			w.Einheit = string(*d.Unit)
		}
		if daten, err := messung.GetDataForId(*d.MeasurementId); err == nil && daten.Value != nil {
			wert := daten.Value.GetValue()
			w.Wert = &wert
			w.Ungueltig = daten.ValueState != nil && *daten.ValueState != model.MeasurementValueStateTypeNormal
		}
		werte = append(werte, w)
	}
	return werte
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

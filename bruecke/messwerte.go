package main

import (
	"log"
	"math"
	"slices"
	"strings"
	"time"

	ucapi "github.com/enbility/eebus-go/usecases/api"
	gcpmgcp "github.com/enbility/eebus-go/usecases/gcp/mgcp"
	mumpc "github.com/enbility/eebus-go/usecases/mu/mpc"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
	"github.com/enbility/spine-go/util"
)

// Messwerte fuer MPC (Anlage) und MGCP (Netzanschlusspunkt): je Leistung und
// Energie. Mehr verlangen weder der FNN-Hinweis (4.1.2.3: aktuelle
// Wirkleistung) noch die Pflicht-Szenarien der Use Cases. Die SPS schreibt die
// Werte in die Holding-Register und meldet per Gueltigkeitsmaske, welche
// gerade stimmen. Die Bruecke gibt sie nur weiter.

// messgroesse ist ein angebotener Messwert: woher er kommt (Holding-Register,
// Gueltigkeitsbit) und wie er an eebus-go geht.
type messgroesse struct {
	name    string
	einheit string
	maske   int // Holding-Register der Gueltigkeitsmaske
	bit     int
	lies    func(r *[anzahlHoldingRegister]uint16) float64
	update  func(wert float64, zustand *model.MeasurementValueStateType) ucapi.UpdateData
}

// --- Register lesen ---

func dint(reg int) func(r *[anzahlHoldingRegister]uint16) float64 {
	return func(r *[anzahlHoldingRegister]uint16) float64 {
		return float64(int32(zuUint32(r[reg], r[reg+1])))
	}
}

func ulint(reg int) func(r *[anzahlHoldingRegister]uint16) float64 {
	return func(r *[anzahlHoldingRegister]uint16) float64 {
		return float64(uint64(r[reg])<<48 | uint64(r[reg+1])<<32 | uint64(r[reg+2])<<16 | uint64(r[reg+3]))
	}
}

// mitZustand passt die Update-Funktionen von eebus-go an (ohne Zeitstempel).
func mitZustand[T ucapi.UpdateData](f func(float64, *time.Time, *model.MeasurementValueStateType) T) func(float64, *model.MeasurementValueStateType) ucapi.UpdateData {
	return func(w float64, z *model.MeasurementValueStateType) ucapi.UpdateData { return f(w, nil, z) }
}

// mitZeitraum ebenso fuer die Energie-Funktionen (ohne Auswertezeitraum).
func mitZeitraum[T ucapi.UpdateData](f func(float64, *time.Time, *model.MeasurementValueStateType, *time.Time, *time.Time) T) func(float64, *model.MeasurementValueStateType) ucapi.UpdateData {
	return func(w float64, z *model.MeasurementValueStateType) ucapi.UpdateData { return f(w, nil, z, nil, nil) }
}

// --- MPC ---

func NeuesMpc(entitaet spineapi.EntityLocalInterface, konf Konfiguration) (*mumpc.MPC, []messgroesse, error) {
	quelle := util.Ptr(konf.MesswertQuelle)
	mpc, err := mumpc.NewMPC(entitaet, nil,
		&mumpc.MonitorPowerConfig{ConnectedPhases: model.ElectricalConnectionPhaseNameTypeAbc, ValueSourceTotal: quelle},
		&mumpc.MonitorEnergyConfig{ValueSourceConsumption: quelle, ValueSourceProduction: quelle},
		nil, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	groessen := []messgroesse{
		{"Leistung", "W", regMpcMaske, 0, dint(regMpcP), mitZustand(mpc.UpdateDataPowerTotal)},
		{"Energie Bezug", "Wh", regMpcMaske, 1, ulint(regMpcEBezug), mitZeitraum(mpc.UpdateDataEnergyConsumed)},
		{"Energie Erzeugung", "Wh", regMpcMaske, 2, ulint(regMpcEErzeugung), mitZeitraum(mpc.UpdateDataEnergyProduced)},
	}
	return mpc, groessen, nil
}

// --- MGCP ---

func NeuesMgcp(entitaet spineapi.EntityLocalInterface, konf Konfiguration) (*gcpmgcp.MGCP, []messgroesse, error) {
	quelle := util.Ptr(konf.MesswertQuelle)
	mgcp, err := gcpmgcp.NewMGCP(entitaet, nil, nil,
		&gcpmgcp.MonitorPowerConfig{ValueSource: quelle},
		&gcpmgcp.MonitorEnergyConfig{ValueSourceProduction: quelle, ValueSourceConsumption: quelle},
		nil, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	// Workaround fuer eebus-go (Stand 30.09.2026): gcp/mgcp kuendigt den Use
	// Case mit dem Akteur der Gegenseite (MonitoringAppliance) an. Richtig ist
	// GridConnectionPoint, sonst erkennt die Steuerbox MGCP nicht. Entfernen,
	// sobald eebus-go das behebt.
	mgcp.UseCaseActor = model.UseCaseActorTypeGridConnectionPoint

	groessen := []messgroesse{
		{"Leistung", "W", regMgcpMaske, 0, dint(regMgcpP), mitZustand(mgcp.UpdateDataPowerTotal)},
		{"Energie Einspeisung", "Wh", regMgcpMaske, 1, ulint(regMgcpEEinspeisung), mitZeitraum(mgcp.UpdateDataEnergyFeedIn)},
		{"Energie Bezug", "Wh", regMgcpMaske, 2, ulint(regMgcpEBezug), mitZeitraum(mgcp.UpdateDataEnergyConsumed)},
	}
	return mgcp, groessen, nil
}

// --- Weitergeben ---

// messwertStand merkt sich, was zuletzt an den EEBUS-Stack ging. Unter mu.
type messwertStand struct {
	gesendet bool
	register [anzahlHoldingRegister]uint16
	spsOk    bool
	fehler   string
}

func gueltig(r *[anzahlHoldingRegister]uint16, spsOk bool, g messgroesse) bool {
	return spsOk && r[g.maske]&(1<<g.bit) != 0
}

// updates baut die Aenderungen fuer eebus-go. "normal" wird ausdruecklich
// mitgeschickt: Die Updates sind partiell, ohne ValueState bliebe ein frueher
// gesendetes "error" bei der Steuerbox stehen.
func updates(groessen []messgroesse, r *[anzahlHoldingRegister]uint16, spsOk bool) []ucapi.UpdateData {
	var liste []ucapi.UpdateData
	for _, g := range groessen {
		zustand := model.MeasurementValueStateTypeError
		if gueltig(r, spsOk, g) {
			zustand = model.MeasurementValueStateTypeNormal
		}
		liste = append(liste, g.update(g.lies(r), &zustand))
	}
	return liste
}

// sendeMesswerte gibt geaenderte Register an den EEBUS-Stack weiter.
// Ungueltige Werte (Masken-Bit fehlt, SPS ausgefallen) gehen mit dem
// ValueState "error" raus, die Steuerbox verwirft sie dann.
func (b *Bruecke) sendeMesswerte() {
	if b.mpc == nil && b.mgcp == nil {
		return
	}
	b.mu.Lock()
	register, spsOk := b.holding, b.spsOk
	stand := &b.messwerte
	geaendert := !stand.gesendet || stand.spsOk != spsOk ||
		!slices.Equal(register[regMesswerteAnfang:], stand.register[regMesswerteAnfang:])
	if geaendert {
		stand.gesendet, stand.register, stand.spsOk = true, register, spsOk
	}
	b.mu.Unlock()
	if !geaendert {
		return
	}

	var fehler []string
	if b.mpc != nil {
		// MPC erwartet Messwert-Updates, die Liste enthaelt nur solche
		var mpcUpdates []ucapi.UpdateMeasurementData
		for _, u := range updates(b.mpcGroessen, &register, spsOk) {
			mpcUpdates = append(mpcUpdates, u.(ucapi.UpdateMeasurementData))
		}
		if err := b.mpc.Update(mpcUpdates...); err != nil {
			fehler = append(fehler, "MPC: "+err.Error())
		}
	}
	if b.mgcp != nil {
		if err := b.mgcp.Update(updates(b.mgcpGroessen, &register, spsOk)...); err != nil {
			fehler = append(fehler, "MGCP: "+err.Error())
		}
	}

	text := strings.Join(fehler, "; ")
	b.mu.Lock()
	neu := text != stand.fehler
	stand.fehler = text
	if text != "" {
		stand.gesendet = false // im naechsten Takt erneut versuchen
	}
	b.mu.Unlock()
	if neu && text != "" {
		log.Printf("Messwerte weitergeben: %s", text)
	}
}

// --- Anzeige im UI ---

type Messwert struct {
	Name    string  `json:"name"`
	Wert    float64 `json:"wert"`
	Einheit string  `json:"einheit"`
	Gueltig bool    `json:"gueltig"`
}

// messwerteUi liefert die angebotenen Messwerte mit aktuellem Stand, nil =
// Use Case aus. Aufruf unter mu.
func (b *Bruecke) messwerteUi(groessen []messgroesse) []Messwert {
	if groessen == nil {
		return nil
	}
	liste := make([]Messwert, 0, len(groessen))
	for _, g := range groessen {
		liste = append(liste, Messwert{
			Name:    g.name,
			Wert:    math.Round(g.lies(&b.holding)),
			Einheit: g.einheit,
			Gueltig: gueltig(&b.holding, b.spsOk, g),
		})
	}
	return liste
}

package main

import (
	"fmt"
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

// Messwerte fuer MPC (Anlage) und MGCP (Netzanschlusspunkt). Die SPS schreibt
// sie in die Holding-Register, die Bruecke gibt sie nur weiter. Welche Werte
// angekuendigt werden (Szenarien), steht in MPC_MESSWERTE und MGCP_MESSWERTE;
// ob ein Wert gerade gueltig ist, in den Gueltigkeitsmasken der SPS.

// Namen in MPC_MESSWERTE / MGCP_MESSWERTE
const (
	mwPhasenleistung   = "phasenleistung"
	mwEnergieBezug     = "energie_bezug"
	mwEnergieErzeugung = "energie_erzeugung"
	mwStrom            = "strom"
	mwSpannung         = "spannung"
	mwFrequenz         = "frequenz"
	mwPvFaktor         = "pv_faktor"
)

var (
	mpcMesswerteErlaubt  = []string{mwPhasenleistung, mwEnergieBezug, mwEnergieErzeugung, mwStrom, mwSpannung, mwFrequenz}
	mgcpMesswerteErlaubt = []string{mwStrom, mwSpannung, mwFrequenz, mwPvFaktor}
)

// messgroesse ist ein angebotener Messwert: woher er kommt (Holding-Register,
// Gueltigkeitsbit) und wie er an eebus-go geht. Die Liste je Use Case entsteht
// einmal beim Start aus der Konfiguration (mpcMessgroessen, mgcpMessgroessen).
type messgroesse struct {
	name    string
	einheit string
	maske   int // Holding-Register der Gueltigkeitsmaske
	bit     int
	lies    func(r *[anzahlHoldingRegister]uint16) float64
	update  func(wert float64, zustand *model.MeasurementValueStateType) ucapi.UpdateData
	// Konfigurationswert ohne ValueState (PV-Faktor): ungueltig heisst, ihn
	// nicht zu aendern.
	ohneZustand bool
}

// --- Register lesen ---

func dint(reg int, teiler float64) func(r *[anzahlHoldingRegister]uint16) float64 {
	return func(r *[anzahlHoldingRegister]uint16) float64 {
		return float64(int32(zuUint32(r[reg], r[reg+1]))) / teiler
	}
}

func ulint(reg int) func(r *[anzahlHoldingRegister]uint16) float64 {
	return func(r *[anzahlHoldingRegister]uint16) float64 {
		return float64(uint64(r[reg])<<48 | uint64(r[reg+1])<<32 | uint64(r[reg+2])<<16 | uint64(r[reg+3]))
	}
}

func uint16Wert(reg int, teiler float64) func(r *[anzahlHoldingRegister]uint16) float64 {
	return func(r *[anzahlHoldingRegister]uint16) float64 { return float64(r[reg]) / teiler }
}

// mitZustand passt die Update-Funktionen von eebus-go an (ohne Zeitstempel).
func mitZustand[T ucapi.UpdateData](f func(float64, *time.Time, *model.MeasurementValueStateType) T) func(float64, *model.MeasurementValueStateType) ucapi.UpdateData {
	return func(w float64, z *model.MeasurementValueStateType) ucapi.UpdateData { return f(w, nil, z) }
}

func phasenIndex(p model.ElectricalConnectionPhaseNameType) int {
	return strings.Index("abc", string(p))
}

// --- MPC ---

func NeuesMpc(entitaet spineapi.EntityLocalInterface, konf Konfiguration) (*mumpc.MPC, error) {
	quelle := util.Ptr(konf.MesswertQuelle)
	jePhase := func() mumpc.PhaseMeasurementSourceMap {
		m := mumpc.PhaseMeasurementSourceMap{}
		for _, p := range konf.MpcPhasen {
			m[p] = quelle
		}
		return m
	}
	auswahl := konf.MpcMesswerte

	leistung := &mumpc.MonitorPowerConfig{
		ConnectedPhases:  model.ElectricalConnectionPhaseNameType(konf.MpcPhasenText),
		ValueSourceTotal: quelle,
	}
	if auswahl[mwPhasenleistung] {
		leistung.ValueSourcePerPhase = jePhase()
	}
	var energie *mumpc.MonitorEnergyConfig
	if auswahl[mwEnergieBezug] || auswahl[mwEnergieErzeugung] {
		energie = &mumpc.MonitorEnergyConfig{}
		if auswahl[mwEnergieBezug] {
			energie.ValueSourceConsumption = quelle
		}
		if auswahl[mwEnergieErzeugung] {
			energie.ValueSourceProduction = quelle
		}
	}
	var strom *mumpc.MonitorCurrentConfig
	if auswahl[mwStrom] {
		strom = &mumpc.MonitorCurrentConfig{ValueSourcePerPhase: jePhase()}
	}
	var spannung *mumpc.MonitorVoltageConfig
	if auswahl[mwSpannung] {
		spannung = &mumpc.MonitorVoltageConfig{ValueSourcePerPhase: jePhase()}
	}
	var frequenz *mumpc.MonitorFrequencyConfig
	if auswahl[mwFrequenz] {
		frequenz = &mumpc.MonitorFrequencyConfig{ValueSource: quelle}
	}
	return mumpc.NewMPC(entitaet, nil, leistung, energie, strom, spannung, frequenz)
}

// mpcMessgroessen: Bits der Maske siehe README (Holding-Register 5).
func mpcMessgroessen(mpc *mumpc.MPC, konf Konfiguration) []messgroesse {
	auswahl := konf.MpcMesswerte
	liste := []messgroesse{
		{name: "Leistung", einheit: "W", bit: 0, lies: dint(regMpcP, 1), update: mitZustand(mpc.UpdateDataPowerTotal)},
	}
	type phasenwert = func(float64, *time.Time, *model.MeasurementValueStateType) ucapi.UpdateMeasurementData
	jePhase := func(name, einheit string, bit int, lies func(phase int) func(*[anzahlHoldingRegister]uint16) float64, f [3]phasenwert) {
		for _, p := range konf.MpcPhasen {
			i := phasenIndex(p)
			liste = append(liste, messgroesse{name: fmt.Sprintf("%s L%d", name, i+1), einheit: einheit, bit: bit + i, lies: lies(i), update: mitZustand(f[i])})
		}
	}
	if auswahl[mwPhasenleistung] {
		jePhase("Leistung", "W", 1, func(i int) func(*[anzahlHoldingRegister]uint16) float64 { return dint(regMpcPL1+2*i, 1) },
			[3]phasenwert{mpc.UpdateDataPowerPhaseA, mpc.UpdateDataPowerPhaseB, mpc.UpdateDataPowerPhaseC})
	}
	if auswahl[mwEnergieBezug] {
		liste = append(liste, messgroesse{name: "Energie Bezug", einheit: "Wh", bit: 4, lies: ulint(regMpcEBezug),
			update: func(w float64, z *model.MeasurementValueStateType) ucapi.UpdateData {
				return mpc.UpdateDataEnergyConsumed(w, nil, z, nil, nil)
			}})
	}
	if auswahl[mwEnergieErzeugung] {
		liste = append(liste, messgroesse{name: "Energie Erzeugung", einheit: "Wh", bit: 5, lies: ulint(regMpcEErzeugung),
			update: func(w float64, z *model.MeasurementValueStateType) ucapi.UpdateData {
				return mpc.UpdateDataEnergyProduced(w, nil, z, nil, nil)
			}})
	}
	if auswahl[mwStrom] {
		jePhase("Strom", "A", 6, func(i int) func(*[anzahlHoldingRegister]uint16) float64 { return dint(regMpcIL1+2*i, 1000) },
			[3]phasenwert{mpc.UpdateDataCurrentPhaseA, mpc.UpdateDataCurrentPhaseB, mpc.UpdateDataCurrentPhaseC})
	}
	if auswahl[mwSpannung] {
		jePhase("Spannung", "V", 9, func(i int) func(*[anzahlHoldingRegister]uint16) float64 { return uint16Wert(regMpcUL1+i, 10) },
			[3]phasenwert{mpc.UpdateDataVoltagePhaseA, mpc.UpdateDataVoltagePhaseB, mpc.UpdateDataVoltagePhaseC})
	}
	if auswahl[mwFrequenz] {
		liste = append(liste, messgroesse{name: "Frequenz", einheit: "Hz", bit: 12, lies: uint16Wert(regMpcF, 100), update: mitZustand(mpc.UpdateDataFrequency)})
	}
	for i := range liste {
		liste[i].maske = regMpcMaske
	}
	return liste
}

// --- MGCP ---

func NeuesMgcp(entitaet spineapi.EntityLocalInterface, konf Konfiguration) (*gcpmgcp.MGCP, error) {
	quelle := util.Ptr(konf.MesswertQuelle)
	auswahl := konf.MgcpMesswerte

	var pv *gcpmgcp.MonitorPvFeedInPowerLimitationFactorConfig
	if auswahl[mwPvFaktor] {
		pv = &gcpmgcp.MonitorPvFeedInPowerLimitationFactorConfig{}
	}
	var strom *gcpmgcp.MonitorCurrentConfig
	if auswahl[mwStrom] {
		strom = &gcpmgcp.MonitorCurrentConfig{ValueSourcePhaseA: quelle, ValueSourcePhaseB: quelle, ValueSourcePhaseC: quelle}
	}
	var spannung *gcpmgcp.MonitorVoltageConfig
	if auswahl[mwSpannung] {
		spannung = &gcpmgcp.MonitorVoltageConfig{ValueSourcePhaseA: quelle, ValueSourcePhaseB: quelle, ValueSourcePhaseC: quelle}
	}
	var frequenz *gcpmgcp.MonitorFrequencyConfig
	if auswahl[mwFrequenz] {
		frequenz = &gcpmgcp.MonitorFrequencyConfig{ValueSource: quelle}
	}
	mgcp, err := gcpmgcp.NewMGCP(entitaet, nil, pv,
		&gcpmgcp.MonitorPowerConfig{ValueSource: quelle},
		&gcpmgcp.MonitorEnergyConfig{ValueSourceProduction: quelle, ValueSourceConsumption: quelle},
		strom, spannung, frequenz)
	if err != nil {
		return nil, err
	}
	// Workaround fuer eebus-go (Stand 30.09.2026): gcp/mgcp kuendigt den Use
	// Case mit dem Akteur der Gegenseite (MonitoringAppliance) an. Richtig ist
	// GridConnectionPoint, sonst erkennt die Steuerbox MGCP nicht. Entfernen,
	// sobald eebus-go das behebt.
	mgcp.UseCaseActor = model.UseCaseActorTypeGridConnectionPoint
	return mgcp, nil
}

// mgcpMessgroessen: Bits der Maske siehe README (Holding-Register 6).
func mgcpMessgroessen(mgcp *gcpmgcp.MGCP, konf Konfiguration) []messgroesse {
	auswahl := konf.MgcpMesswerte
	liste := []messgroesse{
		{name: "Leistung", einheit: "W", bit: 0, lies: dint(regMgcpP, 1), update: mitZustand(mgcp.UpdateDataPowerTotal)},
		{name: "Energie Einspeisung", einheit: "Wh", bit: 1, lies: ulint(regMgcpEEinspeisung),
			update: func(w float64, z *model.MeasurementValueStateType) ucapi.UpdateData {
				return mgcp.UpdateDataEnergyFeedIn(w, nil, z, nil, nil)
			}},
		{name: "Energie Bezug", einheit: "Wh", bit: 2, lies: ulint(regMgcpEBezug),
			update: func(w float64, z *model.MeasurementValueStateType) ucapi.UpdateData {
				return mgcp.UpdateDataEnergyConsumed(w, nil, z, nil, nil)
			}},
	}
	type phasenwert = func(float64, *time.Time, *model.MeasurementValueStateType) ucapi.UpdateData
	if auswahl[mwStrom] {
		for i, f := range [3]phasenwert{mgcp.UpdateDataCurrentPhaseA, mgcp.UpdateDataCurrentPhaseB, mgcp.UpdateDataCurrentPhaseC} {
			liste = append(liste, messgroesse{name: fmt.Sprintf("Strom L%d", i+1), einheit: "A", bit: 3 + i, lies: dint(regMgcpIL1+2*i, 1000), update: mitZustand(f)})
		}
	}
	if auswahl[mwSpannung] {
		for i, f := range [3]phasenwert{mgcp.UpdateDataVoltagePhaseA, mgcp.UpdateDataVoltagePhaseB, mgcp.UpdateDataVoltagePhaseC} {
			liste = append(liste, messgroesse{name: fmt.Sprintf("Spannung L%d", i+1), einheit: "V", bit: 6 + i, lies: uint16Wert(regMgcpUL1+i, 10), update: mitZustand(f)})
		}
	}
	if auswahl[mwFrequenz] {
		liste = append(liste, messgroesse{name: "Frequenz", einheit: "Hz", bit: 9, lies: uint16Wert(regMgcpF, 100), update: mitZustand(mgcp.UpdateDataFrequency)})
	}
	if auswahl[mwPvFaktor] {
		liste = append(liste, messgroesse{name: "PV-Begrenzungsfaktor", einheit: "%", bit: 10, lies: uint16Wert(regMgcpPvFaktor, 10), ohneZustand: true,
			update: func(w float64, _ *model.MeasurementValueStateType) ucapi.UpdateData {
				return mgcp.UpdateDataPowerLimitationFactor(w)
			}})
	}
	for i := range liste {
		liste[i].maske = regMgcpMaske
	}
	return liste
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
		} else if g.ohneZustand {
			continue
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
			Wert:    math.Round(g.lies(&b.holding)*1000) / 1000,
			Einheit: g.einheit,
			Gueltig: gueltig(&b.holding, b.spsOk, g),
		})
	}
	return liste
}

package main

import (
	"fmt"
	"log"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/enbility/eebus-go/api"
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

// Bits der Gueltigkeitsmasken (Holding-Register 5 und 6)
const (
	mpcBitP          = 0
	mpcBitPL1        = 1 // bis 3
	mpcBitEBezug     = 4
	mpcBitEErzeugung = 5
	mpcBitIL1        = 6 // bis 8
	mpcBitUL1        = 9 // bis 11
	mpcBitF          = 12

	mgcpBitP            = 0
	mgcpBitEEinspeisung = 1
	mgcpBitEBezug       = 2
	mgcpBitIL1          = 3 // bis 5
	mgcpBitUL1          = 6 // bis 8
	mgcpBitF            = 9
	mgcpBitPv           = 10
)

// messwerte sind die dekodierten Holding-Register.
type messwerte struct {
	mpcMaske, mgcpMaske uint16

	mpcP, mpcEBezug, mpcEErzeugung, mpcF float64
	mpcPPhase, mpcI, mpcU                [3]float64

	mgcpP, mgcpEEinspeisung, mgcpEBezug, mgcpF, mgcpPv float64
	mgcpI, mgcpU                                       [3]float64
}

func leseMesswerte(r *[anzahlHoldingRegister]uint16) messwerte {
	i32 := func(i int) float64 { return float64(int32(zuUint32(r[i], r[i+1]))) }
	u64 := func(i int) float64 {
		return float64(uint64(r[i])<<48 | uint64(r[i+1])<<32 | uint64(r[i+2])<<16 | uint64(r[i+3]))
	}
	m := messwerte{
		mpcMaske: r[regMpcMaske], mgcpMaske: r[regMgcpMaske],

		mpcP:          i32(regMpcP),
		mpcEBezug:     u64(regMpcEBezug),
		mpcEErzeugung: u64(regMpcEErzeugung),
		mpcF:          float64(r[regMpcF]) / 100,

		mgcpP:            i32(regMgcpP),
		mgcpEEinspeisung: u64(regMgcpEEinspeisung),
		mgcpEBezug:       u64(regMgcpEBezug),
		mgcpF:            float64(r[regMgcpF]) / 100,
		mgcpPv:           float64(r[regMgcpPvFaktor]) / 10,
	}
	for p := range 3 {
		m.mpcPPhase[p] = i32(regMpcPL1 + 2*p)
		m.mpcI[p] = i32(regMpcIL1+2*p) / 1000
		m.mpcU[p] = float64(r[regMpcUL1+p]) / 10
		m.mgcpI[p] = i32(regMgcpIL1+2*p) / 1000
		m.mgcpU[p] = float64(r[regMgcpUL1+p]) / 10
	}
	return m
}

// messwertStand merkt sich, was zuletzt an den EEBUS-Stack ging. Unter mu.
type messwertStand struct {
	gesendet bool
	register [anzahlHoldingRegister]uint16
	spsOk    bool
	fehler   string
}

// --- Einrichten ---

func phasenIndex(p model.ElectricalConnectionPhaseNameType) int {
	return strings.Index("abc", string(p))
}

func NeuesMpc(entitaet spineapi.EntityLocalInterface, cb api.EntityEventCallback, konf Konfiguration) (*mumpc.MPC, error) {
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
	return mumpc.NewMPC(entitaet, cb, leistung, energie, strom, spannung, frequenz)
}

func NeuesMgcp(entitaet spineapi.EntityLocalInterface, cb api.EntityEventCallback, konf Konfiguration) (*gcpmgcp.MGCP, error) {
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
	mgcp, err := gcpmgcp.NewMGCP(entitaet, cb, pv,
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

// --- Weitergeben ---

type updateFunktion func(float64, *time.Time, *model.MeasurementValueStateType) ucapi.UpdateMeasurementData

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

	m := leseMesswerte(&register)
	var fehler []string
	if b.mpc != nil {
		if err := b.mpc.Update(b.mpcUpdates(m, spsOk)...); err != nil {
			fehler = append(fehler, "MPC: "+err.Error())
		}
	}
	if b.mgcp != nil {
		if err := b.mgcp.Update(b.mgcpUpdates(m, spsOk)...); err != nil {
			fehler = append(fehler, "MGCP: "+err.Error())
		}
	}

	text := strings.Join(fehler, "; ")
	b.mu.Lock()
	neu := text != stand.fehler
	stand.fehler = text
	b.mu.Unlock()
	if neu && text != "" {
		log.Printf("Messwerte weitergeben: %s", text)
	}
}

// gueltigkeit liefert den ValueState je Masken-Bit. "normal" wird ausdruecklich
// mitgeschickt: Die Updates sind partiell, ohne ValueState bliebe ein frueher
// gesendetes "error" bei der Steuerbox stehen.
func gueltigkeit(spsOk bool, maske uint16) func(bit int) *model.MeasurementValueStateType {
	return func(bit int) *model.MeasurementValueStateType {
		if spsOk && maske&(1<<bit) != 0 {
			return util.Ptr(model.MeasurementValueStateTypeNormal)
		}
		return util.Ptr(model.MeasurementValueStateTypeError)
	}
}

func istGueltig(zustand *model.MeasurementValueStateType) bool {
	return *zustand == model.MeasurementValueStateTypeNormal
}

func (b *Bruecke) mpcUpdates(m messwerte, spsOk bool) []ucapi.UpdateMeasurementData {
	zustand := gueltigkeit(spsOk, m.mpcMaske)
	auswahl := b.konf.MpcMesswerte
	u := []ucapi.UpdateMeasurementData{b.mpc.UpdateDataPowerTotal(m.mpcP, nil, zustand(mpcBitP))}

	jePhase := func(funktionen [3]updateFunktion, werte [3]float64, bit int) {
		for _, p := range b.konf.MpcPhasen {
			i := phasenIndex(p)
			u = append(u, funktionen[i](werte[i], nil, zustand(bit+i)))
		}
	}
	if auswahl[mwPhasenleistung] {
		jePhase([3]updateFunktion{b.mpc.UpdateDataPowerPhaseA, b.mpc.UpdateDataPowerPhaseB, b.mpc.UpdateDataPowerPhaseC}, m.mpcPPhase, mpcBitPL1)
	}
	if auswahl[mwEnergieBezug] {
		u = append(u, b.mpc.UpdateDataEnergyConsumed(m.mpcEBezug, nil, zustand(mpcBitEBezug), nil, nil))
	}
	if auswahl[mwEnergieErzeugung] {
		u = append(u, b.mpc.UpdateDataEnergyProduced(m.mpcEErzeugung, nil, zustand(mpcBitEErzeugung), nil, nil))
	}
	if auswahl[mwStrom] {
		jePhase([3]updateFunktion{b.mpc.UpdateDataCurrentPhaseA, b.mpc.UpdateDataCurrentPhaseB, b.mpc.UpdateDataCurrentPhaseC}, m.mpcI, mpcBitIL1)
	}
	if auswahl[mwSpannung] {
		jePhase([3]updateFunktion{b.mpc.UpdateDataVoltagePhaseA, b.mpc.UpdateDataVoltagePhaseB, b.mpc.UpdateDataVoltagePhaseC}, m.mpcU, mpcBitUL1)
	}
	if auswahl[mwFrequenz] {
		u = append(u, b.mpc.UpdateDataFrequency(m.mpcF, nil, zustand(mpcBitF)))
	}
	return u
}

func (b *Bruecke) mgcpUpdates(m messwerte, spsOk bool) []ucapi.UpdateData {
	zustand := gueltigkeit(spsOk, m.mgcpMaske)
	auswahl := b.konf.MgcpMesswerte
	u := []ucapi.UpdateData{
		b.mgcp.UpdateDataPowerTotal(m.mgcpP, nil, zustand(mgcpBitP)),
		b.mgcp.UpdateDataEnergyFeedIn(m.mgcpEEinspeisung, nil, zustand(mgcpBitEEinspeisung), nil, nil),
		b.mgcp.UpdateDataEnergyConsumed(m.mgcpEBezug, nil, zustand(mgcpBitEBezug), nil, nil),
	}
	if auswahl[mwStrom] {
		u = append(u,
			b.mgcp.UpdateDataCurrentPhaseA(m.mgcpI[0], nil, zustand(mgcpBitIL1)),
			b.mgcp.UpdateDataCurrentPhaseB(m.mgcpI[1], nil, zustand(mgcpBitIL1+1)),
			b.mgcp.UpdateDataCurrentPhaseC(m.mgcpI[2], nil, zustand(mgcpBitIL1+2)))
	}
	if auswahl[mwSpannung] {
		u = append(u,
			b.mgcp.UpdateDataVoltagePhaseA(m.mgcpU[0], nil, zustand(mgcpBitUL1)),
			b.mgcp.UpdateDataVoltagePhaseB(m.mgcpU[1], nil, zustand(mgcpBitUL1+1)),
			b.mgcp.UpdateDataVoltagePhaseC(m.mgcpU[2], nil, zustand(mgcpBitUL1+2)))
	}
	if auswahl[mwFrequenz] {
		u = append(u, b.mgcp.UpdateDataFrequency(m.mgcpF, nil, zustand(mgcpBitF)))
	}
	// Der Begrenzungsfaktor ist ein Konfigurationswert ohne ValueState:
	// ungueltig heisst hier, ihn nicht zu aendern.
	if auswahl[mwPvFaktor] && istGueltig(zustand(mgcpBitPv)) {
		u = append(u, b.mgcp.UpdateDataPowerLimitationFactor(m.mgcpPv))
	}
	return u
}

// --- Anzeige im UI ---

type Messwert struct {
	Name    string  `json:"name"`
	Wert    float64 `json:"wert"`
	Einheit string  `json:"einheit"`
	Gueltig bool    `json:"gueltig"`
}

// messwerteUi liefert die angekuendigten Messwerte mit aktuellem Stand. Unter mu.
func (b *Bruecke) messwerteUi() (mpc, mgcp []Messwert) {
	m := leseMesswerte(&b.holding)
	if b.mpc != nil {
		gueltig := func(bit int) bool { return b.spsOk && m.mpcMaske&(1<<bit) != 0 }
		auswahl := b.konf.MpcMesswerte
		mpc = append(mpc, Messwert{"Leistung", m.mpcP, "W", gueltig(mpcBitP)})
		jePhase := func(name, einheit string, werte [3]float64, bit int) {
			for _, p := range b.konf.MpcPhasen {
				i := phasenIndex(p)
				mpc = append(mpc, Messwert{fmt.Sprintf("%s L%d", name, i+1), werte[i], einheit, gueltig(bit + i)})
			}
		}
		if auswahl[mwPhasenleistung] {
			jePhase("Leistung", "W", m.mpcPPhase, mpcBitPL1)
		}
		if auswahl[mwEnergieBezug] {
			mpc = append(mpc, Messwert{"Energie Bezug", m.mpcEBezug, "Wh", gueltig(mpcBitEBezug)})
		}
		if auswahl[mwEnergieErzeugung] {
			mpc = append(mpc, Messwert{"Energie Erzeugung", m.mpcEErzeugung, "Wh", gueltig(mpcBitEErzeugung)})
		}
		if auswahl[mwStrom] {
			jePhase("Strom", "A", m.mpcI, mpcBitIL1)
		}
		if auswahl[mwSpannung] {
			jePhase("Spannung", "V", m.mpcU, mpcBitUL1)
		}
		if auswahl[mwFrequenz] {
			mpc = append(mpc, Messwert{"Frequenz", m.mpcF, "Hz", gueltig(mpcBitF)})
		}
	}
	if b.mgcp != nil {
		gueltig := func(bit int) bool { return b.spsOk && m.mgcpMaske&(1<<bit) != 0 }
		auswahl := b.konf.MgcpMesswerte
		mgcp = append(mgcp,
			Messwert{"Leistung", m.mgcpP, "W", gueltig(mgcpBitP)},
			Messwert{"Energie Einspeisung", m.mgcpEEinspeisung, "Wh", gueltig(mgcpBitEEinspeisung)},
			Messwert{"Energie Bezug", m.mgcpEBezug, "Wh", gueltig(mgcpBitEBezug)})
		jePhase := func(name, einheit string, werte [3]float64, bit int) {
			for i := range 3 {
				mgcp = append(mgcp, Messwert{fmt.Sprintf("%s L%d", name, i+1), werte[i], einheit, gueltig(bit + i)})
			}
		}
		if auswahl[mwStrom] {
			jePhase("Strom", "A", m.mgcpI, mgcpBitIL1)
		}
		if auswahl[mwSpannung] {
			jePhase("Spannung", "V", m.mgcpU, mgcpBitUL1)
		}
		if auswahl[mwFrequenz] {
			mgcp = append(mgcp, Messwert{"Frequenz", m.mgcpF, "Hz", gueltig(mgcpBitF)})
		}
		if auswahl[mwPvFaktor] {
			mgcp = append(mgcp, Messwert{"PV-Begrenzungsfaktor", m.mgcpPv, "%", gueltig(mgcpBitPv)})
		}
	}
	for i := range mpc {
		mpc[i].Wert = math.Round(mpc[i].Wert*1000) / 1000
	}
	for i := range mgcp {
		mgcp[i].Wert = math.Round(mgcp[i].Wert*1000) / 1000
	}
	return mpc, mgcp
}

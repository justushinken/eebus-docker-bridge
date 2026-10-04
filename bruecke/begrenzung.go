package main

import (
	"log"
	"time"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/features/server"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	cslpp "github.com/enbility/eebus-go/usecases/cs/lpp"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
	"github.com/enbility/spine-go/util"
)

// Begrenzung ist eine Richtung der Leistungsbegrenzung: Bezug (LPC) oder
// Einspeisung (LPP). Beide haben denselben Zustandsautomaten, teilen sich aber
// Heartbeat und Failsafe-Mindestdauer (gleiche Entitaet, gleicher Schluessel
// in der DeviceConfiguration).
//
// Die Felder ab zustand gehoeren zum Zustand der Bruecke: Zugriff nur unter
// Bruecke.mu. uc wird nur ausserhalb von mu aufgerufen.
type Begrenzung struct {
	name string // "LPC" oder "LPP", fuer Log und UI
	text string // "Grenze" bzw. "Einspeisegrenze"
	uc   begrenzungsUseCase

	zustand               LpcZustand
	zustandSeit           time.Time
	grenze                ucapi.LoadLimit
	grenzeAblauf          time.Time
	letzteGrenzeEmpfangen time.Time
	failsafeGrenzeW       float64
	nennleistungW         float64 // zuletzt gemeldete Nennleistung
	ablehnung             string  // letzter Ablehnungsgrund, nur fuer das Log
}

func neueBegrenzung(name, text string, uc begrenzungsUseCase, failsafeGrenzeW, nennleistungW float64, jetzt time.Time) *Begrenzung {
	return &Begrenzung{
		name: name, text: text, uc: uc,
		zustand: ZustandInit, zustandSeit: jetzt,
		failsafeGrenzeW: failsafeGrenzeW, nennleistungW: nennleistungW,
	}
}

// takt fuehrt den Zustandsautomaten des Controllable System einen Schritt weiter.
// VOR DEM PRODUKTIVEINSATZ gegen die aktuelle Spezifikation der Use Cases
// "Limitation of Power Consumption/Production" pruefen.
func (r *Begrenzung) takt(jetzt time.Time, heartbeatOk bool, mindestdauer time.Duration) {
	grenzeAktiv := r.grenze.IsActive && (r.grenzeAblauf.IsZero() || jetzt.Before(r.grenzeAblauf))
	gesteuert := ZustandUnbegrenztGesteuert
	if grenzeAktiv {
		gesteuert = ZustandBegrenzt
	}

	switch r.zustand {
	case ZustandInit:
		// Bis zur ersten Kommunikation gilt die Failsafe-Grenze.
		if heartbeatOk {
			r.wechsle(gesteuert, jetzt)
		} else if jetzt.Sub(r.zustandSeit) >= heartbeatTimeout {
			r.wechsle(ZustandUnbegrenztAutonom, jetzt)
		}

	case ZustandUnbegrenztGesteuert, ZustandBegrenzt:
		if !heartbeatOk {
			r.wechsle(ZustandFailsafe, jetzt)
		} else {
			r.wechsle(gesteuert, jetzt)
		}

	case ZustandFailsafe:
		// Verlassen nur mit Heartbeat UND neu geschriebener Grenze,
		// sonst fruehestens nach Ablauf der Failsafe-Mindestdauer.
		neueGrenze := r.letzteGrenzeEmpfangen.After(r.zustandSeit)
		if heartbeatOk && neueGrenze {
			r.wechsle(gesteuert, jetzt)
		} else if jetzt.Sub(r.zustandSeit) >= mindestdauer {
			r.wechsle(ZustandUnbegrenztAutonom, jetzt)
		}

	case ZustandUnbegrenztAutonom:
		if heartbeatOk {
			r.wechsle(gesteuert, jetzt)
		}
	}
}

func (r *Begrenzung) wechsle(neu LpcZustand, jetzt time.Time) {
	if neu == r.zustand {
		return
	}
	log.Printf("%s-Zustand: %s -> %s", r.name, r.zustand, neu)
	r.zustand = neu
	r.zustandSeit = jetzt
}

// wirksameGrenze liefert die Grenze, die die SPS einhalten muss.
func (r *Begrenzung) wirksameGrenze() (aktiv bool, wertW float64) {
	switch r.zustand {
	case ZustandInit, ZustandFailsafe:
		return true, r.failsafeGrenzeW
	case ZustandBegrenzt:
		return true, r.grenze.Value
	}
	return false, 0
}

// restdauer der Grenze in s, 0 = unbefristet oder nicht begrenzt.
func (r *Begrenzung) restdauer(jetzt time.Time) float64 {
	if r.zustand == ZustandBegrenzt && !r.grenzeAblauf.IsZero() {
		return max(r.grenzeAblauf.Sub(jetzt).Seconds(), 0)
	}
	return 0
}

func (r *Begrenzung) uebernehmeGrenze(grenze ucapi.LoadLimit, jetzt time.Time) {
	r.grenze = grenze
	r.letzteGrenzeEmpfangen = jetzt
	r.grenzeAblauf = time.Time{}
	if grenze.Duration > 0 {
		r.grenzeAblauf = jetzt.Add(grenze.Duration)
	}
}

// --- Gemeinsame Sicht auf cs/lpc und cs/lpp ---

type begrenzungsUseCase interface {
	Grenze() (ucapi.LoadLimit, error)
	SetzeGrenze(ucapi.LoadLimit) error
	OffeneGrenzen() map[model.MsgCounterType]ucapi.LoadLimit
	GrenzeFreigeben(zaehler model.MsgCounterType, ok bool, grund string)
	OffeneKonfigurationen() map[model.MsgCounterType][]ucapi.PendingDeviceConfiguration
	KonfigurationFreigeben(zaehler model.MsgCounterType, ok bool, grund string)
	FailsafeGrenze() (float64, error)
	SetzeFailsafeGrenze(wertW float64) error
	FailsafeMindestdauer() (time.Duration, error)
	SetzeFailsafeMindestdauer(dauer time.Duration) error
	SetzeNennleistung(wertW float64) error
}

type lpcAdapter struct{ *cslpc.LPC }

func (a lpcAdapter) Grenze() (ucapi.LoadLimit, error)    { return a.ConsumptionLimit() }
func (a lpcAdapter) SetzeGrenze(g ucapi.LoadLimit) error { return a.SetConsumptionLimit(g) }
func (a lpcAdapter) OffeneGrenzen() map[model.MsgCounterType]ucapi.LoadLimit {
	return a.PendingConsumptionLimits()
}
func (a lpcAdapter) GrenzeFreigeben(z model.MsgCounterType, ok bool, grund string) {
	a.ApproveOrDenyConsumptionLimit(z, ok, grund)
}
func (a lpcAdapter) OffeneKonfigurationen() map[model.MsgCounterType][]ucapi.PendingDeviceConfiguration {
	return a.PendingDeviceConfigurations()
}
func (a lpcAdapter) KonfigurationFreigeben(z model.MsgCounterType, ok bool, grund string) {
	a.ApproveOrDenyDeviceConfiguration(z, ok, grund)
}
func (a lpcAdapter) FailsafeGrenze() (float64, error) {
	wert, _, err := a.FailsafeConsumptionActivePowerLimit()
	return wert, err
}
func (a lpcAdapter) SetzeFailsafeGrenze(w float64) error {
	return a.SetFailsafeConsumptionActivePowerLimit(w, true)
}
func (a lpcAdapter) FailsafeMindestdauer() (time.Duration, error) {
	dauer, _, err := a.FailsafeDurationMinimum()
	return dauer, err
}
func (a lpcAdapter) SetzeFailsafeMindestdauer(d time.Duration) error {
	return a.SetFailsafeDurationMinimum(d, true)
}
func (a lpcAdapter) SetzeNennleistung(w float64) error {
	return setzeNennleistung(a.LocalEntity, model.ElectricalConnectionCharacteristicTypeTypeContractualConsumptionNominalMax, w)
}

type lppAdapter struct{ *cslpp.LPP }

func (a lppAdapter) Grenze() (ucapi.LoadLimit, error)    { return a.ProductionLimit() }
func (a lppAdapter) SetzeGrenze(g ucapi.LoadLimit) error { return a.SetProductionLimit(g) }
func (a lppAdapter) OffeneGrenzen() map[model.MsgCounterType]ucapi.LoadLimit {
	return a.PendingProductionLimits()
}
func (a lppAdapter) GrenzeFreigeben(z model.MsgCounterType, ok bool, grund string) {
	a.ApproveOrDenyProductionLimit(z, ok, grund)
}
func (a lppAdapter) OffeneKonfigurationen() map[model.MsgCounterType][]ucapi.PendingDeviceConfiguration {
	return a.PendingDeviceConfigurations()
}
func (a lppAdapter) KonfigurationFreigeben(z model.MsgCounterType, ok bool, grund string) {
	a.ApproveOrDenyDeviceConfiguration(z, ok, grund)
}
func (a lppAdapter) FailsafeGrenze() (float64, error) {
	wert, _, err := a.FailsafeProductionActivePowerLimit()
	return wert, err
}
func (a lppAdapter) SetzeFailsafeGrenze(w float64) error {
	return a.SetFailsafeProductionActivePowerLimit(w, true)
}
func (a lppAdapter) FailsafeMindestdauer() (time.Duration, error) {
	dauer, _, err := a.FailsafeDurationMinimum()
	return dauer, err
}
func (a lppAdapter) SetzeFailsafeMindestdauer(d time.Duration) error {
	return a.SetFailsafeDurationMinimum(d, true)
}
func (a lppAdapter) SetzeNennleistung(w float64) error {
	return setzeNennleistung(a.LocalEntity, model.ElectricalConnectionCharacteristicTypeTypeContractualProductionNominalMax, w)
}

// setzeNennleistung ersetzt Set{Consumption,Production}NominalMax aus eebus-go
// (Stand 30.09.2026): Die suchen die Kennlinie fest unter ElectricalConnection-
// und Parameter-ID 0, angelegt wird sie aber mit den IDs der Gesamtleistung.
// Meldet MPC auf derselben Entitaet, sind das andere IDs und der Wert kommt nie
// an. Hier wird die Kennlinie ueber ihren Typ gesucht. Die Bruecke ist ein
// Energiemanagementsystem, daher die vertraglichen (contractual) Werte.
func setzeNennleistung(entitaet spineapi.EntityLocalInterface, typ model.ElectricalConnectionCharacteristicTypeType, wertW float64) error {
	verbindung, err := server.NewElectricalConnection(entitaet)
	if err != nil {
		return err
	}
	kennlinien, err := verbindung.GetCharacteristicsForFilter(model.ElectricalConnectionCharacteristicDataType{
		CharacteristicContext: util.Ptr(model.ElectricalConnectionCharacteristicContextTypeEntity),
		CharacteristicType:    util.Ptr(typ),
	})
	if err != nil || len(kennlinien) == 0 {
		return api.ErrDataNotAvailable
	}
	k := kennlinien[0]
	return verbindung.UpdateCharacteristic(model.ElectricalConnectionCharacteristicDataType{
		ElectricalConnectionId: k.ElectricalConnectionId,
		ParameterId:            k.ParameterId,
		CharacteristicId:       k.CharacteristicId,
		Value:                  model.NewScaledNumberType(wertW),
	}, nil)
}

// --- Ereignisse beider Use Cases ---

type ereignisArt int

const (
	artSonstiges ereignisArt = iota
	artGrenzeFreigeben
	artKonfigurationFreigeben
	artGrenze
	artHeartbeat
	artFailsafeGrenze
	artMindestdauer
)

var lpcArten = map[string]ereignisArt{
	string(cslpc.LimitWriteApprovalRequired):                    artGrenzeFreigeben,
	string(cslpc.ConfigurationWriteApprovalRequired):            artKonfigurationFreigeben,
	string(cslpc.DataUpdateLimit):                               artGrenze,
	string(cslpc.DataUpdateHeartbeat):                           artHeartbeat,
	string(cslpc.DataUpdateFailsafeConsumptionActivePowerLimit): artFailsafeGrenze,
	string(cslpc.DataUpdateFailsafeDurationMinimum):             artMindestdauer,
}

var lppArten = map[string]ereignisArt{
	string(cslpp.LimitWriteApprovalRequired):                   artGrenzeFreigeben,
	string(cslpp.ConfigurationWriteApprovalRequired):           artKonfigurationFreigeben,
	string(cslpp.DataUpdateLimit):                              artGrenze,
	string(cslpp.DataUpdateHeartbeat):                          artHeartbeat,
	string(cslpp.DataUpdateFailsafeProductionActivePowerLimit): artFailsafeGrenze,
	string(cslpp.DataUpdateFailsafeDurationMinimum):            artMindestdauer,
}

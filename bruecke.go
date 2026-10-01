package main

import (
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/enbility/eebus-go/api"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	shipapi "github.com/enbility/ship-go/api"
	spineapi "github.com/enbility/spine-go/api"
)

// LpcZustand bildet die Zustaende des Controllable System im Use Case LPC ab.
// Die Zahlenwerte sind Teil der Modbus-Schnittstelle (eLpcZustand in CODESYS).
type LpcZustand uint16

const (
	ZustandInit                LpcZustand = 0
	ZustandUnbegrenztGesteuert LpcZustand = 1
	ZustandBegrenzt            LpcZustand = 2
	ZustandFailsafe            LpcZustand = 3
	ZustandUnbegrenztAutonom   LpcZustand = 4
)

func (z LpcZustand) String() string {
	switch z {
	case ZustandInit:
		return "Init"
	case ZustandUnbegrenztGesteuert:
		return "Unbegrenzt/gesteuert"
	case ZustandBegrenzt:
		return "Begrenzt"
	case ZustandFailsafe:
		return "Failsafe"
	case ZustandUnbegrenztAutonom:
		return "Unbegrenzt/autonom"
	}
	return "Unbekannt"
}

// Verbindung zur Steuerbox, ebenfalls Teil der Modbus-Schnittstelle.
type Verbindung uint16

const (
	VerbindungKeinPartner Verbindung = 0
	VerbindungGetrennt    Verbindung = 1
	VerbindungVerbunden   Verbindung = 2
)

func (v Verbindung) String() string {
	switch v {
	case VerbindungKeinPartner:
		return "Kein Partner"
	case VerbindungGetrennt:
		return "Getrennt"
	case VerbindungVerbunden:
		return "Verbunden"
	}
	return "Unbekannt"
}

const (
	// Ohne Heartbeat der Steuerbox fuer diese Dauer -> Failsafe.
	heartbeatTimeout = 120 * time.Second
	// Ohne Aenderung des SPS-Lebenszeichens fuer diese Dauer -> Warnung im Log.
	spsTimeout = 10 * time.Second
)

// Bruecke haelt den gesamten Zustand. Zugriff nur unter mu.
// Aufrufe in den EEBUS-Stack (b.lpc.*) immer ausserhalb von mu,
// damit es keine Verklemmung mit dessen Callbacks gibt.
type Bruecke struct {
	konf       Konfiguration
	lpc        *cslpc.LPC
	eigenerSki string
	gestartet  time.Time

	mu                    sync.Mutex
	gefunden              []shipapi.RemoteService // per mDNS sichtbare EEBUS-Geraete
	pairing               map[string]shipapi.ConnectionState
	zustand               LpcZustand
	zustandSeit           time.Time
	verbindung            Verbindung
	grenze                ucapi.LoadLimit
	grenzeAblauf          time.Time
	letzteGrenzeEmpfangen time.Time
	letzterHeartbeat      time.Time
	failsafeGrenzeW       float64
	failsafeMindestdauer  time.Duration
	lebenszeichen         uint16

	// von der SPS geschriebene Holding-Register
	holding                 [anzahlHoldingRegister]uint16
	letztesSpsLebenszeichen uint16
	spsLebenszeichenSeit    time.Time
	spsOk                   bool
	gemeldeteNennleistungW  float64
}

func NeueBruecke(konf Konfiguration, eigenerSki string) *Bruecke {
	verbindung := VerbindungGetrennt
	if konf.RemoteSki == "" {
		verbindung = VerbindungKeinPartner
	}
	jetzt := time.Now()
	return &Bruecke{
		konf:                   konf,
		eigenerSki:             eigenerSki,
		gestartet:              jetzt,
		zustand:                ZustandInit,
		zustandSeit:            jetzt,
		verbindung:             verbindung,
		failsafeGrenzeW:        konf.FailsafeGrenzeW,
		failsafeMindestdauer:   konf.FailsafeMindestdauer,
		gemeldeteNennleistungW: konf.NennleistungMaxW,
	}
}

// SetzeStartwerte meldet Nennleistung und Failsafe-Werte im SPINE-Modell an.
func (b *Bruecke) SetzeStartwerte() {
	pruefe := func(was string, err error) {
		if err != nil {
			log.Printf("Startwert %s nicht gesetzt: %v", was, err)
		}
	}
	pruefe("Nennleistung", b.lpc.SetConsumptionNominalMax(b.konf.NennleistungMaxW))
	pruefe("Failsafe-Grenze", b.lpc.SetFailsafeConsumptionActivePowerLimit(b.konf.FailsafeGrenzeW, true))
	pruefe("Failsafe-Mindestdauer", b.lpc.SetFailsafeDurationMinimum(b.konf.FailsafeMindestdauer, true))
	pruefe("Grenze", b.lpc.SetConsumptionLimit(ucapi.LoadLimit{IsChangeable: true, IsActive: false, Value: 0}))
}

// --- api.ServiceReaderInterface ---

func (b *Bruecke) RemoteSKIConnected(dienst api.ServiceInterface, ski string) {
	b.mu.Lock()
	b.verbindung = VerbindungVerbunden
	b.mu.Unlock()
	log.Printf("Steuerbox verbunden: %s", ski)
}

func (b *Bruecke) RemoteSKIDisconnected(dienst api.ServiceInterface, ski string) {
	b.mu.Lock()
	b.verbindung = VerbindungGetrennt
	b.mu.Unlock()
	log.Printf("Steuerbox getrennt: %s", ski)
}

// Hilfreich bei der Inbetriebnahme: zeigt die per mDNS gefundenen Geraete samt SKI.
// mDNS meldet die Liste wiederholt, protokolliert werden nur neu gefundene Geraete.
func (b *Bruecke) VisibleRemoteServicesUpdated(dienst api.ServiceInterface, eintraege []shipapi.RemoteService) {
	b.mu.Lock()
	bekannt := make(map[string]bool, len(b.gefunden))
	for _, e := range b.gefunden {
		bekannt[e.Ski] = true
	}
	b.gefunden = slices.Clone(eintraege)
	b.mu.Unlock()

	for _, e := range eintraege {
		if !bekannt[e.Ski] {
			log.Printf("Gefunden: %s %s, SKI %s", e.Brand, e.Model, e.Ski)
		}
	}
}

func (b *Bruecke) ServiceShipIDUpdate(ski string, shipId string) {}

var pairingTexte = map[shipapi.ConnectionState]string{
	shipapi.ConnectionStateNone:                   "kein Pairing",
	shipapi.ConnectionStateQueued:                 "eingereiht",
	shipapi.ConnectionStateInitiated:              "von hier gestartet",
	shipapi.ConnectionStateReceivedPairingRequest: "Anfrage der Gegenseite",
	shipapi.ConnectionStateInProgress:             "Handshake laeuft",
	shipapi.ConnectionStateTrusted:                "vertraut",
	shipapi.ConnectionStatePin:                    "PIN",
	shipapi.ConnectionStateCompleted:              "abgeschlossen",
	shipapi.ConnectionStateRemoteDeniedTrust:      "von Gegenseite abgelehnt",
	shipapi.ConnectionStateError:                  "Fehler",
}

// Protokolliert nur Wechsel, der SHIP-Handshake meldet manche Zustaende mehrfach.
func (b *Bruecke) ServicePairingDetailUpdate(ski string, detail *shipapi.ConnectionStateDetail) {
	zustand := detail.State()
	b.mu.Lock()
	if b.pairing == nil {
		b.pairing = make(map[string]shipapi.ConnectionState)
	}
	alt, bekannt := b.pairing[ski]
	b.pairing[ski] = zustand
	b.mu.Unlock()
	if bekannt && alt == zustand {
		return
	}

	text, ok := pairingTexte[zustand]
	if !ok {
		text = fmt.Sprint(zustand)
	}
	if err := detail.Error(); err != nil {
		text += ": " + err.Error()
	}
	log.Printf("Pairing %s: %s", ski, text)
}

// Nur der konfigurierten Steuerbox wird vertraut.
func (b *Bruecke) AllowWaitingForTrust(ski string) bool {
	return ski == b.konf.RemoteSki
}

// --- Ereignisse des Use Case LPC ---

func (b *Bruecke) LpcEreignis(ski string, geraet spineapi.DeviceRemoteInterface, entitaet spineapi.EntityRemoteInterface, ereignis api.EventType) {
	jetzt := time.Now()

	switch ereignis {
	case cslpc.WriteApprovalRequired:
		// Grenzen des Netzbetreibers werden angenommen, nur offensichtlich
		// ungueltige Werte werden abgelehnt.
		for zaehler, grenze := range b.lpc.PendingConsumptionLimits() {
			gueltig := grenze.Value >= 0
			grund := ""
			if !gueltig {
				grund = "ungueltiger Grenzwert"
			}
			b.lpc.ApproveOrDenyConsumptionLimit(zaehler, gueltig, grund)
		}

	case cslpc.DataUpdateLimit:
		grenze, err := b.lpc.ConsumptionLimit()
		if err != nil {
			log.Printf("Grenze lesen: %v", err)
			return
		}
		b.mu.Lock()
		b.grenze = grenze
		b.letzteGrenzeEmpfangen = jetzt
		b.grenzeAblauf = time.Time{}
		if grenze.Duration > 0 {
			b.grenzeAblauf = jetzt.Add(grenze.Duration)
		}
		b.mu.Unlock()
		log.Printf("Neue Grenze: aktiv=%v, %.0f W, Dauer %v", grenze.IsActive, grenze.Value, grenze.Duration)

	case cslpc.DataUpdateHeartbeat:
		b.mu.Lock()
		b.letzterHeartbeat = jetzt
		b.mu.Unlock()

	case cslpc.DataUpdateFailsafeConsumptionActivePowerLimit:
		if wert, _, err := b.lpc.FailsafeConsumptionActivePowerLimit(); err == nil {
			b.mu.Lock()
			b.failsafeGrenzeW = wert
			b.mu.Unlock()
			log.Printf("Neue Failsafe-Grenze: %.0f W", wert)
		}

	case cslpc.DataUpdateFailsafeDurationMinimum:
		if dauer, _, err := b.lpc.FailsafeDurationMinimum(); err == nil {
			b.mu.Lock()
			b.failsafeMindestdauer = dauer
			b.mu.Unlock()
			log.Printf("Neue Failsafe-Mindestdauer: %v", dauer)
		}
	}
}

// --- Zyklische Bearbeitung (1 s) ---

func (b *Bruecke) Takt(jetzt time.Time) {
	b.pruefeSpsWerte(jetzt)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.lebenszeichen++

	heartbeatOk := !b.letzterHeartbeat.IsZero() && jetzt.Sub(b.letzterHeartbeat) <= heartbeatTimeout
	grenzeAktiv := b.grenze.IsActive && (b.grenzeAblauf.IsZero() || jetzt.Before(b.grenzeAblauf))
	gesteuert := ZustandUnbegrenztGesteuert
	if grenzeAktiv {
		gesteuert = ZustandBegrenzt
	}

	// Zustandsautomat des Controllable System.
	// VOR DEM PRODUKTIVEINSATZ gegen die aktuelle Spezifikation
	// "EEBUS UC Limitation of Power Consumption" pruefen.
	switch b.zustand {
	case ZustandInit:
		// Bis zur ersten Kommunikation gilt die Failsafe-Grenze.
		if heartbeatOk {
			b.wechsle(gesteuert, jetzt)
		} else if jetzt.Sub(b.zustandSeit) >= heartbeatTimeout {
			b.wechsle(ZustandUnbegrenztAutonom, jetzt)
		}

	case ZustandUnbegrenztGesteuert, ZustandBegrenzt:
		if !heartbeatOk {
			b.wechsle(ZustandFailsafe, jetzt)
		} else {
			b.wechsle(gesteuert, jetzt)
		}

	case ZustandFailsafe:
		// Verlassen nur mit Heartbeat UND neu geschriebener Grenze,
		// sonst fruehestens nach Ablauf der Failsafe-Mindestdauer.
		neueGrenze := b.letzteGrenzeEmpfangen.After(b.zustandSeit)
		if heartbeatOk && neueGrenze {
			b.wechsle(gesteuert, jetzt)
		} else if jetzt.Sub(b.zustandSeit) >= b.failsafeMindestdauer {
			b.wechsle(ZustandUnbegrenztAutonom, jetzt)
		}

	case ZustandUnbegrenztAutonom:
		if heartbeatOk {
			b.wechsle(gesteuert, jetzt)
		}
	}
}

func (b *Bruecke) wechsle(neu LpcZustand, jetzt time.Time) {
	if neu == b.zustand {
		return
	}
	log.Printf("LPC-Zustand: %s -> %s", b.zustand, neu)
	b.zustand = neu
	b.zustandSeit = jetzt
}

// wirksameGrenze liefert die Grenze, die die SPS einhalten muss. Aufruf nur unter mu.
func (b *Bruecke) wirksameGrenze() (aktiv bool, wertW float64) {
	switch b.zustand {
	case ZustandInit, ZustandFailsafe:
		return true, b.failsafeGrenzeW
	case ZustandBegrenzt:
		return true, b.grenze.Value
	}
	return false, 0
}

// pruefeSpsWerte ueberwacht das SPS-Lebenszeichen und meldet eine geaenderte
// Nennleistung an den EEBUS-Stack weiter.
func (b *Bruecke) pruefeSpsWerte(jetzt time.Time) {
	b.mu.Lock()
	if b.holding[regSpsLebenszeichen] != b.letztesSpsLebenszeichen {
		b.letztesSpsLebenszeichen = b.holding[regSpsLebenszeichen]
		b.spsLebenszeichenSeit = jetzt
		if !b.spsOk {
			log.Printf("SPS-Lebenszeichen vorhanden")
		}
		b.spsOk = true
	} else if b.spsOk && jetzt.Sub(b.spsLebenszeichenSeit) > spsTimeout {
		b.spsOk = false
		log.Printf("SPS-Lebenszeichen ausgefallen")
	}

	nennleistungW := float64(zuUint32(b.holding[regNennleistungHi], b.holding[regNennleistungLo]))
	aendern := b.spsOk && nennleistungW > 0 && nennleistungW != b.gemeldeteNennleistungW
	if aendern {
		b.gemeldeteNennleistungW = nennleistungW
	}
	b.mu.Unlock()

	if aendern {
		if err := b.lpc.SetConsumptionNominalMax(nennleistungW); err != nil {
			log.Printf("Nennleistung melden: %v", err)
		}
	}
}

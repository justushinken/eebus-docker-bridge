package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	shipapi "github.com/enbility/ship-go/api"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Datei im Datenverzeichnis mit der per Pairing Service gekoppelten Steuerbox.
const KopplungsdateiPairing = "steuerbox-pairing.json"

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
	pairing    gemeinsam.Pairingprotokoll
	lpc        *cslpc.LPC
	eigenerSki string
	gestartet  time.Time
	kennung    Kennung

	mu                    sync.Mutex
	gefunden              []shipapi.RemoteMdnsService // per mDNS sichtbare EEBUS-Geraete
	partner               shipapi.ServiceIdentity     // verbundene Steuerbox, leer = keine
	pairingKopplung       *gemeinsam.Kopplung         // per Pairing Service gekoppelte Steuerbox
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

func NeueBruecke(konf Konfiguration, eigenerSki string, pairingKopplung *gemeinsam.Kopplung) *Bruecke {
	verbindung := VerbindungGetrennt
	if konf.RemoteSki == "" && pairingKopplung == nil {
		verbindung = VerbindungKeinPartner
	}
	jetzt := time.Now()
	return &Bruecke{
		konf:                   konf,
		eigenerSki:             eigenerSki,
		pairingKopplung:        pairingKopplung,
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

// Verbindungswechsel nur einmal protokollieren, ship-go meldet sie teils doppelt.
func (b *Bruecke) RemoteServiceConnected(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
	b.mu.Lock()
	neu := b.verbindung != VerbindungVerbunden
	b.verbindung = VerbindungVerbunden
	b.partner = partner
	b.mu.Unlock()
	if neu {
		log.Printf("Steuerbox verbunden: %s", gemeinsam.Bezeichnung(partner))
	}
}

func (b *Bruecke) RemoteServiceDisconnected(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
	b.mu.Lock()
	neu := b.verbindung == VerbindungVerbunden
	b.verbindung = VerbindungGetrennt
	b.partner = shipapi.ServiceIdentity{}
	b.mu.Unlock()
	if neu {
		log.Printf("Steuerbox getrennt: %s", gemeinsam.Bezeichnung(partner))
	}
}

// Hilfreich bei der Inbetriebnahme: zeigt die per mDNS gefundenen Geraete samt SKI.
// mDNS meldet die Liste wiederholt, protokolliert werden nur neu gefundene Geraete.
func (b *Bruecke) VisibleRemoteMdnsServicesUpdated(dienst api.ServiceInterface, eintraege []shipapi.RemoteMdnsService) {
	b.mu.Lock()
	neu := gemeinsam.NeuGefunden(b.gefunden, eintraege)
	b.gefunden = slices.Clone(eintraege)
	b.mu.Unlock()

	for _, e := range neu {
		log.Printf("Gefunden: %s %s, SHIP-ID %s, SKI %s", e.Brand, e.Model, e.ShipID, e.Ski)
	}
}

func (b *Bruecke) ServiceUpdated(partner shipapi.ServiceIdentity) {}

func (b *Bruecke) ServicePairingDetailUpdate(partner shipapi.ServiceIdentity, detail *shipapi.ConnectionStateDetail) {
	if text, melden := b.pairing.Neu(partner, detail); melden {
		log.Printf("Pairing %s: %s", gemeinsam.Bezeichnung(partner), text)
	}
}

// --- SHIP Pairing Service ---
//
// Eine Steuerbox, die das Secret kennt, wird automatisch vertraut. ship-go
// haelt das nur im Speicher, deshalb wird sie im Volume abgelegt und beim
// Start wieder angemeldet (siehe main.go).

func (b *Bruecke) ServiceAutoTrusted(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
	kopplung := &gemeinsam.Kopplung{Verfahren: gemeinsam.VerfahrenPairing, Identitaet: partner}
	if err := gemeinsam.SpeichereKopplung(b.kopplungsdatei(), kopplung); err != nil {
		log.Printf("Kopplung speichern: %v", err)
	}
	b.mu.Lock()
	b.pairingKopplung = kopplung
	if b.verbindung == VerbindungKeinPartner {
		b.verbindung = VerbindungGetrennt
	}
	b.mu.Unlock()
	log.Printf("Steuerbox per Pairing Service gekoppelt: %s", gemeinsam.Bezeichnung(partner))
}

func (b *Bruecke) ServiceAutoTrustFailed(dienst api.ServiceInterface, partner shipapi.ServiceIdentity, grund error) {
	// Eine schon gekoppelte Steuerbox wiederholt ihre Ankuendigung bis zu 15 min.
	// Nach einem Neustart erkennt ship-go das als Wiederholung und lehnt ab, die
	// Steuerbox ist aber ohnehin vertraut: keine Meldung wert.
	b.mu.Lock()
	gekoppelt := b.pairingKopplung != nil && partner.ShipID != "" && partner.ShipID == b.pairingKopplung.Identitaet.ShipID
	b.mu.Unlock()
	if gekoppelt {
		return
	}
	log.Printf("Pairing Service abgelehnt fuer %s: %v", gemeinsam.Bezeichnung(partner), grund)
}

// Kommt z. B., wenn eine neue Steuerbox die alte ersetzt. Die neue meldet
// sich danach ueber ServiceAutoTrusted.
func (b *Bruecke) ServiceAutoTrustRemoved(dienst api.ServiceInterface, partner shipapi.ServiceIdentity, grund string) {
	if err := gemeinsam.SpeichereKopplung(b.kopplungsdatei(), nil); err != nil {
		log.Printf("Kopplung loeschen: %v", err)
	}
	b.mu.Lock()
	b.pairingKopplung = nil
	b.mu.Unlock()
	log.Printf("Kopplung mit Steuerbox %s aufgehoben: %s", gemeinsam.Bezeichnung(partner), grund)
}

func (b *Bruecke) kopplungsdatei() string {
	return filepath.Join(b.konf.Datenverzeichnis, KopplungsdateiPairing)
}

// --- Ereignisse des Use Case LPC ---

func (b *Bruecke) LpcEreignis(ski string, geraet spineapi.DeviceRemoteInterface, entitaet spineapi.EntityRemoteInterface, ereignis api.EventType) {
	jetzt := time.Now()

	switch ereignis {
	case cslpc.LimitWriteApprovalRequired:
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

	case cslpc.ConfigurationWriteApprovalRequired:
		// Failsafe-Werte der Steuerbox: angenommen, wenn sie im erlaubten Bereich liegen.
		for zaehler, konfigurationen := range b.lpc.PendingDeviceConfigurations() {
			grund := ""
			for _, k := range konfigurationen {
				if grund = pruefeKonfiguration(k); grund != "" {
					break
				}
			}
			if grund != "" {
				log.Printf("Failsafe-Werte abgelehnt: %s", grund)
			}
			b.lpc.ApproveOrDenyDeviceConfiguration(zaehler, grund == "", grund)
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

	// eebus-go meldet bei jeder Aenderung der Konfiguration beide Failsafe-Ereignisse,
	// protokolliert wird nur ein tatsaechlich geaenderter Wert.
	case cslpc.DataUpdateFailsafeConsumptionActivePowerLimit:
		if wert, _, err := b.lpc.FailsafeConsumptionActivePowerLimit(); err == nil {
			b.mu.Lock()
			geaendert := wert != b.failsafeGrenzeW
			b.failsafeGrenzeW = wert
			b.mu.Unlock()
			if geaendert {
				log.Printf("Neue Failsafe-Grenze: %.0f W", wert)
				b.speichereFailsafe()
			}
		}

	case cslpc.DataUpdateFailsafeDurationMinimum:
		if dauer, _, err := b.lpc.FailsafeDurationMinimum(); err == nil {
			b.mu.Lock()
			geaendert := dauer != b.failsafeMindestdauer
			b.failsafeMindestdauer = dauer
			b.mu.Unlock()
			if geaendert {
				log.Printf("Neue Failsafe-Mindestdauer: %v", dauer)
				b.speichereFailsafe()
			}
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

// pruefeKonfiguration liefert den Ablehnungsgrund fuer einen von der Steuerbox
// geschriebenen Failsafe-Wert, leer = in Ordnung.
func pruefeKonfiguration(k ucapi.PendingDeviceConfiguration) string {
	if k.Value == nil {
		return ""
	}
	switch k.KeyName {
	case model.DeviceConfigurationKeyNameTypeFailsafeDurationMinimum:
		if k.Value.Duration != nil {
			dauer, err := k.Value.Duration.GetTimeDuration()
			if err != nil || dauer < 2*time.Hour || dauer > 24*time.Hour {
				return "Failsafe-Mindestdauer ausserhalb 2 bis 24 h"
			}
		}
	case model.DeviceConfigurationKeyNameTypeFailsafeConsumptionActivePowerLimit:
		if k.Value.ScaledNumber != nil && k.Value.ScaledNumber.GetValue() < 0 {
			return "negative Failsafe-Grenze"
		}
	}
	return ""
}

// Kennung sind die Angaben, die der Messstellenbetreiber fuer die Kopplung
// braucht. Nach SetzeKennung unveraenderlich, daher ohne mu lesbar.
type Kennung struct {
	ShipId      string `json:"shipId"`
	Ski         string `json:"ski"`
	Fingerprint string `json:"fingerprint"`
	Secret      string `json:"secret,omitempty"` // leer, wenn der Pairing Service aus ist
	QrText      string `json:"qrText"`
}

func (b *Bruecke) SetzeKennung(dienst api.ServiceInterface, secret shipapi.PairingSecret) {
	b.kennung = Kennung{ShipId: b.konf.ShipId, Ski: b.eigenerSki}
	if fingerprint, err := dienst.GetLocalCertificateFingerprint(); err == nil {
		b.kennung.Fingerprint = fingerprint
	} else {
		log.Printf("Fingerprint ermitteln: %v", err)
	}
	if len(secret) > 0 {
		b.kennung.Secret = gemeinsam.SecretHex(secret)
	}
	// Mit Secret im Format des Pairing Service (FPH256, SPSEC), sonst der
	// klassische SHIP-QR-Code mit SKI und SHIP-ID.
	if text, err := dienst.QRCodeText(); err == nil {
		b.kennung.QrText = text
	} else {
		log.Printf("QR-Text erzeugen: %v", err)
	}
	log.Printf("Fingerprint (SHA-256): %s", b.kennung.Fingerprint)
}

// --- Failsafe-Werte der Steuerbox dauerhaft halten ---
//
// Die Failsafe-Werte gelten gerade nach einem Neustart (Zustand Init), bevor
// die Steuerbox sich wieder meldet. Von ihr vorgegebene Werte muessen deshalb
// einen Neustart ueberstehen und haben Vorrang vor FAILSAFE_GRENZE_W und
// FAILSAFE_MINDESTDAUER.

const FailsafeDatei = "failsafe.json"

type FailsafeWerte struct {
	GrenzeW      float64       `json:"grenzeW"`
	Mindestdauer time.Duration `json:"mindestdauerNs"`
}

func (b *Bruecke) speichereFailsafe() {
	b.mu.Lock()
	werte := FailsafeWerte{GrenzeW: b.failsafeGrenzeW, Mindestdauer: b.failsafeMindestdauer}
	b.mu.Unlock()
	inhalt, err := json.Marshal(werte)
	if err == nil {
		err = os.WriteFile(filepath.Join(b.konf.Datenverzeichnis, FailsafeDatei), inhalt, 0o600)
	}
	if err != nil {
		log.Printf("Failsafe-Werte speichern: %v", err)
	}
}

// LadeFailsafe liefert die zuletzt von der Steuerbox vorgegebenen Werte, nil = keine.
func LadeFailsafe(verzeichnis string) *FailsafeWerte {
	inhalt, err := os.ReadFile(filepath.Join(verzeichnis, FailsafeDatei))
	if err != nil {
		return nil
	}
	var werte FailsafeWerte
	if err := json.Unmarshal(inhalt, &werte); err != nil {
		log.Printf("%s nicht lesbar: %v", FailsafeDatei, err)
		return nil
	}
	return &werte
}

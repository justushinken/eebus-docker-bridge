package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	gcpmgcp "github.com/enbility/eebus-go/usecases/gcp/mgcp"
	mumpc "github.com/enbility/eebus-go/usecases/mu/mpc"
	shipapi "github.com/enbility/ship-go/api"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// LpcZustand bildet die Zustaende des Controllable System in den Use Cases
// LPC und LPP ab (gleicher Automat). Die Zahlenwerte sind Teil der
// Modbus-Schnittstelle (eLpcZustand in CODESYS).
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
	// Ohne Aenderung des SPS-Lebenszeichens fuer diese Dauer -> SPS ausgefallen.
	spsTimeout = 10 * time.Second
)

// Bruecke haelt den gesamten Zustand. Zugriff nur unter mu.
// Aufrufe in den EEBUS-Stack (dienst, Use Cases) immer ausserhalb von mu,
// damit es keine Verklemmung mit dessen Callbacks gibt.
type Bruecke struct {
	// beim Start gesetzt, danach unveraenderlich
	konf         Konfiguration
	pairing      gemeinsam.Pairingprotokoll
	eigenerSki   string
	gestartet    time.Time
	kennung      Kennung
	dienst       api.ServiceInterface
	cem          spineapi.EntityLocalInterface
	mpc          *mumpc.MPC
	mgcp         *gcpmgcp.MGCP
	mpcGroessen  []messgroesse // angebotene Messwerte (messwerte.go), nil = Use Case aus
	mgcpGroessen []messgroesse

	kopplungMu sync.Mutex // reiht Aenderungen der Kopplung (kopplung.go)

	mu         sync.Mutex
	gefunden   []shipapi.RemoteMdnsService // per mDNS sichtbare EEBUS-Geraete
	partner    shipapi.ServiceIdentity     // verbundene Steuerbox, leer = keine
	kopplung   *gemeinsam.Kopplung         // im UI oder per Pairing Service gekoppelte Steuerbox
	verbindung Verbindung

	bezug                *Begrenzung // LPC, nil = Use Case aus
	einspeisung          *Begrenzung // LPP, nil = Use Case aus
	letzterHeartbeat     time.Time
	failsafeMindestdauer time.Duration // gemeinsam fuer LPC und LPP
	lebenszeichen        uint16

	// von der SPS geschriebene Holding-Register
	holding                 [anzahlHoldingRegister]uint16
	letztesSpsLebenszeichen uint16
	spsLebenszeichenSeit    time.Time
	spsOk                   bool
	gemeldeterStatus        model.DeviceDiagnosisOperatingStateType
	anlagenstatusSps        uint16 // zuletzt von der SPS gemeldeter Anlagenstatus
	messwerte               messwertStand

	// Suchmodus und Kopplungsanfragen (kopplung.go)
	suchmodusBis time.Time
	anfragen     map[string]*Anfrage
	unbekannt    map[string]bool // schon gemeldete Verbindungsversuche unbekannter Geraete

	// Diagnose der Steuerbox (gegenstelle.go)
	gegenstelle           *Gegenstelle
	steuerboxUseCases     uint16 // Bitmaske wie Input-Register 16
	herstellerAngefordert string // SKI, fuer die die Herstellerdaten angefordert wurden
}

func NeueBruecke(konf Konfiguration, eigenerSki string, kopplung *gemeinsam.Kopplung) *Bruecke {
	jetzt := time.Now()
	b := &Bruecke{
		konf:                 konf,
		eigenerSki:           eigenerSki,
		kopplung:             kopplung,
		gestartet:            jetzt,
		failsafeMindestdauer: konf.FailsafeMindestdauer,
		anfragen:             make(map[string]*Anfrage),
		unbekannt:            make(map[string]bool),
	}
	b.verbindung = VerbindungGetrennt
	if !b.hatPartner() {
		b.verbindung = VerbindungKeinPartner
	}
	return b
}

// begrenzungen liefert die aktiven Richtungen. Aufruf unter mu oder nach dem
// Start (die Zeiger aendern sich danach nicht mehr).
func (b *Bruecke) begrenzungen() []*Begrenzung {
	var liste []*Begrenzung
	for _, r := range []*Begrenzung{b.bezug, b.einspeisung} {
		if r != nil {
			liste = append(liste, r)
		}
	}
	return liste
}

// SetzeStartwerte meldet Nennleistungen und Failsafe-Werte im SPINE-Modell an.
func (b *Bruecke) SetzeStartwerte() {
	pruefe := func(was string, err error) {
		if err != nil {
			log.Printf("Startwert %s nicht gesetzt: %v", was, err)
		}
	}
	for i, r := range b.begrenzungen() {
		pruefe(r.name+"-Nennleistung", r.uc.SetzeNennleistung(r.nennleistungW))
		pruefe(r.name+"-Failsafe-Grenze", r.uc.SetzeFailsafeGrenze(r.failsafeGrenzeW))
		pruefe(r.name+"-Grenze", r.uc.SetzeGrenze(ucapi.LoadLimit{IsChangeable: true, IsActive: false, Value: 0}))
		// Ein Schluessel fuer beide Use Cases: einmal setzen genuegt.
		if i == 0 {
			pruefe("Failsafe-Mindestdauer", r.uc.SetzeFailsafeMindestdauer(b.konf.FailsafeMindestdauer))
		}
	}
}

// --- Ereignisse der Use Cases LPC und LPP ---

func (b *Bruecke) LpcEreignis(ski string, geraet spineapi.DeviceRemoteInterface, entitaet spineapi.EntityRemoteInterface, ereignis api.EventType) {
	b.begrenzungsEreignis(b.bezug, lpcArten[ereignis])
}

func (b *Bruecke) LppEreignis(ski string, geraet spineapi.DeviceRemoteInterface, entitaet spineapi.EntityRemoteInterface, ereignis api.EventType) {
	b.begrenzungsEreignis(b.einspeisung, lppArten[ereignis])
}

func (b *Bruecke) begrenzungsEreignis(r *Begrenzung, art ereignisArt) {
	if r == nil {
		return
	}
	jetzt := time.Now()

	switch art {
	case artGrenzeFreigeben:
		// FNN-Hinweis 4.1.2.2: Die Anlage bestaetigt die Uebernahme und zeitnahe
		// Umsetzung. Ohne SPS ist die Umsetzung nicht moeglich, dann ablehnen.
		b.mu.Lock()
		spsOk := b.spsOk
		b.mu.Unlock()
		for zaehler, grenze := range r.uc.OffeneGrenzen() {
			grund := ""
			switch {
			case grenze.Value < 0:
				grund = "ungueltiger Grenzwert"
			case !spsOk:
				grund = "SPS nicht erreichbar, Umsetzung nicht moeglich"
			}
			r.uc.GrenzeFreigeben(zaehler, grund == "", grund)

			// Steuerboxen wiederholen abgelehnte Grenzen: nur einmal melden.
			b.mu.Lock()
			neu := grund != r.ablehnung
			r.ablehnung = grund
			b.mu.Unlock()
			if neu && grund != "" {
				log.Printf("%s abgelehnt (%.0f W): %s", r.text, grenze.Value, grund)
			}
		}

	case artKonfigurationFreigeben:
		// Bei LPC und LPP auf derselben Entitaet kommt jede Schreibanfrage fuer
		// Failsafe-Werte in beiden Use Cases an und muss in beiden freigegeben
		// werden, mit derselben Pruefung.
		for zaehler, konfigurationen := range r.uc.OffeneKonfigurationen() {
			grund := ""
			for _, k := range konfigurationen {
				if grund = pruefeKonfiguration(k); grund != "" {
					break
				}
			}
			if grund != "" {
				log.Printf("Failsafe-Werte abgelehnt (%s): %s", r.name, grund)
			}
			r.uc.KonfigurationFreigeben(zaehler, grund == "", grund)
		}

	case artGrenze:
		grenze, err := r.uc.Grenze()
		if err != nil {
			log.Printf("%s lesen: %v", r.text, err)
			return
		}
		b.mu.Lock()
		r.uebernehmeGrenze(grenze, jetzt)
		b.mu.Unlock()
		log.Printf("Neue %s: aktiv=%v, %.0f W, Dauer %v", r.text, grenze.IsActive, grenze.Value, grenze.Duration)

	case artHeartbeat:
		b.mu.Lock()
		b.letzterHeartbeat = jetzt
		b.mu.Unlock()

	// eebus-go meldet bei jeder Aenderung der Konfiguration alle Failsafe-Ereignisse,
	// bei LPC und LPP zusammen sogar doppelt. Protokolliert wird nur ein
	// tatsaechlich geaenderter Wert.
	case artFailsafeGrenze:
		if wert, err := r.uc.FailsafeGrenze(); err == nil {
			b.mu.Lock()
			geaendert := wert != r.failsafeGrenzeW
			r.failsafeGrenzeW = wert
			b.mu.Unlock()
			if geaendert {
				log.Printf("Neue Failsafe-%s: %.0f W", r.text, wert)
				b.speichereFailsafe()
			}
		}

	case artMindestdauer:
		if dauer, err := r.uc.FailsafeMindestdauer(); err == nil {
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
	b.pruefeSuchmodus(jetzt)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.lebenszeichen++

	heartbeatOk := !b.letzterHeartbeat.IsZero() && jetzt.Sub(b.letzterHeartbeat) <= heartbeatTimeout
	for _, r := range b.begrenzungen() {
		r.takt(jetzt, heartbeatOk, b.failsafeMindestdauer)
	}
}

// pruefeSpsWerte ueberwacht das SPS-Lebenszeichen und gibt Nennleistungen,
// Anlagenstatus und Messwerte an den EEBUS-Stack weiter.
func (b *Bruecke) pruefeSpsWerte(jetzt time.Time) {
	type nennleistung struct {
		r    *Begrenzung
		wert float64
	}
	var nennleistungen []nennleistung

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

	for _, n := range []struct {
		r      *Begrenzung
		hi, lo int
	}{{b.bezug, regNennleistungHi, regNennleistungLo}, {b.einspeisung, regNennleistungErzeugungHi, regNennleistungErzeugungLo}} {
		if n.r == nil {
			continue
		}
		wert := float64(zuUint32(b.holding[n.hi], b.holding[n.lo]))
		if b.spsOk && wert > 0 && wert != n.r.nennleistungW {
			n.r.nennleistungW = wert
			nennleistungen = append(nennleistungen, nennleistung{n.r, wert})
		}
	}
	b.mu.Unlock()

	for _, n := range nennleistungen {
		if err := n.r.uc.SetzeNennleistung(n.wert); err != nil {
			log.Printf("%s-Nennleistung melden: %v", n.r.name, err)
		}
	}

	b.meldeAnlagenstatus()
	b.sendeMesswerte()
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
	case model.DeviceConfigurationKeyNameTypeFailsafeConsumptionActivePowerLimit,
		model.DeviceConfigurationKeyNameTypeFailsafeProductionActivePowerLimit:
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
// einen Neustart ueberstehen und haben Vorrang vor FAILSAFE_*.

const FailsafeDatei = "failsafe.json"

type FailsafeWerte struct {
	GrenzeW      float64       `json:"grenzeW"`
	Mindestdauer time.Duration `json:"mindestdauerNs"`
	// Ab LPP: Failsafe-Einspeisegrenze, nil in aelteren Dateien.
	EinspeisegrenzeW *float64 `json:"einspeisegrenzeW,omitempty"`
}

func (b *Bruecke) speichereFailsafe() {
	// Werte einer gerade abgeschalteten Richtung aus der Datei behalten
	werte := LadeFailsafe(b.konf.Datenverzeichnis)
	if werte == nil {
		werte = &FailsafeWerte{GrenzeW: b.konf.FailsafeGrenzeW}
	}
	b.mu.Lock()
	werte.Mindestdauer = b.failsafeMindestdauer
	if b.bezug != nil {
		werte.GrenzeW = b.bezug.failsafeGrenzeW
	}
	if b.einspeisung != nil {
		wert := b.einspeisung.failsafeGrenzeW
		werte.EinspeisegrenzeW = &wert
	}
	b.mu.Unlock()
	inhalt, err := json.Marshal(werte)
	if err == nil {
		err = gemeinsam.SchreibeDatei(filepath.Join(b.konf.Datenverzeichnis, FailsafeDatei), inhalt)
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
	// Gleiche Grenzen wie fuer Werte der Steuerbox (pruefeKonfiguration)
	if werte.Mindestdauer < 2*time.Hour || werte.Mindestdauer > 24*time.Hour || werte.GrenzeW < 0 ||
		(werte.EinspeisegrenzeW != nil && *werte.EinspeisegrenzeW < 0) {
		log.Printf("%s enthaelt ungueltige Werte, es gelten die FAILSAFE_*-Vorgaben", FailsafeDatei)
		return nil
	}
	return &werte
}

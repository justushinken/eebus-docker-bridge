package main

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	eglpc "github.com/enbility/eebus-go/usecases/eg/lpc"
	shipapi "github.com/enbility/ship-go/api"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Datei im Datenverzeichnis mit der gekoppelten Bruecke.
const kopplungsdatei = "kopplung.json"

var skiMuster = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Steuerbox haelt den Zustand der simulierten Steuerbox. Zugriff nur unter mu.
// Aufrufe in den EEBUS-Stack immer ausserhalb von mu, damit es keine
// Verklemmung mit dessen Callbacks gibt.
type Steuerbox struct {
	konf        Konfiguration
	eigenerSki  string
	shipId      string
	fingerprint string
	gestartet   time.Time
	pairing     gemeinsam.Pairingprotokoll

	dienst   api.ServiceInterface
	entitaet spineapi.EntityLocalInterface
	lpc      *eglpc.LPC

	mu           sync.Mutex
	kopplung     *gemeinsam.Kopplung // gekoppelte Bruecke, nil = keine
	unterbrochen bool                // Verbindung absichtlich getrennt (Simulation)
	verbunden    bool
	partner      shipapi.ServiceIdentity // verbundene Bruecke
	gefunden     []shipapi.RemoteMdnsService
	gemeldet     map[string]string // zuletzt von der Bruecke gemeldete Werte, fuer das Log
}

func NeueSteuerbox(konf Konfiguration, eigenerSki string) *Steuerbox {
	s := &Steuerbox{konf: konf, eigenerSki: eigenerSki, gestartet: time.Now()}
	kopplung, err := gemeinsam.LadeKopplung(s.kopplungspfad())
	if err != nil {
		log.Printf("Gespeicherte Kopplung nicht lesbar: %v", err)
	}
	s.kopplung = kopplung
	return s
}

func (s *Steuerbox) kopplungspfad() string {
	return filepath.Join(s.konf.Datenverzeichnis, kopplungsdatei)
}

func (s *Steuerbox) Kopplung() *gemeinsam.Kopplung {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kopplung
}

// --- api.ServiceReaderInterface ---

// Verbindungswechsel nur einmal protokollieren, ship-go meldet sie teils doppelt.
func (s *Steuerbox) RemoteServiceConnected(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
	s.mu.Lock()
	neu := !s.verbunden
	s.verbunden = true
	s.partner = partner
	s.mu.Unlock()
	if neu {
		log.Printf("Bruecke verbunden: %s", gemeinsam.Bezeichnung(partner))
	}
}

func (s *Steuerbox) RemoteServiceDisconnected(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
	s.mu.Lock()
	neu := s.verbunden
	s.verbunden = false
	s.partner = shipapi.ServiceIdentity{}
	s.mu.Unlock()
	if neu {
		log.Printf("Bruecke getrennt: %s", gemeinsam.Bezeichnung(partner))
	}
}

func (s *Steuerbox) VisibleRemoteMdnsServicesUpdated(dienst api.ServiceInterface, eintraege []shipapi.RemoteMdnsService) {
	s.mu.Lock()
	neu := gemeinsam.NeuGefunden(s.gefunden, eintraege)
	s.gefunden = slices.Clone(eintraege)
	s.mu.Unlock()

	for _, e := range neu {
		log.Printf("Gefunden: %s %s, SHIP-ID %s, SKI %s", e.Brand, e.Model, e.ShipID, e.Ski)
	}
}

func (s *Steuerbox) ServiceUpdated(partner shipapi.ServiceIdentity) {}

func (s *Steuerbox) ServicePairingDetailUpdate(partner shipapi.ServiceIdentity, detail *shipapi.ConnectionStateDetail) {
	text, melden := s.pairing.Neu(partner, detail)
	if !melden {
		return
	}
	log.Printf("Pairing %s: %s", gemeinsam.Bezeichnung(partner), text)
	if detail.State() == shipapi.ConnectionStateRemoteDeniedTrust {
		if k := s.Kopplung(); k != nil && k.Verfahren == gemeinsam.VerfahrenSki {
			log.Printf("Die Bruecke vertraut dieser Steuerbox nicht: bei der Bruecke EEBUS_REMOTE_SKI=%s setzen", s.eigenerSki)
		}
	}
}

// Die Steuerbox ist beim Pairing Service die ankuendigende Seite. Diese
// Ereignisse betreffen die annehmende Seite und kommen hier nur zur Vollstaendigkeit.
func (s *Steuerbox) ServiceAutoTrusted(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
}
func (s *Steuerbox) ServiceAutoTrustFailed(dienst api.ServiceInterface, partner shipapi.ServiceIdentity, grund error) {
	log.Printf("Pairing Service fehlgeschlagen fuer %s: %v", gemeinsam.Bezeichnung(partner), grund)
}
func (s *Steuerbox) ServiceAutoTrustRemoved(dienst api.ServiceInterface, partner shipapi.ServiceIdentity, grund string) {
}

// --- Ereignisse des Use Case LPC (Energy Guard) ---

func (s *Steuerbox) LpcEreignis(ski string, geraet spineapi.DeviceRemoteInterface, entitaet spineapi.EntityRemoteInterface, ereignis api.EventType) {
	switch ereignis {
	case eglpc.UseCaseSupportUpdate:
		if szenarien := s.lpc.AvailableScenariosForEntity(entitaet); len(szenarien) > 0 {
			log.Printf("Bruecke unterstuetzt LPC, Szenarien %v", szenarien)
		}

	case eglpc.DataUpdateLimit:
		if grenze, err := s.lpc.ConsumptionLimit(entitaet); err == nil {
			log.Printf("Bruecke meldet Grenze: aktiv=%v, %.0f W, Dauer %v", grenze.IsActive, grenze.Value, grenze.Duration.Round(time.Second))
		}

	// eebus-go meldet bei jeder Aenderung der Konfiguration beide Failsafe-Ereignisse,
	// protokolliert wird nur ein tatsaechlich geaenderter Wert.
	case eglpc.DataUpdateFailsafeConsumptionActivePowerLimit:
		if wert, err := s.lpc.FailsafeConsumptionActivePowerLimit(entitaet); err == nil {
			if s.merke("failsafeGrenze", fmt.Sprintf("%.0f W", wert)) {
				log.Printf("Bruecke meldet Failsafe-Grenze: %.0f W", wert)
			}
		}

	case eglpc.DataUpdateFailsafeDurationMinimum:
		if dauer, err := s.lpc.FailsafeDurationMinimum(entitaet); err == nil {
			if s.merke("failsafeDauer", dauer.String()) {
				log.Printf("Bruecke meldet Failsafe-Mindestdauer: %v", dauer)
			}
		}

	// Die Nennleistung kommt von der SPS ueber die Bruecke: Test der ganzen Kette.
	case eglpc.DataUpdatePowerConsumptionNominalMax:
		if wert, err := s.lpc.ConsumptionNominalMax(entitaet); err == nil {
			if s.merke("nennleistung", fmt.Sprintf("%.0f W", wert)) {
				log.Printf("Bruecke meldet Nennleistung: %.0f W", wert)
			}
		}
	}
}

// merke speichert den zuletzt gemeldeten Wert und liefert, ob er neu ist.
func (s *Steuerbox) merke(schluessel, wert string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gemeldet == nil {
		s.gemeldet = make(map[string]string)
	}
	neu := s.gemeldet[schluessel] != wert
	s.gemeldet[schluessel] = wert
	return neu
}

// ziel liefert die LPC-Entitaet der verbundenen Bruecke.
func (s *Steuerbox) ziel() (spineapi.EntityRemoteInterface, error) {
	s.mu.Lock()
	gekoppelt, verbunden := s.kopplung != nil, s.verbunden
	s.mu.Unlock()
	if !gekoppelt {
		return nil, errors.New("keine Bruecke gekoppelt")
	}
	if !verbunden {
		return nil, errors.New("Bruecke nicht verbunden")
	}
	for _, e := range s.lpc.RemoteEntitiesScenarios() {
		if e.Entity != nil && len(e.Scenarios) > 0 {
			return e.Entity, nil
		}
	}
	return nil, errors.New("Bruecke hat LPC noch nicht gemeldet, kurz warten")
}

// --- Aktionen aus dem Web-UI ---

func (s *Steuerbox) SendeGrenze(wertW float64, dauer time.Duration, aktiv bool) error {
	ziel, err := s.ziel()
	if err != nil {
		return err
	}
	beschreibung := fmt.Sprintf("aktiv=%v, %.0f W, Dauer %v", aktiv, wertW, dauer)
	if dauer == 0 {
		beschreibung = fmt.Sprintf("aktiv=%v, %.0f W, unbefristet", aktiv, wertW)
	}
	grenze := ucapi.LoadLimit{Value: wertW, Duration: dauer, IsActive: aktiv}
	_, err = s.lpc.WriteConsumptionLimit(ziel, grenze, func(ergebnis model.ResultDataType, _ model.MsgCounterType) {
		if ergebnis.ErrorNumber != nil && *ergebnis.ErrorNumber != model.ErrorNumberTypeNoError {
			grund := ""
			if ergebnis.Description != nil {
				grund = string(*ergebnis.Description)
			}
			log.Printf("Bruecke lehnt Grenze ab (Fehler %d): %s", *ergebnis.ErrorNumber, grund)
			return
		}
		log.Printf("Bruecke hat Grenze angenommen")
	})
	if err != nil {
		return err
	}
	log.Printf("Grenze gesendet: %s", beschreibung)
	return nil
}

func (s *Steuerbox) SendeFailsafe(grenzeW float64, mindestdauer time.Duration) error {
	// Bereich laut LPC-Spezifikation. Vorab pruefen, sonst waere die Grenze
	// schon geschrieben, bevor eebus-go die Dauer ablehnt.
	if mindestdauer < 2*time.Hour || mindestdauer > 24*time.Hour {
		return errors.New("Failsafe-Mindestdauer muss zwischen 2 und 24 h liegen")
	}
	ziel, err := s.ziel()
	if err != nil {
		return err
	}
	if _, err := s.lpc.WriteFailsafeConsumptionActivePowerLimit(ziel, grenzeW); err != nil {
		return fmt.Errorf("Failsafe-Grenze: %w", err)
	}
	if _, err := s.lpc.WriteFailsafeDurationMinimum(ziel, mindestdauer); err != nil {
		return fmt.Errorf("Failsafe-Mindestdauer: %w", err)
	}
	log.Printf("Failsafe-Werte gesendet: %.0f W, Mindestdauer %v", grenzeW, mindestdauer)
	return nil
}

func (s *Steuerbox) SetzeHeartbeat(an bool) {
	if an {
		s.lpc.StartHeartbeat()
		log.Printf("Heartbeat gestartet")
	} else {
		s.lpc.StopHeartbeat()
		log.Printf("Heartbeat gestoppt (Test): Bruecke geht nach 120 s ohne Heartbeat in Failsafe")
	}
}

func (s *Steuerbox) HeartbeatLaeuft() bool {
	manager := s.entitaet.HeartbeatManager()
	return manager != nil && manager.IsHeartbeatRunning()
}

// Kurze Unterbrechung: Verbindung schliessen, ship-go baut sie selbst wieder auf.
func (s *Steuerbox) UnterbrecheKurz() error {
	s.mu.Lock()
	partner, verbunden := s.partner, s.verbunden
	s.mu.Unlock()
	if !verbunden {
		return errors.New("Bruecke nicht verbunden")
	}
	log.Printf("Verbindung kurz unterbrochen (Test), Neuaufbau automatisch")
	s.dienst.DisconnectService(partner, "Test: kurze Unterbrechung")
	return nil
}

// Dauerhafte Trennung, bis sie mit Wiederherstellen aufgehoben wird. Die
// Steuerbox vertraut der Bruecke solange nicht, daher baut auch die Bruecke
// die Verbindung nicht wieder auf.
func (s *Steuerbox) Trenne() error {
	s.mu.Lock()
	kopplung := s.kopplung
	s.unterbrochen = kopplung != nil
	s.mu.Unlock()
	if kopplung == nil {
		return errors.New("keine Bruecke gekoppelt")
	}
	log.Printf("Verbindung getrennt (Test), bleibt getrennt bis \"Wiederherstellen\"")
	s.dienst.UnregisterRemoteService(kopplung.Identitaet)
	return nil
}

func (s *Steuerbox) StelleWiederHer() error {
	s.mu.Lock()
	kopplung := s.kopplung
	s.unterbrochen = false
	s.mu.Unlock()
	if kopplung == nil {
		return errors.New("keine Bruecke gekoppelt")
	}
	log.Printf("Verbindung wird wiederhergestellt")
	s.dienst.RegisterRemoteService(kopplung.Identitaet)
	return nil
}

// KoppelnPerSki: bisheriges Verfahren. Die Bruecke muss den SKI dieser
// Steuerbox ebenfalls kennen (EEBUS_REMOTE_SKI).
func (s *Steuerbox) KoppelnPerSki(ski string) error {
	ski = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(ski), " ", ""))
	if !skiMuster.MatchString(ski) {
		return errors.New("ungueltiger SKI, erwartet 40 Hex-Zeichen")
	}
	if ski == s.eigenerSki {
		return errors.New("das ist der eigene SKI")
	}
	kopplung := &gemeinsam.Kopplung{Verfahren: gemeinsam.VerfahrenSki, Identitaet: shipapi.NewServiceIdentity(ski, "", "")}
	if err := s.ersetzeKopplung(kopplung); err != nil {
		return err
	}
	log.Printf("Gekoppelt per SKI mit Bruecke %s", ski)
	s.dienst.RegisterRemoteService(kopplung.Identitaet)
	return nil
}

// KoppelnPerPairingService: neues Verfahren. Der QR-Text der Bruecke enthaelt
// SKI, SHIP-ID, Fingerprint und Secret. Die Steuerbox vertraut der Bruecke und
// kuendigt sich per mDNS mit einem HMAC ueber das Secret an. Die Bruecke prueft
// das und vertraut der Steuerbox dann ohne weiteres Zutun.
func (s *Steuerbox) KoppelnPerPairingService(qrText string) error {
	daten, err := gemeinsam.LiesPairingQr(qrText)
	if err != nil {
		return err
	}
	identitaet := shipapi.NewServiceIdentity(daten.Ski, daten.Fingerprint, daten.ShipId)
	kopplung := &gemeinsam.Kopplung{Verfahren: gemeinsam.VerfahrenPairing, Identitaet: identitaet}
	if err := s.ersetzeKopplung(kopplung); err != nil {
		return err
	}
	// Die ankuendigende Seite muss der Gegenseite vorher vertrauen.
	s.dienst.RegisterRemoteService(identitaet)
	ziel := shipapi.PairingTarget{SKI: daten.Ski, Fingerprint: daten.Fingerprint, ShipID: daten.ShipId, Secret: daten.Secret}
	if err := s.dienst.StartAnnouncementTo(ziel); err != nil {
		return fmt.Errorf("Ankuendigung starten: %w", err)
	}
	log.Printf("Pairing Service: Ankuendigung an Bruecke %s gestartet", gemeinsam.Bezeichnung(identitaet))
	return nil
}

func (s *Steuerbox) Entkoppeln() error {
	s.mu.Lock()
	alt := s.kopplung
	s.mu.Unlock()
	if alt == nil {
		return errors.New("keine Bruecke gekoppelt")
	}
	if err := s.ersetzeKopplung(nil); err != nil {
		return err
	}
	log.Printf("Kopplung mit Bruecke %s aufgehoben", gemeinsam.Bezeichnung(alt.Identitaet))
	return nil
}

// ersetzeKopplung speichert die neue Kopplung und meldet die alte beim
// EEBUS-Stack ab, samt einer noch laufenden Ankuendigung.
func (s *Steuerbox) ersetzeKopplung(neu *gemeinsam.Kopplung) error {
	if err := gemeinsam.SpeichereKopplung(s.kopplungspfad(), neu); err != nil {
		return err
	}
	s.mu.Lock()
	alt := s.kopplung
	s.kopplung = neu
	s.unterbrochen = false
	s.mu.Unlock()

	if alt != nil {
		if id := alt.Identitaet.ShipID; id != "" && s.dienst.IsAnnouncingTo(id) {
			if err := s.dienst.StopAnnouncementTo(id); err != nil {
				log.Printf("Ankuendigung beenden: %v", err)
			}
		}
		s.dienst.UnregisterRemoteService(alt.Identitaet)
	}
	return nil
}

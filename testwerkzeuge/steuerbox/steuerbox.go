package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/features/client"
	"github.com/enbility/eebus-go/service"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	eglpc "github.com/enbility/eebus-go/usecases/eg/lpc"
	shipapi "github.com/enbility/ship-go/api"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Datei im Datenverzeichnis, in der der SKI der gekoppelten Bruecke steht.
const kopplungsdatei = "bruecke-ski.txt"

var skiMuster = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Steuerbox haelt den Zustand der simulierten Steuerbox. Zugriff nur unter mu.
// Aufrufe in den EEBUS-Stack immer ausserhalb von mu, damit es keine
// Verklemmung mit dessen Callbacks gibt.
type Steuerbox struct {
	konf       Konfiguration
	eigenerSki string
	gestartet  time.Time
	pairing    gemeinsam.Pairingprotokoll

	dienst   *service.Service
	entitaet spineapi.EntityLocalInterface
	lpc      *eglpc.LPC

	mu           sync.Mutex
	gekoppelt    string // SKI der Bruecke, leer = nicht gekoppelt
	unterbrochen bool   // Verbindung absichtlich getrennt (Simulation)
	verbunden    bool
	gefunden     []shipapi.RemoteService
	gemeldet     map[string]string // zuletzt von der Bruecke gemeldete Werte, fuer das Log
	abonniert    spineapi.EntityRemoteInterface
}

func NeueSteuerbox(konf Konfiguration, eigenerSki string) *Steuerbox {
	s := &Steuerbox{konf: konf, eigenerSki: eigenerSki, gestartet: time.Now()}
	if inhalt, err := os.ReadFile(filepath.Join(konf.Datenverzeichnis, kopplungsdatei)); err == nil {
		s.gekoppelt = strings.TrimSpace(string(inhalt))
	}
	return s
}

func (s *Steuerbox) Gekoppelt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gekoppelt
}

// --- api.ServiceReaderInterface ---

// Verbindungswechsel nur einmal protokollieren, ship-go meldet sie teils doppelt.
func (s *Steuerbox) RemoteSKIConnected(dienst api.ServiceInterface, ski string) {
	s.mu.Lock()
	neu := !s.verbunden
	s.verbunden = true
	s.mu.Unlock()
	if neu {
		log.Printf("Bruecke verbunden: %s", ski)
	}
}

func (s *Steuerbox) RemoteSKIDisconnected(dienst api.ServiceInterface, ski string) {
	s.mu.Lock()
	neu := s.verbunden
	s.verbunden = false
	s.mu.Unlock()
	if neu {
		log.Printf("Bruecke getrennt: %s", ski)
	}
}

func (s *Steuerbox) VisibleRemoteServicesUpdated(dienst api.ServiceInterface, eintraege []shipapi.RemoteService) {
	s.mu.Lock()
	neu := gemeinsam.NeuGefunden(s.gefunden, eintraege)
	s.gefunden = slices.Clone(eintraege)
	s.mu.Unlock()

	for _, e := range neu {
		log.Printf("Gefunden: %s %s, SKI %s", e.Brand, e.Model, e.Ski)
	}
}

func (s *Steuerbox) ServiceShipIDUpdate(ski string, shipId string) {}

func (s *Steuerbox) ServicePairingDetailUpdate(ski string, detail *shipapi.ConnectionStateDetail) {
	text, melden := s.pairing.Neu(ski, detail)
	if !melden {
		return
	}
	log.Printf("Pairing %s: %s", ski, text)
	if detail.State() == shipapi.ConnectionStateRemoteDeniedTrust && ski == s.Gekoppelt() {
		log.Printf("Die Bruecke vertraut dieser Steuerbox nicht: bei der Bruecke EEBUS_REMOTE_SKI=%s setzen", s.eigenerSki)
	}
}

// Nur der gekoppelten Bruecke wird vertraut, und auch ihr nicht, solange die
// Verbindung absichtlich getrennt ist. Sonst baut die Bruecke sie sofort wieder auf.
func (s *Steuerbox) AllowWaitingForTrust(ski string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ski == s.gekoppelt && !s.unterbrochen
}

// --- Ereignisse des Use Case LPC (Energy Guard) ---

func (s *Steuerbox) LpcEreignis(ski string, geraet spineapi.DeviceRemoteInterface, entitaet spineapi.EntityRemoteInterface, ereignis api.EventType) {
	switch ereignis {
	case eglpc.UseCaseSupportUpdate:
		log.Printf("Bruecke unterstuetzt LPC, Szenarien %v", s.lpc.AvailableScenariosForEntity(entitaet))
		s.pruefeAbo(entitaet)

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

// pruefeAbo abonniert die Nennleistung, sobald eine neue Entitaet der Bruecke
// auftaucht, also auch nach jedem Neuverbinden.
//
// eg/lpc fragt die Nennleistung (Szenario 4) nicht selbst ab. Sie kommt von der
// SPS ueber die Bruecke und ist damit ein guter Test der ganzen Kette.
func (s *Steuerbox) pruefeAbo(entitaet spineapi.EntityRemoteInterface) {
	s.mu.Lock()
	neu := s.abonniert != entitaet
	s.abonniert = entitaet
	s.mu.Unlock()
	if !neu {
		return
	}

	verbindung, err := client.NewElectricalConnection(s.entitaet, entitaet)
	if err != nil {
		log.Printf("Nennleistung abonnieren: %v", err)
		return
	}
	if !verbindung.HasSubscription() {
		if _, err := verbindung.Subscribe(); err != nil {
			log.Printf("Nennleistung abonnieren: %v", err)
		}
	}
	if _, err := verbindung.RequestCharacteristics(nil, nil); err != nil {
		log.Printf("Nennleistung abfragen: %v", err)
	}
}

// ziel liefert die LPC-Entitaet der gekoppelten Bruecke.
func (s *Steuerbox) ziel() (spineapi.EntityRemoteInterface, error) {
	s.mu.Lock()
	ski, verbunden := s.gekoppelt, s.verbunden
	s.mu.Unlock()
	if ski == "" {
		return nil, errors.New("keine Bruecke gekoppelt")
	}
	if !verbunden {
		return nil, errors.New("Bruecke nicht verbunden")
	}
	// Nicht ueber lpc.RemoteEntitiesScenarios(): eebus-go v0.7.0 entfernt dort beim
	// Trennen die Entitaet nicht (das Ereignis traegt keine), nach dem Neuverbinden
	// steht noch das alte Objekt mit der toten Verbindung drin. Daher die Entitaet
	// des aktuell verbundenen Geraets nehmen. Der Szenarien-Abgleich laeuft ueber
	// die Adresse und passt auch zum neuen Objekt.
	geraet := s.dienst.LocalDevice().RemoteDeviceForSki(ski)
	if geraet == nil {
		return nil, errors.New("Bruecke nicht verbunden")
	}
	for _, e := range geraet.Entities() {
		if len(s.lpc.AvailableScenariosForEntity(e)) > 0 {
			s.pruefeAbo(e)
			return e, nil
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
	_, err = s.lpc.WriteConsumptionLimit(ziel, grenze, func(ergebnis model.ResultDataType) {
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
	manager := s.entitaet.HeartbeatManager()
	if an {
		if err := manager.StartHeartbeat(); err != nil {
			log.Printf("Heartbeat starten: %v", err)
			return
		}
		log.Printf("Heartbeat gestartet")
	} else {
		manager.StopHeartbeat()
		log.Printf("Heartbeat gestoppt (Test): Bruecke geht nach 120 s ohne Heartbeat in Failsafe")
	}
}

// Kurze Unterbrechung: Verbindung schliessen, ship-go baut sie selbst wieder auf.
func (s *Steuerbox) UnterbrecheKurz() error {
	s.mu.Lock()
	ski, verbunden := s.gekoppelt, s.verbunden
	s.mu.Unlock()
	if !verbunden {
		return errors.New("Bruecke nicht verbunden")
	}
	log.Printf("Verbindung kurz unterbrochen (Test), Neuaufbau automatisch")
	s.dienst.DisconnectSKI(ski, "Test: kurze Unterbrechung")
	return nil
}

// Dauerhafte Trennung, bis sie mit Wiederherstellen aufgehoben wird.
func (s *Steuerbox) Trenne() error {
	s.mu.Lock()
	ski := s.gekoppelt
	s.unterbrochen = ski != ""
	s.mu.Unlock()
	if ski == "" {
		return errors.New("keine Bruecke gekoppelt")
	}
	log.Printf("Verbindung getrennt (Test), bleibt getrennt bis \"Wiederherstellen\"")
	s.dienst.UnregisterRemoteSKI(ski)
	return nil
}

func (s *Steuerbox) StelleWiederHer() error {
	s.mu.Lock()
	ski := s.gekoppelt
	s.unterbrochen = false
	s.mu.Unlock()
	if ski == "" {
		return errors.New("keine Bruecke gekoppelt")
	}
	log.Printf("Verbindung wird wiederhergestellt")
	s.dienst.RegisterRemoteSKI(ski)
	return nil
}

func (s *Steuerbox) Koppeln(ski string) error {
	ski = strings.ToLower(strings.TrimSpace(ski))
	if !skiMuster.MatchString(ski) {
		return errors.New("ungueltiger SKI, erwartet 40 Hex-Zeichen")
	}
	if ski == s.eigenerSki {
		return errors.New("das ist der eigene SKI")
	}
	if err := s.speichereKopplung(ski); err != nil {
		return err
	}
	s.mu.Lock()
	alt := s.gekoppelt
	s.gekoppelt = ski
	s.unterbrochen = false
	s.mu.Unlock()

	if alt != "" && alt != ski {
		s.dienst.UnregisterRemoteSKI(alt)
	}
	log.Printf("Gekoppelt mit Bruecke %s", ski)
	s.dienst.RegisterRemoteSKI(ski)
	return nil
}

func (s *Steuerbox) Entkoppeln() error {
	if err := s.speichereKopplung(""); err != nil {
		return err
	}
	s.mu.Lock()
	alt := s.gekoppelt
	s.gekoppelt = ""
	s.unterbrochen = false
	s.mu.Unlock()
	if alt == "" {
		return errors.New("keine Bruecke gekoppelt")
	}
	s.dienst.UnregisterRemoteSKI(alt)
	log.Printf("Kopplung mit Bruecke %s aufgehoben", alt)
	return nil
}

func (s *Steuerbox) speichereKopplung(ski string) error {
	pfad := filepath.Join(s.konf.Datenverzeichnis, kopplungsdatei)
	if ski == "" {
		if err := os.Remove(pfad); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(pfad, []byte(ski+"\n"), 0o600)
}

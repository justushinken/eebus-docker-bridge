package main

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	eglpc "github.com/enbility/eebus-go/usecases/eg/lpc"
	eglpp "github.com/enbility/eebus-go/usecases/eg/lpp"
	mamgcp "github.com/enbility/eebus-go/usecases/ma/mgcp"
	mampc "github.com/enbility/eebus-go/usecases/ma/mpc"
	shipapi "github.com/enbility/ship-go/api"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Datei im Datenverzeichnis mit der gekoppelten Bruecke.
const kopplungsdatei = "kopplung.json"

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
	lpp      *eglpp.LPP   // nil = per STEUERBOX_USECASES abgeschaltet
	mpc      *mampc.MPC   // nil = abgeschaltet
	mgcp     *mamgcp.MGCP // nil = abgeschaltet

	mu           sync.Mutex
	kopplung     *gemeinsam.Kopplung // gekoppelte Bruecke, nil = keine
	unterbrochen bool                // Verbindung absichtlich getrennt (Simulation)
	verbunden    bool
	partner      shipapi.ServiceIdentity // verbundene Bruecke
	gefunden     []shipapi.RemoteMdnsService
	gemeldet     map[string]string // zuletzt von der Bruecke gemeldete Werte, fuer das Log

	abonniert          string // MPC-Entitaet, deren Messwerte abonniert sind
	zustandAngefordert string // Entitaet, deren Betriebszustand angefordert wurde
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
	// Nur die Bruecke, mit der die Steuerbox verbunden ist. Andere EEBUS-Geraete
	// im LAN koennen sich ebenfalls melden und wieder trennen.
	if s.verbunden && !s.partner.IsZero() && !gemeinsam.GleicheIdentitaet(partner, s.partner) {
		s.mu.Unlock()
		return
	}
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
			log.Printf("Die Bruecke vertraut dieser Steuerbox nicht: an der Bruecke den Suchmodus starten und die Anfrage annehmen (SKI %s)", s.eigenerSki)
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

// --- Ereignisse der Use Cases LPP, MPC und MGCP ---

func (s *Steuerbox) LppEreignis(ski string, geraet spineapi.DeviceRemoteInterface, entitaet spineapi.EntityRemoteInterface, ereignis api.EventType) {
	switch ereignis {
	case eglpp.UseCaseSupportUpdate:
		if szenarien := s.lpp.AvailableScenariosForEntity(entitaet); len(szenarien) > 0 {
			log.Printf("Bruecke unterstuetzt LPP, Szenarien %v", szenarien)
		}

	case eglpp.DataUpdateLimit:
		if grenze, err := s.lpp.ProductionLimit(entitaet); err == nil {
			log.Printf("Bruecke meldet Einspeisegrenze: aktiv=%v, %.0f W, Dauer %v", grenze.IsActive, grenze.Value, grenze.Duration.Round(time.Second))
		}

	case eglpp.DataUpdateFailsafeProductionActivePowerLimit:
		if wert, err := s.lpp.FailsafeProductionActivePowerLimit(entitaet); err == nil {
			if s.merke("failsafeEinspeisung", fmt.Sprintf("%.0f W", wert)) {
				log.Printf("Bruecke meldet Failsafe-Einspeisegrenze: %.0f W", wert)
			}
		}

	case eglpp.DataUpdatePowerProductionNominalMax:
		if wert, err := s.lpp.ProductionNominalMax(entitaet); err == nil {
			if s.merke("nennleistungErzeugung", fmt.Sprintf("%.0f W", wert)) {
				log.Printf("Bruecke meldet Nennleistung Erzeugung: %.0f W", wert)
			}
		}
	}
}

func (s *Steuerbox) MgcpEreignis(ski string, geraet spineapi.DeviceRemoteInterface, entitaet spineapi.EntityRemoteInterface, ereignis api.EventType) {
	if ereignis == mamgcp.UseCaseSupportUpdate {
		if szenarien := s.mgcp.AvailableScenariosForEntity(entitaet); len(szenarien) > 0 {
			log.Printf("Bruecke unterstuetzt MGCP, Szenarien %v", szenarien)
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

type ucMitEntitaeten interface {
	RemoteEntitiesScenarios() []api.RemoteEntityScenarios
}

// ziel liefert die LPC-Entitaet der verbundenen Bruecke.
func (s *Steuerbox) ziel() (spineapi.EntityRemoteInterface, error) {
	return s.zielFuer(s.lpc, "LPC")
}

// zielFuer liefert die Entitaet der verbundenen Bruecke, die den Use Case anbietet.
func (s *Steuerbox) zielFuer(uc ucMitEntitaeten, name string) (spineapi.EntityRemoteInterface, error) {
	if uc == nil || (name == "LPP" && s.lpp == nil) || (name == "MGCP" && s.mgcp == nil) {
		return nil, fmt.Errorf("%s ist in dieser Steuerbox abgeschaltet (STEUERBOX_USECASES)", name)
	}
	s.mu.Lock()
	gekoppelt, verbunden := s.kopplung != nil, s.verbunden
	s.mu.Unlock()
	if !gekoppelt {
		return nil, errors.New("keine Bruecke gekoppelt")
	}
	if !verbunden {
		return nil, errors.New("Bruecke nicht verbunden")
	}
	for _, e := range uc.RemoteEntitiesScenarios() {
		if e.Entity != nil && len(e.Scenarios) > 0 {
			return e.Entity, nil
		}
	}
	return nil, fmt.Errorf("Bruecke hat %s nicht gemeldet (noch nicht oder dort abgeschaltet)", name)
}

// --- Aktionen aus dem Web-UI: Senden siehe senden.go ---

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
	ski, err := gemeinsam.NormalisiereSki(ski)
	if err != nil {
		return err
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

// UseCaseNamen liefert die eingeschalteten Use Cases dieser Steuerbox.
func (s *Steuerbox) UseCaseNamen() []string {
	namen := []string{"LPC"}
	for _, uc := range []struct {
		an   bool
		name string
	}{{s.lpp != nil, "LPP"}, {s.mpc != nil, "MPC"}, {s.mgcp != nil, "MGCP"}} {
		if uc.an {
			namen = append(namen, uc.name)
		}
	}
	return namen
}

// brueckenUseCases: Welche Use Cases bietet die Bruecke an (Akteur auf ihrer Seite)?
func (s *Steuerbox) brueckenUseCases() []UseCaseDaten {
	eigene := s.UseCaseNamen()
	var liste []UseCaseDaten
	for _, uc := range []struct {
		name   string
		akteur model.UseCaseActorType
		uc     model.UseCaseNameType
	}{
		{"LPC", model.UseCaseActorTypeControllableSystem, model.UseCaseNameTypeLimitationOfPowerConsumption},
		{"LPP", model.UseCaseActorTypeControllableSystem, model.UseCaseNameTypeLimitationOfPowerProduction},
		{"MPC", model.UseCaseActorTypeMonitoredUnit, model.UseCaseNameTypeMonitoringOfPowerConsumption},
		{"MGCP", model.UseCaseActorTypeGridConnectionPoint, model.UseCaseNameTypeMonitoringOfGridConnectionPoint},
	} {
		liste = append(liste, UseCaseDaten{
			Name:         uc.name,
			Steuerbox:    slices.Contains(eigene, uc.name),
			Unterstuetzt: s.entitaetFuerUseCase(uc.akteur, uc.uc) != nil,
		})
	}
	return liste
}

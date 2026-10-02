package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"time"

	"eebus-bruecke/internal/gemeinsam"
)

//go:embed web/index.html
var indexHtml []byte

//go:embed web/anleitung.html
var anleitungHtml []byte

type Antwort struct {
	Text   string `json:"text,omitempty"`
	Fehler string `json:"fehler,omitempty"`
}

func webHandler(s *Steuerbox, protokoll *gemeinsam.Ereignisprotokoll) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", gemeinsam.SeiteAusliefern(gemeinsam.MitStil(indexHtml)))
	mux.HandleFunc("GET /anleitung", gemeinsam.SeiteAusliefern(gemeinsam.MitStil(anleitungHtml)))
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		status := s.Status(time.Now())
		status.Ereignisse = protokoll.Liste()
		gemeinsam.SchreibeJson(w, http.StatusOK, status)
	})

	mux.HandleFunc("POST /api/grenze", aktion(func(daten struct {
		WertW  float64 `json:"wertW"`
		DauerS float64 `json:"dauerS"`
		Aktiv  bool    `json:"aktiv"`
	}) (string, error) {
		if daten.WertW < 0 || daten.DauerS < 0 {
			return "", errors.New("Werte duerfen nicht negativ sein")
		}
		dauer := time.Duration(daten.DauerS) * time.Second
		if err := s.SendeGrenze(daten.WertW, dauer, daten.Aktiv); err != nil {
			return "", err
		}
		return "Gesendet, Antwort der Bruecke unter Ereignisse", nil
	}))

	mux.HandleFunc("POST /api/failsafe", aktion(func(daten struct {
		GrenzeW       float64 `json:"grenzeW"`
		MindestdauerS float64 `json:"mindestdauerS"`
	}) (string, error) {
		if daten.GrenzeW < 0 {
			return "", errors.New("Grenze darf nicht negativ sein")
		}
		if err := s.SendeFailsafe(daten.GrenzeW, time.Duration(daten.MindestdauerS)*time.Second); err != nil {
			return "", err
		}
		return "Gesendet, die Bruecke meldet die neuen Werte zurueck", nil
	}))

	mux.HandleFunc("POST /api/heartbeat", aktion(func(daten struct {
		An bool `json:"an"`
	}) (string, error) {
		s.SetzeHeartbeat(daten.An)
		if daten.An {
			return "Heartbeat laeuft", nil
		}
		return "Heartbeat gestoppt", nil
	}))

	mux.HandleFunc("POST /api/verbindung", aktion(func(daten struct {
		Aktion string `json:"aktion"`
	}) (string, error) {
		switch daten.Aktion {
		case "kurz":
			return "Verbindung geschlossen, Neuaufbau automatisch", s.UnterbrecheKurz()
		case "trennen":
			return "Verbindung getrennt", s.Trenne()
		case "wiederherstellen":
			return "Verbindung wird aufgebaut", s.StelleWiederHer()
		}
		return "", fmt.Errorf("unbekannte Aktion %q", daten.Aktion)
	}))

	mux.HandleFunc("POST /api/kopplung", aktion(func(daten struct {
		Verfahren string `json:"verfahren"`
		Ski       string `json:"ski"`
		QrText    string `json:"qrText"`
	}) (string, error) {
		switch daten.Verfahren {
		case gemeinsam.VerfahrenSki:
			return "Per SKI gekoppelt, Verbindung wird aufgebaut", s.KoppelnPerSki(daten.Ski)
		case gemeinsam.VerfahrenPairing:
			return "Ankuendigung laeuft, die Bruecke sollte sich in einigen Sekunden verbinden", s.KoppelnPerPairingService(daten.QrText)
		}
		return "", fmt.Errorf("unbekanntes Verfahren %q", daten.Verfahren)
	}))

	mux.HandleFunc("DELETE /api/kopplung", aktion(func(struct{}) (string, error) {
		return "Kopplung aufgehoben", s.Entkoppeln()
	}))

	return mux
}

// aktion liest den JSON-Rumpf, fuehrt die Aktion aus und antwortet mit Text
// oder Fehler. Nur Content-Type application/json: Ein Formular einer fremden
// Seite kann das nicht senden, das schuetzt bei gespeicherter Anmeldung.
func aktion[T any](ausfuehren func(T) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); typ != "application/json" {
			gemeinsam.SchreibeJson(w, http.StatusUnsupportedMediaType, Antwort{Fehler: "Content-Type application/json erwartet"})
			return
		}
		var daten T
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&daten); err != nil {
			gemeinsam.SchreibeJson(w, http.StatusBadRequest, Antwort{Fehler: "ungueltige Anfrage: " + err.Error()})
			return
		}
		text, err := ausfuehren(daten)
		if err != nil {
			gemeinsam.SchreibeJson(w, http.StatusConflict, Antwort{Fehler: err.Error()})
			return
		}
		gemeinsam.SchreibeJson(w, http.StatusOK, Antwort{Text: text})
	}
}

// --- Statusabbild ---

type GrenzeDaten struct {
	WertW  float64 `json:"wertW"`
	Aktiv  bool    `json:"aktiv"`
	DauerS float64 `json:"dauerS"`
}

// BrueckenWerte sind die zuletzt von der Bruecke gemeldeten Werte, nil = nicht bekannt.
type BrueckenWerte struct {
	Szenarien             []uint       `json:"szenarien"`
	Grenze                *GrenzeDaten `json:"grenze"`
	FailsafeGrenzeW       *float64     `json:"failsafeGrenzeW"`
	FailsafeMindestdauerS *float64     `json:"failsafeMindestdauerS"`
	NennleistungW         *float64     `json:"nennleistungW"`
}

type StatusDaten struct {
	EigenerSki        string                       `json:"eigenerSki"`
	ShipId            string                       `json:"shipId"`
	Fingerprint       string                       `json:"fingerprint"`
	Gekoppelt         string                       `json:"gekoppelt"`    // Bezeichnung der Bruecke, leer = keine
	GekoppeltSki      string                       `json:"gekoppeltSki"` // fuer die Markierung in der Geraeteliste
	Verfahren         string                       `json:"verfahren"`    // ski | pairing
	Ankuendigung      bool                         `json:"ankuendigung"` // Pairing Service kuendigt gerade an
	Verbunden         bool                         `json:"verbunden"`
	Unterbrochen      bool                         `json:"unterbrochen"`
	HeartbeatLaeuft   bool                         `json:"heartbeatLaeuft"`
	HeartbeatAbstandS float64                      `json:"heartbeatAbstandS"`
	Bruecke           *BrueckenWerte               `json:"bruecke"` // nil = keine LPC-Entitaet
	Gefunden          []gemeinsam.GefundenesGeraet `json:"gefunden"`
	LaufzeitS         float64                      `json:"laufzeitS"`
	Ereignisse        []gemeinsam.Ereignis         `json:"ereignisse"`
}

func wertOderNil[T any](wert T, err error) *T {
	if err != nil {
		return nil
	}
	return &wert
}

func (s *Steuerbox) Status(jetzt time.Time) StatusDaten {
	s.mu.Lock()
	status := StatusDaten{
		EigenerSki:   s.eigenerSki,
		ShipId:       s.shipId,
		Fingerprint:  s.fingerprint,
		Verbunden:    s.verbunden,
		Unterbrochen: s.unterbrochen,
		Gefunden:     gemeinsam.GefundeneGeraete(s.gefunden),
		LaufzeitS:    jetzt.Sub(s.gestartet).Seconds(),
	}
	kopplung := s.kopplung
	s.mu.Unlock()

	if kopplung != nil {
		status.Gekoppelt = gemeinsam.Bezeichnung(kopplung.Identitaet)
		status.GekoppeltSki = kopplung.Identitaet.SKI
		status.Verfahren = kopplung.Verfahren
		if id := kopplung.Identitaet.ShipID; id != "" {
			status.Ankuendigung = s.dienst.IsAnnouncingTo(id)
		}
	}

	// Abfragen an den EEBUS-Stack ausserhalb von mu. Sie lesen nur den lokalen
	// Zwischenspeicher, es geht nichts ueber das Netz.
	status.HeartbeatLaeuft = s.HeartbeatLaeuft()
	// Abstand wie im HeartbeatManager von spine-go: Timeout minus 2 s.
	abstand := s.konf.HeartbeatTimeout
	if abstand > 2*time.Second {
		abstand -= 2 * time.Second
	}
	status.HeartbeatAbstandS = abstand.Seconds()

	if ziel, err := s.ziel(); err == nil {
		werte := &BrueckenWerte{Szenarien: s.lpc.AvailableScenariosForEntity(ziel)}
		if grenze, err := s.lpc.ConsumptionLimit(ziel); err == nil {
			werte.Grenze = &GrenzeDaten{WertW: grenze.Value, Aktiv: grenze.IsActive, DauerS: grenze.Duration.Seconds()}
		}
		werte.FailsafeGrenzeW = wertOderNil(s.lpc.FailsafeConsumptionActivePowerLimit(ziel))
		if dauer, err := s.lpc.FailsafeDurationMinimum(ziel); err == nil {
			sekunden := dauer.Seconds()
			werte.FailsafeMindestdauerS = &sekunden
		}
		werte.NennleistungW = wertOderNil(s.lpc.ConsumptionNominalMax(ziel))
		status.Bruecke = werte
	}
	return status
}

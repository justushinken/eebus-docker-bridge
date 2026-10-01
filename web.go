package main

import (
	"bytes"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Status-UI: eine eingebettete Seite, die /api/status jede Sekunde abfragt.
// Nur Anzeige, keine Aktionen. Zugriff per HTTP Basic Auth.

//go:embed web/index.html
var indexHtml []byte

// --- Ereignisprotokoll ---

type Ereignis struct {
	Zeit time.Time `json:"zeit"`
	Text string    `json:"text"`
}

// Ereignisprotokoll haelt die letzten Log-Zeilen. Es wird per
// log.SetOutput(io.MultiWriter(os.Stderr, protokoll)) eingehaengt, damit alle
// bestehenden log.Printf-Meldungen ohne Aenderung auch im UI erscheinen.
type Ereignisprotokoll struct {
	mu        sync.Mutex
	eintraege []Ereignis
	max       int
}

func NeuesEreignisprotokoll(max int) *Ereignisprotokoll {
	return &Ereignisprotokoll{max: max}
}

// logZeitformat entspricht log.LstdFlags ("2006/01/02 15:04:05 ").
const logZeitformat = "2006/01/02 15:04:05"

func (p *Ereignisprotokoll) Write(daten []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, zeile := range strings.Split(string(bytes.TrimRight(daten, "\n")), "\n") {
		e := Ereignis{Zeit: time.Now(), Text: zeile}
		if len(zeile) > len(logZeitformat) {
			if zeit, err := time.ParseInLocation(logZeitformat, zeile[:len(logZeitformat)], time.Local); err == nil {
				e = Ereignis{Zeit: zeit, Text: zeile[len(logZeitformat)+1:]}
			}
		}
		p.eintraege = append(p.eintraege, e)
	}
	if ueberzaehlig := len(p.eintraege) - p.max; ueberzaehlig > 0 {
		p.eintraege = append([]Ereignis(nil), p.eintraege[ueberzaehlig:]...)
	}
	return len(daten), nil
}

// Neueste zuerst.
func (p *Ereignisprotokoll) Liste() []Ereignis {
	p.mu.Lock()
	defer p.mu.Unlock()
	liste := make([]Ereignis, len(p.eintraege))
	for i, e := range p.eintraege {
		liste[len(liste)-1-i] = e
	}
	return liste
}

// --- Statusabbild ---

type GefundenesGeraet struct {
	Marke  string `json:"marke"`
	Modell string `json:"modell"`
	Typ    string `json:"typ"`
	Ski    string `json:"ski"`
}

// StatusDaten ist ein konsistenter Schnappschuss fuer das UI. Zeitangaben
// als Alter in Sekunden, damit eine abweichende Uhr des Browsers nicht stoert.
type StatusDaten struct {
	Zustand         LpcZustand `json:"zustand"`
	ZustandText     string     `json:"zustandText"`
	ZustandSeitS    float64    `json:"zustandSeitS"`
	Verbindung      Verbindung `json:"verbindung"`
	VerbindungText  string     `json:"verbindungText"`
	BegrenzungAktiv bool       `json:"begrenzungAktiv"`
	WirksameGrenzeW float64    `json:"wirksameGrenzeW"`
	RestdauerS      float64    `json:"restdauerS"` // 0 = unbefristet

	GrenzeNetzAktiv  bool     `json:"grenzeNetzAktiv"`
	GrenzeNetzW      float64  `json:"grenzeNetzW"`
	GrenzeNetzDauerS float64  `json:"grenzeNetzDauerS"`
	GrenzeNetzAlterS *float64 `json:"grenzeNetzAlterS"` // nil = nie empfangen

	FailsafeGrenzeW       float64  `json:"failsafeGrenzeW"`
	FailsafeMindestdauerS float64  `json:"failsafeMindestdauerS"`
	HeartbeatAlterS       *float64 `json:"heartbeatAlterS"` // nil = nie

	EigenerSki string             `json:"eigenerSki"`
	RemoteSki  string             `json:"remoteSki"`
	Gefunden   []GefundenesGeraet `json:"gefunden"`

	SpsOk                  bool     `json:"spsOk"`
	SpsLebenszeichenAlterS *float64 `json:"spsLebenszeichenAlterS"` // nil = nie
	NennleistungW          float64  `json:"nennleistungW"`

	Hersteller            string  `json:"hersteller"`
	Marke                 string  `json:"marke"`
	Modell                string  `json:"modell"`
	Seriennummer          string  `json:"seriennummer"`
	SchnittstellenVersion int     `json:"schnittstellenVersion"`
	LaufzeitS             float64 `json:"laufzeitS"`

	Ereignisse []Ereignis `json:"ereignisse"`
}

func alterS(jetzt, zeit time.Time) *float64 {
	if zeit.IsZero() {
		return nil
	}
	s := jetzt.Sub(zeit).Seconds()
	return &s
}

// Status erstellt das Abbild fuer das UI. Grenze und Restdauer wie in
// inputRegister(), damit UI und Modbus dasselbe zeigen.
func (b *Bruecke) Status(jetzt time.Time) StatusDaten {
	b.mu.Lock()
	defer b.mu.Unlock()

	aktiv, grenzeW := b.wirksameGrenze()
	var restdauer float64
	if b.zustand == ZustandBegrenzt && !b.grenzeAblauf.IsZero() {
		restdauer = max(b.grenzeAblauf.Sub(jetzt).Seconds(), 0)
	}

	gefunden := make([]GefundenesGeraet, 0, len(b.gefunden))
	for _, e := range b.gefunden {
		gefunden = append(gefunden, GefundenesGeraet{Marke: e.Brand, Modell: e.Model, Typ: e.Type, Ski: e.Ski})
	}

	return StatusDaten{
		Zustand:         b.zustand,
		ZustandText:     b.zustand.String(),
		ZustandSeitS:    jetzt.Sub(b.zustandSeit).Seconds(),
		Verbindung:      b.verbindung,
		VerbindungText:  b.verbindung.String(),
		BegrenzungAktiv: aktiv,
		WirksameGrenzeW: grenzeW,
		RestdauerS:      restdauer,

		GrenzeNetzAktiv:  b.grenze.IsActive,
		GrenzeNetzW:      b.grenze.Value,
		GrenzeNetzDauerS: b.grenze.Duration.Seconds(),
		GrenzeNetzAlterS: alterS(jetzt, b.letzteGrenzeEmpfangen),

		FailsafeGrenzeW:       b.failsafeGrenzeW,
		FailsafeMindestdauerS: b.failsafeMindestdauer.Seconds(),
		HeartbeatAlterS:       alterS(jetzt, b.letzterHeartbeat),

		EigenerSki: b.eigenerSki,
		RemoteSki:  b.konf.RemoteSki,
		Gefunden:   gefunden,

		SpsOk:                  b.spsOk,
		SpsLebenszeichenAlterS: alterS(jetzt, b.spsLebenszeichenSeit),
		NennleistungW:          b.gemeldeteNennleistungW,

		Hersteller:            b.konf.Hersteller,
		Marke:                 b.konf.Marke,
		Modell:                b.konf.Modell,
		Seriennummer:          b.konf.Seriennummer,
		SchnittstellenVersion: SchnittstellenVersion,
		LaufzeitS:             jetzt.Sub(b.gestartet).Seconds(),
	}
}

// --- HTTP-Server ---

func starteWebServer(adresse, benutzer, passwort string, b *Bruecke, protokoll *Ereignisprotokoll) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHtml)
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		status := b.Status(time.Now())
		status.Ereignisse = protokoll.Liste()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(status)
	})

	server := &http.Server{
		Handler:           mitAnmeldung(benutzer, passwort, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// Listen vorab, damit ein belegter Port sofort als Fehler zurueckkommt.
	lauscher, err := net.Listen("tcp", adresse)
	if err != nil {
		return nil, err
	}
	go server.Serve(lauscher)
	return server, nil
}

// mitAnmeldung schuetzt alle Pfade per HTTP Basic Auth. Ohne HTTPS ist das
// Passwort im LAN mitlesbar: Schutz vor zufaelligem Zugriff, nicht vor Angriffen.
func mitAnmeldung(benutzer, passwort string, weiter http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, p, ok := r.BasicAuth()
		benutzerOk := subtle.ConstantTimeCompare([]byte(b), []byte(benutzer)) == 1
		passwortOk := subtle.ConstantTimeCompare([]byte(p), []byte(passwort)) == 1
		if !ok || !benutzerOk || !passwortOk {
			w.Header().Set("WWW-Authenticate", `Basic realm="EEBUS-Bruecke", charset="UTF-8"`)
			http.Error(w, "Anmeldung erforderlich", http.StatusUnauthorized)
			return
		}
		weiter.ServeHTTP(w, r)
	})
}

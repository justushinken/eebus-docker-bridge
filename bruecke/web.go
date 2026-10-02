package main

import (
	_ "embed"
	"net/http"
	"time"

	"eebus-bruecke/internal/gemeinsam"
)

// Status-UI: eine eingebettete Seite, die /api/status jede Sekunde abfragt.
// Nur Anzeige, keine Aktionen.

//go:embed web/index.html
var indexHtml []byte

func webHandler(b *Bruecke, protokoll *gemeinsam.Ereignisprotokoll) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", gemeinsam.SeiteAusliefern(gemeinsam.MitStil(indexHtml)))
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		status := b.Status(time.Now())
		status.Ereignisse = protokoll.Liste()
		gemeinsam.SchreibeJson(w, http.StatusOK, status)
	})
	return mux
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

	EigenerSki string                       `json:"eigenerSki"`
	RemoteSki  string                       `json:"remoteSki"`
	Gefunden   []gemeinsam.GefundenesGeraet `json:"gefunden"`

	SpsOk                  bool     `json:"spsOk"`
	SpsLebenszeichenAlterS *float64 `json:"spsLebenszeichenAlterS"` // nil = nie
	NennleistungW          float64  `json:"nennleistungW"`

	Hersteller            string  `json:"hersteller"`
	Marke                 string  `json:"marke"`
	Modell                string  `json:"modell"`
	Seriennummer          string  `json:"seriennummer"`
	SchnittstellenVersion int     `json:"schnittstellenVersion"`
	LaufzeitS             float64 `json:"laufzeitS"`

	Ereignisse []gemeinsam.Ereignis `json:"ereignisse"`
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
		Gefunden:   gemeinsam.GefundeneGeraete(b.gefunden),

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

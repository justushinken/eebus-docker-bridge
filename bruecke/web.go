package main

import (
	_ "embed"
	"net/http"
	"time"

	"eebus-bruecke/internal/gemeinsam"
)

// Status-UI: eine eingebettete Seite, die /api/status jede Sekunde abfragt.
// Nur lesend, mit einer Ausnahme: Suchmodus und Kopplung mit der Steuerbox
// (abschaltbar mit WEB_KOPPLUNG=aus).

//go:embed web/index.html
var indexHtml []byte

//go:embed web/anleitung.html
var anleitungHtml []byte

func webHandler(b *Bruecke, protokoll *gemeinsam.Ereignisprotokoll) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", gemeinsam.SeiteAusliefern(gemeinsam.MitStil(indexHtml)))
	mux.HandleFunc("GET /anleitung", gemeinsam.SeiteAusliefern(gemeinsam.MitStil(anleitungHtml)))
	mux.HandleFunc("GET /api/qr.png", gemeinsam.QrBild(func() string { return b.kennung.QrText }))
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		status := b.Status(time.Now())
		status.Ereignisse = protokoll.Liste()
		gemeinsam.SchreibeJson(w, http.StatusOK, status)
	})

	// Die Konfiguration aendert sich nach dem Start nicht: ohne mu lesbar.
	mux.HandleFunc("GET /api/konfiguration", func(w http.ResponseWriter, r *http.Request) {
		gemeinsam.SchreibeJson(w, http.StatusOK, b.konf.Anzeige())
	})

	mux.HandleFunc("POST /api/suchmodus", gemeinsam.Aktion(func(daten struct {
		An bool `json:"an"`
	}) (string, error) {
		if err := b.SetzeSuchmodus(daten.An); err != nil {
			return "", err
		}
		if daten.An {
			return "Suchmodus laeuft 10 Minuten", nil
		}
		return "Suchmodus beendet", nil
	}))

	mux.HandleFunc("POST /api/kopplung", gemeinsam.Aktion(func(daten struct {
		Ski string `json:"ski"`
	}) (string, error) {
		return "Gekoppelt, die Bruecke verbindet sich, sobald auch die Steuerbox ihr vertraut", b.KoppelnPerSki(daten.Ski)
	}))

	mux.HandleFunc("POST /api/kopplungsanfrage", gemeinsam.Aktion(func(daten struct {
		Ski      string `json:"ski"`
		Annehmen bool   `json:"annehmen"`
	}) (string, error) {
		if err := b.BeantworteAnfrage(daten.Ski, daten.Annehmen); err != nil {
			return "", err
		}
		if daten.Annehmen {
			return "Angenommen, Verbindung wird aufgebaut", nil
		}
		return "Abgelehnt", nil
	}))

	mux.HandleFunc("DELETE /api/kopplung", gemeinsam.Aktion(func(struct{}) (string, error) {
		return "Kopplung aufgehoben", b.Entkoppeln()
	}))
	return mux
}

// BegrenzungStatus ist eine Richtung (Bezug oder Einspeisung) fuer das UI.
type BegrenzungStatus struct {
	Zustand         LpcZustand `json:"zustand"`
	ZustandText     string     `json:"zustandText"`
	ZustandSeitS    float64    `json:"zustandSeitS"`
	BegrenzungAktiv bool       `json:"begrenzungAktiv"`
	WirksameGrenzeW float64    `json:"wirksameGrenzeW"`
	RestdauerS      float64    `json:"restdauerS"` // 0 = unbefristet

	GrenzeNetzAktiv  bool     `json:"grenzeNetzAktiv"`
	GrenzeNetzW      float64  `json:"grenzeNetzW"`
	GrenzeNetzDauerS float64  `json:"grenzeNetzDauerS"`
	GrenzeNetzAlterS *float64 `json:"grenzeNetzAlterS"` // nil = nie empfangen

	FailsafeGrenzeW float64 `json:"failsafeGrenzeW"`
	NennleistungW   float64 `json:"nennleistungW"`
	Ablehnung       string  `json:"ablehnung"` // letzter Grund, aus dem eine Grenze abgelehnt wurde
}

type UseCaseStatus struct {
	Name      string `json:"name"`
	Lokal     bool   `json:"lokal"`
	Steuerbox bool   `json:"steuerbox"`
}

// StatusDaten ist ein konsistenter Schnappschuss fuer das UI. Zeitangaben
// als Alter in Sekunden, damit eine abweichende Uhr des Browsers nicht stoert.
type StatusDaten struct {
	Bezug       *BegrenzungStatus `json:"bezug"`       // nil = LPC aus
	Einspeisung *BegrenzungStatus `json:"einspeisung"` // nil = LPP aus

	Verbindung            Verbindung `json:"verbindung"`
	VerbindungText        string     `json:"verbindungText"`
	FailsafeMindestdauerS float64    `json:"failsafeMindestdauerS"`
	HeartbeatAlterS       *float64   `json:"heartbeatAlterS"` // nil = nie

	UseCases    []UseCaseStatus `json:"useCases"`
	Mpc         []Messwert      `json:"mpc"`  // nil = MPC aus
	Mgcp        []Messwert      `json:"mgcp"` // nil = MGCP aus
	Gegenstelle *Gegenstelle    `json:"gegenstelle"`

	EigenerSki string                       `json:"eigenerSki"`
	Gefunden   []gemeinsam.GefundenesGeraet `json:"gefunden"`

	// Kopplung
	Kennung           Kennung        `json:"kennung"`
	PairingService    bool           `json:"pairingService"` // Pairing Service aktiv
	Kopplung          *KopplungDaten `json:"kopplung"`       // nil = keine Steuerbox gekoppelt
	KopplungAenderbar bool           `json:"kopplungAenderbar"`
	SuchmodusRestS    float64        `json:"suchmodusRestS"` // 0 = aus
	Anfragen          []AnfrageDaten `json:"anfragen"`
	Partner           string         `json:"partner"` // verbundene Steuerbox, leer = keine

	SpsOk                  bool     `json:"spsOk"`
	SpsLebenszeichenAlterS *float64 `json:"spsLebenszeichenAlterS"` // nil = nie
	Anlagenstatus          string   `json:"anlagenstatus"`

	Hersteller            string  `json:"hersteller"`
	Marke                 string  `json:"marke"`
	Modell                string  `json:"modell"`
	Seriennummer          string  `json:"seriennummer"`
	Version               string  `json:"version"`
	SchnittstellenVersion int     `json:"schnittstellenVersion"`
	ErweiterungsVersion   int     `json:"erweiterungsVersion"`
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

// begrenzungStatus: Aufruf unter mu. nil, wenn die Richtung aus ist.
func begrenzungStatus(r *Begrenzung, jetzt time.Time) *BegrenzungStatus {
	if r == nil {
		return nil
	}
	aktiv, grenzeW := r.wirksameGrenze()
	return &BegrenzungStatus{
		Zustand:         r.zustand,
		ZustandText:     r.zustand.String(),
		ZustandSeitS:    jetzt.Sub(r.zustandSeit).Seconds(),
		BegrenzungAktiv: aktiv,
		WirksameGrenzeW: grenzeW,
		RestdauerS:      r.restdauer(jetzt),

		GrenzeNetzAktiv:  r.grenze.IsActive,
		GrenzeNetzW:      r.grenze.Value,
		GrenzeNetzDauerS: r.grenze.Duration.Seconds(),
		GrenzeNetzAlterS: alterS(jetzt, r.letzteGrenzeEmpfangen),

		FailsafeGrenzeW: r.failsafeGrenzeW,
		NennleistungW:   r.nennleistungW,
		Ablehnung:       r.ablehnung,
	}
}

// Status erstellt das Abbild fuer das UI. Grenze und Restdauer wie in
// inputRegister(), damit UI und Modbus dasselbe zeigen.
func (b *Bruecke) Status(jetzt time.Time) StatusDaten {
	b.mu.Lock()
	defer b.mu.Unlock()

	s := StatusDaten{
		Verbindung:            b.verbindung,
		VerbindungText:        b.verbindung.String(),
		FailsafeMindestdauerS: b.failsafeMindestdauer.Seconds(),
		HeartbeatAlterS:       alterS(jetzt, b.letzterHeartbeat),
		Gegenstelle:           b.gegenstelle,

		EigenerSki: b.eigenerSki,
		Gefunden:   gemeinsam.GefundeneGeraete(b.gefunden),

		Kennung:           b.kennung,
		PairingService:    b.konf.PairingService,
		KopplungAenderbar: b.pruefeKopplungAenderbar() == nil,

		SpsOk:                  b.spsOk,
		SpsLebenszeichenAlterS: alterS(jetzt, b.spsLebenszeichenSeit),
		Anlagenstatus:          zustandTexte[betriebszustand(b.spsOk, b.holding[regAnlagenstatus])],

		Hersteller:            b.konf.Hersteller,
		Marke:                 b.konf.Marke,
		Modell:                b.konf.Modell,
		Seriennummer:          b.konf.Seriennummer,
		Version:               Version,
		SchnittstellenVersion: SchnittstellenVersion,
		ErweiterungsVersion:   ErweiterungsVersion,
		LaufzeitS:             jetzt.Sub(b.gestartet).Seconds(),
	}
	s.Bezug = begrenzungStatus(b.bezug, jetzt)
	s.Einspeisung = begrenzungStatus(b.einspeisung, jetzt)
	s.Kopplung, s.Anfragen, s.SuchmodusRestS = b.kopplungStatus(jetzt)
	if !b.partner.IsZero() {
		s.Partner = gemeinsam.Bezeichnung(b.partner)
	}

	lokal := b.useCasesLokal()
	for _, uc := range steuerboxUseCases {
		s.UseCases = append(s.UseCases, UseCaseStatus{
			Name: uc.kurz, Lokal: lokal&uc.bit != 0, Steuerbox: b.steuerboxUseCases&uc.bit != 0,
		})
	}
	s.Mpc, s.Mgcp = b.messwerteUi(b.mpcGroessen), b.messwerteUi(b.mgcpGroessen)
	return s
}

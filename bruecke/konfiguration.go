package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/spine-go/model"
)

// Konfiguration kommt aus Umgebungsvariablen (docker run -e ...). Im UI
// geaenderte Werte (einstellungen.json) haben Vorrang, siehe einstellungen.go.
type Konfiguration struct {
	EebusPort            int
	ModbusUrl            string
	Datenverzeichnis     string
	RemoteSki            string // SKI-Verfahren: SKI der Steuerbox, leer = aus
	PairingService       bool   // SHIP Pairing Service: Steuerbox mit Secret automatisch vertrauen
	ShipId               string
	Hersteller           string
	Marke                string
	Modell               string
	Seriennummer         string
	HwRevision           string
	FailsafeGrenzeW      float64
	FailsafeMindestdauer time.Duration
	NennleistungMaxW     float64
	WebAdresse           string
	WebBenutzer          string
	WebPasswort          string
	WebAenderungen       bool // Kopplung, Einstellungen und Neustart im UI erlaubt
	EebusDebug           bool

	// Use Cases (EEBUS_USECASES)
	Lpc, Lpp, Mpc, Mgcp bool

	// LPP
	FailsafeEinspeisegrenzeW  float64
	NennleistungErzeugungMaxW float64

	// MPC und MGCP
	MpcEntitaet    string // cem oder submeter
	MesswertQuelle model.MeasurementValueSourceType
}

// auswahl liest eine kommagetrennte Liste aus erlaubten Namen.
func auswahl(u gemeinsam.Umgebung, name, vorgabe string, erlaubt []string) (map[string]bool, error) {
	ergebnis := map[string]bool{}
	for _, teil := range strings.Split(u.TextLeerErlaubt(name, vorgabe), ",") {
		teil = strings.ToLower(strings.TrimSpace(teil))
		if teil == "" {
			continue
		}
		if !slices.Contains(erlaubt, teil) {
			return nil, fmt.Errorf("%s: unbekannter Wert %q, erlaubt: %s", name, teil, strings.Join(erlaubt, ", "))
		}
		ergebnis[teil] = true
	}
	return ergebnis, nil
}

func leseKonfiguration(u gemeinsam.Umgebung) (Konfiguration, error) {
	konf := Konfiguration{
		EebusPort:            u.Ganzzahl("EEBUS_PORT", 4712),
		ModbusUrl:            u.Text("MODBUS_URL", "tcp://127.0.0.1:5502"),
		Datenverzeichnis:     u.Text("DATENVERZEICHNIS", "/data"),
		RemoteSki:            u.Text("EEBUS_REMOTE_SKI", ""),
		PairingService:       u.Text("EEBUS_PAIRING_SERVICE", "an") != "aus",
		Hersteller:           u.Text("GERAET_HERSTELLER", "Demo"),
		Marke:                u.Text("GERAET_MARKE", "Demo"),
		Modell:               u.Text("GERAET_MODELL", "PFC200-LPC-Bruecke"),
		Seriennummer:         u.Text("GERAET_SERIENNUMMER", ""),
		HwRevision:           u.Text("GERAET_HW_REVISION", ""),
		FailsafeGrenzeW:      u.Kommazahl("FAILSAFE_GRENZE_W", 4200),
		FailsafeMindestdauer: u.Dauer("FAILSAFE_MINDESTDAUER", 2*time.Hour),
		NennleistungMaxW:     u.Kommazahl("NENNLEISTUNG_MAX_W", 11000),
		WebAdresse:           u.TextLeerErlaubt("WEB_ADRESSE", ":8090"),
		WebBenutzer:          u.Text("WEB_BENUTZER", "admin"),
		WebPasswort:          u.Text("WEB_PASSWORT", ""),
		WebAenderungen:       u.Text("WEB_AENDERUNGEN", "an") != "aus",
		EebusDebug:           u.Text("EEBUS_DEBUG", "aus") == "an",
		MpcEntitaet:          u.Text("MPC_ENTITAET", "cem"),
		MesswertQuelle:       model.MeasurementValueSourceType(u.Text("MESSWERT_QUELLE", "measuredValue")),
	}

	// Fester SKI: genau diese Steuerbox, kein Pairing Service und keine
	// Kopplung im UI. So ist nie mehr als eine Steuerbox vertraut.
	if konf.RemoteSki != "" {
		ski, err := gemeinsam.NormalisiereSki(konf.RemoteSki)
		if err != nil {
			return konf, fmt.Errorf("EEBUS_REMOTE_SKI: %w", err)
		}
		konf.RemoteSki, konf.PairingService = ski, false
	}

	// Ohne Seriennummer die MAC-Adresse des PFC: weltweit eindeutig und auf
	// dem Typenschild ablesbar. Daraus ergibt sich auch die SHIP-ID.
	if konf.Seriennummer == "" {
		if konf.Seriennummer = gemeinsam.GeraeteKennung(); konf.Seriennummer == "" {
			return konf, fmt.Errorf("keine MAC-Adresse gefunden: GERAET_SERIENNUMMER setzen")
		}
	}
	konf.ShipId = u.Text("SHIP_ID", fmt.Sprintf("%s-%s-%s", konf.Marke, konf.Modell, konf.Seriennummer))
	if err := gemeinsam.PruefeShipId(konf.ShipId); err != nil {
		return konf, err
	}

	useCases, err := auswahl(u, "EEBUS_USECASES", "lpc", []string{"lpc", "lpp", "mpc", "mgcp"})
	if err != nil {
		return konf, err
	}
	konf.Lpc, konf.Lpp, konf.Mpc, konf.Mgcp = useCases["lpc"], useCases["lpp"], useCases["mpc"], useCases["mgcp"]
	if !konf.Lpc && !konf.Lpp {
		// Heartbeat und Failsafe haengen an LPC bzw. LPP.
		return konf, fmt.Errorf("EEBUS_USECASES: lpc oder lpp ist Pflicht")
	}

	// Auch ohne LPP lesen, damit das UI die Werte vor dem Einschalten zeigt.
	konf.FailsafeEinspeisegrenzeW = u.Kommazahl("FAILSAFE_EINSPEISEGRENZE_W", 0)
	konf.NennleistungErzeugungMaxW = u.Kommazahl("NENNLEISTUNG_ERZEUGUNG_MAX_W", 0)
	if konf.Lpp {
		// Keine stillschweigende Vorgabe: Die Einspeisegrenze im Failsafe haengt
		// von der Anlage ab (z. B. 60 % nach Netzanschlussvertrag).
		for _, name := range []string{"FAILSAFE_EINSPEISEGRENZE_W", "NENNLEISTUNG_ERZEUGUNG_MAX_W"} {
			if u.Text(name, "") == "" {
				return konf, fmt.Errorf("%s ist Pflicht, wenn lpp in EEBUS_USECASES steht", name)
			}
		}
	}

	if konf.Mpc && konf.MpcEntitaet != "cem" && konf.MpcEntitaet != "submeter" {
		return konf, fmt.Errorf("MPC_ENTITAET: cem oder submeter erwartet, nicht %q", konf.MpcEntitaet)
	}
	switch konf.MesswertQuelle {
	case model.MeasurementValueSourceTypeMeasuredValue, model.MeasurementValueSourceTypeCalculatedValue, model.MeasurementValueSourceTypeEmpiricalValue:
	default:
		return konf, fmt.Errorf("MESSWERT_QUELLE: measuredValue, calculatedValue oder empiricalValue erwartet")
	}
	return konf, nil
}

// --- Anzeige im UI (Tab "Konfiguration") ---

// KonfigEintrag ist eine Einstellung mit der Env-Variable, ueber die sie sich
// aendern laesst. Das Web-Passwort wird nicht angezeigt. Im UI aenderbare
// Eintraege haben zusaetzlich Eingabe, Rohwert (im Format der Env) und Quelle.
type KonfigEintrag struct {
	Name     string   `json:"name"`
	Wert     string   `json:"wert"`
	Variable string   `json:"variable"`
	Eingabe  *Eingabe `json:"eingabe,omitempty"`
	Roh      string   `json:"roh"`
	Quelle   string   `json:"quelle,omitempty"` // ui, env oder vorgabe
}

type KonfigGruppe struct {
	Name      string          `json:"name"`
	Eintraege []KonfigEintrag `json:"eintraege"`
}

// Anzeige liefert die Konfiguration fuer das UI, ohne die Failsafe-Werte der
// Steuerbox (failsafe.json): Die stehen in der Uebersicht.
func (k Konfiguration) Anzeige() []KonfigGruppe {
	anAus := func(b bool) string {
		if b {
			return "an"
		}
		return "aus"
	}
	text := func(s string) string {
		if s == "" {
			return "–"
		}
		return s
	}
	watt := func(w float64) string { return fmt.Sprintf("%.0f W", w) }
	nurMitLpp := func(w float64) string {
		if !k.Lpp {
			return "– (LPP aus)"
		}
		return watt(w)
	}

	return []KonfigGruppe{
		{"Use Cases", []KonfigEintrag{
			{Name: "Angeboten", Wert: useCaseListe(k), Variable: "EEBUS_USECASES"},
			{Name: "MPC auf Entität", Wert: k.MpcEntitaet, Variable: "MPC_ENTITAET"},
			{Name: "Herkunft der Messwerte", Wert: string(k.MesswertQuelle), Variable: "MESSWERT_QUELLE"},
		}},
		{"Grenzen beim Start", []KonfigEintrag{
			{Name: "Nennleistung Bezug", Wert: watt(k.NennleistungMaxW), Variable: "NENNLEISTUNG_MAX_W"},
			{Name: "Nennleistung Erzeugung", Wert: nurMitLpp(k.NennleistungErzeugungMaxW), Variable: "NENNLEISTUNG_ERZEUGUNG_MAX_W"},
			{Name: "Failsafe-Grenze Bezug", Wert: watt(k.FailsafeGrenzeW), Variable: "FAILSAFE_GRENZE_W"},
			{Name: "Failsafe-Grenze Einspeisung", Wert: nurMitLpp(k.FailsafeEinspeisegrenzeW), Variable: "FAILSAFE_EINSPEISEGRENZE_W"},
			{Name: "Failsafe-Mindestdauer", Wert: stundenMinuten(k.FailsafeMindestdauer), Variable: "FAILSAFE_MINDESTDAUER"},
		}},
		{"Kopplung", []KonfigEintrag{
			{Name: "SHIP Pairing Service", Wert: anAus(k.PairingService), Variable: "EEBUS_PAIRING_SERVICE"},
			{Name: "Steuerbox fest (SKI)", Wert: text(k.RemoteSki), Variable: "EEBUS_REMOTE_SKI"},
			{Name: "Änderungen im UI", Wert: anAus(k.WebAenderungen), Variable: "WEB_AENDERUNGEN"},
			{Name: "SHIP-ID", Wert: k.ShipId, Variable: "SHIP_ID"},
		}},
		{"Gerät", []KonfigEintrag{
			{Name: "Hersteller", Wert: k.Hersteller, Variable: "GERAET_HERSTELLER"},
			{Name: "Marke", Wert: k.Marke, Variable: "GERAET_MARKE"},
			{Name: "Modell", Wert: k.Modell, Variable: "GERAET_MODELL"},
			{Name: "Seriennummer", Wert: k.Seriennummer, Variable: "GERAET_SERIENNUMMER"},
			{Name: "Hardware-Revision", Wert: text(k.HwRevision), Variable: "GERAET_HW_REVISION"},
			{Name: "Software-Version", Wert: Version, Variable: "Image-Version"},
		}},
		{"Schnittstellen", []KonfigEintrag{
			{Name: "EEBUS-Port", Wert: fmt.Sprint(k.EebusPort), Variable: "EEBUS_PORT"},
			{Name: "Modbus-Server", Wert: k.ModbusUrl, Variable: "MODBUS_URL"},
			{Name: "Datenverzeichnis", Wert: k.Datenverzeichnis, Variable: "DATENVERZEICHNIS"},
			{Name: "Web-UI", Wert: k.WebAdresse, Variable: "WEB_ADRESSE"},
			{Name: "Web-Benutzer", Wert: k.WebBenutzer, Variable: "WEB_BENUTZER"},
			{Name: "EEBUS-Protokoll auf stdout", Wert: anAus(k.EebusDebug), Variable: "EEBUS_DEBUG"},
		}},
	}
}

func stundenMinuten(d time.Duration) string {
	minuten := int(d.Round(time.Minute).Minutes())
	if minuten%60 == 0 {
		return fmt.Sprintf("%d h", minuten/60)
	}
	return fmt.Sprintf("%d h %d min", minuten/60, minuten%60)
}

package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/spine-go/model"
)

// Konfiguration kommt vollstaendig aus Umgebungsvariablen (docker run -e ...).
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
	WebKopplung          bool // Kopplung und Suchmodus im UI erlaubt
	EebusDebug           bool

	// Use Cases (EEBUS_USECASES)
	Lpc, Lpp, Mpc, Mgcp bool

	// LPP
	FailsafeEinspeisegrenzeW  float64
	NennleistungErzeugungMaxW float64

	// MPC und MGCP
	MpcEntitaet    string // cem oder submeter
	MpcPhasenText  string // z. B. "abc"
	MpcPhasen      []model.ElectricalConnectionPhaseNameType
	MpcMesswerte   map[string]bool
	MgcpMesswerte  map[string]bool
	MesswertQuelle model.MeasurementValueSourceType
}

// auswahl liest eine kommagetrennte Liste aus erlaubten Namen.
func auswahl(name, vorgabe string, erlaubt []string) (map[string]bool, error) {
	ergebnis := map[string]bool{}
	for _, teil := range strings.Split(gemeinsam.EnvTextLeerErlaubt(name, vorgabe), ",") {
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

func leseKonfiguration() (Konfiguration, error) {
	konf := Konfiguration{
		EebusPort:            gemeinsam.EnvGanzzahl("EEBUS_PORT", 4712),
		ModbusUrl:            gemeinsam.EnvText("MODBUS_URL", "tcp://127.0.0.1:5502"),
		Datenverzeichnis:     gemeinsam.EnvText("DATENVERZEICHNIS", "/data"),
		RemoteSki:            gemeinsam.EnvText("EEBUS_REMOTE_SKI", ""),
		PairingService:       gemeinsam.EnvText("EEBUS_PAIRING_SERVICE", "an") != "aus",
		Hersteller:           gemeinsam.EnvText("GERAET_HERSTELLER", "Demo"),
		Marke:                gemeinsam.EnvText("GERAET_MARKE", "Demo"),
		Modell:               gemeinsam.EnvText("GERAET_MODELL", "PFC200-LPC-Bruecke"),
		Seriennummer:         gemeinsam.EnvText("GERAET_SERIENNUMMER", ""),
		HwRevision:           gemeinsam.EnvText("GERAET_HW_REVISION", ""),
		FailsafeGrenzeW:      gemeinsam.EnvKommazahl("FAILSAFE_GRENZE_W", 4200),
		FailsafeMindestdauer: gemeinsam.EnvDauer("FAILSAFE_MINDESTDAUER", 2*time.Hour),
		NennleistungMaxW:     gemeinsam.EnvKommazahl("NENNLEISTUNG_MAX_W", 11000),
		WebAdresse:           gemeinsam.EnvTextLeerErlaubt("WEB_ADRESSE", ":8090"),
		WebBenutzer:          gemeinsam.EnvText("WEB_BENUTZER", "admin"),
		WebPasswort:          gemeinsam.EnvText("WEB_PASSWORT", ""),
		WebKopplung:          gemeinsam.EnvText("WEB_KOPPLUNG", "an") != "aus",
		EebusDebug:           gemeinsam.EnvText("EEBUS_DEBUG", "aus") == "an",
		MpcEntitaet:          gemeinsam.EnvText("MPC_ENTITAET", "cem"),
		MpcPhasenText:        strings.ToLower(gemeinsam.EnvText("MPC_PHASEN", "abc")),
		MesswertQuelle:       model.MeasurementValueSourceType(gemeinsam.EnvText("MESSWERT_QUELLE", "measuredValue")),
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
	konf.ShipId = gemeinsam.EnvText("SHIP_ID", fmt.Sprintf("%s-%s-%s", konf.Marke, konf.Modell, konf.Seriennummer))
	if err := gemeinsam.PruefeShipId(konf.ShipId); err != nil {
		return konf, err
	}

	useCases, err := auswahl("EEBUS_USECASES", "lpc", []string{"lpc", "lpp", "mpc", "mgcp"})
	if err != nil {
		return konf, err
	}
	konf.Lpc, konf.Lpp, konf.Mpc, konf.Mgcp = useCases["lpc"], useCases["lpp"], useCases["mpc"], useCases["mgcp"]
	if !konf.Lpc && !konf.Lpp {
		// Heartbeat und Failsafe haengen an LPC bzw. LPP.
		return konf, fmt.Errorf("EEBUS_USECASES: lpc oder lpp ist Pflicht")
	}

	if konf.Lpp {
		// Keine stillschweigende Vorgabe: Die Einspeisegrenze im Failsafe haengt
		// von der Anlage ab (z. B. 60 % nach Netzanschlussvertrag).
		for _, name := range []string{"FAILSAFE_EINSPEISEGRENZE_W", "NENNLEISTUNG_ERZEUGUNG_MAX_W"} {
			if gemeinsam.EnvText(name, "") == "" {
				return konf, fmt.Errorf("%s ist Pflicht, wenn lpp in EEBUS_USECASES steht", name)
			}
		}
		konf.FailsafeEinspeisegrenzeW = gemeinsam.EnvKommazahl("FAILSAFE_EINSPEISEGRENZE_W", 0)
		konf.NennleistungErzeugungMaxW = gemeinsam.EnvKommazahl("NENNLEISTUNG_ERZEUGUNG_MAX_W", 0)
	}

	if konf.Mpc {
		if konf.MpcEntitaet != "cem" && konf.MpcEntitaet != "submeter" {
			return konf, fmt.Errorf("MPC_ENTITAET: cem oder submeter erwartet, nicht %q", konf.MpcEntitaet)
		}
		if !slices.Contains([]string{"a", "b", "c", "ab", "bc", "ac", "abc"}, konf.MpcPhasenText) {
			return konf, fmt.Errorf("MPC_PHASEN: z. B. abc oder a erwartet, nicht %q", konf.MpcPhasenText)
		}
		for _, p := range konf.MpcPhasenText {
			konf.MpcPhasen = append(konf.MpcPhasen, model.ElectricalConnectionPhaseNameType(string(p)))
		}
		if konf.MpcMesswerte, err = auswahl("MPC_MESSWERTE", strings.Join(mpcMesswerteErlaubt, ","), mpcMesswerteErlaubt); err != nil {
			return konf, err
		}
	}
	if konf.Mgcp {
		if konf.MgcpMesswerte, err = auswahl("MGCP_MESSWERTE", "strom,spannung,frequenz", mgcpMesswerteErlaubt); err != nil {
			return konf, err
		}
	}
	switch konf.MesswertQuelle {
	case model.MeasurementValueSourceTypeMeasuredValue, model.MeasurementValueSourceTypeCalculatedValue, model.MeasurementValueSourceTypeEmpiricalValue:
	default:
		return konf, fmt.Errorf("MESSWERT_QUELLE: measuredValue, calculatedValue oder empiricalValue erwartet")
	}
	return konf, nil
}

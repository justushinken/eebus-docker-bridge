package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"eebus-bruecke/internal/gemeinsam"
)

// Einstellungen aus dem UI: einstellungen.json im Datenverzeichnis enthaelt
// geaenderte Umgebungsvariablen und hat Vorrang vor der Env des Containers.
// Wirksam werden sie mit einem Neustart der Bruecke, den das UI ausloest.

const einstellungenDatei = "einstellungen.json"

// Eingabe beschreibt, wie das UI eine aenderbare Variable abfragt.
type Eingabe struct {
	Art      string   `json:"art"` // zahl (W), stunden, text, auswahl, liste, anAus
	Optionen []string `json:"optionen,omitempty"`
}

// einstellbar sind nur Werte, die die Anlage beschreiben. Nicht dabei: Ports,
// Adressen, Datenverzeichnis und Web-Zugang (sonst sperrt man sich aus), die
// Seriennummer und der feste SKI (Kopplung im Reiter "Kopplung").
var einstellbar = map[string]Eingabe{
	"EEBUS_USECASES":               {"liste", []string{"lpc", "lpp", "mpc", "mgcp"}},
	"MPC_ENTITAET":                 {"auswahl", []string{"cem", "submeter"}},
	"MESSWERT_QUELLE":              {"auswahl", []string{"measuredValue", "calculatedValue", "empiricalValue"}},
	"NENNLEISTUNG_MAX_W":           {Art: "zahl"},
	"NENNLEISTUNG_ERZEUGUNG_MAX_W": {Art: "zahl"},
	"FAILSAFE_GRENZE_W":            {Art: "zahl"},
	"FAILSAFE_EINSPEISEGRENZE_W":   {Art: "zahl"},
	"FAILSAFE_MINDESTDAUER":        {Art: "stunden"},
	"EEBUS_PAIRING_SERVICE":        {Art: "anAus"},
	"SHIP_ID":                      {Art: "text"},
	"GERAET_HERSTELLER":            {Art: "text"},
	"GERAET_MARKE":                 {Art: "text"},
	"GERAET_MODELL":                {Art: "text"},
	"GERAET_HW_REVISION":           {Art: "text"},
	"EEBUS_DEBUG":                  {Art: "anAus"},
}

var einstellungenMu sync.Mutex // serialisiert Lesen und Schreiben der Datei

// ladeEinstellungen liest die im UI geaenderten Werte. Fehlt die Datei, gibt
// es keine. Unbekannte Variablen werden ignoriert.
func ladeEinstellungen(verzeichnis string) (map[string]string, error) {
	werte := map[string]string{}
	inhalt, err := os.ReadFile(filepath.Join(verzeichnis, einstellungenDatei))
	if errors.Is(err, fs.ErrNotExist) {
		return werte, nil
	}
	if err != nil {
		return werte, err
	}
	if err := json.Unmarshal(inhalt, &werte); err != nil {
		return map[string]string{}, err
	}
	maps.DeleteFunc(werte, func(name, _ string) bool { _, ok := einstellbar[name]; return !ok })
	return werte, nil
}

// mitEinstellungen legt die Werte aus dem UI vor die Env.
func mitEinstellungen(werte map[string]string) gemeinsam.Umgebung {
	return func(name string) (string, bool) {
		if wert, ok := werte[name]; ok {
			return wert, true
		}
		return os.LookupEnv(name)
	}
}

// startKonfiguration liest die Konfiguration beim Start. Passen die Werte aus
// dem UI nicht (mehr), z. B. weil sich die Env geaendert hat, startet die
// Bruecke mit der Env allein, statt gar nicht.
func startKonfiguration() (Konfiguration, map[string]string, error) {
	verzeichnis := gemeinsam.EnvText("DATENVERZEICHNIS", "/data")
	werte, err := ladeEinstellungen(verzeichnis)
	if err != nil {
		log.Printf("%s nicht lesbar, gilt nur die Env: %v", einstellungenDatei, err)
	}
	konf, err := leseKonfiguration(mitEinstellungen(werte))
	if err != nil && len(werte) > 0 {
		log.Printf("Einstellungen aus dem UI ungueltig (%v), gilt nur die Env", err)
		werte = map[string]string{}
		konf, err = leseKonfiguration(gemeinsam.Env)
	}
	if len(werte) > 0 {
		log.Printf("Im UI geaendert: %s", strings.Join(slices.Sorted(maps.Keys(werte)), ", "))
	}
	return konf, werte, err
}

// pruefeEingabe prueft einen einzelnen Wert streng. Die Env-Hilfen nehmen bei
// ungueltigen Werten stillschweigend die Vorgabe, das soll im UI nicht passieren.
func pruefeEingabe(name, wert string) error {
	eingabe := einstellbar[name]
	switch eingabe.Art {
	case "zahl":
		w, err := strconv.ParseFloat(wert, 64)
		if err != nil || math.IsNaN(w) || w < 0 || w > 1e7 {
			return fmt.Errorf("%s: Leistung in W zwischen 0 und 10 000 000 erwartet", name)
		}
	case "stunden":
		d, err := time.ParseDuration(wert)
		if err != nil || d < 2*time.Hour || d > 24*time.Hour {
			return fmt.Errorf("%s: zwischen 2 und 24 Stunden erwartet", name)
		}
	case "liste":
		if wert == "" {
			break // leer prueft leseKonfiguration (lpc oder lpp ist Pflicht)
		}
		for _, teil := range strings.Split(wert, ",") {
			if !slices.Contains(eingabe.Optionen, teil) {
				return fmt.Errorf("%s: unbekannter Wert %q", name, teil)
			}
		}
	case "auswahl":
		if !slices.Contains(eingabe.Optionen, wert) {
			return fmt.Errorf("%s: %s erwartet", name, strings.Join(eingabe.Optionen, " oder "))
		}
	case "anAus":
		if wert != "an" && wert != "aus" {
			return fmt.Errorf("%s: an oder aus erwartet", name)
		}
	case "text":
		if len(wert) > 63 || strings.ContainsFunc(wert, unicode.IsControl) {
			return fmt.Errorf("%s: hoechstens 63 Zeichen, keine Steuerzeichen", name)
		}
	}
	return nil
}

// AendereEinstellungen uebernimmt Aenderungen aus dem UI (nil = zuruecksetzen
// auf Env bzw. Vorgabe), prueft die ganze Konfiguration und speichert. Wirksam
// nach dem naechsten Neustart.
func (b *Bruecke) AendereEinstellungen(aenderungen map[string]*string) error {
	if !b.konf.WebAenderungen {
		return errUiNurLesend
	}
	einstellungenMu.Lock()
	defer einstellungenMu.Unlock()

	werte, err := ladeEinstellungen(b.konf.Datenverzeichnis)
	if err != nil {
		return err
	}
	for name, wert := range aenderungen {
		if _, ok := einstellbar[name]; !ok {
			return fmt.Errorf("%s laesst sich im UI nicht aendern", name)
		}
		if wert == nil {
			delete(werte, name)
			continue
		}
		if err := pruefeEingabe(name, strings.TrimSpace(*wert)); err != nil {
			return err
		}
		werte[name] = strings.TrimSpace(*wert)
	}
	// Abhaengigkeiten (lpp braucht Werte, SHIP-ID-Format) wie beim Start pruefen
	if _, err := leseKonfiguration(mitEinstellungen(werte)); err != nil {
		return err
	}
	inhalt, err := json.MarshalIndent(werte, "", "  ")
	if err != nil {
		return err
	}
	if err := gemeinsam.SchreibeDatei(filepath.Join(b.konf.Datenverzeichnis, einstellungenDatei), inhalt); err != nil {
		return err
	}
	log.Printf("Einstellungen geaendert: %s (wirksam nach Neustart)", strings.Join(slices.Sorted(maps.Keys(aenderungen)), ", "))
	return nil
}

// KonfigAntwort ist der Reiter "Konfiguration": die Werte fuer den naechsten
// Start, also mit allen gespeicherten Aenderungen.
type KonfigAntwort struct {
	Gruppen        []KonfigGruppe `json:"gruppen"`
	Aenderbar      bool           `json:"aenderbar"`
	NeustartNoetig bool           `json:"neustartNoetig"` // gespeicherte Aenderungen noch nicht wirksam
}

func (b *Bruecke) KonfigurationAnzeige() (KonfigAntwort, error) {
	einstellungenMu.Lock()
	werte, err := ladeEinstellungen(b.konf.Datenverzeichnis)
	einstellungenMu.Unlock()
	if err != nil {
		return KonfigAntwort{}, err
	}
	u := mitEinstellungen(werte)
	naechste, err := leseKonfiguration(u)
	if err != nil {
		return KonfigAntwort{}, err
	}

	gruppen := naechste.Anzeige()
	for _, gruppe := range gruppen {
		for i := range gruppe.Eintraege {
			e := &gruppe.Eintraege[i]
			eingabe, ok := einstellbar[e.Variable]
			if !ok {
				continue
			}
			e.Eingabe, e.Roh = &eingabe, naechste.rohwert(u, e.Variable)
			_, imUi := werte[e.Variable]
			_, inEnv := os.LookupEnv(e.Variable)
			switch {
			case imUi:
				e.Quelle = "ui"
			case inEnv:
				e.Quelle = "env"
			default:
				e.Quelle = "vorgabe"
			}
		}
	}
	return KonfigAntwort{
		Gruppen:        gruppen,
		Aenderbar:      b.konf.WebAenderungen,
		NeustartNoetig: !maps.Equal(werte, b.einstellungen),
	}, nil
}

// rohwert liefert den Wert einer aenderbaren Variable im Format der Env, so
// wie das UI ihn zurueckschickt.
func (k Konfiguration) rohwert(u gemeinsam.Umgebung, name string) string {
	zahl := func(w float64) string { return strconv.FormatFloat(w, 'f', -1, 64) }
	anAus := func(b bool) string {
		if b {
			return "an"
		}
		return "aus"
	}
	switch name {
	case "EEBUS_USECASES":
		return strings.ToLower(strings.ReplaceAll(useCaseListe(k), ", ", ","))
	case "MPC_ENTITAET":
		return k.MpcEntitaet
	case "MESSWERT_QUELLE":
		return string(k.MesswertQuelle)
	case "NENNLEISTUNG_MAX_W":
		return zahl(k.NennleistungMaxW)
	case "FAILSAFE_GRENZE_W":
		return zahl(k.FailsafeGrenzeW)
	case "NENNLEISTUNG_ERZEUGUNG_MAX_W", "FAILSAFE_EINSPEISEGRENZE_W":
		return u.Text(name, "") // ohne Vorgabe, leer bis gesetzt
	case "FAILSAFE_MINDESTDAUER":
		return zahl(k.FailsafeMindestdauer.Hours()) + "h"
	case "EEBUS_PAIRING_SERVICE":
		// nicht k.PairingService: Ein fester SKI schaltet ihn zusaetzlich ab
		return anAus(u.Text(name, "an") != "aus")
	case "EEBUS_DEBUG":
		return anAus(k.EebusDebug)
	case "SHIP_ID":
		return k.ShipId
	case "GERAET_HERSTELLER":
		return k.Hersteller
	case "GERAET_MARKE":
		return k.Marke
	case "GERAET_MODELL":
		return k.Modell
	case "GERAET_HW_REVISION":
		return k.HwRevision
	}
	return ""
}

// FordereNeustartAn startet die Bruecke neu, damit geaenderte Einstellungen
// wirken. Der Hauptablauf beendet dazu alles und ersetzt sich selbst.
func (b *Bruecke) FordereNeustartAn() error {
	if !b.konf.WebAenderungen {
		return errUiNurLesend
	}
	select {
	case b.neustart <- struct{}{}:
	default: // laeuft schon
	}
	return nil
}

// starteNeu ersetzt den Prozess durch einen neuen Start desselben Programms.
// Das geht ohne Docker: Der Container laeuft weiter, die Restart-Policy ist
// egal. Schlaegt es fehl, beendet sich die Bruecke, Docker startet sie dann
// mit --restart unless-stopped neu.
func starteNeu() {
	programm, err := os.Executable()
	if err == nil {
		err = syscall.Exec(programm, os.Args, os.Environ())
	}
	log.Fatalf("Neustart fehlgeschlagen: %v", err)
}

package gemeinsam

import (
	"os"
	"strconv"
	"time"
)

// Hilfsfunktionen fuer Umgebungsvariablen (docker run -e ...).
// Nicht gesetzte, leere oder ungueltige Werte ergeben die Vorgabe.

// Umgebung liefert eine Einstellung nach Namen. Normalerweise os.LookupEnv,
// die Bruecke legt ihre im UI geaenderten Einstellungen davor.
type Umgebung func(name string) (string, bool)

var Env Umgebung = os.LookupEnv

func (u Umgebung) Text(name, vorgabe string) string {
	if wert, ok := u(name); ok && wert != "" {
		return wert
	}
	return vorgabe
}

// Wie Text, aber ein gesetzter leerer Wert bleibt leer (z. B. WEB_ADRESSE= schaltet ab).
func (u Umgebung) TextLeerErlaubt(name, vorgabe string) string {
	if wert, ok := u(name); ok {
		return wert
	}
	return vorgabe
}

func (u Umgebung) Ganzzahl(name string, vorgabe int) int {
	if wert, err := strconv.Atoi(u.Text(name, "")); err == nil {
		return wert
	}
	return vorgabe
}

func (u Umgebung) Kommazahl(name string, vorgabe float64) float64 {
	if wert, err := strconv.ParseFloat(u.Text(name, ""), 64); err == nil {
		return wert
	}
	return vorgabe
}

func (u Umgebung) Dauer(name string, vorgabe time.Duration) time.Duration {
	if wert, err := time.ParseDuration(u.Text(name, "")); err == nil {
		return wert
	}
	return vorgabe
}

func EnvText(name, vorgabe string) string            { return Env.Text(name, vorgabe) }
func EnvTextLeerErlaubt(name, vorgabe string) string { return Env.TextLeerErlaubt(name, vorgabe) }
func EnvGanzzahl(name string, vorgabe int) int       { return Env.Ganzzahl(name, vorgabe) }
func EnvDauer(name string, vorgabe time.Duration) time.Duration {
	return Env.Dauer(name, vorgabe)
}

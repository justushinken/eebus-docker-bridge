package gemeinsam

import (
	"os"
	"strconv"
	"time"
)

// Hilfsfunktionen fuer Umgebungsvariablen (docker run -e ...).
// Nicht gesetzte, leere oder ungueltige Werte ergeben die Vorgabe.

func EnvText(name, vorgabe string) string {
	if wert, ok := os.LookupEnv(name); ok && wert != "" {
		return wert
	}
	return vorgabe
}

// Wie EnvText, aber ein gesetzter leerer Wert bleibt leer (z. B. WEB_ADRESSE= schaltet ab).
func EnvTextLeerErlaubt(name, vorgabe string) string {
	if wert, ok := os.LookupEnv(name); ok {
		return wert
	}
	return vorgabe
}

func EnvGanzzahl(name string, vorgabe int) int {
	if wert, err := strconv.Atoi(EnvText(name, "")); err == nil {
		return wert
	}
	return vorgabe
}

func EnvKommazahl(name string, vorgabe float64) float64 {
	if wert, err := strconv.ParseFloat(EnvText(name, ""), 64); err == nil {
		return wert
	}
	return vorgabe
}

func EnvDauer(name string, vorgabe time.Duration) time.Duration {
	if wert, err := time.ParseDuration(EnvText(name, "")); err == nil {
		return wert
	}
	return vorgabe
}

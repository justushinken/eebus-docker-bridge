package gemeinsam

import (
	"bytes"
	_ "embed"
)

//go:embed stil.css
var stil []byte

// MitStil setzt das gemeinsame Stylesheet an der Stelle <!--STIL--> ein.
// Inline statt als eigene Datei: Bei Aufruf als http://benutzer:pw@ip/
// blockieren Browser nachgeladene Dateien mit Zugangsdaten in der URL.
func MitStil(html []byte) []byte {
	return bytes.Replace(html, []byte("<!--STIL-->"), append(append([]byte("<style>\n"), stil...), "</style>"...), 1)
}

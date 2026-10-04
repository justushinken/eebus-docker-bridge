package gemeinsam

import (
	"slices"

	shipapi "github.com/enbility/ship-go/api"
)

// GefundenesGeraet ist ein per mDNS sichtbares EEBUS-Geraet fuer das Web-UI.
type GefundenesGeraet struct {
	Marke     string `json:"marke"`
	Modell    string `json:"modell"`
	Typ       string `json:"typ"`
	Ski       string `json:"ski"`
	ShipId    string `json:"shipId"`
	Steuerbox bool   `json:"steuerbox"` // sieht nach Steuerbox aus (Kategorie bzw. Geraetetyp)
}

// IstSteuerbox: Steuerboxen melden sich als "Grid Connection Hub" bzw. mit
// dem Geraetetyp ElectricitySupplySystem.
func IstSteuerbox(d shipapi.RemoteMdnsService) bool {
	return slices.Contains(d.Categories, shipapi.DeviceCategoryTypeGridConnectionHub) ||
		d.Type == "ElectricitySupplySystem"
}

// GefundeneGeraete liefert die Liste fuer das UI, Steuerboxen zuerst.
func GefundeneGeraete(dienste []shipapi.RemoteMdnsService) []GefundenesGeraet {
	geraete := make([]GefundenesGeraet, 0, len(dienste))
	for _, d := range dienste {
		geraete = append(geraete, GefundenesGeraet{
			Marke: d.Brand, Modell: d.Model, Typ: d.Type, Ski: d.Ski, ShipId: d.ShipID,
			Steuerbox: IstSteuerbox(d),
		})
	}
	slices.SortStableFunc(geraete, func(a, b GefundenesGeraet) int {
		switch {
		case a.Steuerbox && !b.Steuerbox:
			return -1
		case !a.Steuerbox && b.Steuerbox:
			return 1
		}
		return 0
	})
	return geraete
}

// NeuGefunden liefert die Dienste aus neu, deren SKI in alt noch nicht vorkam.
// mDNS meldet die Liste wiederholt, protokolliert werden sollen nur neue Geraete.
func NeuGefunden(alt, neu []shipapi.RemoteMdnsService) []shipapi.RemoteMdnsService {
	bekannt := make(map[string]bool, len(alt))
	for _, d := range alt {
		bekannt[d.Ski] = true
	}
	var ergebnis []shipapi.RemoteMdnsService
	for _, d := range neu {
		if !bekannt[d.Ski] {
			ergebnis = append(ergebnis, d)
		}
	}
	return ergebnis
}

package gemeinsam

import (
	"errors"
	"net"
	"slices"
	"strings"
	"unicode"
)

// Bevorzugte Schnittstellen: br0 ist auf dem PFC X1 (bzw. X1+X2 im Switch-Modus).
var bevorzugteSchnittstellen = []string{"br0", "eth0", "ethX1"}

// Virtuelle Schnittstellen haben zufaellige MACs und taugen nicht als Kennung.
var virtuellePraefixe = []string{"docker", "veth", "br-", "virbr", "lo"}

// GeraeteKennung liefert die MAC-Adresse der Haupt-Netzwerkschnittstelle in
// Grossbuchstaben ohne Trennzeichen, z. B. "0030DE683ADC". Mit --network host
// sieht der Container die Schnittstellen des PFC, die MAC ist weltweit
// eindeutig und steht auf dessen Typenschild. Leer, wenn keine gefunden wird.
func GeraeteKennung() string {
	schnittstellen, err := net.Interfaces()
	if err != nil {
		return ""
	}
	rang := func(s net.Interface) int {
		if i := slices.Index(bevorzugteSchnittstellen, s.Name); i >= 0 {
			return i
		}
		return len(bevorzugteSchnittstellen)
	}
	slices.SortStableFunc(schnittstellen, func(a, b net.Interface) int { return rang(a) - rang(b) })

	for _, s := range schnittstellen {
		if s.Flags&net.FlagLoopback != 0 || len(s.HardwareAddr) != 6 || virtuell(s.Name) {
			continue
		}
		return strings.ToUpper(strings.ReplaceAll(s.HardwareAddr.String(), ":", ""))
	}
	return ""
}

func virtuell(name string) bool {
	for _, praefix := range virtuellePraefixe {
		if strings.HasPrefix(name, praefix) {
			return true
		}
	}
	return false
}

// PruefeShipId prueft die Regeln fuer die SHIP-ID (mDNS-Schluessel "id"):
// hoechstens 63 Byte, kein Semikolon, keine Leer- oder Steuerzeichen.
func PruefeShipId(id string) error {
	if id == "" {
		return errors.New("SHIP-ID ist leer")
	}
	if len(id) > 63 {
		return errors.New("SHIP-ID ist laenger als 63 Byte")
	}
	for _, z := range id {
		if z == ';' || unicode.IsSpace(z) || unicode.IsControl(z) {
			return errors.New("SHIP-ID darf keine Leerzeichen, Steuerzeichen oder Semikolons enthalten")
		}
	}
	return nil
}

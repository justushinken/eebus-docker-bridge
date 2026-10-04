package gemeinsam

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	shipapi "github.com/enbility/ship-go/api"
)

// Kopplungsverfahren
const (
	VerfahrenSki     = "ski"     // SKI auf beiden Seiten eingetragen (bisheriges Verfahren)
	VerfahrenPairing = "pairing" // SHIP Pairing Service: SHIP-ID, Fingerprint, Secret
)

// Kopplung ist ein gespeicherter Kopplungspartner.
type Kopplung struct {
	Verfahren  string                  `json:"verfahren"`
	Identitaet shipapi.ServiceIdentity `json:"identitaet"`
}

// Bezeichnung liefert eine lesbare Kennung des Partners fuer Log und UI.
func Bezeichnung(id shipapi.ServiceIdentity) string {
	switch {
	case id.ShipID != "" && id.SKI != "":
		return fmt.Sprintf("%s (SKI %s)", id.ShipID, id.SKI)
	case id.ShipID != "":
		return id.ShipID
	case id.SKI != "":
		return "SKI " + id.SKI
	}
	return id.String()
}

var skiMuster = regexp.MustCompile(`^[0-9a-f]{40}$`)

// NormalisiereSki entfernt Leerzeichen (QR-Codes zeigen den SKI in
// Vierergruppen), schreibt klein und prueft auf 40 Hex-Zeichen.
func NormalisiereSki(ski string) (string, error) {
	ski = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(ski), " ", ""))
	if !skiMuster.MatchString(ski) {
		return "", errors.New("ungueltiger SKI, erwartet 40 Hex-Zeichen")
	}
	return ski, nil
}

// GleicheIdentitaet: a und b bezeichnen dasselbe Geraet. Verglichen wird, was
// beide kennen: SKI, sonst Fingerprint, sonst SHIP-ID. Eine per Pairing
// Service entstandene Kopplung kennt oft nur Fingerprint und SHIP-ID.
func GleicheIdentitaet(a, b shipapi.ServiceIdentity) bool {
	switch {
	case a.SKI != "" && b.SKI != "":
		return strings.EqualFold(a.SKI, b.SKI)
	case a.Fingerprint != "" && b.Fingerprint != "":
		return strings.EqualFold(a.Fingerprint, b.Fingerprint)
	case a.ShipID != "" && b.ShipID != "":
		return a.ShipID == b.ShipID
	}
	return false
}

// LadeKopplung liest eine gespeicherte Kopplung, nil wenn keine vorhanden ist.
func LadeKopplung(pfad string) (*Kopplung, error) {
	inhalt, err := os.ReadFile(pfad)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var k Kopplung
	if err := json.Unmarshal(inhalt, &k); err != nil {
		return nil, fmt.Errorf("%s: %w", pfad, err)
	}
	return &k, nil
}

// SpeichereKopplung schreibt die Kopplung, nil loescht die Datei.
func SpeichereKopplung(pfad string, k *Kopplung) error {
	if k == nil {
		if err := os.Remove(pfad); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	inhalt, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return SchreibeDatei(pfad, inhalt)
}

// --- Secret fuer den SHIP Pairing Service ---

const secretDatei = "pairing-secret.txt"

// LadeOderErzeugeSecret haelt das 16-Byte-Secret persistent im Volume. Es wird
// einmalig zufaellig erzeugt und bleibt danach gleich, denn die Steuerbox hat es
// ueber den Messstellenbetreiber bekommen.
func LadeOderErzeugeSecret(verzeichnis string) (shipapi.PairingSecret, error) {
	pfad := filepath.Join(verzeichnis, secretDatei)
	if inhalt, err := os.ReadFile(pfad); err == nil {
		secret, err := hex.DecodeString(strings.TrimSpace(string(inhalt)))
		if err != nil || !shipapi.PairingSecret(secret).IsValidLength() {
			return nil, fmt.Errorf("%s: kein gueltiges 16-Byte-Secret", pfad)
		}
		return secret, nil
	}

	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(verzeichnis, 0o700); err != nil {
		return nil, err
	}
	if err := SchreibeDatei(pfad, []byte(strings.ToUpper(hex.EncodeToString(secret))+"\n")); err != nil {
		return nil, err
	}
	return secret, nil
}

// SecretHex liefert das Secret so, wie es im QR-Code (SPSEC) steht.
func SecretHex(secret shipapi.PairingSecret) string {
	return strings.ToUpper(hex.EncodeToString(secret))
}

// --- Verlauf gegen Wiederholungsangriffe ---

// RingpufferDatei speichert die bereits gesehenen Pairing-Digests, damit eine
// aufgezeichnete Ankuendigung auch nach einem Neustart nicht erneut wirkt.
// Die Logik liegt in ship-go, hier nur die Ablage.
type RingpufferDatei struct {
	Pfad string
}

type ringpufferInhalt struct {
	Eintraege []shipapi.DigestEntry `json:"eintraege"`
	Naechster int                   `json:"naechster"`
}

func (r RingpufferDatei) LoadRingBuffer() ([]shipapi.DigestEntry, int, error) {
	inhalt, err := os.ReadFile(r.Pfad)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var gespeichert ringpufferInhalt
	if err := json.Unmarshal(inhalt, &gespeichert); err != nil {
		return nil, 0, err
	}
	return gespeichert.Eintraege, gespeichert.Naechster, nil
}

func (r RingpufferDatei) SaveRingBuffer(eintraege []shipapi.DigestEntry, naechster int) error {
	inhalt, err := json.Marshal(ringpufferInhalt{Eintraege: eintraege, Naechster: naechster})
	if err != nil {
		return err
	}
	return SchreibeDatei(r.Pfad, inhalt)
}

// --- QR-Text des SHIP Pairing Service ---

// PairingDaten sind die Angaben aus einem SHIP-QR-Code.
type PairingDaten struct {
	Ski         string
	ShipId      string
	Fingerprint string
	Secret      shipapi.PairingSecret
}

// LiesPairingQr liest einen SHIP-QR-Text der Form
// SHIP;SKI:...;ID:...;...;FPH256:...;SPSEC:...;ENDSHIP;
// Unbekannte Felder werden uebersprungen, wie es die Spezifikation verlangt.
func LiesPairingQr(text string) (*PairingDaten, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "SHIP;") {
		return nil, errors.New("QR-Text muss mit \"SHIP;\" beginnen")
	}
	text = strings.TrimSuffix(strings.TrimPrefix(text, "SHIP;"), "ENDSHIP;")

	daten := &PairingDaten{}
	for _, feld := range strings.Split(text, ";") {
		schluessel, wert, ok := strings.Cut(feld, ":")
		if !ok {
			continue
		}
		wert = strings.TrimSpace(wert)
		switch strings.ToUpper(strings.TrimSpace(schluessel)) {
		case "SKI":
			// Im QR-Code in Vierergruppen mit Leerzeichen
			daten.Ski = strings.ToLower(strings.ReplaceAll(wert, " ", ""))
		case "ID":
			daten.ShipId = wert
		case "FPH256":
			daten.Fingerprint = strings.ToUpper(wert)
		case "SPSEC":
			secret, err := hex.DecodeString(wert)
			if err != nil || !shipapi.PairingSecret(secret).IsValidLength() {
				return nil, errors.New("SPSEC ist kein gueltiges 16-Byte-Secret (32 Hex-Zeichen)")
			}
			daten.Secret = secret
		}
	}

	var fehlt []string
	for _, feld := range []struct{ name, wert string }{
		{"SKI", daten.Ski}, {"ID", daten.ShipId}, {"FPH256", daten.Fingerprint}, {"SPSEC", string(daten.Secret)},
	} {
		if feld.wert == "" {
			fehlt = append(fehlt, feld.name)
		}
	}
	if len(fehlt) > 0 {
		return nil, fmt.Errorf("im QR-Text fehlt: %s", strings.Join(fehlt, ", "))
	}
	return daten, nil
}

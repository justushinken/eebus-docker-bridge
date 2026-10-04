package main

import (
	"cmp"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	shipapi "github.com/enbility/ship-go/api"
)

// Dateien im Datenverzeichnis mit der gekoppelten Steuerbox, je Verfahren.
// Es gibt immer hoechstens eine Kopplung (eine Steuerbox je Anlage).
const (
	KopplungsdateiPairing = "steuerbox-pairing.json" // per Pairing Service
	KopplungsdateiSki     = "steuerbox-ski.json"     // im UI per SKI (Suchmodus)
)

const (
	suchmodusDauer = 10 * time.Minute
	anfrageGueltig = 2 * time.Minute // ship-go wartet bis zu 60 s plus Verlaengerungen
)

// Anfrage ist ein Verbindungsversuch einer Steuerbox, der die Bruecke noch
// nicht vertraut. Im Suchmodus wartet ship-go auf die Entscheidung im UI.
type Anfrage struct {
	Identitaet shipapi.ServiceIdentity
	Seit       time.Time
}

func LadeGespeicherteKopplung(verzeichnis string) (*gemeinsam.Kopplung, error) {
	for _, datei := range []string{KopplungsdateiPairing, KopplungsdateiSki} {
		if k, err := gemeinsam.LadeKopplung(filepath.Join(verzeichnis, datei)); k != nil || err != nil {
			return k, err
		}
	}
	return nil, nil
}

// hatPartner: Es ist eine Steuerbox eingetragen. Aufruf unter mu.
func (b *Bruecke) hatPartner() bool {
	return b.konf.RemoteSki != "" || b.kopplung != nil
}

// vertraut: Dieser Partner ist bereits als Steuerbox eingetragen. Aufruf unter mu.
func (b *Bruecke) vertraut(partner shipapi.ServiceIdentity) bool {
	return (b.konf.RemoteSki != "" && strings.EqualFold(partner.SKI, b.konf.RemoteSki)) ||
		(b.kopplung != nil && gemeinsam.GleicheIdentitaet(partner, b.kopplung.Identitaet))
}

// --- api.ServiceReaderInterface ---

// Verbindungswechsel nur einmal protokollieren, ship-go meldet sie teils doppelt.
func (b *Bruecke) RemoteServiceConnected(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
	b.mu.Lock()
	neu := b.verbindung != VerbindungVerbunden
	b.verbindung = VerbindungVerbunden
	b.partner = partner
	delete(b.anfragen, partner.SKI)
	b.mu.Unlock()
	if neu {
		log.Printf("Steuerbox verbunden: %s", gemeinsam.Bezeichnung(partner))
	}
}

func (b *Bruecke) RemoteServiceDisconnected(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
	b.mu.Lock()
	// Nur die verbundene Steuerbox. Andere EEBUS-Geraete im LAN koennen sich
	// ebenfalls melden und wieder trennen.
	if !b.partner.IsZero() && !gemeinsam.GleicheIdentitaet(partner, b.partner) {
		b.mu.Unlock()
		return
	}
	neu := b.verbindung == VerbindungVerbunden
	b.verbindung = VerbindungGetrennt
	if !b.hatPartner() {
		b.verbindung = VerbindungKeinPartner
	}
	b.partner = shipapi.ServiceIdentity{}
	b.gegenstelle = nil
	b.steuerboxUseCases = 0
	b.herstellerAngefordert = ""
	b.mu.Unlock()
	if neu {
		log.Printf("Steuerbox getrennt: %s", gemeinsam.Bezeichnung(partner))
	}
}

// Hilfreich bei der Inbetriebnahme: zeigt die per mDNS gefundenen Geraete samt SKI.
// mDNS meldet die Liste wiederholt, protokolliert werden nur neu gefundene Geraete.
func (b *Bruecke) VisibleRemoteMdnsServicesUpdated(dienst api.ServiceInterface, eintraege []shipapi.RemoteMdnsService) {
	b.mu.Lock()
	neu := gemeinsam.NeuGefunden(b.gefunden, eintraege)
	b.gefunden = slices.Clone(eintraege)
	b.mu.Unlock()

	for _, e := range neu {
		log.Printf("Gefunden: %s %s, SHIP-ID %s, SKI %s", e.Brand, e.Model, e.ShipID, e.Ski)
	}
}

func (b *Bruecke) ServiceUpdated(partner shipapi.ServiceIdentity) {}

// ServicePairingDetailUpdate meldet die Zustaende des SHIP-Handshakes. Ein
// Geraet, dem die Bruecke nicht vertraut, erscheint hier mit
// ConnectionStateReceivedPairingRequest. Wird aus ship-go heraus aufgerufen:
// hier nur Zustand merken, nicht in den Stack zurueckrufen.
func (b *Bruecke) ServicePairingDetailUpdate(partner shipapi.ServiceIdentity, detail *shipapi.ConnectionStateDetail) {
	if text, melden := b.pairing.Neu(partner, detail); melden {
		log.Printf("Pairing %s: %s", gemeinsam.Bezeichnung(partner), text)
	}
	if partner.SKI == "" {
		return
	}

	b.mu.Lock()
	var meldung string
	switch detail.State() {
	case shipapi.ConnectionStateReceivedPairingRequest:
		switch {
		case b.vertraut(partner):
		case time.Now().Before(b.suchmodusBis):
			if _, bekannt := b.anfragen[partner.SKI]; !bekannt {
				meldung = fmt.Sprintf("Kopplungsanfrage von %s: im UI annehmen oder ablehnen", gemeinsam.Bezeichnung(partner))
			}
			b.anfragen[partner.SKI] = &Anfrage{Identitaet: partner, Seit: time.Now()}
		case !b.unbekannt[partner.SKI]:
			// Ohne Suchmodus lehnt ship-go sofort ab. Einmal melden: Oft hat der
			// Messstellenbetreiber die Bruecke eingetragen, die Bruecke die
			// Steuerbox aber noch nicht.
			b.unbekannt[partner.SKI] = true
			meldung = fmt.Sprintf("Verbindungsversuch eines unbekannten Geraets abgelehnt: %s. Zum Koppeln den Suchmodus starten.", gemeinsam.Bezeichnung(partner))
		}
	case shipapi.ConnectionStateQueued, shipapi.ConnectionStateInitiated, shipapi.ConnectionStateInProgress:
		// Zwischenschritte, Anfrage bleibt offen
	default:
		delete(b.anfragen, partner.SKI)
	}
	b.mu.Unlock()

	if meldung != "" {
		log.Print(meldung)
	}
}

// --- SHIP Pairing Service ---
//
// Eine Steuerbox, die das Secret kennt, wird automatisch vertraut. ship-go
// haelt das nur im Speicher, deshalb wird sie im Volume abgelegt und beim
// Start wieder angemeldet (siehe main.go).

func (b *Bruecke) ServiceAutoTrusted(dienst api.ServiceInterface, partner shipapi.ServiceIdentity) {
	// Eigene Goroutine: Aus diesem Callback heraus nicht in ship-go zurueckrufen.
	go func() {
		kopplung := &gemeinsam.Kopplung{Verfahren: gemeinsam.VerfahrenPairing, Identitaet: partner}
		if err := b.ersetzeKopplung(kopplung); err != nil {
			log.Printf("Kopplung speichern: %v", err)
		}
		log.Printf("Steuerbox per Pairing Service gekoppelt: %s", gemeinsam.Bezeichnung(partner))
	}()
}

func (b *Bruecke) ServiceAutoTrustFailed(dienst api.ServiceInterface, partner shipapi.ServiceIdentity, grund error) {
	// Eine schon gekoppelte Steuerbox wiederholt ihre Ankuendigung bis zu 15 min.
	// Nach einem Neustart erkennt ship-go das als Wiederholung und lehnt ab, die
	// Steuerbox ist aber ohnehin vertraut: keine Meldung wert.
	b.mu.Lock()
	gekoppelt := b.kopplung != nil && partner.ShipID != "" && partner.ShipID == b.kopplung.Identitaet.ShipID
	b.mu.Unlock()
	if gekoppelt {
		return
	}
	log.Printf("Pairing Service abgelehnt fuer %s: %v", gemeinsam.Bezeichnung(partner), grund)
}

// Kommt z. B., wenn eine neue Steuerbox die alte ersetzt. Die neue meldet
// sich danach ueber ServiceAutoTrusted.
func (b *Bruecke) ServiceAutoTrustRemoved(dienst api.ServiceInterface, partner shipapi.ServiceIdentity, grund string) {
	go b.entfernePairingKopplung(partner, grund)
}

func (b *Bruecke) entfernePairingKopplung(partner shipapi.ServiceIdentity, grund string) {
	b.kopplungMu.Lock()
	defer b.kopplungMu.Unlock()
	b.mu.Lock()
	betroffen := b.kopplung != nil && b.kopplung.Verfahren == gemeinsam.VerfahrenPairing &&
		gemeinsam.GleicheIdentitaet(b.kopplung.Identitaet, partner)
	b.mu.Unlock()
	if !betroffen {
		return
	}
	if err := b.speichereKopplung(nil); err != nil {
		log.Printf("Kopplung loeschen: %v", err)
	}
	b.mu.Lock()
	b.kopplung = nil
	if !b.hatPartner() && b.verbindung != VerbindungVerbunden {
		b.verbindung = VerbindungKeinPartner
	}
	b.mu.Unlock()
	log.Printf("Kopplung mit Steuerbox %s aufgehoben: %s", gemeinsam.Bezeichnung(partner), grund)
}

// --- Kopplung verwalten ---

func (b *Bruecke) speichereKopplung(k *gemeinsam.Kopplung) error {
	var fehler []error
	for _, datei := range []string{KopplungsdateiPairing, KopplungsdateiSki} {
		var inhalt *gemeinsam.Kopplung
		if k != nil && (datei == KopplungsdateiPairing) == (k.Verfahren == gemeinsam.VerfahrenPairing) {
			inhalt = k
		}
		fehler = append(fehler, gemeinsam.SpeichereKopplung(filepath.Join(b.konf.Datenverzeichnis, datei), inhalt))
	}
	return errors.Join(fehler...)
}

// ersetzeKopplung speichert die neue Kopplung und meldet eine andere, bisher
// gekoppelte Steuerbox beim EEBUS-Stack ab. Es gibt nur eine Steuerbox.
// kopplungMu haelt Datei, Zustand und EEBUS-Stack in derselben Reihenfolge.
// Nicht aus Callbacks von ship-go heraus aufrufen (dort per Goroutine).
func (b *Bruecke) ersetzeKopplung(neu *gemeinsam.Kopplung) error {
	b.kopplungMu.Lock()
	defer b.kopplungMu.Unlock()
	if err := b.speichereKopplung(neu); err != nil {
		return err
	}
	b.mu.Lock()
	alt := b.kopplung
	b.kopplung = neu
	switch {
	case b.verbindung == VerbindungVerbunden:
	case b.hatPartner():
		b.verbindung = VerbindungGetrennt
	default:
		b.verbindung = VerbindungKeinPartner
	}
	b.mu.Unlock()

	if alt != nil && (neu == nil || !gemeinsam.GleicheIdentitaet(alt.Identitaet, neu.Identitaet)) {
		b.dienst.UnregisterRemoteService(alt.Identitaet)
	}
	if neu != nil && neu.Verfahren == gemeinsam.VerfahrenSki {
		b.dienst.RegisterRemoteService(neu.Identitaet)
	}
	return nil
}

// --- Aktionen aus dem Web-UI ---

var errKopplungEnv = fmt.Errorf("%w: Kopplung ist per EEBUS_REMOTE_SKI fest eingestellt", gemeinsam.ErrVerboten)
var errUiNurLesend = fmt.Errorf("%w: Kopplung im UI abgeschaltet (WEB_KOPPLUNG=aus)", gemeinsam.ErrVerboten)

func (b *Bruecke) pruefeKopplungAenderbar() error {
	switch {
	case !b.konf.WebKopplung:
		return errUiNurLesend
	case b.konf.RemoteSki != "":
		return errKopplungEnv
	}
	return nil
}

// SetzeSuchmodus: Solange er laeuft, halten eingehende Verbindungen unbekannter
// Geraete an, bis sie im UI angenommen oder abgelehnt werden. SetAutoAccept
// wird bewusst nicht verwendet: Sonst koennte jedes Geraet im LAN, das sich in
// dieser Zeit meldet, Leistungsgrenzen setzen.
func (b *Bruecke) SetzeSuchmodus(an bool) error {
	if !b.konf.WebKopplung {
		return errUiNurLesend
	}
	if !an {
		b.beendeSuchmodus("Suchmodus beendet")
		return nil
	}
	b.mu.Lock()
	war := time.Now().Before(b.suchmodusBis)
	b.suchmodusBis = time.Now().Add(suchmodusDauer)
	clear(b.unbekannt)
	b.mu.Unlock()

	b.dienst.UserIsAbleToApproveOrCancelPairingRequests(true)
	if !war {
		log.Printf("Suchmodus gestartet fuer %v", suchmodusDauer)
	}
	return nil
}

// beendeSuchmodus lehnt noch wartende Anfragen ab, damit ship-go sie nicht
// bis zum Timeout offen haelt.
func (b *Bruecke) beendeSuchmodus(meldung string) {
	b.mu.Lock()
	war := !b.suchmodusBis.IsZero()
	b.suchmodusBis = time.Time{}
	var offen []shipapi.ServiceIdentity
	for _, a := range b.anfragen {
		offen = append(offen, a.Identitaet)
	}
	clear(b.anfragen)
	b.mu.Unlock()

	b.dienst.UserIsAbleToApproveOrCancelPairingRequests(false)
	for _, id := range offen {
		b.dienst.CancelPairing(id)
	}
	if war {
		log.Print(meldung)
	}
}

func (b *Bruecke) pruefeSuchmodus(jetzt time.Time) {
	b.mu.Lock()
	abgelaufen := !b.suchmodusBis.IsZero() && jetzt.After(b.suchmodusBis)
	for ski, a := range b.anfragen {
		if jetzt.Sub(a.Seit) > anfrageGueltig {
			delete(b.anfragen, ski)
		}
	}
	b.mu.Unlock()

	if abgelaufen {
		b.beendeSuchmodus("Suchmodus abgelaufen")
	}
}

// KoppelnPerSki vertraut einer gefundenen Steuerbox. Die Bruecke baut die
// Verbindung dann selbst auf, sobald auch die Steuerbox ihr vertraut.
func (b *Bruecke) KoppelnPerSki(ski string) error {
	if err := b.pruefeKopplungAenderbar(); err != nil {
		return err
	}
	ski, err := gemeinsam.NormalisiereSki(ski)
	if err != nil {
		return err
	}
	if ski == b.eigenerSki {
		return errors.New("das ist der eigene SKI")
	}
	kopplung := &gemeinsam.Kopplung{Verfahren: gemeinsam.VerfahrenSki, Identitaet: shipapi.NewServiceIdentity(ski, "", "")}
	if err := b.ersetzeKopplung(kopplung); err != nil {
		return err
	}
	log.Printf("Steuerbox per SKI gekoppelt: %s", ski)
	return nil
}

// BeantworteAnfrage nimmt eine wartende Kopplungsanfrage an oder lehnt sie ab.
func (b *Bruecke) BeantworteAnfrage(ski string, annehmen bool) error {
	if err := b.pruefeKopplungAenderbar(); err != nil {
		return err
	}
	ski, err := gemeinsam.NormalisiereSki(ski)
	if err != nil {
		return err
	}
	b.mu.Lock()
	anfrage := b.anfragen[ski]
	delete(b.anfragen, ski)
	b.mu.Unlock()
	if anfrage == nil {
		return errors.New("keine offene Anfrage von diesem Geraet (abgelaufen?)")
	}

	if !annehmen {
		b.dienst.CancelPairing(anfrage.Identitaet)
		log.Printf("Kopplungsanfrage abgelehnt: %s", gemeinsam.Bezeichnung(anfrage.Identitaet))
		return nil
	}
	kopplung := &gemeinsam.Kopplung{Verfahren: gemeinsam.VerfahrenSki, Identitaet: shipapi.NewServiceIdentity(ski, "", "")}
	if err := b.ersetzeKopplung(kopplung); err != nil {
		return err
	}
	log.Printf("Kopplungsanfrage angenommen: %s", gemeinsam.Bezeichnung(anfrage.Identitaet))
	return nil
}

func (b *Bruecke) Entkoppeln() error {
	if err := b.pruefeKopplungAenderbar(); err != nil {
		return err
	}
	b.mu.Lock()
	alt := b.kopplung
	b.mu.Unlock()
	if alt == nil {
		return errors.New("keine Steuerbox gekoppelt")
	}
	if err := b.ersetzeKopplung(nil); err != nil {
		return err
	}
	log.Printf("Kopplung mit Steuerbox %s aufgehoben", gemeinsam.Bezeichnung(alt.Identitaet))
	return nil
}

// --- Statusabbild ---

type KopplungDaten struct {
	Verfahren string `json:"verfahren"` // ski | pairing
	Quelle    string `json:"quelle"`    // env | ui | pairing
	Ski       string `json:"ski"`
	ShipId    string `json:"shipId"`
}

type AnfrageDaten struct {
	Ski    string  `json:"ski"`
	ShipId string  `json:"shipId"`
	Marke  string  `json:"marke"`
	Modell string  `json:"modell"`
	AlterS float64 `json:"alterS"`
}

// kopplungStatus: Aufruf unter mu.
func (b *Bruecke) kopplungStatus(jetzt time.Time) (kopplung *KopplungDaten, anfragen []AnfrageDaten, suchmodusRestS float64) {
	switch {
	case b.konf.RemoteSki != "":
		kopplung = &KopplungDaten{Verfahren: gemeinsam.VerfahrenSki, Quelle: "env", Ski: b.konf.RemoteSki}
	case b.kopplung != nil:
		quelle := "ui"
		if b.kopplung.Verfahren == gemeinsam.VerfahrenPairing {
			quelle = "pairing"
		}
		kopplung = &KopplungDaten{Verfahren: b.kopplung.Verfahren, Quelle: quelle,
			Ski: b.kopplung.Identitaet.SKI, ShipId: b.kopplung.Identitaet.ShipID}
	}
	// Fehlende Angaben von der verbundenen Steuerbox ergaenzen (eine Kopplung per
	// Pairing Service kennt oft keinen SKI, eine per SKI keine SHIP-ID).
	if kopplung != nil && !b.partner.IsZero() {
		gleich := strings.EqualFold(b.partner.SKI, kopplung.Ski) ||
			(b.kopplung != nil && gemeinsam.GleicheIdentitaet(b.partner, b.kopplung.Identitaet))
		if gleich {
			kopplung.Ski = cmp.Or(kopplung.Ski, b.partner.SKI)
			kopplung.ShipId = cmp.Or(kopplung.ShipId, b.partner.ShipID)
		}
	}

	if jetzt.Before(b.suchmodusBis) {
		suchmodusRestS = b.suchmodusBis.Sub(jetzt).Seconds()
	}
	anfragen = []AnfrageDaten{}
	for ski, a := range b.anfragen {
		d := AnfrageDaten{Ski: ski, ShipId: a.Identitaet.ShipID, AlterS: jetzt.Sub(a.Seit).Seconds()}
		// Marke und Modell aus mDNS, falls die Steuerbox dort sichtbar ist
		for _, g := range b.gefunden {
			if strings.EqualFold(g.Ski, ski) {
				d.Marke, d.Modell = g.Brand, g.Model
				if d.ShipId == "" {
					d.ShipId = g.ShipID
				}
			}
		}
		anfragen = append(anfragen, d)
	}
	slices.SortFunc(anfragen, func(x, y AnfrageDaten) int { return strings.Compare(x.Ski, y.Ski) })
	return kopplung, anfragen, suchmodusRestS
}

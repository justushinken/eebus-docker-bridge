package gemeinsam

import (
	"fmt"
	"sync"

	shipapi "github.com/enbility/ship-go/api"
)

var pairingTexte = map[shipapi.ConnectionState]string{
	shipapi.ConnectionStateNone:                   "kein Pairing",
	shipapi.ConnectionStateQueued:                 "eingereiht",
	shipapi.ConnectionStateInitiated:              "von hier gestartet",
	shipapi.ConnectionStateReceivedPairingRequest: "Anfrage der Gegenseite",
	shipapi.ConnectionStateInProgress:             "Handshake laeuft",
	shipapi.ConnectionStateTrusted:                "vertraut",
	shipapi.ConnectionStatePin:                    "PIN",
	shipapi.ConnectionStateCompleted:              "abgeschlossen",
	shipapi.ConnectionStateRemoteDeniedTrust:      "von Gegenseite abgelehnt",
	shipapi.ConnectionStateError:                  "Fehler",
}

// Zustaende, die im Log erscheinen. Die Zwischenschritte des Handshakes
// (eingereiht, Handshake laeuft, vertraut, ...) wechseln bei jedem
// Verbindungsaufbau mehrfach und wuerden das Ereignisprotokoll fuellen.
var pairingMelden = map[shipapi.ConnectionState]bool{
	shipapi.ConnectionStateCompleted:         true,
	shipapi.ConnectionStateRemoteDeniedTrust: true,
	shipapi.ConnectionStateError:             true,
}

// Pairingprotokoll meldet SHIP-Pairing-Zustaende als Text, nur die
// aussagekraeftigen und nur bei Wechsel.
type Pairingprotokoll struct {
	mu      sync.Mutex
	zustand map[string]shipapi.ConnectionState
}

// Neu liefert den Text zum Zustand und ob er ins Log gehoert: Der Zustand ist
// aussagekraeftig und hat sich fuer diesen Partner geaendert.
func (p *Pairingprotokoll) Neu(partner shipapi.ServiceIdentity, detail *shipapi.ConnectionStateDetail) (text string, melden bool) {
	zustand := detail.State()
	schluessel := Bezeichnung(partner)
	p.mu.Lock()
	if p.zustand == nil {
		p.zustand = make(map[string]shipapi.ConnectionState)
	}
	alt, bekannt := p.zustand[schluessel]
	p.zustand[schluessel] = zustand
	p.mu.Unlock()

	text, ok := pairingTexte[zustand]
	if !ok {
		text = fmt.Sprint(zustand)
	}
	if err := detail.Error(); err != nil {
		text += ": " + err.Error()
	}
	return text, pairingMelden[zustand] && (!bekannt || alt != zustand)
}

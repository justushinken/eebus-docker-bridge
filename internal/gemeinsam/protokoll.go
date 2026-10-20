package gemeinsam

import (
	"bytes"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type Ereignis struct {
	Zeit time.Time `json:"zeit"`
	Text string    `json:"text"`
}

// Ereignisprotokoll haelt die letzten Log-Zeilen fuer das Web-UI.
type Ereignisprotokoll struct {
	mu        sync.Mutex
	eintraege []Ereignis
	max       int
	filter    func(text string) (string, bool)
}

// SetzeFilter: Der Filter sieht jede Zeile ohne Zeitstempel und kann sie fuer
// das UI umschreiben oder weglassen (false). stderr bekommt sie unveraendert.
// Er laeuft unter der Sperre des Standard-Logs und darf selbst nicht loggen.
func (p *Ereignisprotokoll) SetzeFilter(filter func(text string) (string, bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.filter = filter
}

// ProtokolliereLog haengt ein Ereignisprotokoll an das Standard-Log, damit alle
// log.Printf-Meldungen ohne Aenderung auch im UI erscheinen.
func ProtokolliereLog(max int) *Ereignisprotokoll {
	p := &Ereignisprotokoll{max: max}
	log.SetOutput(io.MultiWriter(os.Stderr, p))
	return p
}

// logZeitformat entspricht log.LstdFlags ("2006/01/02 15:04:05 ").
const logZeitformat = "2006/01/02 15:04:05"

func (p *Ereignisprotokoll) Write(daten []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, zeile := range strings.Split(string(bytes.TrimRight(daten, "\n")), "\n") {
		e := Ereignis{Zeit: time.Now(), Text: zeile}
		if len(zeile) > len(logZeitformat) {
			if zeit, err := time.ParseInLocation(logZeitformat, zeile[:len(logZeitformat)], time.Local); err == nil {
				e = Ereignis{Zeit: zeit, Text: zeile[len(logZeitformat)+1:]}
			}
		}
		if p.filter != nil {
			var behalten bool
			if e.Text, behalten = p.filter(e.Text); !behalten {
				continue
			}
		}
		p.eintraege = append(p.eintraege, e)
	}
	if ueberzaehlig := len(p.eintraege) - p.max; ueberzaehlig > 0 {
		p.eintraege = append([]Ereignis(nil), p.eintraege[ueberzaehlig:]...)
	}
	return len(daten), nil
}

// Neueste zuerst.
func (p *Ereignisprotokoll) Liste() []Ereignis {
	p.mu.Lock()
	defer p.mu.Unlock()
	liste := make([]Ereignis, len(p.eintraege))
	for i, e := range p.eintraege {
		liste[len(liste)-1-i] = e
	}
	return liste
}

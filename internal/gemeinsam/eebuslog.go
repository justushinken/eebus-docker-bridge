package gemeinsam

import (
	"fmt"
	"log"
	"os"
)

// EebusLog nimmt die internen Meldungen von eebus-go, ship-go und spine-go
// entgegen. Mit Ausgabe (EEBUS_DEBUG) kommen sie auf stdout (docker logs), fuer
// die Fehlersuche mit einem neuen Geraet. Bewusst ein eigener Logger und nicht
// das Standard-Log: Sonst liefe das Ereignisprotokoll im UI in Sekunden ueber.
//
// Der Beobachter sieht alle Meldungen ausser Trace, auch ohne Ausgabe, und
// kann einzelne von stdout fernhalten (Rueckgabe false).
type EebusLog struct {
	l          *log.Logger // nil = keine Ausgabe
	beobachter func(text string) bool
}

func NeuesEebusLog(ausgabe bool, beobachter func(text string) bool) *EebusLog {
	e := &EebusLog{beobachter: beobachter}
	if ausgabe {
		e.l = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	}
	return e
}

func (e *EebusLog) ausgeben(stufe, text string) {
	if e.beobachter != nil && !e.beobachter(text) {
		return
	}
	if e.l != nil {
		e.l.Output(3, stufe+" "+text)
	}
}

// Trace enthaelt jede SPINE-Nachricht: ohne Ausgabe gar nicht erst formatieren.
func (e *EebusLog) Trace(args ...interface{}) {
	if e.l != nil {
		e.l.Output(2, "TRACE "+fmt.Sprint(args...))
	}
}
func (e *EebusLog) Tracef(format string, args ...interface{}) {
	if e.l != nil {
		e.l.Output(2, "TRACE "+fmt.Sprintf(format, args...))
	}
}
func (e *EebusLog) Debug(args ...interface{}) { e.ausgeben("DEBUG", fmt.Sprint(args...)) }
func (e *EebusLog) Debugf(format string, args ...interface{}) {
	e.ausgeben("DEBUG", fmt.Sprintf(format, args...))
}
func (e *EebusLog) Info(args ...interface{}) { e.ausgeben("INFO ", fmt.Sprint(args...)) }
func (e *EebusLog) Infof(format string, args ...interface{}) {
	e.ausgeben("INFO ", fmt.Sprintf(format, args...))
}
func (e *EebusLog) Error(args ...interface{}) { e.ausgeben("ERROR", fmt.Sprint(args...)) }
func (e *EebusLog) Errorf(format string, args ...interface{}) {
	e.ausgeben("ERROR", fmt.Sprintf(format, args...))
}

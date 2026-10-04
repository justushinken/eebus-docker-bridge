package gemeinsam

import (
	"fmt"
	"log"
	"os"
)

// EebusLog gibt die internen Meldungen von eebus-go, ship-go und spine-go auf
// stdout aus (docker logs), fuer die Fehlersuche mit einem neuen Geraet. Bewusst
// ein eigener Logger und nicht das Standard-Log: Sonst liefe das
// Ereignisprotokoll im UI in Sekunden ueber.
type EebusLog struct {
	l *log.Logger
}

func NeuesEebusLog() *EebusLog {
	return &EebusLog{l: log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)}
}

func (e *EebusLog) ausgeben(stufe string, args ...interface{}) {
	e.l.Output(3, stufe+" "+fmt.Sprint(args...))
}

func (e *EebusLog) Trace(args ...interface{}) { e.ausgeben("TRACE", args...) }
func (e *EebusLog) Tracef(format string, args ...interface{}) {
	e.ausgeben("TRACE", fmt.Sprintf(format, args...))
}
func (e *EebusLog) Debug(args ...interface{}) { e.ausgeben("DEBUG", args...) }
func (e *EebusLog) Debugf(format string, args ...interface{}) {
	e.ausgeben("DEBUG", fmt.Sprintf(format, args...))
}
func (e *EebusLog) Info(args ...interface{}) { e.ausgeben("INFO ", args...) }
func (e *EebusLog) Infof(format string, args ...interface{}) {
	e.ausgeben("INFO ", fmt.Sprintf(format, args...))
}
func (e *EebusLog) Error(args ...interface{}) { e.ausgeben("ERROR", args...) }
func (e *EebusLog) Errorf(format string, args ...interface{}) {
	e.ausgeben("ERROR", fmt.Sprintf(format, args...))
}

// SPS-Simulator fuer lokale Tests: spielt die CODESYS-Seite.
//
// Liest jede Sekunde die Input-Register der Bruecke und rechnet eine kleine
// Anlage durch: eine steuerbare Last, die der Bezugsgrenze folgt, eine
// PV-Anlage, die bei einer Einspeisegrenze abgeregelt wird, und eine nicht
// steuerbare Grundlast. Daraus entstehen die Messwerte fuer MPC (Anlage =
// Last und PV) und MGCP (Netzanschlusspunkt), die zusammen mit Lebenszeichen,
// Nennleistungen und Anlagenstatus in die Holding-Register gehen.
//
//	go run ./testwerkzeuge/spssimulator -url tcp://127.0.0.1:5502 -nennleistung 22000 -last 9000 -pv 8000
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"time"

	"github.com/simonvetter/modbus"
)

var zustaende = []string{"Init", "Unbegrenzt/gesteuert", "Begrenzt", "Failsafe", "Unbegrenzt/autonom"}
var verbindungen = []string{"kein Partner", "getrennt", "verbunden"}

func text(liste []string, i uint16) string {
	if int(i) < len(liste) {
		return liste[i]
	}
	return fmt.Sprintf("?(%d)", i)
}

func u32(r []uint16, i int) uint32 {
	return uint32(r[i])<<16 | uint32(r[i+1])
}

// Registerbelegung siehe bruecke/modbus.go
const (
	blockBezug       = 2
	blockEinspeisung = 17
	anzahlInput      = 31
	anzahlHolding    = 61
)

type register []uint16

func (r register) dint(i int, wert float64) {
	v := uint32(int32(math.Round(wert)))
	r[i], r[i+1] = uint16(v>>16), uint16(v)
}

func (r register) udint(i int, wert float64) {
	v := uint32(max(math.Round(wert), 0))
	r[i], r[i+1] = uint16(v>>16), uint16(v)
}

func (r register) ulint(i int, wert float64) {
	v := uint64(max(math.Round(wert), 0))
	r[i], r[i+1], r[i+2], r[i+3] = uint16(v>>48), uint16(v>>32), uint16(v>>16), uint16(v)
}

func (r register) uint(i int, wert float64) {
	r[i] = uint16(min(max(math.Round(wert), 0), math.MaxUint16))
}

func main() {
	url := flag.String("url", "tcp://127.0.0.1:5502", "Modbus-Server der Bruecke")
	nennleistung := flag.Uint("nennleistung", 0, "an die Bruecke gemeldete Nennleistung Bezug in W, 0 = Vorgabe der Bruecke")
	nennErzeugung := flag.Uint("nennleistung-erzeugung", 0, "an die Bruecke gemeldete Nennleistung Erzeugung in W, 0 = Vorgabe der Bruecke")
	last := flag.Float64("last", 9000, "Leistungsbedarf der steuerbaren Last in W")
	pv := flag.Float64("pv", 0, "PV-Leistung ohne Abregelung in W")
	grundlast := flag.Float64("grundlast", 500, "nicht steuerbare Grundlast am Netzanschlusspunkt in W")
	ungueltigMpc := flag.Uint("ungueltig-mpc", 0, "diese Bits der MPC-Gueltigkeitsmaske loeschen (z. B. 1 = Leistung)")
	ungueltigMgcp := flag.Uint("ungueltig-mgcp", 0, "diese Bits der MGCP-Gueltigkeitsmaske loeschen")
	zaehlerstart := flag.Float64("zaehlerstart", 0, "Startwert der Energiezaehler in Wh (ueber 4294967296 testet 64 Bit)")
	stoerung := flag.Bool("stoerung", false, "Anlagenstatus Stoerung melden")
	flag.Parse()

	client, err := modbus.NewClient(&modbus.ClientConfiguration{URL: *url, Timeout: 2 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	for client.Open() != nil {
		log.Printf("warte auf %s ...", *url)
		time.Sleep(2 * time.Second)
	}
	defer client.Close()

	mpcBezug, mpcErzeugung := *zaehlerstart, *zaehlerstart
	napBezug, napEinspeisung := *zaehlerstart, *zaehlerstart
	var lebenszeichen uint16
	var r []uint16 // zuletzt gelesene Input-Register

	for range time.Tick(time.Second) {
		lebenszeichen++

		// --- Anlage durchrechnen ---
		verbrauch, erzeugung := *last, *pv
		var lpcAktiv, lppAktiv bool
		var lpcGrenze, lppGrenze float64
		if r != nil {
			lpcAktiv, lpcGrenze = r[blockBezug+4] == 1, float64(u32(r, blockBezug+2))
			lppAktiv, lppGrenze = r[blockEinspeisung+4] == 1 && r[15]&2 != 0, float64(u32(r, blockEinspeisung+2))
		}
		if lpcAktiv {
			verbrauch = min(verbrauch, lpcGrenze)
		}
		// Einspeisung am Netzanschlusspunkt auf die Grenze abregeln
		if lppAktiv {
			erzeugung = max(min(erzeugung, lppGrenze+verbrauch+*grundlast), 0)
		}
		pAnlage := verbrauch - erzeugung
		pNap := pAnlage + *grundlast

		mpcBezug += max(pAnlage, 0) / 3600
		mpcErzeugung += max(-pAnlage, 0) / 3600
		napBezug += max(pNap, 0) / 3600
		napEinspeisung += max(-pNap, 0) / 3600

		h := register(make([]uint16, anzahlHolding))
		h[0] = lebenszeichen
		h.udint(1, float64(*nennleistung))
		h.udint(3, float64(*nennErzeugung))
		h[5] = 0x1FFF &^ uint16(*ungueltigMpc)
		h[6] = 0x07FF &^ uint16(*ungueltigMgcp)
		if *stoerung {
			h[7] = 1
		}
		for p := range 3 {
			u := 230 + rand.Float64()*4 - 2
			// MPC
			h.dint(10+2*p, pAnlage/3)
			h.dint(24+2*p, pAnlage/3/u*1000)
			h.uint(30+p, u*10)
			// MGCP
			h.dint(50+2*p, pNap/3/u*1000)
			h.uint(56+p, u*10)
		}
		f := 50 + rand.Float64()*0.04 - 0.02
		h.dint(8, pAnlage)
		h.ulint(16, mpcBezug)
		h.ulint(20, mpcErzeugung)
		h.uint(33, f*100)
		h.dint(40, pNap)
		h.ulint(42, napEinspeisung)
		h.ulint(46, napBezug)
		h.uint(59, f*100)
		faktor := 100.0
		if lppAktiv && *pv > 0 {
			faktor = min(lppGrenze / *pv * 100, 100)
		}
		h.uint(60, faktor*10)

		// Wie Auto-Reconnect in CODESYS: Nach einem Neustart der Bruecke ist die
		// alte TCP-Verbindung tot, also schliessen und neu aufbauen.
		if err := client.WriteRegisters(0, h); err != nil {
			log.Printf("Holding schreiben: %v, verbinde neu", err)
			client.Close()
			client.Open()
			continue
		}

		if r, err = client.ReadRegisters(0, anzahlInput, modbus.INPUT_REGISTER); err != nil {
			log.Printf("Input lesen: %v", err)
			r = nil
			continue
		}
		heartbeat := "nie"
		if r[13] != 65535 {
			heartbeat = fmt.Sprintf("vor %d s", r[13])
		}
		log.Printf("LZ %5d | V%d.%d | %-12s | LPC %-20s aktiv %d %5d W | LPP %-20s aktiv %d %5d W | Last %5.0f W, PV %5.0f W, NAP %6.0f W | Heartbeat %s",
			r[0], r[1], r[14], text(verbindungen, r[3]),
			text(zustaende, r[2]), r[6], u32(r, 4),
			text(zustaende, r[17]), r[21], u32(r, 19),
			verbrauch, erzeugung, pNap, heartbeat)
	}
}

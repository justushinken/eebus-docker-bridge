// SPS-Simulator fuer lokale Tests: spielt die CODESYS-Seite.
// Liest jede Sekunde die Input-Register der Bruecke, zeigt sie dekodiert an
// und schreibt SPS-Lebenszeichen und Nennleistung in die Holding-Register.
//
//	go run ./testwerkzeuge/spssimulator -url tcp://127.0.0.1:5502 -nennleistung 22000
package main

import (
	"flag"
	"fmt"
	"log"
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

func main() {
	url := flag.String("url", "tcp://127.0.0.1:5502", "Modbus-Server der Bruecke")
	nennleistung := flag.Uint("nennleistung", 0, "an die Bruecke gemeldete Nennleistung in W, 0 = nicht schreiben")
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

	var lebenszeichen uint16
	for range time.Tick(time.Second) {
		lebenszeichen++
		werte := []uint16{lebenszeichen}
		if *nennleistung > 0 {
			werte = append(werte, uint16(*nennleistung>>16), uint16(*nennleistung))
		}
		if err := client.WriteRegisters(0, werte); err != nil {
			log.Printf("Holding schreiben: %v", err)
		}

		r, err := client.ReadRegisters(0, 14, modbus.INPUT_REGISTER)
		if err != nil {
			log.Printf("Input lesen: %v", err)
			continue
		}
		heartbeat := "nie"
		if r[13] != 65535 {
			heartbeat = fmt.Sprintf("vor %d s", r[13])
		}
		log.Printf("LZ %5d | V%d | %-20s | %-12s | aktiv %d, Grenze %5d W | Netz %5d W, Rest %3d s | Failsafe %5d W | Heartbeat %s",
			r[0], r[1], text(zustaende, r[2]), text(verbindungen, r[3]),
			r[6], u32(r, 4), u32(r, 7), u32(r, 9), u32(r, 11), heartbeat)
	}
}

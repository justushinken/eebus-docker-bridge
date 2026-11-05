package main

import (
	"testing"

	"eebus-gateway/internal/gemeinsam"

	shipapi "github.com/enbility/ship-go/api"
)

func umgebung(werte map[string]string) gemeinsam.Umgebung {
	return func(name string) (string, bool) {
		w, ok := werte[name]
		return w, ok
	}
}

func TestInstanzVorgaben(t *testing.T) {
	faelle := []struct {
		instanz            string
		eebus              int
		modbus, web, serie string
	}{
		{"", 4712, "tcp://127.0.0.1:5502", ":8090", "SN"},
		{"1", 4712, "tcp://127.0.0.1:5502", ":8090", "SN"},
		{"2", 4713, "tcp://127.0.0.1:5503", ":8092", "SN"},
		{"4", 4715, "tcp://127.0.0.1:5505", ":8094", "SN"},
	}
	for _, f := range faelle {
		konf, err := leseKonfiguration(umgebung(map[string]string{"GATEWAY_INSTANZ": f.instanz, "GERAET_SERIENNUMMER": "SN"}))
		if err != nil {
			t.Fatalf("Instanz %q: %v", f.instanz, err)
		}
		if konf.EebusPort != f.eebus || konf.ModbusUrl != f.modbus || konf.WebAdresse != f.web || konf.Seriennummer != f.serie {
			t.Errorf("Instanz %q: %d %s %s %s", f.instanz, konf.EebusPort, konf.ModbusUrl, konf.WebAdresse, konf.Seriennummer)
		}
	}
}

// Ausdrueckliche Werte haben Vorrang vor den Vorgaben der Instanz.
func TestInstanzMitEigenenWerten(t *testing.T) {
	konf, err := leseKonfiguration(umgebung(map[string]string{
		"GATEWAY_INSTANZ": "3", "GERAET_SERIENNUMMER": "X", "EEBUS_PORT": "5000",
		"MODBUS_URL": "tcp://0.0.0.0:6000", "WEB_ADRESSE": ":9000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if konf.EebusPort != 5000 || konf.ModbusUrl != "tcp://0.0.0.0:6000" || konf.WebAdresse != ":9000" || konf.Seriennummer != "X" {
		t.Errorf("eigene Werte ueberschrieben: %+v", konf)
	}
}

// Ohne GERAET_SERIENNUMMER: MAC, ab Instanz 2 mit Nummer, auch in der SHIP-ID.
func TestInstanzSeriennummerAusMac(t *testing.T) {
	mac := gemeinsam.GeraeteKennung()
	if mac == "" {
		t.Skip("keine MAC-Adresse im Testsystem")
	}
	konf, err := leseKonfiguration(umgebung(map[string]string{"GATEWAY_INSTANZ": "2"}))
	if err != nil {
		t.Fatal(err)
	}
	if konf.Seriennummer != mac+"-2" || konf.ShipId != "Demo-EEBUS-Gateway-"+mac+"-2" {
		t.Errorf("Seriennummer %q, SHIP-ID %q", konf.Seriennummer, konf.ShipId)
	}
}

func TestInstanzUngueltig(t *testing.T) {
	for _, wert := range []string{"0", "21", "zwei", "-1"} {
		if _, err := leseKonfiguration(umgebung(map[string]string{"GATEWAY_INSTANZ": wert, "GERAET_SERIENNUMMER": "SN"})); err == nil {
			t.Errorf("GATEWAY_INSTANZ=%s angenommen", wert)
		}
	}
}

func TestDoppelteKennung(t *testing.T) {
	eintraege := []shipapi.RemoteMdnsService{
		{Name: "Demo-EEBUS-Gateway-AABB", Ski: "1", ShipID: "andere"},
		{Name: "Demo-EEBUS-Gateway-AABB-2", Ski: "2", ShipID: "Demo-EEBUS-Gateway-AABB-2"},
		{Name: "Steuerbox", Ski: "3", ShipID: "eigene-ship-id"},
	}
	doppelt := doppelteKennung(eintraege, "Demo-EEBUS-Gateway-AABB", "eigene-ship-id")
	if len(doppelt) != 2 || doppelt[0].Ski != "1" || doppelt[1].Ski != "3" {
		t.Errorf("erwartet SKI 1 und 3, erhalten %+v", doppelt)
	}
	if len(doppelteKennung(eintraege, "", "")) != 0 {
		t.Error("ohne eigene Kennung darf nichts doppelt sein")
	}
}

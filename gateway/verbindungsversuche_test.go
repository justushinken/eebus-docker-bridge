package main

import "testing"

// Meldungen so, wie ship-go a84426bc3810 sie an den Logger gibt (fmt.Sprint
// setzt zwischen zwei Strings kein Leerzeichen, daher "failed:dial ...").
const (
	skiBox = "d4e4c4aeb6082d84bc5b09ecd679e0edd6241a5c"
	ipBox  = "192.168.1.100"
)

func TestAusgehendPortZu(t *testing.T) {
	var v Verbindungsversuche
	v.EebusMeldung("initiating connection to " + skiBox + " at GSWIC026090004.local.:51555/ship/")
	if v.EebusMeldung("connection to " + skiBox + " at GSWIC026090004.local. failed:dial tcp: lookup GSWIC026090004.local. on [::1]:53: read udp [::1]:40000->[::1]:53: read: connection refused") {
		t.Error("Lookup-Fehler ueber den mDNS-Namen soll nicht auf stdout")
	}
	if aus, _ := v.Stand(); aus != nil {
		t.Errorf("Versuch ueber den mDNS-Namen soll nicht zaehlen: %+v", aus)
	}
	for i := 0; i < 3; i++ {
		v.EebusMeldung("initiating connection to " + skiBox + " at " + ipBox + ":51555/ship/")
		v.EebusMeldung("connection to " + skiBox + " at " + ipBox + " failed:dial tcp " + ipBox + ":51555: connect: connection refused")
	}
	aus, _ := v.Stand()
	if aus == nil || aus.Ergebnis != ErgebnisPortZu || aus.Adresse != ipBox+":51555" || aus.Anzahl != 3 || aus.Ski != skiBox {
		t.Fatalf("ausgehend: %+v", aus)
	}

	// Neuer Port nach Neustart der Steuerbox: neue Zaehlung, neue Meldung
	v.EebusMeldung("initiating connection to " + skiBox + " at " + ipBox + ":54375/ship/")
	if aus, _ = v.Stand(); aus.Ergebnis != ErgebnisAufbau || aus.Anzahl != 1 {
		t.Fatalf("Aufbau: %+v", aus)
	}
	v.Verbunden()
	if aus, _ = v.Stand(); aus.Ergebnis != ErgebnisVerbunden {
		t.Fatalf("verbunden: %+v", aus)
	}
}

func TestKlassifiziere(t *testing.T) {
	for fehler, erwartet := range map[string]Ergebnis{
		"dial tcp 192.168.1.100:51555: connect: connection refused": ErgebnisPortZu,
		"dial tcp 192.168.1.100:51555: i/o timeout":                 ErgebnisKeineAntwort,
		"dial tcp 192.168.1.100:51555: connect: no route to host":   ErgebnisUnerreichbar,
		"dial tcp 10.0.0.1:4712: connect: network is unreachable":   ErgebnisUnerreichbar,
		"remote error: tls: bad certificate":                        ErgebnisTls,
		"certificate validation failed for d4e4: SKI mismatch":      ErgebnisTls,
		"EOF":                      ErgebnisTls,
		"websocket: bad handshake": ErgebnisFehler,
	} {
		if e := klassifiziere(fehler); e != erwartet {
			t.Errorf("%q: %s, erwartet %s", fehler, e, erwartet)
		}
	}
}

func TestEingehendTls(t *testing.T) {
	var v Verbindungsversuche
	text, behalten := v.BeobachteLog("http: TLS handshake error from 192.168.1.20:54321: EOF")
	if !behalten || text == "" {
		t.Fatal("erster TLS-Fehler soll ins Ereignisprotokoll")
	}
	if _, behalten = v.BeobachteLog("http: TLS handshake error from 192.168.1.20:54400: EOF"); behalten {
		t.Error("Wiederholung von derselben IP soll nicht ins Ereignisprotokoll")
	}
	_, ein := v.Stand()
	if ein == nil || ein.Adresse != "192.168.1.20" || ein.Anzahl != 2 || ein.Ergebnis != ErgebnisTls {
		t.Fatalf("eingehend: %+v", ein)
	}
	if text, behalten = v.BeobachteLog("Steuerbox verbunden: x"); !behalten || text != "Steuerbox verbunden: x" {
		t.Error("andere Zeilen unveraendert")
	}

	v.EebusMeldung("incoming connection request from" + skiBox)
	if _, ein = v.Stand(); ein.Ski != skiBox || ein.Ergebnis != ErgebnisTlsOk {
		t.Fatalf("eingehend mit SKI: %+v", ein)
	}
}

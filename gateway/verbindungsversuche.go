package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// Verbindungsversuche unterhalb von SHIP: Erreicht das Gateway die Steuerbox,
// und kommt die Steuerbox beim Gateway an? ship-go meldet fehlgeschlagene
// Verbindungen nur in seinem Log, nicht ueber Callbacks. Das Gateway liest
// deshalb dessen Meldungen mit (EebusLog, auch ohne EEBUS_DEBUG) und dazu die
// TLS-Fehler des HTTP-Servers von ship-go aus dem Standard-Log.
//
// Die Texte stammen aus ship-go a84426bc3810 (hub/hub_connections_client.go,
// hub/hub_connections_server.go) und net/http. Nach einem Update von ship-go
// verbindungsversuche_test.go gegen die neuen Texte pruefen.

const (
	meldungAufbau    = "initiating connection to " // <ski> at <host>:<port><pfad>
	meldungFehler    = "connection to "            // <ski> at <host> failed:<fehler>
	meldungEingehend = "incoming connection request from"
	meldungTls       = "http: TLS handshake error from " // <ip>:<port>: <fehler>
)

// Ergebnis eines Versuchs, Klartext im UI.
type Ergebnis string

const (
	ErgebnisAufbau       Ergebnis = "aufbau"       // ausgehend gestartet, noch kein Ergebnis
	ErgebnisVerbunden    Ergebnis = "verbunden"    // SHIP-Verbindung steht
	ErgebnisTlsOk        Ergebnis = "tlsOk"        // eingehend: TLS fertig, SKI bekannt
	ErgebnisPortZu       Ergebnis = "portZu"       // TCP-RST: Geraet erreichbar, Port geschlossen
	ErgebnisKeineAntwort Ergebnis = "keineAntwort" // Zeitueberschreitung
	ErgebnisUnerreichbar Ergebnis = "unerreichbar" // keine ARP-Antwort
	ErgebnisKeinNetz     Ergebnis = "keinNetz"     // keine eigene Adresse im Netz des Ziels
	ErgebnisTls          Ergebnis = "tls"          // TLS- oder Zertifikatsfehler
	ErgebnisFehler       Ergebnis = "fehler"       // sonstiges
)

var ergebnisTexte = map[Ergebnis]string{
	ErgebnisPortZu:       "Port geschlossen (TCP-RST)",
	ErgebnisKeineAntwort: "keine Antwort (Zeitueberschreitung)",
	ErgebnisUnerreichbar: "nicht erreichbar (keine Antwort auf ARP)",
	ErgebnisKeinNetz:     "keine eigene Adresse in diesem Netz",
	ErgebnisTls:          "TLS fehlgeschlagen",
	ErgebnisFehler:       "fehlgeschlagen",
}

// klassifiziere ordnet eine Fehlermeldung von net/tls/websocket ein.
func klassifiziere(fehler string) Ergebnis {
	f := strings.ToLower(fehler)
	switch {
	case strings.Contains(f, "connection refused"), strings.Contains(f, "connection reset"):
		return ErgebnisPortZu
	case strings.Contains(f, "i/o timeout"), strings.Contains(f, "deadline exceeded"), strings.Contains(f, "timed out"):
		return ErgebnisKeineAntwort
	case strings.Contains(f, "network is unreachable"):
		return ErgebnisKeinNetz
	case strings.Contains(f, "no route to host"), strings.Contains(f, "host is unreachable"):
		return ErgebnisUnerreichbar
	case strings.Contains(f, "tls"), strings.Contains(f, "certificate"), strings.Contains(f, "x509"), f == "eof":
		return ErgebnisTls
	}
	return ErgebnisFehler
}

type Versuch struct {
	Zeit     time.Time
	Adresse  string // ausgehend Ziel host:port, eingehend Quelle (IP, nur bei TLS-Fehlern bekannt)
	Ski      string
	Ergebnis Ergebnis
	Fehler   string // Originalmeldung
	Anzahl   int    // Versuche an derselben Adresse hintereinander
}

// Verbindungsversuche merkt sich den letzten Versuch je Richtung. Eigene
// Sperre statt mu: Wird aus dem Log heraus aufgerufen, auch waehrend andere
// Teile mu halten. Unter v.mu nie loggen (das Standard-Log ruft beobachteLog
// unter seiner eigenen Sperre auf).
type Verbindungsversuche struct {
	mu                sync.Mutex
	ausgehend         *Versuch
	eingehend         *Versuch
	gemeldetAusgehend string // zuletzt protokolliert, nur bei Wechsel neu
	gemeldetEingehend string
}

// EebusMeldung wertet eine Meldung von ship-go aus. Liefert false fuer
// Meldungen, die auch mit EEBUS_DEBUG nicht auf stdout gehoeren.
func (v *Verbindungsversuche) EebusMeldung(text string) bool {
	jetzt := time.Now()
	switch {
	case strings.HasPrefix(text, meldungAufbau):
		ski, ziel, ok := strings.Cut(strings.TrimPrefix(text, meldungAufbau), " at ")
		if !ok {
			return true
		}
		if i := strings.Index(ziel, "/"); i >= 0 {
			ziel = ziel[:i]
		}
		if host, _, err := net.SplitHostPort(ziel); err == nil && net.ParseIP(host) == nil {
			return true // erst ueber den Namen, gleich danach ueber die IP
		}
		v.mu.Lock()
		v.ausgehend = naechster(v.ausgehend, Versuch{Zeit: jetzt, Adresse: ziel, Ski: ski, Ergebnis: ErgebnisAufbau})
		v.mu.Unlock()

	case strings.HasPrefix(text, meldungFehler) && strings.Contains(text, " failed:"):
		kopf, fehler, _ := strings.Cut(strings.TrimPrefix(text, meldungFehler), " failed:")
		ski, host, _ := strings.Cut(kopf, " at ")
		fehler = strings.TrimSpace(fehler)
		if net.ParseIP(host) == nil {
			// Name aus mDNS (<SHIP-ID>.local.) ist im Container per DNS nicht
			// aufloesbar. ship-go versucht danach die IP: kein Fehler.
			return !strings.Contains(fehler, "lookup ")
		}
		ergebnis := klassifiziere(fehler)
		v.mu.Lock()
		adresse := host
		if v.ausgehend != nil && strings.HasPrefix(v.ausgehend.Adresse, host+":") {
			adresse = v.ausgehend.Adresse // mit Port
		}
		v.ausgehend = naechster(v.ausgehend, Versuch{Zeit: jetzt, Adresse: adresse, Ski: ski, Ergebnis: ergebnis, Fehler: fehler})
		meldung := v.wechsel(&v.gemeldetAusgehend, adresse+" "+string(ergebnis),
			fmt.Sprintf("Verbindung zur Steuerbox %s: %s (%s)", adresse, ergebnisTexte[ergebnis], fehler))
		v.mu.Unlock()
		if meldung != "" {
			log.Print(meldung)
		}

	case strings.HasPrefix(text, meldungEingehend):
		ski := strings.TrimSpace(strings.TrimPrefix(text, meldungEingehend))
		v.mu.Lock()
		v.eingehend = naechster(v.eingehend, Versuch{Zeit: jetzt, Ski: ski, Ergebnis: ErgebnisTlsOk})
		v.gemeldetEingehend = ""
		v.mu.Unlock()
	}
	return true
}

// BeobachteLog sieht jede Zeile des Standard-Logs (ohne Zeitstempel), bevor
// sie ins Ereignisprotokoll kommt. TLS-Fehler eingehender Verbindungen
// erscheinen dort auf Deutsch und nur bei Wechsel von Quelle oder Grund.
// Laeuft unter der Sperre des Standard-Logs: hier nicht loggen.
func (v *Verbindungsversuche) BeobachteLog(zeile string) (string, bool) {
	rest, ok := strings.CutPrefix(zeile, meldungTls)
	if !ok {
		return zeile, true
	}
	// "192.168.1.20:54321: remote error: tls: bad certificate"
	quelle, fehler, _ := strings.Cut(rest, ": ")
	ip, _, err := net.SplitHostPort(quelle)
	if err != nil {
		ip = quelle
	}
	ergebnis := klassifiziere(fehler)
	v.mu.Lock()
	defer v.mu.Unlock()
	v.eingehend = naechster(v.eingehend, Versuch{Zeit: time.Now(), Adresse: ip, Ergebnis: ergebnis, Fehler: fehler})
	grund := fehler
	if fehler == "EOF" {
		grund = "Verbindung ohne TLS beendet, z. B. Portpruefung"
	}
	meldung := v.wechsel(&v.gemeldetEingehend, ip+" "+fehler,
		fmt.Sprintf("Eingehende Verbindung von %s: TLS fehlgeschlagen (%s)", ip, grund))
	return meldung, meldung != ""
}

// Verbunden: Die Steuerbox ist per SHIP verbunden. Ein laufender ausgehender
// Versuch war damit erfolgreich, ein spaeterer Fehler wird wieder gemeldet.
func (v *Verbindungsversuche) Verbunden() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.ausgehend != nil && v.ausgehend.Ergebnis == ErgebnisAufbau {
		v.ausgehend.Ergebnis = ErgebnisVerbunden
	}
	v.gemeldetAusgehend, v.gemeldetEingehend = "", ""
}

// naechster zaehlt die Versuche an derselben Adresse. Das Ergebnis eines
// laufenden Aufbaus ist kein eigener Versuch.
func naechster(alt *Versuch, neu Versuch) *Versuch {
	neu.Anzahl = 1
	if alt != nil && alt.Adresse == neu.Adresse && alt.Ski == neu.Ski {
		neu.Anzahl = alt.Anzahl
		if neu.Ergebnis == ErgebnisAufbau || alt.Ergebnis != ErgebnisAufbau {
			neu.Anzahl++
		}
	}
	return &neu
}

// wechsel liefert die Meldung nur, wenn sich der Schluessel seit der letzten
// Meldung geaendert hat. Aufruf unter v.mu.
func (v *Verbindungsversuche) wechsel(gemeldet *string, schluessel, meldung string) string {
	if *gemeldet == schluessel {
		return ""
	}
	*gemeldet = schluessel
	return meldung
}

// Stand liefert Kopien der letzten Versuche.
func (v *Verbindungsversuche) Stand() (ausgehend, eingehend *Versuch) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.ausgehend != nil {
		kopie := *v.ausgehend
		ausgehend = &kopie
	}
	if v.eingehend != nil {
		kopie := *v.eingehend
		eingehend = &kopie
	}
	return ausgehend, eingehend
}

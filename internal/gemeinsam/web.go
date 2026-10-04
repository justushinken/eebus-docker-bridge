package gemeinsam

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net"
	"net/http"
	"time"
)

// StarteWebUi startet den Webserver hinter HTTP Basic Auth. Das UI ist
// optional: Ohne Adresse oder Passwort, oder wenn der Port belegt ist, wird es
// nur protokolliert und nicht gestartet. Liefert eine Funktion zum Beenden.
func StarteWebUi(adresse, benutzer, passwort string, handler http.Handler) (beenden func()) {
	beenden = func() {}
	switch {
	case adresse == "":
		log.Printf("Web-UI deaktiviert: WEB_ADRESSE leer")
		return
	case passwort == "":
		log.Printf("Web-UI deaktiviert: WEB_PASSWORT nicht gesetzt")
		return
	}

	// Listen vorab, damit ein belegter Port sofort als Fehler auffaellt.
	lauscher, err := net.Listen("tcp", adresse)
	if err != nil {
		log.Printf("Web-UI nicht gestartet: %v", err)
		return
	}
	server := &http.Server{
		Handler:           mitAnmeldung(benutzer, passwort, handler),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go server.Serve(lauscher)
	log.Printf("Web-UI auf %s", adresse)

	return func() {
		ctx, abbrechen := context.WithTimeout(context.Background(), 2*time.Second)
		defer abbrechen()
		server.Shutdown(ctx)
	}
}

// mitAnmeldung schuetzt alle Pfade per HTTP Basic Auth. Ohne HTTPS ist das
// Passwort im LAN mitlesbar: Schutz vor zufaelligem Zugriff, nicht vor Angriffen.
func mitAnmeldung(benutzer, passwort string, weiter http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, p, ok := r.BasicAuth()
		benutzerOk := subtle.ConstantTimeCompare([]byte(b), []byte(benutzer)) == 1
		passwortOk := subtle.ConstantTimeCompare([]byte(p), []byte(passwort)) == 1
		if !ok || !benutzerOk || !passwortOk {
			w.Header().Set("WWW-Authenticate", `Basic realm="EEBUS", charset="UTF-8"`)
			http.Error(w, "Anmeldung erforderlich", http.StatusUnauthorized)
			return
		}
		weiter.ServeHTTP(w, r)
	})
}

// SeiteAusliefern liefert eine eingebettete HTML-Seite.
func SeiteAusliefern(html []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(html)
	}
}

// SchreibeJson antwortet mit JSON und verhindert Caching.
func SchreibeJson(w http.ResponseWriter, status int, wert any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(wert)
}

// Antwort auf eine Aktion aus dem Web-UI.
type Antwort struct {
	Text   string `json:"text,omitempty"`
	Fehler string `json:"fehler,omitempty"`
}

// ErrVerboten laesst Aktion mit 403 statt 409 antworten.
var ErrVerboten = errors.New("verboten")

// Aktion liest den JSON-Rumpf, fuehrt die Aktion aus und antwortet mit Text
// oder Fehler. Nur Content-Type application/json: Ein Formular einer fremden
// Seite kann das nicht senden, das schuetzt bei gespeicherter Anmeldung.
func Aktion[T any](ausfuehren func(T) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); typ != "application/json" {
			SchreibeJson(w, http.StatusUnsupportedMediaType, Antwort{Fehler: "Content-Type application/json erwartet"})
			return
		}
		var daten T
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&daten); err != nil {
			SchreibeJson(w, http.StatusBadRequest, Antwort{Fehler: "ungueltige Anfrage: " + err.Error()})
			return
		}
		text, err := ausfuehren(daten)
		switch {
		case errors.Is(err, ErrVerboten):
			SchreibeJson(w, http.StatusForbidden, Antwort{Fehler: err.Error()})
		case err != nil:
			SchreibeJson(w, http.StatusConflict, Antwort{Fehler: err.Error()})
		default:
			SchreibeJson(w, http.StatusOK, Antwort{Text: text})
		}
	}
}

package gemeinsam

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
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

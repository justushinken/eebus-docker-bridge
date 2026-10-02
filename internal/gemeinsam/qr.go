package gemeinsam

import (
	"net/http"

	"github.com/skip2/go-qrcode"
)

// QrBild liefert den Text als QR-Code (PNG). Mittlere Fehlerkorrektur, wie es
// die EEBUS-Installationsspezifikation empfiehlt. Serverseitig erzeugt, damit
// das UI auch ohne Internetzugang funktioniert.
func QrBild(text func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inhalt := text()
		if inhalt == "" {
			http.NotFound(w, r)
			return
		}
		png, err := qrcode.Encode(inhalt, qrcode.Medium, 320)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(png)
	}
}

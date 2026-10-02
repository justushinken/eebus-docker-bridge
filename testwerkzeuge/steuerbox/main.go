// Test-Steuerbox
//
// Simuliert die FNN-Steuerbox (Energy Guard) fuer Tests der EEBUS-LPC-Bruecke
// ohne echtes Smart Meter Gateway. Ueber das Web-UI lassen sich Grenzen und
// Failsafe-Werte senden, der Heartbeat anhalten und Verbindungsabbrueche
// simulieren. Nur fuer Tests, nicht fuer den Produktivbetrieb.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	eglpc "github.com/enbility/eebus-go/usecases/eg/lpc"
	"github.com/enbility/spine-go/model"
)

type Konfiguration struct {
	EebusPort        int
	Datenverzeichnis string
	HeartbeatTimeout time.Duration
	WebAdresse       string
	WebBenutzer      string
	WebPasswort      string
}

func leseKonfiguration() Konfiguration {
	return Konfiguration{
		EebusPort:        gemeinsam.EnvGanzzahl("EEBUS_PORT", 4713),
		Datenverzeichnis: gemeinsam.EnvText("DATENVERZEICHNIS", "/data"),
		// Heartbeat wird alle (Timeout - 2 s) gesendet. Kurz gewaehlt, damit die
		// Bruecke nach dem Verbinden schnell aus "Init" kommt.
		HeartbeatTimeout: gemeinsam.EnvDauer("HEARTBEAT_TIMEOUT", 10*time.Second),
		WebAdresse:       gemeinsam.EnvTextLeerErlaubt("WEB_ADRESSE", ":8091"),
		WebBenutzer:      gemeinsam.EnvText("WEB_BENUTZER", "admin"),
		WebPasswort:      gemeinsam.EnvText("WEB_PASSWORT", ""),
	}
}

func main() {
	protokoll := gemeinsam.ProtokolliereLog(200)
	konf := leseKonfiguration()

	// Ohne Web-UI laesst sich die Steuerbox nicht bedienen.
	if konf.WebAdresse == "" || konf.WebPasswort == "" {
		log.Fatalf("Die Test-Steuerbox wird ueber ihr Web-UI bedient: WEB_PASSWORT setzen und WEB_ADRESSE nicht leeren")
	}

	zertifikat, eigenerSki, err := gemeinsam.LadeOderErzeugeZertifikat(
		konf.Datenverzeichnis, "Test-Steuerbox", "EEBUS-Test-Steuerbox")
	if err != nil {
		log.Fatalf("Zertifikat: %v", err)
	}
	log.Printf("Eigener SKI: %s (bei der Bruecke als EEBUS_REMOTE_SKI eintragen)", eigenerSki)

	konfiguration, err := api.NewConfiguration(
		"Test", "Test", "Steuerbox-Simulator", "0001",
		model.DeviceTypeTypeElectricitySupplySystem,
		[]model.EntityTypeType{model.EntityTypeTypeGridGuard},
		konf.EebusPort, zertifikat, konf.HeartbeatTimeout)
	if err != nil {
		log.Fatalf("EEBUS-Konfiguration: %v", err)
	}
	konfiguration.SetAlternateIdentifier("Test-Steuerbox-Simulator-0001")

	steuerbox := NeueSteuerbox(konf, eigenerSki)
	dienst := service.NewService(konfiguration, steuerbox)
	if err := dienst.Setup(); err != nil {
		log.Fatalf("EEBUS-Dienst einrichten: %v", err)
	}
	steuerbox.dienst = dienst
	steuerbox.entitaet = dienst.LocalDevice().EntityForType(model.EntityTypeTypeGridGuard)
	steuerbox.lpc = eglpc.NewLPC(steuerbox.entitaet, steuerbox.LpcEreignis)
	dienst.AddUseCase(steuerbox.lpc)

	if ski := steuerbox.Gekoppelt(); ski != "" {
		log.Printf("Gekoppelt mit Bruecke %s", ski)
		dienst.RegisterRemoteSKI(ski)
	} else {
		log.Printf("Noch keine Bruecke gekoppelt: im Web-UI unter \"Kopplung\" auswaehlen")
	}

	webBeenden := gemeinsam.StarteWebUi(konf.WebAdresse, konf.WebBenutzer, konf.WebPasswort, webHandler(steuerbox, protokoll))
	defer webBeenden()

	dienst.Start()
	defer dienst.Shutdown()

	signale := make(chan os.Signal, 1)
	signal.Notify(signale, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("Beende nach Signal %v", <-signale)
}

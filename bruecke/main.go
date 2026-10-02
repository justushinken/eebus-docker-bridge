// EEBUS-LPC-Bruecke
//
// Laeuft als Docker-Container auf dem WAGO PFC200. Tritt gegenueber der
// FNN-Steuerbox (Energy Guard) als "Controllable System" auf, nimmt ueber den
// Use Case LPC Leistungsgrenzen nach Paragraf 14a EnWG entgegen und stellt sie
// der CODESYS-Applikation als Modbus-TCP-Server auf 127.0.0.1 bereit.
//
// Geschrieben gegen eebus-go v0.7.x. Bei anderer Version Signaturen pruefen.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	"github.com/enbility/spine-go/model"
)

// Konfiguration kommt vollstaendig aus Umgebungsvariablen (docker run -e ...).
type Konfiguration struct {
	EebusPort            int
	ModbusUrl            string
	Datenverzeichnis     string
	RemoteSki            string
	Hersteller           string
	Marke                string
	Modell               string
	Seriennummer         string
	FailsafeGrenzeW      float64
	FailsafeMindestdauer time.Duration
	NennleistungMaxW     float64
	WebAdresse           string
	WebBenutzer          string
	WebPasswort          string
}

func leseKonfiguration() Konfiguration {
	return Konfiguration{
		EebusPort:            gemeinsam.EnvGanzzahl("EEBUS_PORT", 4712),
		ModbusUrl:            gemeinsam.EnvText("MODBUS_URL", "tcp://127.0.0.1:5502"),
		Datenverzeichnis:     gemeinsam.EnvText("DATENVERZEICHNIS", "/data"),
		RemoteSki:            gemeinsam.EnvText("EEBUS_REMOTE_SKI", ""),
		Hersteller:           gemeinsam.EnvText("GERAET_HERSTELLER", "Demo"),
		Marke:                gemeinsam.EnvText("GERAET_MARKE", "Demo"),
		Modell:               gemeinsam.EnvText("GERAET_MODELL", "PFC200-LPC-Bruecke"),
		Seriennummer:         gemeinsam.EnvText("GERAET_SERIENNUMMER", "0001"),
		FailsafeGrenzeW:      gemeinsam.EnvKommazahl("FAILSAFE_GRENZE_W", 4200),
		FailsafeMindestdauer: gemeinsam.EnvDauer("FAILSAFE_MINDESTDAUER", 2*time.Hour),
		NennleistungMaxW:     gemeinsam.EnvKommazahl("NENNLEISTUNG_MAX_W", 11000),
		WebAdresse:           gemeinsam.EnvTextLeerErlaubt("WEB_ADRESSE", ":8090"),
		WebBenutzer:          gemeinsam.EnvText("WEB_BENUTZER", "admin"),
		WebPasswort:          gemeinsam.EnvText("WEB_PASSWORT", ""),
	}
}

func main() {
	// Alle Log-Meldungen zusaetzlich fuer das Status-UI vorhalten.
	protokoll := gemeinsam.ProtokolliereLog(100)

	konf := leseKonfiguration()

	zertifikat, eigenerSki, err := gemeinsam.LadeOderErzeugeZertifikat(
		konf.Datenverzeichnis, "LPC-Bruecke", "EEBUS-LPC-Bruecke-"+konf.Seriennummer)
	if err != nil {
		log.Fatalf("Zertifikat: %v", err)
	}
	// Diesen SKI traegt der Messstellenbetreiber in der Steuerbox ein.
	log.Printf("Eigener SKI: %s", eigenerSki)

	konfiguration, err := api.NewConfiguration(
		konf.Hersteller, konf.Marke, konf.Modell, konf.Seriennummer,
		model.DeviceTypeTypeEnergyManagementSystem,
		[]model.EntityTypeType{model.EntityTypeTypeCEM},
		konf.EebusPort, zertifikat, 4*time.Second)
	if err != nil {
		log.Fatalf("EEBUS-Konfiguration: %v", err)
	}
	konfiguration.SetAlternateIdentifier(fmt.Sprintf("%s-%s-%s", konf.Marke, konf.Modell, konf.Seriennummer))

	bruecke := NeueBruecke(konf, eigenerSki)
	dienst := service.NewService(konfiguration, bruecke)
	if err := dienst.Setup(); err != nil {
		log.Fatalf("EEBUS-Dienst einrichten: %v", err)
	}

	lokaleEntitaet := dienst.LocalDevice().EntityForType(model.EntityTypeTypeCEM)
	bruecke.lpc = cslpc.NewLPC(lokaleEntitaet, bruecke.LpcEreignis)
	dienst.AddUseCase(bruecke.lpc)
	bruecke.SetzeStartwerte()

	if konf.RemoteSki != "" {
		dienst.RegisterRemoteSKI(konf.RemoteSki)
	} else {
		log.Printf("EEBUS_REMOTE_SKI nicht gesetzt: keine Steuerbox gekoppelt, gefundene Dienste werden nur protokolliert")
	}

	modbusServer, err := starteModbusServer(konf.ModbusUrl, bruecke)
	if err != nil {
		log.Fatalf("Modbus-Server: %v", err)
	}
	defer modbusServer.Stop()

	webBeenden := gemeinsam.StarteWebUi(konf.WebAdresse, konf.WebBenutzer, konf.WebPasswort, webHandler(bruecke, protokoll))
	defer webBeenden()

	dienst.Start()
	defer dienst.Shutdown()

	takt := time.NewTicker(time.Second)
	defer takt.Stop()
	signale := make(chan os.Signal, 1)
	signal.Notify(signale, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case jetzt := <-takt.C:
			bruecke.Takt(jetzt)
		case sig := <-signale:
			log.Printf("Beende nach Signal %v", sig)
			return
		}
	}
}

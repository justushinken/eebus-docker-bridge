// EEBUS-LPC-Bruecke
//
// Laeuft als Docker-Container auf dem WAGO PFC200. Tritt gegenueber der
// FNN-Steuerbox (Energy Guard) als "Controllable System" auf, nimmt ueber den
// Use Case LPC Leistungsgrenzen nach Paragraf 14a EnWG entgegen und stellt sie
// der CODESYS-Applikation als Modbus-TCP-Server auf 127.0.0.1 bereit.
//
// Geschrieben gegen den Entwicklungsstand von eebus-go (nach v0.7.0, mit
// SHIP Pairing Service). Bei anderer Version Signaturen pruefen.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	shipapi "github.com/enbility/ship-go/api"
	"github.com/enbility/spine-go/model"
)

// Konfiguration kommt vollstaendig aus Umgebungsvariablen (docker run -e ...).
type Konfiguration struct {
	EebusPort            int
	ModbusUrl            string
	Datenverzeichnis     string
	RemoteSki            string // SKI-Verfahren: SKI der Steuerbox, leer = aus
	PairingService       bool   // SHIP Pairing Service: Steuerbox mit Secret automatisch vertrauen
	ShipId               string
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

func leseKonfiguration() (Konfiguration, error) {
	konf := Konfiguration{
		EebusPort:            gemeinsam.EnvGanzzahl("EEBUS_PORT", 4712),
		ModbusUrl:            gemeinsam.EnvText("MODBUS_URL", "tcp://127.0.0.1:5502"),
		Datenverzeichnis:     gemeinsam.EnvText("DATENVERZEICHNIS", "/data"),
		RemoteSki:            gemeinsam.EnvText("EEBUS_REMOTE_SKI", ""),
		PairingService:       gemeinsam.EnvText("EEBUS_PAIRING_SERVICE", "an") != "aus",
		Hersteller:           gemeinsam.EnvText("GERAET_HERSTELLER", "Demo"),
		Marke:                gemeinsam.EnvText("GERAET_MARKE", "Demo"),
		Modell:               gemeinsam.EnvText("GERAET_MODELL", "PFC200-LPC-Bruecke"),
		Seriennummer:         gemeinsam.EnvText("GERAET_SERIENNUMMER", ""),
		FailsafeGrenzeW:      gemeinsam.EnvKommazahl("FAILSAFE_GRENZE_W", 4200),
		FailsafeMindestdauer: gemeinsam.EnvDauer("FAILSAFE_MINDESTDAUER", 2*time.Hour),
		NennleistungMaxW:     gemeinsam.EnvKommazahl("NENNLEISTUNG_MAX_W", 11000),
		WebAdresse:           gemeinsam.EnvTextLeerErlaubt("WEB_ADRESSE", ":8090"),
		WebBenutzer:          gemeinsam.EnvText("WEB_BENUTZER", "admin"),
		WebPasswort:          gemeinsam.EnvText("WEB_PASSWORT", ""),
	}

	// Ohne Seriennummer die MAC-Adresse des PFC: weltweit eindeutig und auf
	// dem Typenschild ablesbar. Daraus ergibt sich auch die SHIP-ID.
	if konf.Seriennummer == "" {
		if konf.Seriennummer = gemeinsam.GeraeteKennung(); konf.Seriennummer == "" {
			return konf, fmt.Errorf("keine MAC-Adresse gefunden: GERAET_SERIENNUMMER setzen")
		}
	}
	konf.ShipId = gemeinsam.EnvText("SHIP_ID", fmt.Sprintf("%s-%s-%s", konf.Marke, konf.Modell, konf.Seriennummer))
	if err := gemeinsam.PruefeShipId(konf.ShipId); err != nil {
		return konf, err
	}
	return konf, nil
}

func main() {
	// Alle Log-Meldungen zusaetzlich fuer das Status-UI vorhalten.
	protokoll := gemeinsam.ProtokolliereLog(100)

	konf, err := leseKonfiguration()
	if err != nil {
		log.Fatalf("Konfiguration: %v", err)
	}

	zertifikat, eigenerSki, err := gemeinsam.LadeOderErzeugeZertifikat(
		konf.Datenverzeichnis, "LPC-Bruecke", "EEBUS-LPC-Bruecke-"+konf.Seriennummer)
	if err != nil {
		log.Fatalf("Zertifikat: %v", err)
	}
	// Diesen SKI traegt der Messstellenbetreiber in der Steuerbox ein.
	log.Printf("Eigener SKI: %s, SHIP-ID: %s", eigenerSki, konf.ShipId)

	// SHIP Pairing Service: Die Bruecke wartet als "Listener" auf eine Steuerbox,
	// die das Secret kennt. Verlauf gegen Wiederholungsangriffe im Volume.
	var pairingKonfig *shipapi.PairingConfig
	var secret shipapi.PairingSecret
	if konf.PairingService {
		if secret, err = gemeinsam.LadeOderErzeugeSecret(konf.Datenverzeichnis); err != nil {
			log.Fatalf("Pairing-Secret: %v", err)
		}
		pairingKonfig = shipapi.NewPairingConfig(shipapi.PairingModeListener, secret)
	}
	verlauf := gemeinsam.RingpufferDatei{Pfad: filepath.Join(konf.Datenverzeichnis, "pairing-verlauf.json")}

	konfiguration, err := api.NewConfiguration(
		konf.Hersteller, konf.Marke, konf.Modell, konf.Seriennummer,
		[]shipapi.DeviceCategoryType{shipapi.DeviceCategoryTypeEnergyManagementSystem},
		model.DeviceTypeTypeEnergyManagementSystem,
		[]model.EntityTypeType{model.EntityTypeTypeCEM},
		konf.EebusPort, zertifikat, 4*time.Second,
		pairingKonfig, verlauf)
	if err != nil {
		log.Fatalf("EEBUS-Konfiguration: %v", err)
	}
	konfiguration.SetAlternateIdentifier(konf.ShipId)

	if werte := LadeFailsafe(konf.Datenverzeichnis); werte != nil {
		konf.FailsafeGrenzeW, konf.FailsafeMindestdauer = werte.GrenzeW, werte.Mindestdauer
		log.Printf("Failsafe-Werte aus der letzten Vorgabe der Steuerbox: %.0f W, %v", werte.GrenzeW, werte.Mindestdauer)
	}

	pairingKopplung, err := gemeinsam.LadeKopplung(filepath.Join(konf.Datenverzeichnis, KopplungsdateiPairing))
	if err != nil {
		log.Printf("Gespeicherte Kopplung nicht lesbar: %v", err)
	}

	bruecke := NeueBruecke(konf, eigenerSki, pairingKopplung)
	dienst := service.NewService(konfiguration, bruecke)
	if err := dienst.Setup(); err != nil {
		log.Fatalf("EEBUS-Dienst einrichten: %v", err)
	}

	lokaleEntitaet := dienst.LocalDevice().EntityForType(model.EntityTypeTypeCEM)
	bruecke.lpc = cslpc.NewLPC(lokaleEntitaet, bruecke.LpcEreignis)
	if err := dienst.AddUseCase(bruecke.lpc); err != nil {
		log.Fatalf("Use Case LPC: %v", err)
	}
	bruecke.SetzeStartwerte()
	bruecke.SetzeKennung(dienst, secret)

	// Beide Kopplungsverfahren koennen gleichzeitig aktiv sein.
	if konf.RemoteSki != "" {
		log.Printf("SKI-Verfahren: Steuerbox mit SKI %s wird vertraut", konf.RemoteSki)
		dienst.RegisterRemoteService(shipapi.NewServiceIdentity(konf.RemoteSki, "", ""))
	}
	if pairingKopplung != nil {
		log.Printf("Pairing Service: gekoppelte Steuerbox %s wird vertraut", gemeinsam.Bezeichnung(pairingKopplung.Identitaet))
		dienst.RegisterRemoteService(pairingKopplung.Identitaet)
	}
	switch {
	case konf.PairingService:
		log.Printf("Pairing Service aktiv: wartet auf Steuerboxen, die das Secret kennen")
	case konf.RemoteSki == "":
		log.Printf("Keine Kopplung konfiguriert: EEBUS_REMOTE_SKI setzen oder EEBUS_PAIRING_SERVICE aktivieren")
	}

	modbusServer, err := starteModbusServer(konf.ModbusUrl, bruecke)
	if err != nil {
		log.Fatalf("Modbus-Server: %v", err)
	}
	defer modbusServer.Stop()

	webBeenden := gemeinsam.StarteWebUi(konf.WebAdresse, konf.WebBenutzer, konf.WebPasswort, webHandler(bruecke, protokoll))
	defer webBeenden()

	if err := dienst.Start(); err != nil {
		log.Fatalf("EEBUS-Dienst starten: %v", err)
	}
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

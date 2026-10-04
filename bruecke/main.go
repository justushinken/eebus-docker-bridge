// EEBUS-Bruecke
//
// Laeuft als Docker-Container auf dem WAGO PFC200. Tritt gegenueber der
// FNN-Steuerbox als "Controllable System" auf und bildet die Use Cases des
// VDE FNN Hinweises "Schnittstellen der Steuerungseinrichtung" ab:
//
//   - LPC: Begrenzung des Bezugs (Paragraf 14a EnWG)
//   - LPP: Begrenzung der Einspeisung (Paragraf 9 EEG)
//   - MPC: Messwerte der Anlage
//   - MGCP: Messwerte am Netzanschlusspunkt
//
// Grenzen und Messwerte tauscht sie mit der CODESYS-Applikation als
// Modbus-TCP-Server auf 127.0.0.1 aus.
//
// Geschrieben gegen den Entwicklungsstand von eebus-go (nach v0.7.0, mit
// SHIP Pairing Service). Bei anderer Version Signaturen pruefen.
package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"eebus-bruecke/internal/gemeinsam"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	cslpp "github.com/enbility/eebus-go/usecases/cs/lpp"
	shipapi "github.com/enbility/ship-go/api"
	"github.com/enbility/spine-go/model"
)

func main() {
	// Alle Log-Meldungen zusaetzlich fuer das Status-UI vorhalten.
	protokoll := gemeinsam.ProtokolliereLog(100)

	konf, err := leseKonfiguration()
	if err != nil {
		log.Fatalf("Konfiguration: %v", err)
	}
	log.Printf("EEBUS-Bruecke %s, Use Cases: %s", Version, useCaseListe(konf))

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

	// Entitaeten: CEM immer zuerst, damit die Adressen gegenueber frueheren
	// Versionen gleich bleiben. MGCP braucht eine eigene Entitaet fuer den
	// Netzanschlusspunkt, MPC wahlweise eine eigene als Unterzaehler.
	entitaeten := []model.EntityTypeType{model.EntityTypeTypeCEM}
	if konf.Mgcp {
		entitaeten = append(entitaeten, model.EntityTypeTypeGridConnectionPointOfPremises)
	}
	if konf.Mpc && konf.MpcEntitaet == "submeter" {
		entitaeten = append(entitaeten, model.EntityTypeTypeSubMeterElectricity)
	}

	konfiguration, err := api.NewConfiguration(
		konf.Hersteller, konf.Marke, konf.Modell, konf.Seriennummer,
		[]shipapi.DeviceCategoryType{shipapi.DeviceCategoryTypeEnergyManagementSystem},
		model.DeviceTypeTypeEnergyManagementSystem,
		entitaeten,
		konf.EebusPort, zertifikat, 4*time.Second,
		pairingKonfig, verlauf)
	if err != nil {
		log.Fatalf("EEBUS-Konfiguration: %v", err)
	}
	konfiguration.SetAlternateIdentifier(konf.ShipId)

	if werte := LadeFailsafe(konf.Datenverzeichnis); werte != nil {
		konf.FailsafeGrenzeW, konf.FailsafeMindestdauer = werte.GrenzeW, werte.Mindestdauer
		if werte.EinspeisegrenzeW != nil && konf.Lpp {
			konf.FailsafeEinspeisegrenzeW = *werte.EinspeisegrenzeW
		}
		log.Printf("Failsafe-Werte aus der letzten Vorgabe der Steuerbox: %.0f W, %v", werte.GrenzeW, werte.Mindestdauer)
	}

	var kopplung *gemeinsam.Kopplung
	if konf.RemoteSki == "" {
		if kopplung, err = LadeGespeicherteKopplung(konf.Datenverzeichnis); err != nil {
			log.Printf("Gespeicherte Kopplung nicht lesbar: %v", err)
		}
	}

	bruecke := NeueBruecke(konf, eigenerSki, kopplung)
	dienst := service.NewService(konfiguration, bruecke)
	if konf.EebusDebug {
		dienst.SetLogging(gemeinsam.NeuesEebusLog())
		log.Printf("EEBUS_DEBUG: Protokoll von eebus-go auf stdout (docker logs)")
	}
	if err := dienst.Setup(); err != nil {
		log.Fatalf("EEBUS-Dienst einrichten: %v", err)
	}
	bruecke.dienst = dienst
	bruecke.cem = dienst.LocalDevice().EntityForType(model.EntityTypeTypeCEM)
	richteUseCasesEin(bruecke, dienst, konf)
	bruecke.SetzeStartwerte()
	bruecke.SetzeKennung(dienst, secret)
	SetzeAnlageninfo(dienst, konf)
	RichteAnlagenstatusEin(bruecke.cem)
	// Fuer die Diagnose der Steuerbox: deren Herstellerdaten lesen
	bruecke.cem.GetOrAddFeature(model.FeatureTypeTypeDeviceClassification, model.RoleTypeClient)

	switch {
	case konf.RemoteSki != "":
		log.Printf("Steuerbox fest per EEBUS_REMOTE_SKI: %s (Pairing Service und Kopplung im UI aus)", konf.RemoteSki)
		dienst.RegisterRemoteService(shipapi.NewServiceIdentity(konf.RemoteSki, "", ""))
	case kopplung != nil:
		log.Printf("Gekoppelte Steuerbox %s wird vertraut (%s)", gemeinsam.Bezeichnung(kopplung.Identitaet), kopplung.Verfahren)
		dienst.RegisterRemoteService(kopplung.Identitaet)
	default:
		log.Printf("Keine Steuerbox gekoppelt: im UI den Suchmodus starten oder den QR-Code an den Messstellenbetreiber geben")
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

	for i := 0; ; i++ {
		select {
		case jetzt := <-takt.C:
			bruecke.Takt(jetzt)
			if i%5 == 0 {
				bruecke.PruefeGegenstelle()
			}
		case sig := <-signale:
			log.Printf("Beende nach Signal %v", sig)
			return
		}
	}
}

func useCaseListe(konf Konfiguration) string {
	var namen []string
	for _, uc := range []struct {
		an   bool
		name string
	}{{konf.Lpc, "LPC"}, {konf.Lpp, "LPP"}, {konf.Mpc, "MPC"}, {konf.Mgcp, "MGCP"}} {
		if uc.an {
			namen = append(namen, uc.name)
		}
	}
	return strings.Join(namen, ", ")
}

// richteUseCasesEin meldet die Use Cases in fester Reihenfolge an. MPC auf
// der CEM-Entitaet kommt vor LPC/LPP: Deren Grenzen verweisen auf die
// Gesamtleistung (ACPowerTotal) derselben Entitaet, und die Nennleistung
// nutzt die ersten IDs der ElectricalConnection.
func richteUseCasesEin(b *Bruecke, dienst api.ServiceInterface, konf Konfiguration) {
	jetzt := time.Now()
	melde := func(name string, uc api.UseCaseInterface) {
		if err := dienst.AddUseCase(uc); err != nil {
			log.Fatalf("Use Case %s: %v", name, err)
		}
	}

	if konf.Mpc {
		entitaet := b.cem
		if konf.MpcEntitaet == "submeter" {
			entitaet = dienst.LocalDevice().EntityForType(model.EntityTypeTypeSubMeterElectricity)
		}
		var err error
		if b.mpc, b.mpcGroessen, err = NeuesMpc(entitaet, konf); err != nil {
			log.Fatalf("Use Case MPC: %v", err)
		}
		melde("MPC", b.mpc)
	}
	if konf.Lpc {
		lpc := cslpc.NewLPC(b.cem, b.LpcEreignis)
		b.bezug = neueBegrenzung("LPC", "Grenze", lpcAdapter{lpc}, konf.FailsafeGrenzeW, konf.NennleistungMaxW, jetzt)
		melde("LPC", lpc)
	}
	if konf.Lpp {
		lpp := cslpp.NewLPP(b.cem, b.LppEreignis)
		b.einspeisung = neueBegrenzung("LPP", "Einspeisegrenze", lppAdapter{lpp}, konf.FailsafeEinspeisegrenzeW, konf.NennleistungErzeugungMaxW, jetzt)
		melde("LPP", lpp)
	}
	if konf.Mgcp {
		entitaet := dienst.LocalDevice().EntityForType(model.EntityTypeTypeGridConnectionPointOfPremises)
		var err error
		if b.mgcp, b.mgcpGroessen, err = NeuesMgcp(entitaet, konf); err != nil {
			log.Fatalf("Use Case MGCP: %v", err)
		}
		melde("MGCP", b.mgcp)
	}
}

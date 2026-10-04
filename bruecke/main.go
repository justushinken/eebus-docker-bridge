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
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
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
	HwRevision           string
	FailsafeGrenzeW      float64
	FailsafeMindestdauer time.Duration
	NennleistungMaxW     float64
	WebAdresse           string
	WebBenutzer          string
	WebPasswort          string
	WebKopplung          bool // Kopplung und Suchmodus im UI erlaubt
	EebusDebug           bool

	// Use Cases (EEBUS_USECASES)
	Lpc, Lpp, Mpc, Mgcp bool

	// LPP
	FailsafeEinspeisegrenzeW  float64
	NennleistungErzeugungMaxW float64

	// MPC und MGCP
	MpcEntitaet    string // cem oder submeter
	MpcPhasenText  string // z. B. "abc"
	MpcPhasen      []model.ElectricalConnectionPhaseNameType
	MpcMesswerte   map[string]bool
	MgcpMesswerte  map[string]bool
	MesswertQuelle model.MeasurementValueSourceType
}

// auswahl liest eine kommagetrennte Liste aus erlaubten Namen.
func auswahl(name, vorgabe string, erlaubt []string) (map[string]bool, error) {
	ergebnis := map[string]bool{}
	for _, teil := range strings.Split(gemeinsam.EnvTextLeerErlaubt(name, vorgabe), ",") {
		teil = strings.ToLower(strings.TrimSpace(teil))
		if teil == "" {
			continue
		}
		if !slices.Contains(erlaubt, teil) {
			return nil, fmt.Errorf("%s: unbekannter Wert %q, erlaubt: %s", name, teil, strings.Join(erlaubt, ", "))
		}
		ergebnis[teil] = true
	}
	return ergebnis, nil
}

func leseKonfiguration() (Konfiguration, error) {
	konf := Konfiguration{
		EebusPort:            gemeinsam.EnvGanzzahl("EEBUS_PORT", 4712),
		ModbusUrl:            gemeinsam.EnvText("MODBUS_URL", "tcp://127.0.0.1:5502"),
		Datenverzeichnis:     gemeinsam.EnvText("DATENVERZEICHNIS", "/data"),
		RemoteSki:            normalisiereSki(gemeinsam.EnvText("EEBUS_REMOTE_SKI", "")),
		PairingService:       gemeinsam.EnvText("EEBUS_PAIRING_SERVICE", "an") != "aus",
		Hersteller:           gemeinsam.EnvText("GERAET_HERSTELLER", "Demo"),
		Marke:                gemeinsam.EnvText("GERAET_MARKE", "Demo"),
		Modell:               gemeinsam.EnvText("GERAET_MODELL", "PFC200-LPC-Bruecke"),
		Seriennummer:         gemeinsam.EnvText("GERAET_SERIENNUMMER", ""),
		HwRevision:           gemeinsam.EnvText("GERAET_HW_REVISION", ""),
		FailsafeGrenzeW:      gemeinsam.EnvKommazahl("FAILSAFE_GRENZE_W", 4200),
		FailsafeMindestdauer: gemeinsam.EnvDauer("FAILSAFE_MINDESTDAUER", 2*time.Hour),
		NennleistungMaxW:     gemeinsam.EnvKommazahl("NENNLEISTUNG_MAX_W", 11000),
		WebAdresse:           gemeinsam.EnvTextLeerErlaubt("WEB_ADRESSE", ":8090"),
		WebBenutzer:          gemeinsam.EnvText("WEB_BENUTZER", "admin"),
		WebPasswort:          gemeinsam.EnvText("WEB_PASSWORT", ""),
		WebKopplung:          gemeinsam.EnvText("WEB_KOPPLUNG", "an") != "aus",
		EebusDebug:           gemeinsam.EnvText("EEBUS_DEBUG", "aus") == "an",
		MpcEntitaet:          gemeinsam.EnvText("MPC_ENTITAET", "cem"),
		MpcPhasenText:        strings.ToLower(gemeinsam.EnvText("MPC_PHASEN", "abc")),
		MesswertQuelle:       model.MeasurementValueSourceType(gemeinsam.EnvText("MESSWERT_QUELLE", "measuredValue")),
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

	useCases, err := auswahl("EEBUS_USECASES", "lpc", []string{"lpc", "lpp", "mpc", "mgcp"})
	if err != nil {
		return konf, err
	}
	konf.Lpc, konf.Lpp, konf.Mpc, konf.Mgcp = useCases["lpc"], useCases["lpp"], useCases["mpc"], useCases["mgcp"]
	if !konf.Lpc && !konf.Lpp {
		// Heartbeat und Failsafe haengen an LPC bzw. LPP.
		return konf, fmt.Errorf("EEBUS_USECASES: lpc oder lpp ist Pflicht")
	}

	if konf.Lpp {
		// Keine stillschweigende Vorgabe: Die Einspeisegrenze im Failsafe haengt
		// von der Anlage ab (z. B. 60 % nach Netzanschlussvertrag).
		for _, name := range []string{"FAILSAFE_EINSPEISEGRENZE_W", "NENNLEISTUNG_ERZEUGUNG_MAX_W"} {
			if gemeinsam.EnvText(name, "") == "" {
				return konf, fmt.Errorf("%s ist Pflicht, wenn lpp in EEBUS_USECASES steht", name)
			}
		}
		konf.FailsafeEinspeisegrenzeW = gemeinsam.EnvKommazahl("FAILSAFE_EINSPEISEGRENZE_W", 0)
		konf.NennleistungErzeugungMaxW = gemeinsam.EnvKommazahl("NENNLEISTUNG_ERZEUGUNG_MAX_W", 0)
	}

	if konf.Mpc {
		if konf.MpcEntitaet != "cem" && konf.MpcEntitaet != "submeter" {
			return konf, fmt.Errorf("MPC_ENTITAET: cem oder submeter erwartet, nicht %q", konf.MpcEntitaet)
		}
		if !slices.Contains([]string{"a", "b", "c", "ab", "bc", "ac", "abc"}, konf.MpcPhasenText) {
			return konf, fmt.Errorf("MPC_PHASEN: z. B. abc oder a erwartet, nicht %q", konf.MpcPhasenText)
		}
		for _, p := range konf.MpcPhasenText {
			konf.MpcPhasen = append(konf.MpcPhasen, model.ElectricalConnectionPhaseNameType(string(p)))
		}
		if konf.MpcMesswerte, err = auswahl("MPC_MESSWERTE", strings.Join(mpcMesswerteErlaubt, ","), mpcMesswerteErlaubt); err != nil {
			return konf, err
		}
	}
	if konf.Mgcp {
		if konf.MgcpMesswerte, err = auswahl("MGCP_MESSWERTE", "strom,spannung,frequenz", mgcpMesswerteErlaubt); err != nil {
			return konf, err
		}
	}
	switch konf.MesswertQuelle {
	case model.MeasurementValueSourceTypeMeasuredValue, model.MeasurementValueSourceTypeCalculatedValue, model.MeasurementValueSourceTypeEmpiricalValue:
	default:
		return konf, fmt.Errorf("MESSWERT_QUELLE: measuredValue, calculatedValue oder empiricalValue erwartet")
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

	kopplung, err := LadeGespeicherteKopplung(konf.Datenverzeichnis)
	if err != nil {
		log.Printf("Gespeicherte Kopplung nicht lesbar: %v", err)
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

	// EEBUS_REMOTE_SKI und eine gespeicherte Kopplung koennen gleichzeitig gelten.
	if konf.RemoteSki != "" {
		log.Printf("SKI-Verfahren: Steuerbox mit SKI %s wird vertraut (EEBUS_REMOTE_SKI)", konf.RemoteSki)
		dienst.RegisterRemoteService(shipapi.NewServiceIdentity(konf.RemoteSki, "", ""))
	}
	if kopplung != nil {
		log.Printf("Gekoppelte Steuerbox %s wird vertraut (%s)", gemeinsam.Bezeichnung(kopplung.Identitaet), kopplung.Verfahren)
		dienst.RegisterRemoteService(kopplung.Identitaet)
	}
	switch {
	case konf.PairingService:
		log.Printf("Pairing Service aktiv: wartet auf Steuerboxen, die das Secret kennen")
	case konf.RemoteSki == "" && kopplung == nil:
		log.Printf("Keine Steuerbox gekoppelt: im UI den Suchmodus starten oder EEBUS_REMOTE_SKI setzen")
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
		mpc, err := NeuesMpc(entitaet, b.MonitoringEreignis, konf)
		if err != nil {
			log.Fatalf("Use Case MPC: %v", err)
		}
		b.mpc = mpc
		melde("MPC", mpc)
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
		mgcp, err := NeuesMgcp(entitaet, b.MonitoringEreignis, konf)
		if err != nil {
			log.Fatalf("Use Case MGCP: %v", err)
		}
		b.mgcp = mgcp
		melde("MGCP", mgcp)
	}
}

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
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	"github.com/enbility/ship-go/cert"
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
}

func leseKonfiguration() Konfiguration {
	return Konfiguration{
		EebusPort:            envGanzzahl("EEBUS_PORT", 4712),
		ModbusUrl:            envText("MODBUS_URL", "tcp://127.0.0.1:5502"),
		Datenverzeichnis:     envText("DATENVERZEICHNIS", "/data"),
		RemoteSki:            envText("EEBUS_REMOTE_SKI", ""),
		Hersteller:           envText("GERAET_HERSTELLER", "Demo"),
		Marke:                envText("GERAET_MARKE", "Demo"),
		Modell:               envText("GERAET_MODELL", "PFC200-LPC-Bruecke"),
		Seriennummer:         envText("GERAET_SERIENNUMMER", "0001"),
		FailsafeGrenzeW:      envKommazahl("FAILSAFE_GRENZE_W", 4200),
		FailsafeMindestdauer: envDauer("FAILSAFE_MINDESTDAUER", 2*time.Hour),
		NennleistungMaxW:     envKommazahl("NENNLEISTUNG_MAX_W", 11000),
	}
}

func main() {
	konf := leseKonfiguration()

	zertifikat, err := ladeOderErzeugeZertifikat(konf.Datenverzeichnis, konf.Seriennummer)
	if err != nil {
		log.Fatalf("Zertifikat: %v", err)
	}
	blatt, err := x509.ParseCertificate(zertifikat.Certificate[0])
	if err != nil {
		log.Fatalf("Zertifikat parsen: %v", err)
	}
	eigenerSki, err := cert.SkiFromCertificate(blatt)
	if err != nil {
		log.Fatalf("SKI ermitteln: %v", err)
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

	bruecke := NeueBruecke(konf)
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

// ladeOderErzeugeZertifikat haelt das SHIP-Zertifikat persistent im Volume.
// Ohne Persistenz aendert sich bei jedem Container-Neustart der SKI und das
// Pairing mit der Steuerbox waere verloren.
func ladeOderErzeugeZertifikat(verzeichnis, seriennummer string) (tls.Certificate, error) {
	zertPfad := filepath.Join(verzeichnis, "zertifikat.pem")
	schluesselPfad := filepath.Join(verzeichnis, "schluessel.pem")

	if vorhanden, err := tls.LoadX509KeyPair(zertPfad, schluesselPfad); err == nil {
		return vorhanden, nil
	}

	neu, err := cert.CreateCertificate("LPC-Bruecke", "Demo", "DE", "EEBUS-LPC-Bruecke-"+seriennummer)
	if err != nil {
		return tls.Certificate{}, err
	}
	schluessel, ok := neu.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return tls.Certificate{}, errors.New("unerwarteter Schluesseltyp")
	}
	schluesselDer, err := x509.MarshalECPrivateKey(schluessel)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(verzeichnis, 0o700); err != nil {
		return tls.Certificate{}, err
	}
	zertPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: neu.Certificate[0]})
	schluesselPem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: schluesselDer})
	if err := os.WriteFile(zertPfad, zertPem, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(schluesselPfad, schluesselPem, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	log.Printf("Neues Zertifikat erzeugt in %s", verzeichnis)
	return neu, nil
}

// --- Hilfsfunktionen fuer Umgebungsvariablen ---

func envText(name, vorgabe string) string {
	if wert, ok := os.LookupEnv(name); ok && wert != "" {
		return wert
	}
	return vorgabe
}

func envGanzzahl(name string, vorgabe int) int {
	if wert, err := strconv.Atoi(envText(name, "")); err == nil {
		return wert
	}
	return vorgabe
}

func envKommazahl(name string, vorgabe float64) float64 {
	if wert, err := strconv.ParseFloat(envText(name, ""), 64); err == nil {
		return wert
	}
	return vorgabe
}

func envDauer(name string, vorgabe time.Duration) time.Duration {
	if wert, err := time.ParseDuration(envText(name, "")); err == nil {
		return wert
	}
	return vorgabe
}

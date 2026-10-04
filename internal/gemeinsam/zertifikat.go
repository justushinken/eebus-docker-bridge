// Package gemeinsam enthaelt, was Bruecke und Test-Steuerbox gleichermassen
// brauchen: Zertifikat, Umgebungsvariablen, Ereignisprotokoll, Web-UI.
package gemeinsam

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/enbility/ship-go/cert"
)

// LadeOderErzeugeZertifikat haelt das SHIP-Zertifikat persistent im Volume und
// liefert es samt SKI. Ohne Persistenz aendert sich bei jedem Container-Neustart
// der SKI und das Pairing waere verloren.
func LadeOderErzeugeZertifikat(verzeichnis, einheit, name string) (tls.Certificate, string, error) {
	zertPfad := filepath.Join(verzeichnis, "zertifikat.pem")
	schluesselPfad := filepath.Join(verzeichnis, "schluessel.pem")

	zertifikat, err := tls.LoadX509KeyPair(zertPfad, schluesselPfad)
	if err != nil {
		if zertifikat, err = erzeugeZertifikat(zertPfad, schluesselPfad, einheit, name); err != nil {
			return tls.Certificate{}, "", err
		}
		log.Printf("Neues Zertifikat erzeugt in %s", verzeichnis)
	}

	blatt, err := x509.ParseCertificate(zertifikat.Certificate[0])
	if err != nil {
		return tls.Certificate{}, "", err
	}
	ski, err := cert.SkiFromCertificate(blatt)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return zertifikat, ski, nil
}

func erzeugeZertifikat(zertPfad, schluesselPfad, einheit, name string) (tls.Certificate, error) {
	neu, err := cert.CreateCertificate(einheit, "Demo", "DE", name)
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
	if err := os.MkdirAll(filepath.Dir(zertPfad), 0o700); err != nil {
		return tls.Certificate{}, err
	}
	zertPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: neu.Certificate[0]})
	schluesselPem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: schluesselDer})
	if err := SchreibeDatei(zertPfad, zertPem); err != nil {
		return tls.Certificate{}, err
	}
	if err := SchreibeDatei(schluesselPfad, schluesselPem); err != nil {
		return tls.Certificate{}, err
	}
	return neu, nil
}

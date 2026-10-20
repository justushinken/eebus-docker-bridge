package main

import (
	"context"
	"log"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Netzwerk beim Start pruefen: Auf welchen Schnittstellen und mit welchen
// Adressen kuendigt das Gateway sich per mDNS an?
//
// ship-go nimmt den Avahi-Daemon des Hosts (ueber D-Bus), wenn er laeuft,
// sonst das eingebaute Zeroconf. Zeroconf sammelt die Adressen aller
// gewaehlten Schnittstellen in einer Liste und sendet diese auf jeder
// Schnittstelle. Ohne EEBUS_SCHNITTSTELLEN landen so auch Adressen aus
// fremden Netzen und von docker0 in der Ankuendigung, und die Steuerbox
// versucht womoeglich die falsche zuerst.

// meldeSchnittstellen protokolliert die Adressen der gewaehlten Schnittstellen
// bzw. warnt, wenn ohne Auswahl mehrere Netze angekuendigt werden.
func meldeSchnittstellen(namen []string) {
	if len(namen) == 0 {
		alle := multicastSchnittstellen()
		if len(alle) > 1 {
			log.Printf("mDNS auf allen Schnittstellen (%s). Bei getrennten Netzen EEBUS_SCHNITTSTELLEN setzen, sonst kuendigt Zeroconf alle Adressen ueberall an.",
				strings.Join(alle, ", "))
		}
		return
	}
	// Aendert sich spaeter eine vorhandene Adresse, bleibt die alte in der
	// Ankuendigung (Zeroconf liest sie nur beim Start): dann neu starten.
	for _, name := range namen {
		s, err := net.InterfaceByName(name)
		if err != nil {
			log.Printf("EEBUS_SCHNITTSTELLEN: %s nicht gefunden, vorhanden: %s. mDNS wartet, bis sie erscheint.",
				name, strings.Join(multicastSchnittstellen(), ", "))
			continue
		}
		var adressen []string
		if liste, err := s.Addrs(); err == nil {
			for _, a := range liste {
				adressen = append(adressen, a.String())
			}
		}
		switch {
		case s.Flags&net.FlagUp == 0:
			log.Printf("mDNS auf %s: Schnittstelle ist aus, mDNS wartet", name)
		case len(adressen) == 0:
			// ship-go prueft alle 15 s und kuendigt an, sobald eine Adresse da ist
			log.Printf("mDNS auf %s: noch keine IP-Adresse, mDNS wartet", name)
		default:
			log.Printf("mDNS nur auf %s (%s)", name, strings.Join(adressen, ", "))
		}
	}
}

// multicastSchnittstellen: Namen der aktiven Schnittstellen mit IPv4-Adresse,
// die auch Zeroconf ohne Auswahl nimmt (ohne Loopback).
func multicastSchnittstellen() []string {
	liste, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var namen []string
	for _, s := range liste {
		if s.Flags&net.FlagUp == 0 || s.Flags&net.FlagMulticast == 0 || s.Flags&net.FlagLoopback != 0 {
			continue
		}
		adressen, _ := s.Addrs()
		if slices.ContainsFunc(adressen, func(a net.Addr) bool {
			ip, ok := a.(*net.IPNet)
			return ok && ip.IP.To4() != nil
		}) {
			namen = append(namen, s.Name)
		}
	}
	return namen
}

// meldeMdnsAnbieter prueft, ob ship-go den Avahi-Daemon nutzen kann. ship-go
// selbst verschweigt den Grund und meldet nur "Avahi provider failed" im
// Debug-Log. Voraussetzungen: avahi-daemon laeuft auf dem Host und der
// Container hat /var/run/dbus eingebunden.
func meldeMdnsAnbieter() {
	if err := avahiErreichbar(); err != nil {
		log.Printf("mDNS ueber Zeroconf, Avahi nicht nutzbar: %v", err)
		return
	}
	log.Printf("mDNS ueber den Avahi-Daemon des Hosts")
}

func avahiErreichbar() error {
	verbindung, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer verbindung.Close()
	ctx, abbrechen := context.WithTimeout(context.Background(), 2*time.Second)
	defer abbrechen()
	var version uint32
	return verbindung.Object("org.freedesktop.Avahi", "/").
		CallWithContext(ctx, "org.freedesktop.Avahi.Server.GetAPIVersion", 0).Store(&version)
}

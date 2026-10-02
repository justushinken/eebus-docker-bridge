package main

import (
	"math"
	"time"

	"github.com/simonvetter/modbus"
)

// Schnittstellenversion. Bei jeder Aenderung des Registerlayouts erhoehen;
// FbEebusLpc prueft den Wert und faellt bei Abweichung auf die Ersatzgrenze.
const SchnittstellenVersion = 1

// Input-Register (FC04), Bruecke -> SPS. 32-Bit-Werte: High-Word zuerst.
const (
	regLebenszeichen    = 0 // zaehlt jede Sekunde hoch
	regVersion          = 1 // SchnittstellenVersion
	regZustand          = 2 // LpcZustand
	regVerbindung       = 3 // Verbindung
	regWirksameGrenzeHi = 4 // einzuhaltende Grenze in W (nur gueltig, wenn regBegrenzungAktiv = 1)
	regWirksameGrenzeLo = 5
	regBegrenzungAktiv  = 6 // 0/1
	regGrenzeNetzHi     = 7 // zuletzt empfangene Grenze des Netzbetreibers in W (Diagnose)
	regGrenzeNetzLo     = 8
	regRestdauerHi      = 9 // Restlaufzeit der Grenze in s, 0 = unbefristet
	regRestdauerLo      = 10
	regFailsafeGrenzeHi = 11 // aktuelle Failsafe-Grenze in W (Diagnose)
	regFailsafeGrenzeLo = 12
	regHeartbeatAlter   = 13 // s seit letztem Heartbeat, 65535 = nie
	anzahlInputRegister = 14
)

// Holding-Register (FC03/FC16), SPS -> Bruecke.
const (
	regSpsLebenszeichen   = 0 // zaehlt in der SPS jede Sekunde hoch
	regNennleistungHi     = 1 // max. Leistungsaufnahme der Anlage in W
	regNennleistungLo     = 2
	anzahlHoldingRegister = 3
)

func zuUint32(hi, lo uint16) uint32 {
	return uint32(hi)<<16 | uint32(lo)
}

func teileUint32(wert uint32) (hi, lo uint16) {
	return uint16(wert >> 16), uint16(wert)
}

func alsUint32(wert float64) uint32 {
	if wert <= 0 {
		return 0
	}
	if wert >= math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(math.Round(wert))
}

func starteModbusServer(url string, b *Bruecke) (*modbus.ModbusServer, error) {
	server, err := modbus.NewServer(&modbus.ServerConfiguration{
		URL:        url,
		Timeout:    30 * time.Second,
		MaxClients: 4,
	}, &modbusHandler{b: b})
	if err != nil {
		return nil, err
	}
	return server, server.Start()
}

// inputRegister erstellt ein konsistentes Abbild aller Input-Register.
func (b *Bruecke) inputRegister(jetzt time.Time) [anzahlInputRegister]uint16 {
	b.mu.Lock()
	defer b.mu.Unlock()

	var r [anzahlInputRegister]uint16
	r[regLebenszeichen] = b.lebenszeichen
	r[regVersion] = SchnittstellenVersion
	r[regZustand] = uint16(b.zustand)
	r[regVerbindung] = uint16(b.verbindung)

	aktiv, grenzeW := b.wirksameGrenze()
	r[regWirksameGrenzeHi], r[regWirksameGrenzeLo] = teileUint32(alsUint32(grenzeW))
	if aktiv {
		r[regBegrenzungAktiv] = 1
	}
	r[regGrenzeNetzHi], r[regGrenzeNetzLo] = teileUint32(alsUint32(b.grenze.Value))

	var restdauer float64
	if b.zustand == ZustandBegrenzt && !b.grenzeAblauf.IsZero() {
		restdauer = b.grenzeAblauf.Sub(jetzt).Seconds()
	}
	r[regRestdauerHi], r[regRestdauerLo] = teileUint32(alsUint32(restdauer))
	r[regFailsafeGrenzeHi], r[regFailsafeGrenzeLo] = teileUint32(alsUint32(b.failsafeGrenzeW))

	r[regHeartbeatAlter] = math.MaxUint16
	if !b.letzterHeartbeat.IsZero() {
		r[regHeartbeatAlter] = uint16(min(jetzt.Sub(b.letzterHeartbeat).Seconds(), math.MaxUint16))
	}
	return r
}

type modbusHandler struct {
	b *Bruecke
}

func (h *modbusHandler) HandleCoils(req *modbus.CoilsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalFunction
}

func (h *modbusHandler) HandleDiscreteInputs(req *modbus.DiscreteInputsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalFunction
}

func (h *modbusHandler) HandleInputRegisters(req *modbus.InputRegistersRequest) ([]uint16, error) {
	ende := int(req.Addr) + int(req.Quantity)
	if ende > anzahlInputRegister {
		return nil, modbus.ErrIllegalDataAddress
	}
	abbild := h.b.inputRegister(time.Now())
	ergebnis := make([]uint16, req.Quantity)
	copy(ergebnis, abbild[req.Addr:ende])
	return ergebnis, nil
}

func (h *modbusHandler) HandleHoldingRegisters(req *modbus.HoldingRegistersRequest) ([]uint16, error) {
	ende := int(req.Addr) + int(req.Quantity)
	if ende > anzahlHoldingRegister {
		return nil, modbus.ErrIllegalDataAddress
	}
	h.b.mu.Lock()
	defer h.b.mu.Unlock()
	if req.IsWrite {
		copy(h.b.holding[req.Addr:ende], req.Args)
	}
	ergebnis := make([]uint16, req.Quantity)
	copy(ergebnis, h.b.holding[req.Addr:ende])
	return ergebnis, nil
}

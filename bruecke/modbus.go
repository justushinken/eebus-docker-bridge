package main

import (
	"math"
	"time"

	"github.com/simonvetter/modbus"
)

// Schnittstellenversion (Input-Register 1). Nur bei inkompatiblen Aenderungen
// erhoehen; FbEebusLpc prueft den Wert und faellt bei Abweichung auf die
// Ersatzgrenze. Neue Register werden nur angehaengt, das zeigt die
// Erweiterungsversion (Input-Register 14).
const (
	SchnittstellenVersion = 1
	ErweiterungsVersion   = 2 // 2: LPP, MPC, MGCP, Anlagenstatus
)

// Input-Register (FC04), Bruecke -> SPS. 32-Bit-Werte: High-Word zuerst.
//
// Je Richtung ein Block mit gleichem Aufbau: Bezug (LPC) ab Register 2,
// Einspeisung (LPP) ab Register 17. Offsets im Block siehe blk*.
const (
	regLebenszeichen = 0 // zaehlt jede Sekunde hoch
	regVersion       = 1 // SchnittstellenVersion

	blockBezug       = 2
	blockEinspeisung = 17

	regErweiterung       = 14 // ErweiterungsVersion
	regUseCasesLokal     = 15 // Bitmaske ucBit*: von der Bruecke angeboten
	regUseCasesSteuerbox = 16 // Bitmaske ucBit*: von der Steuerbox unterstuetzt

	regMindestdauerHi   = 29 // Failsafe-Mindestdauer in s (gemeinsam fuer LPC und LPP)
	regMindestdauerLo   = 30
	anzahlInputRegister = 31
)

// Offsets in einem Begrenzungsblock
const (
	blkZustand          = 0  // LpcZustand
	blkVerbindung       = 1  // Verbindung (in beiden Bloecken gleich)
	blkWirksameGrenzeHi = 2  // einzuhaltende Grenze in W (nur gueltig, wenn blkBegrenzungAktiv = 1)
	blkWirksameGrenzeLo = 3  //
	blkBegrenzungAktiv  = 4  // 0/1
	blkGrenzeNetzHi     = 5  // zuletzt empfangene Grenze des Netzbetreibers in W (Diagnose)
	blkGrenzeNetzLo     = 6  //
	blkRestdauerHi      = 7  // Restlaufzeit der Grenze in s, 0 = unbefristet
	blkRestdauerLo      = 8  //
	blkFailsafeGrenzeHi = 9  // aktuelle Failsafe-Grenze in W (Diagnose)
	blkFailsafeGrenzeLo = 10 //
	blkHeartbeatAlter   = 11 // s seit letztem Heartbeat, 65535 = nie (in beiden Bloecken gleich)
)

// Bits der Use-Case-Masken (Input-Register 15 und 16)
const (
	ucBitLpc  = 1 << 0
	ucBitLpp  = 1 << 1
	ucBitMpc  = 1 << 2
	ucBitMgcp = 1 << 3
)

// Holding-Register (FC03/FC16), SPS -> Bruecke. Alle in einem FC16-Aufruf
// schreiben, dann sieht die Bruecke immer einen zusammengehoerigen Stand.
const (
	regSpsLebenszeichen        = 0 // zaehlt in der SPS jede Sekunde hoch
	regNennleistungHi          = 1 // max. Leistungsaufnahme der Anlage in W
	regNennleistungLo          = 2
	regNennleistungErzeugungHi = 3 // max. Einspeiseleistung in W (LPP), 0 = Vorgabe aus Env
	regNennleistungErzeugungLo = 4
	regMpcMaske                = 5 // Gueltigkeit der MPC-Werte: Bit 0 P, 1 E Bezug, 2 E Erzeugung
	regMgcpMaske               = 6 // Gueltigkeit der MGCP-Werte: Bit 0 P, 1 E Einspeisung, 2 E Bezug
	regAnlagenstatus           = 7 // 0 normal, 1 Stoerung, 2 Standby

	regMesswerteAnfang = regMpcMaske

	// MPC: Werte der Anlage. Leistung Bezug positiv, Erzeugung negativ.
	regMpcP          = 8  // DINT W
	regMpcEBezug     = 10 // ULINT Wh (4 Register), Zaehlerstand
	regMpcEErzeugung = 14 // ULINT Wh

	// MGCP: Werte am Netzanschlusspunkt. Bezug aus dem Netz positiv,
	// Einspeisung negativ.
	regMgcpP            = 18 // DINT W
	regMgcpEEinspeisung = 20 // ULINT Wh
	regMgcpEBezug       = 24 // ULINT Wh

	anzahlHoldingRegister = 28
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

// useCasesLokal liefert die Bitmaske der angebotenen Use Cases.
func (b *Bruecke) useCasesLokal() uint16 {
	var bits uint16
	for _, uc := range []struct {
		an  bool
		bit uint16
	}{{b.bezug != nil, ucBitLpc}, {b.einspeisung != nil, ucBitLpp}, {b.mpc != nil, ucBitMpc}, {b.mgcp != nil, ucBitMgcp}} {
		if uc.an {
			bits |= uc.bit
		}
	}
	return bits
}

// inputRegister erstellt ein konsistentes Abbild aller Input-Register.
func (b *Bruecke) inputRegister(jetzt time.Time) [anzahlInputRegister]uint16 {
	b.mu.Lock()
	defer b.mu.Unlock()

	var r [anzahlInputRegister]uint16
	r[regLebenszeichen] = b.lebenszeichen
	r[regVersion] = SchnittstellenVersion
	r[regErweiterung] = ErweiterungsVersion
	r[regUseCasesLokal] = b.useCasesLokal()
	r[regUseCasesSteuerbox] = b.steuerboxUseCases
	r[regMindestdauerHi], r[regMindestdauerLo] = teileUint32(alsUint32(b.failsafeMindestdauer.Seconds()))

	heartbeatAlter := uint16(math.MaxUint16)
	if !b.letzterHeartbeat.IsZero() {
		heartbeatAlter = uint16(min(jetzt.Sub(b.letzterHeartbeat).Seconds(), math.MaxUint16))
	}

	for _, blk := range []struct {
		basis int
		r     *Begrenzung
	}{{blockBezug, b.bezug}, {blockEinspeisung, b.einspeisung}} {
		r[blk.basis+blkVerbindung] = uint16(b.verbindung)
		r[blk.basis+blkHeartbeatAlter] = heartbeatAlter
		if blk.r == nil {
			continue
		}
		g := blk.r
		r[blk.basis+blkZustand] = uint16(g.zustand)
		aktiv, grenzeW := g.wirksameGrenze()
		r[blk.basis+blkWirksameGrenzeHi], r[blk.basis+blkWirksameGrenzeLo] = teileUint32(alsUint32(grenzeW))
		if aktiv {
			r[blk.basis+blkBegrenzungAktiv] = 1
		}
		r[blk.basis+blkGrenzeNetzHi], r[blk.basis+blkGrenzeNetzLo] = teileUint32(alsUint32(g.grenze.Value))
		r[blk.basis+blkRestdauerHi], r[blk.basis+blkRestdauerLo] = teileUint32(alsUint32(g.restdauer(jetzt)))
		r[blk.basis+blkFailsafeGrenzeHi], r[blk.basis+blkFailsafeGrenzeLo] = teileUint32(alsUint32(g.failsafeGrenzeW))
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

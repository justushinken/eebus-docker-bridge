package main

import (
	"log"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/features/server"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
	"github.com/enbility/spine-go/util"
)

// Anlageninformationen und Anlagenstatus nach FNN-Hinweis 4.1.2.7:
// Hersteller, Produkt, Seriennummer, Hardware- und Software-Revision sowie
// eine Stoerung der Anlage.

// Version der Bruecke, beim Bauen per -ldflags "-X main.Version=..." gesetzt.
var Version = "dev"

// Werte von Holding-Register regAnlagenstatus
const (
	anlagenstatusNormal   = 0
	anlagenstatusStoerung = 1
	anlagenstatusStandby  = 2
)

// SetzeAnlageninfo ergaenzt die DeviceClassification um Hersteller und
// Revisionen. eebus-go setzt dort nur Marke, Modell und Seriennummer.
func SetzeAnlageninfo(dienst api.ServiceInterface, konf Konfiguration) {
	info := dienst.LocalDevice().Entity([]model.AddressEntityType{0})
	if info == nil {
		return
	}
	feature := info.FeatureOfTypeAndRole(model.FeatureTypeTypeDeviceClassification, model.RoleTypeServer)
	if feature == nil {
		return
	}
	daten, ok := feature.DataCopy(model.FunctionTypeDeviceClassificationManufacturerData).(*model.DeviceClassificationManufacturerDataType)
	if !ok || daten == nil {
		daten = &model.DeviceClassificationManufacturerDataType{}
	}
	text := func(s string) *model.DeviceClassificationStringType {
		return util.Ptr(model.DeviceClassificationStringType(s))
	}
	daten.VendorName = text(konf.Hersteller)
	daten.SoftwareRevision = text(Version)
	if konf.HwRevision != "" {
		daten.HardwareRevision = text(konf.HwRevision)
	}
	feature.SetData(model.FunctionTypeDeviceClassificationManufacturerData, daten)
}

// RichteAnlagenstatusEin stellt den Betriebszustand der Anlage in der
// DeviceDiagnosis der CEM-Entitaet bereit (dort liegt schon der Heartbeat von
// LPC/LPP). Vor dienst.Start aufrufen.
func RichteAnlagenstatusEin(cem spineapi.EntityLocalInterface) {
	feature := cem.GetOrAddFeature(model.FeatureTypeTypeDeviceDiagnosis, model.RoleTypeServer)
	feature.AddFunctionType(model.FunctionTypeDeviceDiagnosisStateData, true, false)
}

func betriebszustand(spsOk bool, register uint16) model.DeviceDiagnosisOperatingStateType {
	switch {
	case !spsOk:
		return model.DeviceDiagnosisOperatingStateTypeFailure
	case register == anlagenstatusStoerung:
		return model.DeviceDiagnosisOperatingStateTypeFailure
	case register == anlagenstatusStandby:
		return model.DeviceDiagnosisOperatingStateTypeStandby
	}
	return model.DeviceDiagnosisOperatingStateTypeNormalOperation
}

var zustandTexte = map[model.DeviceDiagnosisOperatingStateType]string{
	model.DeviceDiagnosisOperatingStateTypeNormalOperation: "normal",
	model.DeviceDiagnosisOperatingStateTypeStandby:         "Standby",
	model.DeviceDiagnosisOperatingStateTypeFailure:         "Stoerung",
}

// meldeAnlagenstatus gibt einen geaenderten Betriebszustand an die Steuerbox.
// Ohne SPS gilt die Anlage als gestoert, die Bruecke allein kann nichts umsetzen.
func (b *Bruecke) meldeAnlagenstatus() {
	b.mu.Lock()
	register := b.holding[regAnlagenstatus]
	zustand := betriebszustand(b.spsOk, register)
	vorher := b.gemeldeterStatus
	b.gemeldeterStatus = zustand
	// Protokolliert wird nur, was die SPS meldet. Ein Ausfall der SPS steht
	// schon als "SPS-Lebenszeichen ausgefallen" im Log.
	spsMeldung := b.spsOk && register != b.anlagenstatusSps
	if b.spsOk {
		b.anlagenstatusSps = register
	}
	b.mu.Unlock()

	if spsMeldung {
		log.Printf("Anlagenstatus der SPS: %s", zustandTexte[betriebszustand(true, register)])
	}
	if zustand == vorher {
		return
	}
	diagnose, err := server.NewDeviceDiagnosis(b.cem)
	if err != nil {
		log.Printf("Anlagenstatus melden: %v", err)
		return
	}
	diagnose.SetLocalOperatingState(zustand)
}

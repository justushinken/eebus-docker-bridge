package main

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/enbility/eebus-go/features/client"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Diagnose der verbundenen Steuerbox: Was meldet sie ueber SPINE? Gedacht fuer
// den ersten Anschluss eines neuen Geraets (z. B. PROLAN STB-142E): Welche Use
// Cases in welcher Version, auf welchen Entitaeten, mit welchen Szenarien.

type Gegenstelle struct {
	Ski          string                `json:"ski"`
	Geraetetyp   string                `json:"geraetetyp"`
	Hersteller   string                `json:"hersteller"`
	Marke        string                `json:"marke"`
	Modell       string                `json:"modell"`
	Seriennummer string                `json:"seriennummer"`
	Software     string                `json:"software"`
	Hardware     string                `json:"hardware"`
	Entitaeten   []GegenstelleEntitaet `json:"entitaeten"`
	UseCases     []GegenstelleUseCase  `json:"useCases"`
}

type GegenstelleEntitaet struct {
	Adresse string `json:"adresse"`
	Typ     string `json:"typ"`
}

type GegenstelleUseCase struct {
	Entitaet   string `json:"entitaet"`
	Akteur     string `json:"akteur"`
	Name       string `json:"name"`
	Kurz       string `json:"kurz"` // LPC, LPP, ... oder leer
	Version    string `json:"version"`
	Szenarien  []uint `json:"szenarien"`
	Verfuegbar bool   `json:"verfuegbar"`
}

// Kurznamen und Bits (Input-Register 16) der Use Cases, die die Bruecke als
// Gegenstueck erwartet: Akteur auf Seiten der Steuerbox.
var steuerboxUseCases = []struct {
	akteur model.UseCaseActorType
	name   model.UseCaseNameType
	kurz   string
	bit    uint16
}{
	{model.UseCaseActorTypeEnergyGuard, model.UseCaseNameTypeLimitationOfPowerConsumption, "LPC", ucBitLpc},
	{model.UseCaseActorTypeEnergyGuard, model.UseCaseNameTypeLimitationOfPowerProduction, "LPP", ucBitLpp},
	{model.UseCaseActorTypeMonitoringAppliance, model.UseCaseNameTypeMonitoringOfPowerConsumption, "MPC", ucBitMpc},
	{model.UseCaseActorTypeMonitoringAppliance, model.UseCaseNameTypeMonitoringOfGridConnectionPoint, "MGCP", ucBitMgcp},
}

func adresseText(adresse []model.AddressEntityType) string {
	teile := make([]string, len(adresse))
	for i, a := range adresse {
		teile[i] = fmt.Sprint(a)
	}
	return strings.Join(teile, ".")
}

func text[T ~string](wert *T) string {
	if wert == nil {
		return ""
	}
	return string(*wert)
}

// PruefeGegenstelle liest das SPINE-Modell der verbundenen Steuerbox aus dem
// lokalen Zwischenspeicher von spine-go und meldet Aenderungen. Zyklisch
// ausserhalb von mu aufrufen.
func (b *Bruecke) PruefeGegenstelle() {
	b.mu.Lock()
	ski, verbunden := b.partner.SKI, b.verbindung == VerbindungVerbunden
	b.mu.Unlock()
	if !verbunden || ski == "" {
		return
	}
	geraet := b.dienst.LocalDevice().RemoteDeviceForSki(ski)
	if geraet == nil {
		return
	}

	g := &Gegenstelle{Ski: ski, Entitaeten: []GegenstelleEntitaet{}, UseCases: []GegenstelleUseCase{}}
	g.Geraetetyp = text(geraet.DeviceType())
	var info spineapi.EntityRemoteInterface
	for _, e := range geraet.Entities() {
		adresse := e.Address().Entity
		if len(adresse) == 1 && adresse[0] == 0 {
			info = e
		}
		g.Entitaeten = append(g.Entitaeten, GegenstelleEntitaet{Adresse: adresseText(adresse), Typ: string(e.EntityType())})
	}

	var bits uint16
	for _, uc := range geraet.UseCases() {
		entitaet := ""
		if uc.Address != nil {
			entitaet = adresseText(uc.Address.Entity)
		}
		for _, s := range uc.UseCaseSupport {
			eintrag := GegenstelleUseCase{
				Entitaet: entitaet, Akteur: text(uc.Actor), Name: text(s.UseCaseName),
				Version: text(s.UseCaseVersion), Verfuegbar: s.UseCaseAvailable == nil || *s.UseCaseAvailable,
			}
			for _, sz := range s.ScenarioSupport {
				eintrag.Szenarien = append(eintrag.Szenarien, uint(sz))
			}
			for _, bekannt := range steuerboxUseCases {
				if uc.Actor != nil && *uc.Actor == bekannt.akteur && s.UseCaseName != nil && *s.UseCaseName == bekannt.name {
					eintrag.Kurz = bekannt.kurz
					if eintrag.Verfuegbar {
						bits |= bekannt.bit
					}
				}
			}
			g.UseCases = append(g.UseCases, eintrag)
		}
	}

	// Hersteller und Softwarestand: einmal je Verbindung anfordern, die Antwort
	// landet im Zwischenspeicher und erscheint beim naechsten Durchlauf.
	if info != nil {
		if klassifikation, err := client.NewDeviceClassification(b.cem, info); err == nil {
			if d, err := klassifikation.GetManufacturerDetails(); err == nil && d != nil {
				g.Hersteller, g.Marke, g.Modell = text(d.VendorName), text(d.BrandName), text(d.DeviceName)
				g.Seriennummer, g.Software, g.Hardware = text(d.SerialNumber), text(d.SoftwareRevision), text(d.HardwareRevision)
			} else if b.herstellerAnfordern(ski) {
				if _, err := klassifikation.RequestManufacturerDetails(); err != nil {
					log.Printf("Herstellerdaten der Steuerbox anfordern: %v", err)
				}
			}
		}
	}

	b.mu.Lock()
	alt := b.gegenstelle
	b.gegenstelle = g
	b.steuerboxUseCases = bits
	b.mu.Unlock()

	if zusammenfassung(alt) != zusammenfassung(g) {
		log.Printf("Steuerbox meldet: %s", zusammenfassung(g))
	}
}

// herstellerAnfordern liefert true beim ersten Aufruf je Steuerbox.
func (b *Bruecke) herstellerAnfordern(ski string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.herstellerAngefordert == ski {
		return false
	}
	b.herstellerAngefordert = ski
	return true
}

// zusammenfassung ist die Zeile fuer das Ereignisprotokoll.
func zusammenfassung(g *Gegenstelle) string {
	if g == nil {
		return ""
	}
	var ucs []string
	for _, uc := range g.UseCases {
		name := uc.Kurz
		if name == "" {
			name = uc.Name
		}
		ucs = append(ucs, fmt.Sprintf("%s %s (%s, Entitaet %s)", name, uc.Version, uc.Akteur, uc.Entitaet))
	}
	slices.Sort(ucs)
	geraet := strings.TrimSpace(strings.Join([]string{g.Marke, g.Modell}, " "))
	if g.Software != "" {
		geraet += ", Software " + g.Software
	}
	if geraet == "" {
		geraet = "?"
	}
	return fmt.Sprintf("%s, Typ %s, Use Cases: %s", geraet, g.Geraetetyp, strings.Join(ucs, "; "))
}

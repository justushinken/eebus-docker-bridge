package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/enbility/eebus-go/features/client"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
	"github.com/enbility/spine-go/util"
)

// Grenzen und Failsafe-Werte an die Bruecke senden (Aktionen aus dem Web-UI).

func (s *Steuerbox) SendeGrenze(wertW float64, dauer time.Duration, aktiv bool) error {
	ziel, err := s.ziel()
	if err != nil {
		return err
	}
	grenze := ucapi.LoadLimit{Value: wertW, Duration: dauer, IsActive: aktiv}
	if _, err := s.lpc.WriteConsumptionLimit(ziel, grenze, ergebnisMelden("Grenze")); err != nil {
		return err
	}
	log.Printf("Grenze gesendet: %s", beschreibeGrenze(wertW, dauer, aktiv))
	return nil
}

func (s *Steuerbox) SendeFailsafe(grenzeW float64, mindestdauer time.Duration) error {
	// Bereich laut LPC-Spezifikation. Vorab pruefen, sonst waere die Grenze
	// schon geschrieben, bevor eebus-go die Dauer ablehnt.
	if mindestdauer < 2*time.Hour || mindestdauer > 24*time.Hour {
		return errors.New("Failsafe-Mindestdauer muss zwischen 2 und 24 h liegen")
	}
	ziel, err := s.ziel()
	if err != nil {
		return err
	}
	if _, err := s.lpc.WriteFailsafeConsumptionActivePowerLimit(ziel, grenzeW); err != nil {
		return fmt.Errorf("Failsafe-Grenze: %w", err)
	}
	if _, err := s.lpc.WriteFailsafeDurationMinimum(ziel, mindestdauer); err != nil {
		return fmt.Errorf("Failsafe-Mindestdauer: %w", err)
	}
	log.Printf("Failsafe-Werte gesendet: %.0f W, Mindestdauer %v", grenzeW, mindestdauer)
	return nil
}

// ergebnisMelden protokolliert die Antwort der Bruecke auf eine Grenze.
func ergebnisMelden(was string) func(model.ResultDataType, model.MsgCounterType) {
	return func(ergebnis model.ResultDataType, _ model.MsgCounterType) {
		if ergebnis.ErrorNumber != nil && *ergebnis.ErrorNumber != model.ErrorNumberTypeNoError {
			grund := ""
			if ergebnis.Description != nil {
				grund = string(*ergebnis.Description)
			}
			log.Printf("Bruecke lehnt %s ab (Fehler %d): %s", was, *ergebnis.ErrorNumber, grund)
			return
		}
		log.Printf("Bruecke hat %s angenommen", was)
	}
}

func beschreibeGrenze(wertW float64, dauer time.Duration, aktiv bool) string {
	if dauer == 0 {
		return fmt.Sprintf("aktiv=%v, %.0f W, unbefristet", aktiv, wertW)
	}
	return fmt.Sprintf("aktiv=%v, %.0f W, Dauer %v", aktiv, wertW, dauer)
}

func (s *Steuerbox) SendeEinspeisegrenze(wertW float64, dauer time.Duration, aktiv bool) error {
	ziel, err := s.zielFuer(s.lpp, "LPP")
	if err != nil {
		return err
	}
	grenze := ucapi.LoadLimit{Value: wertW, Duration: dauer, IsActive: aktiv}
	if _, err := s.lpp.WriteProductionLimit(ziel, grenze, ergebnisMelden("Einspeisegrenze")); err != nil {
		return err
	}
	log.Printf("Einspeisegrenze gesendet: %s", beschreibeGrenze(wertW, dauer, aktiv))
	return nil
}

func (s *Steuerbox) SendeFailsafeEinspeisung(grenzeW float64, mindestdauer time.Duration) error {
	if mindestdauer < 2*time.Hour || mindestdauer > 24*time.Hour {
		return errors.New("Failsafe-Mindestdauer muss zwischen 2 und 24 h liegen")
	}
	ziel, err := s.zielFuer(s.lpp, "LPP")
	if err != nil {
		return err
	}
	if _, err := s.lpp.WriteFailsafeProductionActivePowerLimit(ziel, grenzeW); err != nil {
		return fmt.Errorf("Failsafe-Einspeisegrenze: %w", err)
	}
	if _, err := s.lpp.WriteFailsafeDurationMinimum(ziel, mindestdauer); err != nil {
		return fmt.Errorf("Failsafe-Mindestdauer: %w", err)
	}
	log.Printf("Failsafe-Werte Einspeisung gesendet: %.0f W, Mindestdauer %v", grenzeW, mindestdauer)
	return nil
}

// SendeGrenzenGemeinsam schreibt Bezugs- und Einspeisegrenze in einer
// Nachricht. eebus-go schreibt jede Grenze einzeln; andere EEBUS-Stacks
// (z. B. in einer echten Steuerbox) koennen beide zusammen schreiben. Die
// Bruecke muss das in LPC und LPP getrennt freigeben.
func (s *Steuerbox) SendeGrenzenGemeinsam(bezugW, einspeisungW float64, dauer time.Duration) error {
	ziel, err := s.ziel()
	if err != nil {
		return err
	}
	if _, err := s.zielFuer(s.lpp, "LPP"); err != nil {
		return err
	}
	steuerung, err := client.NewLoadControl(s.entitaet, ziel)
	if err != nil {
		return err
	}
	var daten []model.LoadControlLimitDataType
	for _, g := range []struct {
		richtung model.EnergyDirectionType
		wertW    float64
	}{{model.EnergyDirectionTypeConsume, bezugW}, {model.EnergyDirectionTypeProduce, einspeisungW}} {
		vorhanden, err := steuerung.GetLimitDataForFilter(model.LoadControlLimitDescriptionDataType{
			LimitType:      util.Ptr(model.LoadControlLimitTypeTypeSignDependentAbsValueLimit),
			LimitDirection: util.Ptr(g.richtung),
			ScopeType:      util.Ptr(model.ScopeTypeTypeActivePowerLimit),
		})
		if err != nil || len(vorhanden) != 1 || vorhanden[0].LimitId == nil {
			return fmt.Errorf("Grenze %s bei der Bruecke nicht gefunden", g.richtung)
		}
		grenze := model.LoadControlLimitDataType{
			LimitId:       vorhanden[0].LimitId,
			IsLimitActive: util.Ptr(true),
			Value:         model.NewScaledNumberType(g.wertW),
		}
		if dauer > 0 {
			grenze.TimePeriod = &model.TimePeriodType{EndTime: model.NewAbsoluteOrRelativeTimeTypeFromDuration(dauer)}
		}
		daten = append(daten, grenze)
	}
	zaehler, err := steuerung.WriteLimitData(daten, nil, nil)
	if err != nil {
		return err
	}
	if zaehler != nil {
		melden := ergebnisMelden("beide Grenzen")
		steuerung.AddResponseCallback(*zaehler, func(msg spineapi.ResponseMessage) {
			if ergebnis, ok := msg.Data.(*model.ResultDataType); ok {
				melden(*ergebnis, *zaehler)
			}
		})
	}
	log.Printf("Beide Grenzen in einer Nachricht gesendet: Bezug %.0f W, Einspeisung %.0f W", bezugW, einspeisungW)
	return nil
}

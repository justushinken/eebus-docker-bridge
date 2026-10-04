// Eine Richtung der Leistungsbegrenzung durch die Steuerbox:
//   iBasis := 2   Bezug (LPC, Paragraf 14a EnWG)
//   iBasis := 17  Einspeisung (LPP, Paragraf 9 EEG)
// Beide Blöcke der Input-Register sind gleich aufgebaut.
//
// Die Grenze bezieht sich laut FNN-Hinweis auf den Netzanschlusspunkt: Die
// Regelung muss sie dort einhalten (Messwert am Netzanschlusspunkt), nicht an
// einer einzelnen Last.
//
// Ist die Brücke ausgefallen, gilt die lokale Ersatzgrenze (sichere Seite, wie
// im Failsafe). Bietet die Brücke den Use Case bewusst nicht an
// (EEBUS_USECASES), gibt es in dieser Richtung keine Begrenzung.
FUNCTION_BLOCK FbEebusBegrenzung
VAR_INPUT
	xBrueckeOk       : BOOL;                     // von FbEebusBruecke
	xUseCaseAktiv    : BOOL;                     // von FbEebusBruecke (xLpcAktiv bzw. xLppAktiv), FALSE = Use Case aus, keine Begrenzung
	iBasis           : INT := 2;                 // 2 = Bezug, 17 = Einspeisung
	udiErsatzgrenzeW : UDINT := 4200;            // gilt, solange die Brücke nicht verfügbar ist
END_VAR
VAR_IN_OUT
	aInputRegister   : ARRAY [0..30] OF WORD;
END_VAR
VAR_OUTPUT
	xBegrenzungAktiv    : BOOL;                  // udiLeistungsgrenzeW muss eingehalten werden
	udiLeistungsgrenzeW : UDINT;                 // nur gültig bei xBegrenzungAktiv
	eZustand            : eLpcZustand;
	udiRestdauerS       : UDINT;                 // Restlaufzeit der Grenze, 0 = unbefristet
	udiGrenzeNetzW      : UDINT;                 // zuletzt empfangene Grenze (Diagnose)
	udiFailsafeGrenzeW  : UDINT;                 // aktuelle Failsafe-Grenze (Diagnose)
	xErsatzgrenze       : BOOL;                  // Ersatzgrenze aktiv, Brücke nicht verfügbar
END_VAR
VAR
	uiRohZustand : UINT;
END_VAR
VAR CONSTANT
	// Offsets im Block, identisch mit blk* in modbus.go
	iBlkZustand          : INT := 0;
	iBlkWirksameGrenze   : INT := 2;
	iBlkBegrenzungAktiv  : INT := 4;
	iBlkGrenzeNetz       : INT := 5;
	iBlkRestdauer        : INT := 7;
	iBlkFailsafeGrenze   : INT := 9;
END_VAR

uiRohZustand := WORD_TO_UINT(aInputRegister[iBasis + iBlkZustand]);

IF xBrueckeOk AND NOT xUseCaseAktiv THEN
	// Use Case abgeschaltet: keine Vorgaben in dieser Richtung
	eZustand            := eLpcZustand.Unbekannt;
	xBegrenzungAktiv    := FALSE;
	udiLeistungsgrenzeW := 0;
	udiRestdauerS       := 0;
	udiGrenzeNetzW      := 0;
	udiFailsafeGrenzeW  := 0;
	xErsatzgrenze       := FALSE;

ELSIF xBrueckeOk AND uiRohZustand <= 4 THEN
	CASE uiRohZustand OF
		0: eZustand := eLpcZustand.Init;
		1: eZustand := eLpcZustand.UnbegrenztGesteuert;
		2: eZustand := eLpcZustand.Begrenzt;
		3: eZustand := eLpcZustand.Failsafe;
		4: eZustand := eLpcZustand.UnbegrenztAutonom;
	END_CASE
	xBegrenzungAktiv    := aInputRegister[iBasis + iBlkBegrenzungAktiv] <> 0;
	udiLeistungsgrenzeW := FuEebusLiesUdint(aRegister := aInputRegister, iIndex := iBasis + iBlkWirksameGrenze);
	udiRestdauerS       := FuEebusLiesUdint(aRegister := aInputRegister, iIndex := iBasis + iBlkRestdauer);
	udiGrenzeNetzW      := FuEebusLiesUdint(aRegister := aInputRegister, iIndex := iBasis + iBlkGrenzeNetz);
	udiFailsafeGrenzeW  := FuEebusLiesUdint(aRegister := aInputRegister, iIndex := iBasis + iBlkFailsafeGrenze);
	xErsatzgrenze       := FALSE;
ELSE
	// Brücke ausgefallen oder Zustand ungültig: wie Failsafe verhalten
	eZustand            := eLpcZustand.Unbekannt;
	xBegrenzungAktiv    := TRUE;
	udiLeistungsgrenzeW := udiErsatzgrenzeW;
	udiRestdauerS       := 0;
	udiGrenzeNetzW      := 0;
	udiFailsafeGrenzeW  := udiErsatzgrenzeW;
	xErsatzgrenze       := TRUE;
END_IF
END_FUNCTION_BLOCK

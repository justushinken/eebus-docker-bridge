// Eine Richtung der Leistungsbegrenzung durch die Steuerbox:
//   iBasis := 2   Bezug (LPC, Paragraf 14a EnWG)
//   iBasis := 17  Einspeisung (LPP, Paragraf 9 EEG)
// Beide Blöcke der Input-Register sind gleich aufgebaut.
//
// Die Grenze bezieht sich laut FNN-Hinweis auf den Netzanschlusspunkt: Die
// Regelung muss sie dort einhalten (Messwert am Netzanschlusspunkt), nicht an
// einer einzelnen Last.
//
// Ist das Gateway ausgefallen, gilt die lokale Ersatzgrenze (sichere Seite, wie
// im Failsafe). Bietet das Gateway den Use Case bewusst nicht an (im Gateway
// abgeschaltet), gibt es in dieser Richtung keine Begrenzung.
//
// Für die Regelung genügen xBegrenzungAktiv und udiLeistungsgrenzeW. Den
// Zustandsautomaten (Init, Begrenzt, Failsafe, ...) führt das Gateway, die
// übrigen Ausgänge dienen der Anzeige.
FUNCTION_BLOCK FbEebusBegrenzung
VAR_INPUT
	xGatewayOk       : BOOL;                     // von FbEebusGateway
	xUseCaseGateway  : BOOL;                     // von FbEebusGateway (xLpcGateway bzw. xLppGateway), FALSE = Use Case im Gateway aus, keine Begrenzung
	iBasis           : INT := 2;                 // 2 = Bezug, 17 = Einspeisung
	udiErsatzgrenzeW : UDINT := 4200;            // gilt, solange das Gateway nicht verfügbar ist
END_VAR
VAR_IN_OUT
	aInputRegister   : ARRAY [0..30] OF WORD;    // GvlEebus.aInputRegister
END_VAR
VAR_OUTPUT
	xBegrenzungAktiv    : BOOL;                  // TRUE = udiLeistungsgrenzeW am Netzanschlusspunkt einhalten
	udiLeistungsgrenzeW : UDINT;                 // einzuhaltende Grenze in W, nur gültig bei xBegrenzungAktiv
	eZustand            : eLpcZustand;           // Zustand laut Gateway, Unbekannt bei Ausfall oder Use Case aus
	udiRestdauerS       : UDINT;                 // Restlaufzeit der Grenze des Netzbetreibers in s, 0 = unbefristet (Diagnose)
	udiGrenzeNetzW      : UDINT;                 // zuletzt empfangene Grenze des Netzbetreibers in W (Diagnose)
	udiFailsafeGrenzeW  : UDINT;                 // aktuelle Failsafe-Grenze in W (Diagnose)
	xErsatzgrenze       : BOOL;                  // Ersatzgrenze aktiv, Gateway nicht verfügbar
END_VAR
VAR
	uiRohZustand : UINT;
END_VAR
VAR CONSTANT
	// Offsets im Block (Register = iBasis + Offset), Belegung siehe GvlEebus
	iBlkZustand          : INT := 0;   // Zustand 0..4 (eLpcZustand)
	iBlkWirksameGrenze   : INT := 2;   // wirksame Grenze in W, 2 Register
	iBlkBegrenzungAktiv  : INT := 4;   // 1 = Grenze einhalten
	iBlkGrenzeNetz       : INT := 5;   // letzte Grenze des Netzbetreibers in W, 2 Register
	iBlkRestdauer        : INT := 7;   // Restlaufzeit in s, 2 Register
	iBlkFailsafeGrenze   : INT := 9;   // Failsafe-Grenze in W, 2 Register
END_VAR

uiRohZustand := WORD_TO_UINT(aInputRegister[iBasis + iBlkZustand]);

IF xGatewayOk AND NOT xUseCaseGateway THEN
	// Use Case abgeschaltet: keine Vorgaben in dieser Richtung
	eZustand            := eLpcZustand.Unbekannt;
	xBegrenzungAktiv    := FALSE;
	udiLeistungsgrenzeW := 0;
	udiRestdauerS       := 0;
	udiGrenzeNetzW      := 0;
	udiFailsafeGrenzeW  := 0;
	xErsatzgrenze       := FALSE;

ELSIF xGatewayOk AND uiRohZustand <= 4 THEN
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
	// Gateway ausgefallen oder Zustand ungültig: wie Failsafe verhalten
	eZustand            := eLpcZustand.Unbekannt;
	xBegrenzungAktiv    := TRUE;
	udiLeistungsgrenzeW := udiErsatzgrenzeW;
	udiRestdauerS       := 0;
	udiGrenzeNetzW      := 0;
	udiFailsafeGrenzeW  := udiErsatzgrenzeW;
	xErsatzgrenze       := TRUE;
END_IF
END_FUNCTION_BLOCK

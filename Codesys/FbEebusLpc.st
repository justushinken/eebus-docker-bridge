// Auswertung der EEBUS-LPC-Brücke (Docker-Container auf derselben Steuerung).
//
// Die Brücke liefert die vom Netzbetreiber vorgegebene Leistungsgrenze
// (Use Case LPC, Paragraf 14a EnWG) als Modbus-Input-Register. Der Baustein
// - überwacht das Lebenszeichen und die Schnittstellenversion der Brücke,
// - dekodiert Zustand, Verbindung und wirksame Leistungsgrenze,
// - fällt bei Ausfall der Brücke auf eine lokale Ersatzgrenze zurück (sichere Seite),
// - erzeugt die Holding-Register (SPS-Lebenszeichen, Nennleistung) für die Brücke.
//
// Der Baustein greift auf keine globalen Variablen zu. Die Kopplung an das
// Modbus-Prozessabbild erfolgt beim Aufruf.
FUNCTION_BLOCK FbEebusLpc
VAR_INPUT
	xFreigabe           : BOOL;                    // Auswertung aktiv
	aInputRegister      : ARRAY [0..13] OF WORD;   // Abbild der Input-Register der Brücke
	udiNennleistungMaxW : UDINT;                   // max. Leistungsaufnahme der Anlage in W, wird an die Steuerbox gemeldet
	udiErsatzgrenzeW    : UDINT := 4200;           // gilt, solange die Brücke nicht verfügbar ist
	tBrueckeTimeout     : TIME := T#10S;           // max. Zeit ohne Änderung des Brücken-Lebenszeichens
END_VAR
VAR_OUTPUT
	xBrueckeOk          : BOOL;                    // Brücke lebt und Schnittstellenversion passt
	xBegrenzungAktiv    : BOOL;                    // udiLeistungsgrenzeW muss eingehalten werden
	udiLeistungsgrenzeW : UDINT;                   // nur gültig bei xBegrenzungAktiv
	eZustand            : eLpcZustand;
	eVerbindung         : eEebusVerbindung;
	udiRestdauerS       : UDINT;                   // Restlaufzeit der Grenze, 0 = unbefristet
	uiHeartbeatAlterS   : UINT;                    // Sekunden seit letztem Heartbeat der Steuerbox
	xFehler             : BOOL;
	udiFehlerId         : UDINT;                   // siehe Konstanten udiFehler*
	aHoldingRegister    : ARRAY [0..2] OF WORD;    // an die Brücke zu schreibende Register
END_VAR
VAR
	wLebenszeichenAlt   : WORD;
	tonBrueckeTimeout   : TON;
	tonSekundentakt     : TON;
	uiLebenszeichenSps  : UINT;
	uiRohZustand        : UINT;
	uiRohVerbindung     : UINT;
	xVersionOk          : BOOL;
	xZustandGueltig     : BOOL;
END_VAR
VAR CONSTANT
	uiSchnittstellenVersion   : UINT := 1;         // muss zu SchnittstellenVersion in modbus.go passen

	// Input-Register, identisch mit modbus.go
	iRegLebenszeichen         : INT := 0;
	iRegVersion               : INT := 1;
	iRegZustand               : INT := 2;
	iRegVerbindung            : INT := 3;
	iRegWirksameGrenzeHi      : INT := 4;
	iRegWirksameGrenzeLo      : INT := 5;
	iRegBegrenzungAktiv       : INT := 6;
	iRegRestdauerHi           : INT := 9;
	iRegRestdauerLo           : INT := 10;
	iRegHeartbeatAlter        : INT := 13;

	// Holding-Register, identisch mit modbus.go
	iRegSpsLebenszeichen      : INT := 0;
	iRegNennleistungHi        : INT := 1;
	iRegNennleistungLo        : INT := 2;

	// Fehlernummern
	udiFehlerKeine            : UDINT := 0;
	udiFehlerBrueckeTimeout   : UDINT := 1;    // Lebenszeichen der Brücke steht
	udiFehlerVersion          : UDINT := 2;    // Registerlayout passt nicht
	udiFehlerZustandUngueltig : UDINT := 3;    // unbekannter Zustandswert
END_VAR

// ---------------------------------------------------------------------------
// Register an die Brücke: Lebenszeichen im Sekundentakt und Nennleistung
// ---------------------------------------------------------------------------
tonSekundentakt(IN := NOT tonSekundentakt.Q, PT := T#1S);
IF tonSekundentakt.Q THEN
	uiLebenszeichenSps := uiLebenszeichenSps + 1;   // Überlauf ist gewollt
END_IF

aHoldingRegister[iRegSpsLebenszeichen] := UINT_TO_WORD(uiLebenszeichenSps);
aHoldingRegister[iRegNennleistungHi]   := UDINT_TO_WORD(SHR(udiNennleistungMaxW, 16));
aHoldingRegister[iRegNennleistungLo]   := UDINT_TO_WORD(udiNennleistungMaxW AND 16#0000FFFF);

// ---------------------------------------------------------------------------
// Überwachung der Brücke
// ---------------------------------------------------------------------------
tonBrueckeTimeout(IN := xFreigabe AND (aInputRegister[iRegLebenszeichen] = wLebenszeichenAlt),
                  PT := tBrueckeTimeout);
wLebenszeichenAlt := aInputRegister[iRegLebenszeichen];

uiRohZustand    := WORD_TO_UINT(aInputRegister[iRegZustand]);
uiRohVerbindung := WORD_TO_UINT(aInputRegister[iRegVerbindung]);
xVersionOk      := WORD_TO_UINT(aInputRegister[iRegVersion]) = uiSchnittstellenVersion;
xZustandGueltig := uiRohZustand <= 4;

xBrueckeOk := xFreigabe AND NOT tonBrueckeTimeout.Q AND xVersionOk AND xZustandGueltig;

// ---------------------------------------------------------------------------
// Auswertung
// ---------------------------------------------------------------------------
IF NOT xFreigabe THEN
	xBegrenzungAktiv    := FALSE;
	udiLeistungsgrenzeW := 0;
	eZustand            := eLpcZustand.Unbekannt;
	eVerbindung         := eEebusVerbindung.Unbekannt;
	udiRestdauerS       := 0;
	uiHeartbeatAlterS   := 0;
	xFehler             := FALSE;
	udiFehlerId         := udiFehlerKeine;

ELSIF xBrueckeOk THEN
	CASE uiRohZustand OF
		0: eZustand := eLpcZustand.Init;
		1: eZustand := eLpcZustand.UnbegrenztGesteuert;
		2: eZustand := eLpcZustand.Begrenzt;
		3: eZustand := eLpcZustand.Failsafe;
		4: eZustand := eLpcZustand.UnbegrenztAutonom;
	ELSE
		eZustand := eLpcZustand.Unbekannt;
	END_CASE

	CASE uiRohVerbindung OF
		0: eVerbindung := eEebusVerbindung.KeinPartner;
		1: eVerbindung := eEebusVerbindung.Getrennt;
		2: eVerbindung := eEebusVerbindung.Verbunden;
	ELSE
		eVerbindung := eEebusVerbindung.Unbekannt;
	END_CASE

	xBegrenzungAktiv    := aInputRegister[iRegBegrenzungAktiv] <> 0;
	udiLeistungsgrenzeW := SHL(WORD_TO_UDINT(aInputRegister[iRegWirksameGrenzeHi]), 16)
	                       OR WORD_TO_UDINT(aInputRegister[iRegWirksameGrenzeLo]);
	udiRestdauerS       := SHL(WORD_TO_UDINT(aInputRegister[iRegRestdauerHi]), 16)
	                       OR WORD_TO_UDINT(aInputRegister[iRegRestdauerLo]);
	uiHeartbeatAlterS   := WORD_TO_UINT(aInputRegister[iRegHeartbeatAlter]);
	xFehler             := FALSE;
	udiFehlerId         := udiFehlerKeine;

ELSE
	// Brücke ausgefallen oder inkompatibel: Zustand der Steuerbox unbekannt,
	// daher wie Failsafe verhalten und die lokale Ersatzgrenze einhalten.
	xBegrenzungAktiv    := TRUE;
	udiLeistungsgrenzeW := udiErsatzgrenzeW;
	eZustand            := eLpcZustand.Unbekannt;
	eVerbindung         := eEebusVerbindung.Unbekannt;
	udiRestdauerS       := 0;
	uiHeartbeatAlterS   := 16#FFFF;
	xFehler             := TRUE;

	IF tonBrueckeTimeout.Q THEN
		udiFehlerId := udiFehlerBrueckeTimeout;
	ELSIF NOT xVersionOk THEN
		udiFehlerId := udiFehlerVersion;
	ELSE
		udiFehlerId := udiFehlerZustandUngueltig;
	END_IF
END_IF
END_FUNCTION_BLOCK

// Auswertung des EEBUS-Gateways, nur LPC (Docker-Container auf derselben Steuerung).
//
// VERALTET: Für neue Projekte FbEebusGateway, FbEebusBegrenzung und
// FbEebusMesswerte verwenden (LPP, Messwerte, Anlagenstatus). Dieser Baustein
// läuft mit neueren Gateway-Versionen unverändert weiter, weil die Register 0 bis 13
// und 0 bis 2 gleich geblieben sind (Schnittstellenversion 1).
//
// Das Gateway liefert die vom Netzbetreiber vorgegebene Leistungsgrenze
// (Use Case LPC, Paragraf 14a EnWG) als Modbus-Input-Register. Der Baustein
// - überwacht das Lebenszeichen und die Schnittstellenversion des Gateways,
// - dekodiert Zustand, Verbindung und wirksame Leistungsgrenze,
// - fällt bei Ausfall des Gateways auf eine lokale Ersatzgrenze zurück (sichere Seite),
// - erzeugt die Holding-Register (SPS-Lebenszeichen, Nennleistung) für das Gateway.
//
// Der Baustein greift auf keine globalen Variablen zu. Die Kopplung an das
// Modbus-Prozessabbild erfolgt beim Aufruf.
FUNCTION_BLOCK FbEebusLpc
VAR_INPUT
	xFreigabe           : BOOL;                    // Auswertung aktiv
	aInputRegister      : ARRAY [0..13] OF WORD;   // Abbild der Input-Register des Gateways
	udiNennleistungMaxW : UDINT;                   // max. Leistungsaufnahme der Anlage in W, wird an die Steuerbox gemeldet
	udiErsatzgrenzeW    : UDINT := 4200;           // gilt, solange das Gateway nicht verfügbar ist
	tGatewayTimeout     : TIME := T#10S;           // max. Zeit ohne Änderung des Gateway-Lebenszeichens
END_VAR
VAR_OUTPUT
	xGatewayOk          : BOOL;                    // Gateway lebt und Schnittstellenversion passt
	xBegrenzungAktiv    : BOOL;                    // udiLeistungsgrenzeW muss eingehalten werden
	udiLeistungsgrenzeW : UDINT;                   // nur gültig bei xBegrenzungAktiv
	eZustand            : eLpcZustand;
	eVerbindung         : eEebusVerbindung;
	udiRestdauerS       : UDINT;                   // Restlaufzeit der Grenze, 0 = unbefristet
	uiHeartbeatAlterS   : UINT;                    // Sekunden seit letztem Heartbeat der Steuerbox
	xFehler             : BOOL;
	udiFehlerId         : UDINT;                   // siehe Konstanten udiFehler*
	aHoldingRegister    : ARRAY [0..2] OF WORD;    // an das Gateway zu schreibende Register
END_VAR
VAR
	wLebenszeichenAlt   : WORD;
	tonGatewayTimeout   : TON;
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
	udiFehlerGatewayTimeout   : UDINT := 1;    // Lebenszeichen des Gateways steht
	udiFehlerVersion          : UDINT := 2;    // Registerlayout passt nicht
	udiFehlerZustandUngueltig : UDINT := 3;    // unbekannter Zustandswert
END_VAR

// ---------------------------------------------------------------------------
// Register an das Gateway: Lebenszeichen im Sekundentakt und Nennleistung
// ---------------------------------------------------------------------------
tonSekundentakt(IN := NOT tonSekundentakt.Q, PT := T#1S);
IF tonSekundentakt.Q THEN
	uiLebenszeichenSps := uiLebenszeichenSps + 1;   // Überlauf ist gewollt
END_IF

aHoldingRegister[iRegSpsLebenszeichen] := UINT_TO_WORD(uiLebenszeichenSps);
aHoldingRegister[iRegNennleistungHi]   := UDINT_TO_WORD(SHR(udiNennleistungMaxW, 16));
aHoldingRegister[iRegNennleistungLo]   := UDINT_TO_WORD(udiNennleistungMaxW AND 16#0000FFFF);

// ---------------------------------------------------------------------------
// Überwachung des Gateways
// ---------------------------------------------------------------------------
tonGatewayTimeout(IN := xFreigabe AND (aInputRegister[iRegLebenszeichen] = wLebenszeichenAlt),
                  PT := tGatewayTimeout);
wLebenszeichenAlt := aInputRegister[iRegLebenszeichen];

uiRohZustand    := WORD_TO_UINT(aInputRegister[iRegZustand]);
uiRohVerbindung := WORD_TO_UINT(aInputRegister[iRegVerbindung]);
xVersionOk      := WORD_TO_UINT(aInputRegister[iRegVersion]) = uiSchnittstellenVersion;
xZustandGueltig := uiRohZustand <= 4;

xGatewayOk := xFreigabe AND NOT tonGatewayTimeout.Q AND xVersionOk AND xZustandGueltig;

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

ELSIF xGatewayOk THEN
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
	// Gateway ausgefallen oder inkompatibel: Zustand der Steuerbox unbekannt,
	// daher wie Failsafe verhalten und die lokale Ersatzgrenze einhalten.
	xBegrenzungAktiv    := TRUE;
	udiLeistungsgrenzeW := udiErsatzgrenzeW;
	eZustand            := eLpcZustand.Unbekannt;
	eVerbindung         := eEebusVerbindung.Unbekannt;
	udiRestdauerS       := 0;
	uiHeartbeatAlterS   := 16#FFFF;
	xFehler             := TRUE;

	IF tonGatewayTimeout.Q THEN
		udiFehlerId := udiFehlerGatewayTimeout;
	ELSIF NOT xVersionOk THEN
		udiFehlerId := udiFehlerVersion;
	ELSE
		udiFehlerId := udiFehlerZustandUngueltig;
	END_IF
END_IF
END_FUNCTION_BLOCK

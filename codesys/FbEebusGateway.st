// Verbindung zum EEBUS-Gateway (Erweiterungsversion 2: LPC, LPP, MPC, MGCP).
//
// Der Baustein
// - überwacht Lebenszeichen und Versionen des Gateways (xGatewayOk),
// - dekodiert Verbindung, Heartbeat und die Use-Case-Masken,
// - schreibt SPS-Lebenszeichen, Nennleistungen und Anlagenstatus in die
//   Holding-Register (Register 0 bis 4 und 7).
//
// Die Begrenzungen je Richtung wertet FbEebusBegrenzung aus, die Messwerte
// schreibt FbEebusMesswerte. Alle drei arbeiten auf denselben Registerabbildern
// (GvlEebus, dort auch die Belegung aller Register).
FUNCTION_BLOCK FbEebusGateway
VAR_INPUT
	xFreigabe                 : BOOL;            // Auswertung aktiv
	udiNennleistungBezugW     : UDINT;           // max. Leistungsaufnahme der Anlage in W (LPC), 0 = Einstellung im Gateway
	udiNennleistungErzeugungW : UDINT;           // max. Einspeiseleistung in W (LPP), 0 = Einstellung im Gateway
	eStatus                   : eAnlagenstatus;  // Störung der Anlage an die Steuerbox melden
	tGatewayTimeout           : TIME := T#10S;   // max. Zeit ohne Änderung des Gateway-Lebenszeichens
END_VAR
VAR_IN_OUT
	aInputRegister   : ARRAY [0..30] OF WORD;    // GvlEebus.aInputRegister
	aHoldingRegister : ARRAY [0..27] OF WORD;    // GvlEebus.aHoldingRegister
END_VAR
VAR_OUTPUT
	xGatewayOk          : BOOL;                  // Gateway lebt und beide Versionen passen; sonst gelten die Ersatzgrenzen
	eVerbindung         : eEebusVerbindung;      // Verbindung Gateway <-> Steuerbox
	uiHeartbeatAlterS   : UINT;                  // Sekunden seit letztem Heartbeat der Steuerbox, 65535 = nie

	// Use Cases, die das Gateway anbietet (Register 15, im Gateway fest
	// eingestellt). Steuern, ob FbEebusBegrenzung eine Richtung auswertet.
	wUseCasesGateway    : WORD;                  // Bit 0 LPC, 1 LPP, 2 MPC, 3 MGCP
	xLpcGateway         : BOOL;                  // Bezug begrenzen (LPC) angeboten
	xLppGateway         : BOOL;                  // Einspeisung begrenzen (LPP) angeboten
	xMpcGateway         : BOOL;                  // Messwerte der Anlage (MPC) angeboten
	xMgcpGateway        : BOOL;                  // Messwerte Netzanschlusspunkt (MGCP) angeboten

	// Use Cases, die die verbundene Steuerbox meldet (Register 16), nur zur
	// Information. 0 ohne Verbindung.
	wUseCasesSteuerbox  : WORD;                  // Bits wie wUseCasesGateway

	udiFailsafeMindestdauerS : UDINT;            // Failsafe-Mindestdauer in s, gemeinsam für LPC und LPP
	xFehler             : BOOL;
	udiFehlerId         : UDINT;                 // siehe Konstanten udiFehler*
END_VAR
VAR
	wLebenszeichenAlt   : WORD;
	tonGatewayTimeout   : TON;
	tonSekundentakt     : TON;
	uiLebenszeichenSps  : UINT;
	xVersionOk          : BOOL;
END_VAR
VAR CONSTANT
	// Versionsprüfung. Register 1 muss genau uiSchnittstellenVersion sein.
	// Register 14 (Erweiterungsversion) muss mindestens uiErweiterungsVersionMin
	// sein: Erst ab Erweiterung 2 liefert das Gateway die Register 14 bis 30
	// (LPP, Use Cases, Mindestdauer). Ein älteres Gateway, oder ein FC04-Kanal,
	// der weniger als 31 Register liest, liefert dort 0. Dann meldet der
	// Baustein Fehler 2, statt mit falschen Werten zu arbeiten. Ein neueres
	// Gateway (Erweiterung 3, 4, ...) ist in Ordnung: Es hängt Register nur an.
	uiSchnittstellenVersion  : UINT := 1;   // Gateway: SchnittstellenVersion
	uiErweiterungsVersionMin : UINT := 2;   // Gateway: ErweiterungsVersion

	// Input-Register (Belegung siehe GvlEebus)
	iRegLebenszeichen       : INT := 0;    // Lebenszeichen Gateway
	iRegVersion             : INT := 1;    // Schnittstellenversion
	iRegVerbindung          : INT := 3;    // Verbindung (aus dem Block Bezug, in beiden Blöcken gleich)
	iRegHeartbeatAlter      : INT := 13;   // Heartbeat-Alter (aus dem Block Bezug, in beiden Blöcken gleich)
	iRegErweiterung         : INT := 14;   // Erweiterungsversion
	iRegUseCasesGateway     : INT := 15;   // Use Cases des Gateways
	iRegUseCasesSteuerbox   : INT := 16;   // Use Cases der Steuerbox
	iRegMindestdauer        : INT := 29;   // Failsafe-Mindestdauer, 2 Register

	// Holding-Register (Belegung siehe GvlEebus)
	iRegSpsLebenszeichen      : INT := 0;  // Lebenszeichen SPS
	iRegNennleistung          : INT := 1;  // Nennleistung Bezug, 2 Register
	iRegNennleistungErzeugung : INT := 3;  // Nennleistung Erzeugung, 2 Register
	iRegAnlagenstatus         : INT := 7;  // Anlagenstatus

	// Bits der Use-Case-Masken (Register 15 und 16)
	wUseCaseLpc             : WORD := 16#0001;
	wUseCaseLpp             : WORD := 16#0002;
	wUseCaseMpc             : WORD := 16#0004;
	wUseCaseMgcp            : WORD := 16#0008;

	// Fehlernummern
	udiFehlerKeine          : UDINT := 0;
	udiFehlerGatewayTimeout : UDINT := 1;   // Lebenszeichen des Gateways steht (Container gestoppt, Modbus-Verbindung weg)
	udiFehlerVersion        : UDINT := 2;   // Register 1 <> 1 oder Register 14 < 2: Gateway zu alt oder FC04-Kanal kürzer als 31 Register
END_VAR

// ---------------------------------------------------------------------------
// Register an das Gateway
// ---------------------------------------------------------------------------
tonSekundentakt(IN := NOT tonSekundentakt.Q, PT := T#1S);
IF tonSekundentakt.Q THEN
	uiLebenszeichenSps := uiLebenszeichenSps + 1;   // Überlauf ist gewollt
END_IF

aHoldingRegister[iRegSpsLebenszeichen] := UINT_TO_WORD(uiLebenszeichenSps);
FuEebusSchreibeUdint(aRegister := aHoldingRegister, iIndex := iRegNennleistung, udiWert := udiNennleistungBezugW);
FuEebusSchreibeUdint(aRegister := aHoldingRegister, iIndex := iRegNennleistungErzeugung, udiWert := udiNennleistungErzeugungW);
aHoldingRegister[iRegAnlagenstatus] := TO_WORD(eStatus);

// ---------------------------------------------------------------------------
// Überwachung des Gateways
// ---------------------------------------------------------------------------
tonGatewayTimeout(IN := xFreigabe AND (aInputRegister[iRegLebenszeichen] = wLebenszeichenAlt),
                  PT := tGatewayTimeout);
wLebenszeichenAlt := aInputRegister[iRegLebenszeichen];

xVersionOk := WORD_TO_UINT(aInputRegister[iRegVersion]) = uiSchnittstellenVersion
              AND WORD_TO_UINT(aInputRegister[iRegErweiterung]) >= uiErweiterungsVersionMin;
xGatewayOk := xFreigabe AND NOT tonGatewayTimeout.Q AND xVersionOk;

IF xGatewayOk THEN
	CASE WORD_TO_UINT(aInputRegister[iRegVerbindung]) OF
		0: eVerbindung := eEebusVerbindung.KeinPartner;
		1: eVerbindung := eEebusVerbindung.Getrennt;
		2: eVerbindung := eEebusVerbindung.Verbunden;
	ELSE
		eVerbindung := eEebusVerbindung.Unbekannt;
	END_CASE
	uiHeartbeatAlterS        := WORD_TO_UINT(aInputRegister[iRegHeartbeatAlter]);
	wUseCasesGateway         := aInputRegister[iRegUseCasesGateway];
	wUseCasesSteuerbox       := aInputRegister[iRegUseCasesSteuerbox];
	udiFailsafeMindestdauerS := FuEebusLiesUdint(aRegister := aInputRegister, iIndex := iRegMindestdauer);
	xFehler                  := FALSE;
	udiFehlerId              := udiFehlerKeine;
ELSE
	eVerbindung              := eEebusVerbindung.Unbekannt;
	uiHeartbeatAlterS        := 16#FFFF;
	wUseCasesGateway         := 0;
	wUseCasesSteuerbox       := 0;
	udiFailsafeMindestdauerS := 0;
	xFehler                  := xFreigabe;
	IF NOT xFreigabe THEN
		udiFehlerId := udiFehlerKeine;
	ELSIF tonGatewayTimeout.Q THEN
		udiFehlerId := udiFehlerGatewayTimeout;
	ELSE
		udiFehlerId := udiFehlerVersion;
	END_IF
END_IF

xLpcGateway  := (wUseCasesGateway AND wUseCaseLpc) <> 0;
xLppGateway  := (wUseCasesGateway AND wUseCaseLpp) <> 0;
xMpcGateway  := (wUseCasesGateway AND wUseCaseMpc) <> 0;
xMgcpGateway := (wUseCasesGateway AND wUseCaseMgcp) <> 0;
END_FUNCTION_BLOCK

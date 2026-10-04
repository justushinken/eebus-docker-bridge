// Verbindung zur EEBUS-Brücke (Erweiterungsversion 2: LPC, LPP, MPC, MGCP).
//
// Der Baustein
// - überwacht Lebenszeichen und Schnittstellenversion der Brücke,
// - dekodiert Verbindung, Heartbeat und die Use-Case-Masken,
// - schreibt SPS-Lebenszeichen, Nennleistungen und Anlagenstatus in die
//   Holding-Register (Register 0 bis 4 und 7).
//
// Die Begrenzungen je Richtung wertet FbEebusBegrenzung aus, die Messwerte
// schreibt FbEebusMesswerte. Alle drei arbeiten auf denselben Registerabbildern.
FUNCTION_BLOCK FbEebusBruecke
VAR_INPUT
	xFreigabe                 : BOOL;            // Auswertung aktiv
	udiNennleistungBezugW     : UDINT;           // max. Leistungsaufnahme der Anlage in W (LPC), 0 = Vorgabe der Brücke
	udiNennleistungErzeugungW : UDINT;           // max. Einspeiseleistung in W (LPP), 0 = Vorgabe der Brücke
	eStatus                   : eAnlagenstatus;  // Störung der Anlage an die Steuerbox melden
	tBrueckeTimeout           : TIME := T#10S;   // max. Zeit ohne Änderung des Brücken-Lebenszeichens
END_VAR
VAR_IN_OUT
	aInputRegister   : ARRAY [0..30] OF WORD;    // Abbild der Input-Register der Brücke
	aHoldingRegister : ARRAY [0..27] OF WORD;    // an die Brücke zu schreibende Register
END_VAR
VAR_OUTPUT
	xBrueckeOk          : BOOL;                  // Brücke lebt und Version passt
	eVerbindung         : eEebusVerbindung;
	uiHeartbeatAlterS   : UINT;                  // Sekunden seit letztem Heartbeat der Steuerbox, 65535 = nie
	xLpcAktiv           : BOOL;                  // Use Case von der Brücke angeboten ...
	xLppAktiv           : BOOL;
	xMpcAktiv           : BOOL;
	xMgcpAktiv          : BOOL;
	wUseCasesSteuerbox  : WORD;                  // ... und von der Steuerbox unterstützt (Bits wie wUseCases*)
	udiFailsafeMindestdauerS : UDINT;            // gemeinsam für LPC und LPP
	xFehler             : BOOL;
	udiFehlerId         : UDINT;                 // siehe Konstanten udiFehler*
END_VAR
VAR
	wLebenszeichenAlt   : WORD;
	tonBrueckeTimeout   : TON;
	tonSekundentakt     : TON;
	uiLebenszeichenSps  : UINT;
	xVersionOk          : BOOL;
	wUseCases           : WORD;
END_VAR
VAR CONSTANT
	uiSchnittstellenVersion : UINT := 1;   // SchnittstellenVersion in modbus.go
	uiErweiterungMindestens : UINT := 2;   // ErweiterungsVersion in modbus.go

	// Input-Register, identisch mit modbus.go
	iRegLebenszeichen       : INT := 0;
	iRegVersion             : INT := 1;
	iRegVerbindung          : INT := 3;
	iRegHeartbeatAlter      : INT := 13;
	iRegErweiterung         : INT := 14;
	iRegUseCasesLokal       : INT := 15;
	iRegUseCasesSteuerbox   : INT := 16;
	iRegMindestdauer        : INT := 29;

	// Holding-Register, identisch mit modbus.go
	iRegSpsLebenszeichen    : INT := 0;
	iRegNennleistung        : INT := 1;
	iRegNennleistungErzeugung : INT := 3;
	iRegAnlagenstatus       : INT := 7;

	// Bits der Use-Case-Masken
	wUseCaseLpc             : WORD := 16#0001;
	wUseCaseLpp             : WORD := 16#0002;
	wUseCaseMpc             : WORD := 16#0004;
	wUseCaseMgcp            : WORD := 16#0008;

	// Fehlernummern
	udiFehlerKeine          : UDINT := 0;
	udiFehlerBrueckeTimeout : UDINT := 1;   // Lebenszeichen der Brücke steht
	udiFehlerVersion        : UDINT := 2;   // Registerlayout passt nicht (Brücke zu alt?)
END_VAR

// ---------------------------------------------------------------------------
// Register an die Brücke
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
// Überwachung der Brücke
// ---------------------------------------------------------------------------
tonBrueckeTimeout(IN := xFreigabe AND (aInputRegister[iRegLebenszeichen] = wLebenszeichenAlt),
                  PT := tBrueckeTimeout);
wLebenszeichenAlt := aInputRegister[iRegLebenszeichen];

xVersionOk := WORD_TO_UINT(aInputRegister[iRegVersion]) = uiSchnittstellenVersion
              AND WORD_TO_UINT(aInputRegister[iRegErweiterung]) >= uiErweiterungMindestens;
xBrueckeOk := xFreigabe AND NOT tonBrueckeTimeout.Q AND xVersionOk;

IF xBrueckeOk THEN
	CASE WORD_TO_UINT(aInputRegister[iRegVerbindung]) OF
		0: eVerbindung := eEebusVerbindung.KeinPartner;
		1: eVerbindung := eEebusVerbindung.Getrennt;
		2: eVerbindung := eEebusVerbindung.Verbunden;
	ELSE
		eVerbindung := eEebusVerbindung.Unbekannt;
	END_CASE
	uiHeartbeatAlterS        := WORD_TO_UINT(aInputRegister[iRegHeartbeatAlter]);
	wUseCases                := aInputRegister[iRegUseCasesLokal];
	wUseCasesSteuerbox       := aInputRegister[iRegUseCasesSteuerbox];
	udiFailsafeMindestdauerS := FuEebusLiesUdint(aRegister := aInputRegister, iIndex := iRegMindestdauer);
	xFehler                  := FALSE;
	udiFehlerId              := udiFehlerKeine;
ELSE
	eVerbindung              := eEebusVerbindung.Unbekannt;
	uiHeartbeatAlterS        := 16#FFFF;
	wUseCases                := 0;
	wUseCasesSteuerbox       := 0;
	udiFailsafeMindestdauerS := 0;
	xFehler                  := xFreigabe;
	IF NOT xFreigabe THEN
		udiFehlerId := udiFehlerKeine;
	ELSIF tonBrueckeTimeout.Q THEN
		udiFehlerId := udiFehlerBrueckeTimeout;
	ELSE
		udiFehlerId := udiFehlerVersion;
	END_IF
END_IF

xLpcAktiv  := (wUseCases AND wUseCaseLpc) <> 0;
xLppAktiv  := (wUseCases AND wUseCaseLpp) <> 0;
xMpcAktiv  := (wUseCases AND wUseCaseMpc) <> 0;
xMgcpAktiv := (wUseCases AND wUseCaseMgcp) <> 0;
END_FUNCTION_BLOCK

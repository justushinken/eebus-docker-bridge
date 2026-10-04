// Schreibt die Messwerte für MPC (Anlage) und MGCP (Netzanschlusspunkt) in
// die Holding-Register 5 bis 60 der Brücke. Welche Werte die Brücke an die
// Steuerbox weitergibt, steht in ihrer Konfiguration (MPC_MESSWERTE,
// MGCP_MESSWERTE); ob ein Wert gerade gültig ist, in wGueltig der Strukturen.
//
// Mindestens die aktuelle Wirkleistung der Anlage muss die Steuerbox abfragen
// können (FNN-Hinweis 4.1.2.3). Ohne gültigen Wert meldet die Brücke ihn als
// ungültig.
FUNCTION_BLOCK FbEebusMesswerte
VAR_INPUT
	stMpc  : ST_EebusMpc;
	stMgcp : ST_EebusMgcp;
END_VAR
VAR_IN_OUT
	aHoldingRegister : ARRAY [0..60] OF WORD;
END_VAR
VAR
	i : INT;
END_VAR
VAR CONSTANT
	// Holding-Register, identisch mit modbus.go
	iRegMpcMaske        : INT := 5;
	iRegMgcpMaske       : INT := 6;
	iRegMpcP            : INT := 8;
	iRegMpcPL1          : INT := 10;
	iRegMpcEBezug       : INT := 16;
	iRegMpcEErzeugung   : INT := 20;
	iRegMpcIL1          : INT := 24;
	iRegMpcUL1          : INT := 30;
	iRegMpcF            : INT := 33;
	iRegMgcpP           : INT := 40;
	iRegMgcpEEinspeisung: INT := 42;
	iRegMgcpEBezug      : INT := 46;
	iRegMgcpIL1         : INT := 50;
	iRegMgcpUL1         : INT := 56;
	iRegMgcpF           : INT := 59;
	iRegMgcpPvFaktor    : INT := 60;
END_VAR

aHoldingRegister[iRegMpcMaske]  := stMpc.wGueltig;
aHoldingRegister[iRegMgcpMaske] := stMgcp.wGueltig;

// MPC
FuEebusSchreibeDint(aRegister := aHoldingRegister, iIndex := iRegMpcP, diWert := stMpc.diLeistungW);
FuEebusSchreibeUlint(aRegister := aHoldingRegister, iIndex := iRegMpcEBezug, ulWert := stMpc.ulEnergieBezugWh);
FuEebusSchreibeUlint(aRegister := aHoldingRegister, iIndex := iRegMpcEErzeugung, ulWert := stMpc.ulEnergieErzeugungWh);
aHoldingRegister[iRegMpcF] := UINT_TO_WORD(stMpc.uiFrequenz001Hz);
FOR i := 1 TO 3 DO
	FuEebusSchreibeDint(aRegister := aHoldingRegister, iIndex := iRegMpcPL1 + 2 * (i - 1), diWert := stMpc.aLeistungPhaseW[i]);
	FuEebusSchreibeDint(aRegister := aHoldingRegister, iIndex := iRegMpcIL1 + 2 * (i - 1), diWert := stMpc.aStromPhaseMA[i]);
	aHoldingRegister[iRegMpcUL1 + i - 1] := UINT_TO_WORD(stMpc.aSpannungPhase01V[i]);
END_FOR

// MGCP
FuEebusSchreibeDint(aRegister := aHoldingRegister, iIndex := iRegMgcpP, diWert := stMgcp.diLeistungW);
FuEebusSchreibeUlint(aRegister := aHoldingRegister, iIndex := iRegMgcpEEinspeisung, ulWert := stMgcp.ulEnergieEinspeisungWh);
FuEebusSchreibeUlint(aRegister := aHoldingRegister, iIndex := iRegMgcpEBezug, ulWert := stMgcp.ulEnergieBezugWh);
aHoldingRegister[iRegMgcpF]        := UINT_TO_WORD(stMgcp.uiFrequenz001Hz);
aHoldingRegister[iRegMgcpPvFaktor] := UINT_TO_WORD(stMgcp.uiPvFaktor01Prozent);
FOR i := 1 TO 3 DO
	FuEebusSchreibeDint(aRegister := aHoldingRegister, iIndex := iRegMgcpIL1 + 2 * (i - 1), diWert := stMgcp.aStromPhaseMA[i]);
	aHoldingRegister[iRegMgcpUL1 + i - 1] := UINT_TO_WORD(stMgcp.aSpannungPhase01V[i]);
END_FOR
END_FUNCTION_BLOCK

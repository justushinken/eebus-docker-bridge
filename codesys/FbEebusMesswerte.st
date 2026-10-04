// Schreibt Leistung und Energie für MPC (Anlage) und MGCP (Netzanschlusspunkt)
// in die Holding-Register 5, 6 und 8 bis 27 der Brücke. Ob ein Wert gerade
// gültig ist, steht in wGueltig der Strukturen.
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
	aHoldingRegister : ARRAY [0..27] OF WORD;
END_VAR
VAR CONSTANT
	// Holding-Register, identisch mit modbus.go
	iRegMpcMaske         : INT := 5;
	iRegMgcpMaske        : INT := 6;
	iRegMpcP             : INT := 8;
	iRegMpcEBezug        : INT := 10;
	iRegMpcEErzeugung    : INT := 14;
	iRegMgcpP            : INT := 18;
	iRegMgcpEEinspeisung : INT := 20;
	iRegMgcpEBezug       : INT := 24;
END_VAR

aHoldingRegister[iRegMpcMaske]  := stMpc.wGueltig;
aHoldingRegister[iRegMgcpMaske] := stMgcp.wGueltig;

FuEebusSchreibeDint(aRegister := aHoldingRegister, iIndex := iRegMpcP, diWert := stMpc.diLeistungW);
FuEebusSchreibeUlint(aRegister := aHoldingRegister, iIndex := iRegMpcEBezug, ulWert := stMpc.ulEnergieBezugWh);
FuEebusSchreibeUlint(aRegister := aHoldingRegister, iIndex := iRegMpcEErzeugung, ulWert := stMpc.ulEnergieErzeugungWh);

FuEebusSchreibeDint(aRegister := aHoldingRegister, iIndex := iRegMgcpP, diWert := stMgcp.diLeistungW);
FuEebusSchreibeUlint(aRegister := aHoldingRegister, iIndex := iRegMgcpEEinspeisung, ulWert := stMgcp.ulEnergieEinspeisungWh);
FuEebusSchreibeUlint(aRegister := aHoldingRegister, iIndex := iRegMgcpEBezug, ulWert := stMgcp.ulEnergieBezugWh);
END_FUNCTION_BLOCK

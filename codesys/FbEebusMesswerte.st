// Schreibt Leistung und Energie für MPC (Anlage) und MGCP (Netzanschlusspunkt)
// in die Holding-Register 5, 6 und 8 bis 27 des Gateways. Ob ein Wert gerade
// gültig ist, steht in wGueltig der Strukturen.
//
// Mindestens die aktuelle Wirkleistung der Anlage muss die Steuerbox abfragen
// können (FNN-Hinweis 4.1.2.3). Ohne gültigen Wert meldet das Gateway ihn als
// ungültig.
FUNCTION_BLOCK FbEebusMesswerte
VAR_INPUT
	stMpc  : ST_EebusMpc;
	stMgcp : ST_EebusMgcp;
END_VAR
VAR_IN_OUT
	aHoldingRegister : ARRAY [0..27] OF WORD;   // GvlEebus.aHoldingRegister
END_VAR
VAR CONSTANT
	// Holding-Register (Belegung siehe GvlEebus)
	iRegMpcMaske         : INT := 5;    // Gültigkeitsbits MPC
	iRegMgcpMaske        : INT := 6;    // Gültigkeitsbits MGCP
	iRegMpcP             : INT := 8;    // MPC Leistung in W, DINT, 2 Register
	iRegMpcEBezug        : INT := 10;   // MPC Energie Bezug in Wh, ULINT, 4 Register
	iRegMpcEErzeugung    : INT := 14;   // MPC Energie Erzeugung in Wh, ULINT, 4 Register
	iRegMgcpP            : INT := 18;   // MGCP Leistung in W, DINT, 2 Register
	iRegMgcpEEinspeisung : INT := 20;   // MGCP Energie Einspeisung in Wh, ULINT, 4 Register
	iRegMgcpEBezug       : INT := 24;   // MGCP Energie Bezug in Wh, ULINT, 4 Register
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

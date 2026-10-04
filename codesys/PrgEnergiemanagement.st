// Beispielaufruf mit allen Use Cases (Erweiterungsversion 2). Die Aufteilung
// der Grenzen auf die einzelnen Verbraucher und Erzeuger (Wallboxen,
// Wärmepumpe, PV-Wechselrichter, Speicher, ...) ist anlagenspezifisch.
//
// Wichtig (FNN-Hinweis, Fußnote 4): Die Grenzen beziehen sich auf den
// Netzanschlusspunkt. Die Regelung muss sie am Zähler des Netzanschlusspunkts
// einhalten. Bei einem EMS ist außerdem die Mindestbezugsleistung nach BNetzA
// BK6-22-300 Anlage 1 zu berücksichtigen.
PROGRAM PrgEnergiemanagement
VAR
	fbBruecke     : FbEebusBruecke;
	fbBezug       : FbEebusBegrenzung;
	fbEinspeisung : FbEebusBegrenzung;
	fbMesswerte   : FbEebusMesswerte;

	stMpc  : ST_EebusMpc;    // aus der Messung der steuerbaren Anlage füllen
	stMgcp : ST_EebusMgcp;   // aus dem Zähler am Netzanschlusspunkt füllen

	diNetzleistungW          : DINT;    // Messwert am Netzanschlusspunkt, Bezug positiv
	udiAnforderungW          : UDINT;   // ungeregelte Summenanforderung der steuerbaren Verbraucher
	udiFreigegebeneLeistungW : UDINT;   // Vorgabe an die Verteillogik
	udiPvSollW               : UDINT;   // Vorgabe an die PV-Wechselrichter
	udiPvMaxW                : UDINT := 8000;
	xAnlagenstoerung         : BOOL;
END_VAR

fbBruecke(
	xFreigabe                 := TRUE,
	udiNennleistungBezugW     := 22000,
	udiNennleistungErzeugungW := udiPvMaxW,
	eStatus                   := SEL(xAnlagenstoerung, eAnlagenstatus.Normal, eAnlagenstatus.Stoerung),
	tBrueckeTimeout           := T#10S,
	aInputRegister            := GvlEebus.aInputRegister,
	aHoldingRegister          := GvlEebus.aHoldingRegister);

fbBezug(
	xBrueckeOk       := fbBruecke.xBrueckeOk,
	xUseCaseAktiv    := fbBruecke.xLpcAktiv,
	iBasis           := 2,
	udiErsatzgrenzeW := 4200,
	aInputRegister   := GvlEebus.aInputRegister);

fbEinspeisung(
	xBrueckeOk       := fbBruecke.xBrueckeOk,
	xUseCaseAktiv    := fbBruecke.xLppAktiv,
	iBasis           := 17,
	udiErsatzgrenzeW := udiPvMaxW * 6 / 10,   // z. B. 60 % nach Netzanschlussvertrag
	aInputRegister   := GvlEebus.aInputRegister);

// Messwerte: hier nur die Netzleistung als Beispiel, alle Bits nur für
// tatsächlich gemessene Werte setzen.
stMgcp.diLeistungW := diNetzleistungW;
stMgcp.wGueltig    := 16#0001;
stMpc.diLeistungW  := UDINT_TO_DINT(udiFreigegebeneLeistungW);
stMpc.wGueltig     := 16#0001;
fbMesswerte(stMpc := stMpc, stMgcp := stMgcp, aHoldingRegister := GvlEebus.aHoldingRegister);

// Bezug begrenzen
IF fbBezug.xBegrenzungAktiv THEN
	udiFreigegebeneLeistungW := MIN(udiAnforderungW, fbBezug.udiLeistungsgrenzeW);
ELSE
	udiFreigegebeneLeistungW := udiAnforderungW;
END_IF

// Einspeisung begrenzen (vereinfacht: ohne Eigenverbrauch)
IF fbEinspeisung.xBegrenzungAktiv THEN
	udiPvSollW := MIN(udiPvMaxW, fbEinspeisung.udiLeistungsgrenzeW);
ELSE
	udiPvSollW := udiPvMaxW;
END_IF
END_PROGRAM

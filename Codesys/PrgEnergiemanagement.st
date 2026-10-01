// Beispielaufruf. Die Aufteilung der Grenze auf die einzelnen steuerbaren
// Verbraucher (Wallboxen, Wärmepumpe, ...) ist anlagenspezifisch.
PROGRAM PrgEnergiemanagement
VAR
	fbEebusLpc               : FbEebusLpc;
	udiAnforderungW          : UDINT;   // ungeregelte Summenanforderung der steuerbaren Verbraucher
	udiFreigegebeneLeistungW : UDINT;   // Vorgabe an die Verteillogik
END_VAR

fbEebusLpc(
	xFreigabe           := TRUE,
	aInputRegister      := GvlEebus.aInputRegister,
	udiNennleistungMaxW := 22000,
	udiErsatzgrenzeW    := 4200,
	tBrueckeTimeout     := T#10S);

GvlEebus.aHoldingRegister := fbEebusLpc.aHoldingRegister;

IF fbEebusLpc.xBegrenzungAktiv THEN
	udiFreigegebeneLeistungW := MIN(udiAnforderungW, fbEebusLpc.udiLeistungsgrenzeW);
ELSE
	udiFreigegebeneLeistungW := udiAnforderungW;
END_IF
END_PROGRAM

// Hilfsfunktionen: Werte in die Holding-Register der Brücke schreiben bzw.
// aus den Input-Registern lesen. Mehrwortige Werte: High-Word zuerst.

FUNCTION FuEebusSchreibeDint : BOOL
VAR_IN_OUT
	aRegister : ARRAY [0..60] OF WORD;
END_VAR
VAR_INPUT
	iIndex : INT;
	diWert : DINT;
END_VAR
VAR
	dwWert : DWORD;
END_VAR
dwWert := DINT_TO_DWORD(diWert);
aRegister[iIndex]     := DWORD_TO_WORD(SHR(dwWert, 16));
aRegister[iIndex + 1] := DWORD_TO_WORD(dwWert);
FuEebusSchreibeDint := TRUE;
END_FUNCTION

FUNCTION FuEebusSchreibeUdint : BOOL
VAR_IN_OUT
	aRegister : ARRAY [0..60] OF WORD;
END_VAR
VAR_INPUT
	iIndex  : INT;
	udiWert : UDINT;
END_VAR
aRegister[iIndex]     := UDINT_TO_WORD(SHR(udiWert, 16));
aRegister[iIndex + 1] := UDINT_TO_WORD(udiWert AND 16#0000FFFF);
FuEebusSchreibeUdint := TRUE;
END_FUNCTION

FUNCTION FuEebusSchreibeUlint : BOOL
VAR_IN_OUT
	aRegister : ARRAY [0..60] OF WORD;
END_VAR
VAR_INPUT
	iIndex : INT;
	ulWert : ULINT;
END_VAR
aRegister[iIndex]     := ULINT_TO_WORD(SHR(ulWert, 48));
aRegister[iIndex + 1] := ULINT_TO_WORD(SHR(ulWert, 32));
aRegister[iIndex + 2] := ULINT_TO_WORD(SHR(ulWert, 16));
aRegister[iIndex + 3] := ULINT_TO_WORD(ulWert);
FuEebusSchreibeUlint := TRUE;
END_FUNCTION

FUNCTION FuEebusLiesUdint : UDINT
VAR_IN_OUT
	aRegister : ARRAY [0..30] OF WORD;
END_VAR
VAR_INPUT
	iIndex : INT;
END_VAR
FuEebusLiesUdint := SHL(WORD_TO_UDINT(aRegister[iIndex]), 16) OR WORD_TO_UDINT(aRegister[iIndex + 1]);
END_FUNCTION

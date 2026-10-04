// Messwerte der Anlage für den Use Case MPC (Monitoring of Power Consumption).
// Vorzeichen nach EEBUS: Bezug positiv, Erzeugung negativ.
// Ein Wert geht nur als gültig an die Steuerbox, wenn sein Bit in wGueltig
// gesetzt ist. Ohne eigenen Energiezähler Bit 1 und 2 nicht setzen.
TYPE ST_EebusMpc :
STRUCT
	diLeistungW          : DINT;    // Bit 0
	ulEnergieBezugWh     : ULINT;   // Bit 1, Zählerstand
	ulEnergieErzeugungWh : ULINT;   // Bit 2, Zählerstand
	wGueltig             : WORD;    // Gültigkeitsbits wie oben
END_STRUCT
END_TYPE

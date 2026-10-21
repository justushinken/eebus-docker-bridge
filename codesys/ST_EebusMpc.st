// Messwerte der Anlage für den Use Case MPC (Monitoring of Power Consumption).
// Vorzeichen nach EEBUS: Bezug positiv, Erzeugung negativ.
// Ein Wert geht nur als gültig an die Steuerbox, wenn sein Bit in wGueltig
// gesetzt ist. Ohne eigenen Energiezähler Bit 1 und 2 nicht setzen.
TYPE ST_EebusMpc :
STRUCT
	diLeistungW          : DINT;    // Bit 0: Wirkleistung der Anlage in W, Summe aller Phasen
	ulEnergieBezugWh     : ULINT;   // Bit 1: bezogene Energie in Wh, Zählerstand (steigt nur)
	ulEnergieErzeugungWh : ULINT;   // Bit 2: erzeugte Energie in Wh, Zählerstand (steigt nur)
	wGueltig             : WORD;    // Gültigkeitsbits wie oben, z. B. 16#0001 = nur Leistung gültig
END_STRUCT
END_TYPE

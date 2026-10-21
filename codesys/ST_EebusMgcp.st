// Messwerte am Netzanschlusspunkt für den Use Case MGCP (Monitoring of Grid
// Connection Point). Vorzeichen: Bezug aus dem Netz positiv, Einspeisung negativ.
// Ein Wert geht nur als gültig an die Steuerbox, wenn sein Bit in wGueltig
// gesetzt ist.
TYPE ST_EebusMgcp :
STRUCT
	diLeistungW            : DINT;    // Bit 0: Wirkleistung am Netzanschlusspunkt in W, Summe aller Phasen
	ulEnergieEinspeisungWh : ULINT;   // Bit 1: ins Netz eingespeiste Energie in Wh, Zählerstand
	ulEnergieBezugWh       : ULINT;   // Bit 2: aus dem Netz bezogene Energie in Wh, Zählerstand
	wGueltig               : WORD;    // Gültigkeitsbits wie oben, z. B. 16#0001 = nur Leistung gültig
END_STRUCT
END_TYPE

// Messwerte am Netzanschlusspunkt für den Use Case MGCP (Monitoring of Grid
// Connection Point). Vorzeichen: Bezug aus dem Netz positiv, Einspeisung negativ.
// Ein Wert geht nur als gültig an die Steuerbox, wenn sein Bit in wGueltig
// gesetzt ist.
TYPE ST_EebusMgcp :
STRUCT
	diLeistungW            : DINT;    // Bit 0
	ulEnergieEinspeisungWh : ULINT;   // Bit 1, Zählerstand
	ulEnergieBezugWh       : ULINT;   // Bit 2, Zählerstand
	wGueltig               : WORD;    // Gültigkeitsbits wie oben
END_STRUCT
END_TYPE

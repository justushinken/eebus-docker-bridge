// Messwerte am Netzanschlusspunkt für den Use Case MGCP (Monitoring of Grid
// Connection Point). Vorzeichen: Bezug aus dem Netz positiv, Einspeisung negativ.
// Ein Wert geht nur als gültig an die Steuerbox, wenn sein Bit in wGueltig
// gesetzt ist. Leistung und beide Energien sind Pflicht, Strom, Spannung,
// Frequenz und PV-Faktor je nach MGCP_MESSWERTE.
TYPE ST_EebusMgcp :
STRUCT
	diLeistungW           : DINT;                   // Bit 0
	ulEnergieEinspeisungWh: ULINT;                  // Bit 1, Zählerstand
	ulEnergieBezugWh      : ULINT;                  // Bit 2, Zählerstand
	aStromPhaseMA         : ARRAY [1..3] OF DINT;   // Bit 3..5, in mA
	aSpannungPhase01V     : ARRAY [1..3] OF UINT;   // Bit 6..8, in 0,1 V
	uiFrequenz001Hz       : UINT;                   // Bit 9, in 0,01 Hz
	uiPvFaktor01Prozent   : UINT;                   // Bit 10, PV-Einspeisebegrenzungsfaktor in 0,1 %
	wGueltig              : WORD;                   // Gültigkeitsbits wie oben
END_STRUCT
END_TYPE

// Messwerte der Anlage für den Use Case MPC (Monitoring of Power Consumption).
// Vorzeichen nach EEBUS: Bezug positiv, Erzeugung negativ.
// Ein Wert geht nur als gültig an die Steuerbox, wenn sein Bit in wGueltig
// gesetzt ist. Welche Werte die Brücke überhaupt anbietet, steht in
// MPC_MESSWERTE (Konfiguration des Containers).
TYPE ST_EebusMpc :
STRUCT
	diLeistungW         : DINT;                     // Bit 0
	aLeistungPhaseW     : ARRAY [1..3] OF DINT;     // Bit 1..3 (L1..L3)
	ulEnergieBezugWh    : ULINT;                    // Bit 4, Zählerstand
	ulEnergieErzeugungWh: ULINT;                    // Bit 5, Zählerstand
	aStromPhaseMA       : ARRAY [1..3] OF DINT;     // Bit 6..8, in mA
	aSpannungPhase01V   : ARRAY [1..3] OF UINT;     // Bit 9..11, in 0,1 V
	uiFrequenz001Hz     : UINT;                     // Bit 12, in 0,01 Hz
	wGueltig            : WORD;                     // Gültigkeitsbits wie oben
END_STRUCT
END_TYPE

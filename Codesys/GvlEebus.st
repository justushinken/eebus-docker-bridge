// Prozessabbild der Modbus-Verbindung zur EEBUS-Brücke.
// Mapping im Gerätebaum (Modbus TCP Client -> Server 127.0.0.1:5502):
//   Kanal 1: FC04 Read Input Registers,  Offset 0, Länge 14, Zyklus 200 ms -> aInputRegister
//   Kanal 2: FC16 Write Multiple Registers, Offset 0, Länge 3,  Zyklus 1 s  -> aHoldingRegister
{attribute 'qualified_only'}
VAR_GLOBAL
	aInputRegister   : ARRAY [0..13] OF WORD;   // Brücke -> SPS
	aHoldingRegister : ARRAY [0..2] OF WORD;    // SPS -> Brücke
END_VAR

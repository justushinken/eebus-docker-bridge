// Prozessabbild der Modbus-Verbindung zur EEBUS-Brücke (Erweiterungsversion 2).
// Mapping im Gerätebaum (Modbus TCP Client -> Server 127.0.0.1:5502):
//   Kanal 1: FC04 Read Input Registers,    Offset 0, Länge 31, Zyklus 200 ms -> aInputRegister
//   Kanal 2: FC16 Write Multiple Registers, Offset 0, Länge 61, Zyklus 1 s   -> aHoldingRegister
// Alle Holding-Register in EINEM Kanal schreiben: Dann sieht die Brücke immer
// einen zusammengehörigen Stand von Messwerten und Gültigkeitsmasken.
//
// Bestehende Projekte mit FbEebusLpc (14 Input-, 3 Holding-Register) laufen
// unverändert weiter, siehe FbEebusLpc.
{attribute 'qualified_only'}
VAR_GLOBAL
	aInputRegister   : ARRAY [0..30] OF WORD;   // Brücke -> SPS
	aHoldingRegister : ARRAY [0..60] OF WORD;   // SPS -> Brücke
END_VAR

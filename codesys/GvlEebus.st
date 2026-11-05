// Prozessabbild der Modbus-Verbindung zum EEBUS-Gateway (Schnittstellenversion 1,
// Erweiterungsversion 2).
//
// Das Gateway ist ein Docker-Container auf derselben Steuerung. Es spricht EEBUS
// mit der Steuerbox des Messstellenbetreibers und stellt das Ergebnis als
// Modbus-TCP-Server auf 127.0.0.1:5502 bereit. Die SPS ist Client.
//
// Mapping im Gerätebaum (ModbusTCP Master -> Slave 127.0.0.1:5502):
//   Kanal 1: FC04 Read Input Registers,     Offset 0, Länge 31, Zyklus 200 ms -> aInputRegister
//   Kanal 2: FC16 Write Multiple Registers, Offset 0, Länge 28, Zyklus 1 s   -> aHoldingRegister
// Die Längen sind Pflicht. Liest Kanal 1 weniger als 31 Register (z. B. noch 14
// aus einem alten Projekt mit FbEebusLpc), bleiben die Register 14 bis 30 auf 0.
// FbEebusGateway meldet dann Fehler 2, und xGatewayOk bleibt FALSE.
// Alle Holding-Register in EINEM Kanal schreiben: Dann sieht das Gateway immer
// einen zusammengehörigen Stand von Messwerten und Gültigkeitsbits.
//
// Die Register nicht direkt auswerten, sondern über die Bausteine:
//   FbEebusGateway     Lebenszeichen, Versionsprüfung, Verbindung, Use Cases;
//                      schreibt Holding 0 bis 4 und 7
//   FbEebusBegrenzung  je Richtung einmal: Bezug (Input 2 bis 13) bzw.
//                      Einspeisung (Input 17 bis 28)
//   FbEebusMesswerte   schreibt Holding 5, 6 und 8 bis 27
//
// 32- und 64-Bit-Werte stehen in mehreren Registern, das High-Word zuerst
// (Adresse n = höchstwertiges Wort). Nicht per Zeiger als DWORD lesen: Der PFC
// legt das Low-Word vorn ab. Die Hilfsfunktionen in FuEebusRegister erledigen
// das richtig.
//
// ===========================================================================
// Input-Register (FC04), Gateway -> SPS: aInputRegister[0..30]
// ===========================================================================
//  Adr.   Inhalt                                                    Typ, Einheit
//  0      Lebenszeichen des Gateways, +1 pro Sekunde. Steht es      UINT
//         länger als tGatewayTimeout, gilt das Gateway als
//         ausgefallen.
//  1      Schnittstellenversion, immer 1. Ändert sich nur bei       UINT
//         inkompatiblen Änderungen am Registerlayout.
//
//  2..13  Block Bezug (LPC, Leistungsaufnahme begrenzen, §14a EnWG), Aufbau
//         siehe unten. Auswertung mit FbEebusBegrenzung, iBasis := 2.
//
//  14     Erweiterungsversion des Gateways, derzeit 2. Steigt,      UINT
//         wenn hinten Register angehängt werden. Die Bausteine
//         verlangen mindestens 2 (uiErweiterungsVersionMin).
//  15     Use Cases, die das Gateway anbietet (fest eingestellt     WORD
//         im Gateway, ändert sich nur nach dessen Neustart):
//           Bit 0  LPC   Bezug begrenzen
//           Bit 1  LPP   Einspeisung begrenzen
//           Bit 2  MPC   Messwerte der Anlage an die Steuerbox
//           Bit 3  MGCP  Messwerte des Netzanschlusspunkts an die Steuerbox
//  16     Use Cases, die die verbundene Steuerbox meldet, Bits    WORD
//         wie Register 15. 0, solange keine Steuerbox verbunden
//         ist; nach dem Verbinden bis zu 5 s Verzögerung.
//         Nur zur Information: Ob begrenzt wird, steht in den
//         Blöcken.
//
//  17..28 Block Einspeisung (LPP, Einspeisung begrenzen, §9 EEG), Aufbau wie
//         Bezug. Auswertung mit FbEebusBegrenzung, iBasis := 17.
//
//  29..30 Failsafe-Mindestdauer in s, gilt für beide Richtungen.   UDINT s
//         So lange gilt die Failsafe-Grenze mindestens, wenn die
//         Steuerbox ausfällt. Vorgabe der Steuerbox.
//
// Aufbau eines Blocks (Offset zur Basis 2 bzw. 17):
//  Off. Bezug Einsp. Inhalt                                         Typ, Einheit
//  0    2     17     Zustand (eLpcZustand):                          UINT
//                      0 Init                nach Start des Gateways, Failsafe-Grenze gilt
//                      1 Unbegrenzt/gesteuert Steuerbox da, keine Grenze
//                      2 Begrenzt            Grenze des Netzbetreibers gilt
//                      3 Failsafe            Steuerbox ausgefallen, Failsafe-Grenze gilt
//                      4 Unbegrenzt/autonom  Failsafe-Mindestdauer abgelaufen, keine Grenze
//  1    3     18     Verbindung zur Steuerbox (eEebusVerbindung):    UINT
//                      0 keine Steuerbox gekoppelt, 1 getrennt, 2 verbunden
//                      (in beiden Blöcken gleich)
//  2..3 4..5  19..20 Wirksame Leistungsgrenze, die die Anlage am     UDINT W
//                    Netzanschlusspunkt einhalten muss. Nur gültig,
//                    wenn Offset 4 = 1. Je nach Zustand die Grenze
//                    des Netzbetreibers oder die Failsafe-Grenze.
//  4    6     21     Begrenzung aktiv: 1 = Grenze einhalten,         UINT 0/1
//                    0 = keine Begrenzung (Zustand 1 oder 4)
//  5..6 7..8  22..23 Zuletzt empfangene Grenze des Netzbetreibers,   UDINT W
//                    auch wenn sie nicht (mehr) gilt. Nur Diagnose.
//  7..8 9..10 24..25 Restlaufzeit der Grenze des Netzbetreibers,     UDINT s
//                    0 = unbefristet oder nicht begrenzt. Nur
//                    Diagnose, das Gateway beendet die Grenze selbst.
//  9..10 11..12 26..27 Aktuelle Failsafe-Grenze (Vorgabe der         UDINT W
//                    Steuerbox, sonst Einstellung im Gateway).
//                    Nur Diagnose.
//  11   13    28     Sekunden seit dem letzten Heartbeat der         UINT s
//                    Steuerbox, 65535 = noch keiner. Nach 120 s
//                    geht das Gateway in Failsafe. In beiden
//                    Blöcken gleich.
// Bietet das Gateway einen Use Case nicht an (Register 15), ist sein Block bis
// auf Verbindung und Heartbeat 0.
//
// ===========================================================================
// Holding-Register (FC16), SPS -> Gateway: aHoldingRegister[0..27]
// ===========================================================================
//  Adr.   Inhalt                                                    Typ, Einheit
//  0      Lebenszeichen der SPS, +1 pro Sekunde. Steht es 10 s,     UINT
//         lehnt das Gateway neue Grenzen ab und meldet der
//         Steuerbox eine Störung.
//  1..2   Nennleistung Bezug: maximale Leistungsaufnahme der        UDINT W
//         Anlage, wird an die Steuerbox gemeldet. 0 = Einstellung
//         im Gateway (NENNLEISTUNG_MAX_W).
//  3..4   Nennleistung Erzeugung: maximale Einspeiseleistung (LPP). UDINT W
//         0 = Einstellung im Gateway (NENNLEISTUNG_ERZEUGUNG_MAX_W).
//  5     Gültigkeit der MPC-Werte (Register 8 bis 17):             WORD
//           Bit 0 Leistung, Bit 1 Energie Bezug, Bit 2 Energie Erzeugung
//  6      Gültigkeit der MGCP-Werte (Register 18 bis 27):           WORD
//           Bit 0 Leistung, Bit 1 Energie Einspeisung, Bit 2 Energie Bezug
//         Ein Wert ohne Bit geht als "ungültig" an die Steuerbox.
//  7      Anlagenstatus (eAnlagenstatus): 0 normal, 1 Störung       UINT
//         (Anlage kann Grenzen nicht umsetzen), 2 Standby
//  8..9   MPC: Wirkleistung der steuerbaren Anlage, Summe aller     DINT W
//         Phasen. Bezug positiv, Erzeugung negativ.
//  10..13 MPC: Energie Bezug der Anlage, Zählerstand                ULINT Wh
//  14..17 MPC: Energie Erzeugung der Anlage, Zählerstand            ULINT Wh
//  18..19 MGCP: Wirkleistung am Netzanschlusspunkt. Bezug aus dem   DINT W
//         Netz positiv, Einspeisung negativ.
//  20..23 MGCP: Energie Einspeisung ins Netz, Zählerstand           ULINT Wh
//  24..27 MGCP: Energie Bezug aus dem Netz, Zählerstand             ULINT Wh
//
// Bestehende Projekte mit FbEebusLpc (14 Input-, 3 Holding-Register) laufen
// unverändert weiter, siehe FbEebusLpc.
//
// Quelle des Layouts für die Pflege: gateway/modbus.go im Repository.
{attribute 'qualified_only'}
VAR_GLOBAL
	aInputRegister   : ARRAY [0..30] OF WORD;   // Gateway -> SPS, Belegung siehe oben
	aHoldingRegister : ARRAY [0..27] OF WORD;   // SPS -> Gateway, Belegung siehe oben
END_VAR

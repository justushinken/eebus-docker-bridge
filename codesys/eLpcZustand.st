// Zustand der Use Cases LPC (Bezug) und LPP (Einspeisung), wie ihn die
// EEBUS-Brücke meldet. Beide haben denselben Zustandsautomaten.
// Zahlenwerte identisch mit LpcZustand in bruecke.go.
{attribute 'qualified_only'}
{attribute 'strict'}
TYPE eLpcZustand :
(
	Init                := 0,   // nach Neustart, Failsafe-Grenze gilt
	UnbegrenztGesteuert := 1,   // Steuerbox verbunden, keine aktive Grenze
	Begrenzt            := 2,   // Grenze des Netzbetreibers aktiv
	Failsafe            := 3,   // Heartbeat ausgefallen, Failsafe-Grenze gilt
	UnbegrenztAutonom   := 4,   // keine Kommunikation, Failsafe-Mindestdauer abgelaufen
	Unbekannt           := 99   // Brücke nicht erreichbar oder Wert ungültig
) UINT;
END_TYPE

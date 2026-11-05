// Verbindungszustand zwischen Gateway und Steuerbox.
// Zahlenwerte identisch mit Verbindung in gateway.go.
{attribute 'qualified_only'}
{attribute 'strict'}
TYPE eEebusVerbindung :
(
	KeinPartner := 0,    // keine Steuerbox gekoppelt
	Getrennt    := 1,    // Steuerbox gekoppelt, aber nicht verbunden
	Verbunden   := 2,    // EEBUS-Verbindung zur Steuerbox steht
	Unbekannt   := 99    // Gateway nicht erreichbar oder Wert ungültig
) UINT;
END_TYPE

// Verbindungszustand zwischen Brücke und Steuerbox.
// Zahlenwerte identisch mit Verbindung in bruecke.go.
{attribute 'qualified_only'}
{attribute 'strict'}
TYPE eEebusVerbindung :
(
	KeinPartner := 0,    // kein SKI der Steuerbox konfiguriert
	Getrennt    := 1,
	Verbunden   := 2,
	Unbekannt   := 99
) UINT;
END_TYPE

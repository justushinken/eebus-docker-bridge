// Betriebszustand der Anlage, den die Brücke an die Steuerbox meldet
// (FNN-Hinweis 4.1.2.7). Zahlenwerte identisch mit anlagenstatus* in anlage.go.
{attribute 'qualified_only'}
{attribute 'strict'}
TYPE eAnlagenstatus :
(
	Normal   := 0,
	Stoerung := 1,   // Anlage kann Grenzen nicht umsetzen
	Standby  := 2
) UINT;
END_TYPE

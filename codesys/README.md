# CODESYS-Anbindung

Das Gateway ist Modbus-TCP-Server auf `127.0.0.1:5502`, die CODESYS-Applikation ist Client. Die Quellen in diesem Ordner liegen als Text vor und werden ins CODESYS-Projekt übernommen.

> Die Bausteine der Erweiterungsversion 2 sind ohne CODESYS-Umgebung geschrieben und **noch nicht kompiliert**. Beim Übernehmen auf Typfehler achten.

## Gerätebaum

1. *Ethernet-Adapter → Ethernet* anhängen, Schnittstelle mit der IP des PFC wählen (X1 meist `br0`). Für die Verbindung zu `127.0.0.1` ist die Wahl egal, CODESYS verlangt aber einen Adapter.
2. Darunter *ModbusTCP Master*, **Auto-Reconnect aktivieren**. Sonst gibt CODESYS nach einem Neustart des Gateways auf, und das Gateway zeigt „SPS ausgefallen“.
3. Darunter *ModbusTCP Slave* mit IP `127.0.0.1`, Port `5502`, Unit-ID beliebig.
4. Kanäle wie in `GvlEebus.st`: FC04 Offset 0 Länge 31 (200 ms) und FC16 Offset 0 Länge 28 (1 s).
5. Im E/A-Abbild die Kanäle als Ganzes auf `GvlEebus.aInputRegister` bzw. `GvlEebus.aHoldingRegister` legen, Buszyklus-Task = Task von `PrgEnergiemanagement`.

Falls der Gerätebaum Localhost als Ziel nicht akzeptiert, alternativ `FbMbMasterTcp` aus WagoAppPlcModbus mit `sHost := '127.0.0.1'` verwenden. Die Bausteine bleiben dabei unverändert, da sie nur die Register-Arrays sehen.

## Bausteine

Beispiel in `PrgEnergiemanagement.st`.

| Baustein | Aufgabe |
|---|---|
| `FbEebusGateway` | Lebenszeichen, Versionsprüfung (`xGatewayOk`), Verbindung, Use Cases (`wUseCasesGateway` mit `xLpcGateway` … `xMgcpGateway`, dazu `wUseCasesSteuerbox`); schreibt SPS-Lebenszeichen, Nennleistungen und Anlagenstatus (`eAnlagenstatus`) |
| `FbEebusBegrenzung` | eine Richtung, zweimal aufrufen: `iBasis := 2` Bezug mit `xUseCaseGateway := xLpcGateway`, `iBasis := 17` Einspeisung mit `xLppGateway`. Ersatzgrenze, wenn das Gateway ausfällt; keine Begrenzung, wenn das Gateway den Use Case nicht anbietet |
| `FbEebusMesswerte` | schreibt `ST_EebusMpc` und `ST_EebusMgcp` samt Gültigkeitsbits |
| `FuEebusRegister` | Hilfsfunktionen zum Lesen und Schreiben mehrwortiger Register |
| `FbEebusLpc` | veraltet, nur für bestehende Projekte mit 14/3 Registern |

## Registerlayout (Schnittstellenversion 1, Erweiterung 2)

Mehrwortige Werte High-Word zuerst. Unit-ID beliebig.

Die Belegung steht mit Erläuterungen auch als Kommentar in `GvlEebus.st`, für CODESYS-Entwickler ohne Blick in den Go-Code.

**Versionsregel:** Register 1 ist die Schnittstellenversion und ändert sich nur bei inkompatiblen Änderungen. Neue Register werden nur angehängt, dann steigt die Erweiterungsversion in Register 14. Ältere SPS-Programme (14 Input-, 3 Holding-Register, `FbEebusLpc`) laufen deshalb unverändert weiter.

**Versionsprüfung in `FbEebusGateway`:** Register 1 muss 1 sein, Register 14 mindestens `uiErweiterungsVersionMin` (2), denn erst ab Erweiterung 2 gibt es die Register 14–30. Ein neueres Gateway ist in Ordnung. Sonst bleibt `xGatewayOk` FALSE mit `udiFehlerId = 2`, und die Ersatzgrenzen gelten. Häufigste Ursache: Der FC04-Kanal liest noch 14 statt 31 Register (Einstellung aus einem alten Projekt mit `FbEebusLpc`). Dann sind Register 14–30 und damit auch die Use-Case-Masken 0.

### Input-Register (FC04), Gateway → SPS

Bezug (LPC) und Einspeisung (LPP) haben je einen gleich aufgebauten Block, Bezug ab Register 2, Einspeisung ab Register 17:

| Bezug | Einspeisung | Inhalt | Einheit |
|---|---|---|---|
| 2 | 17 | Zustand (0 Init, 1 Unbegrenzt/gesteuert, 2 Begrenzt, 3 Failsafe, 4 Unbegrenzt/autonom) | – |
| 3 | 18 | Verbindung (0 kein Partner, 1 getrennt, 2 verbunden), in beiden Blöcken gleich | – |
| 4–5 | 19–20 | wirksame Leistungsgrenze | W |
| 6 | 21 | Begrenzung aktiv (0/1) | – |
| 7–8 | 22–23 | letzte Grenze des Netzbetreibers (Diagnose) | W |
| 9–10 | 24–25 | Restlaufzeit der Grenze, 0 = unbefristet | s |
| 11–12 | 26–27 | Failsafe-Grenze (Diagnose) | W |
| 13 | 28 | Sekunden seit letztem Heartbeat, 65535 = nie, in beiden Blöcken gleich | s |

| Adr. | Inhalt |
|---|---|
| 0 | Lebenszeichen Gateway (+1 pro Sekunde) |
| 1 | Schnittstellenversion (1) |
| 14 | Erweiterungsversion (2) |
| 15 | Use Cases Gateway: im Gateway eingeschaltet (`EEBUS_USECASES`), Bit 0 LPC, 1 LPP, 2 MPC, 3 MGCP |
| 16 | Use Cases Steuerbox: von der verbundenen Steuerbox gemeldet, Bits wie Register 15, 0 ohne Verbindung (nur Information) |
| 29–30 | Failsafe-Mindestdauer in s, gilt für beide Richtungen |

Für die Regelung genügen je Richtung „Begrenzung aktiv“ und „wirksame Leistungsgrenze“.

### Holding-Register (FC03/FC16), SPS → Gateway

Alle in **einem** FC16-Aufruf schreiben, dann sieht das Gateway einen zusammengehörigen Stand:

| Adr. | Inhalt | Typ, Einheit |
|---|---|---|
| 0 | Lebenszeichen SPS (+1 pro Sekunde) | UINT |
| 1–2 | Nennleistung Bezug (an die Steuerbox), 0 = `NENNLEISTUNG_MAX_W` | UDINT W |
| 3–4 | Nennleistung Erzeugung (LPP), 0 = `NENNLEISTUNG_ERZEUGUNG_MAX_W` | UDINT W |
| 5 | Gültigkeit MPC: Bit 0 Leistung, 1 Energie Bezug, 2 Energie Erzeugung | WORD |
| 6 | Gültigkeit MGCP: Bit 0 Leistung, 1 Energie Einspeisung, 2 Energie Bezug | WORD |
| 7 | Anlagenstatus: 0 normal, 1 Störung, 2 Standby | UINT |
| 8–9 | MPC Leistung (Bezug +, Erzeugung −) | DINT W |
| 10–13 | MPC Energie Bezug (Zählerstand) | ULINT Wh |
| 14–17 | MPC Energie Erzeugung (Zählerstand) | ULINT Wh |
| 18–19 | MGCP Leistung (Bezug aus dem Netz +, Einspeisung −) | DINT W |
| 20–23 | MGCP Energie Einspeisung | ULINT Wh |
| 24–27 | MGCP Energie Bezug | ULINT Wh |

Als Messwerte gibt das Gateway je Use Case Leistung und Energie weiter. Mehr verlangen weder der FNN-Hinweis (4.1.2.3: aktuelle Wirkleistung) noch die Pflicht-Szenarien von MPC und MGCP. Strom, Spannung, Frequenz oder Werte je Phase ließen sich bei Bedarf hinten anhängen (neue Erweiterungsversion). Ein Wert ohne Gültigkeitsbit, oder alle Werte bei ausgefallener SPS, gehen als „ungültig“ an die Steuerbox. Energie in 64 Bit, weil 32 Bit Wh an einem größeren Netzanschluss nach wenigen Jahren überlaufen.

Ist die SPS nicht erreichbar (Lebenszeichen steht), lehnt das Gateway neue Grenzen ab und meldet der Steuerbox eine Störung.

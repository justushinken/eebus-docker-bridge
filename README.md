# EEBUS-Brücke für WAGO PFC200

Docker-Container, der auf dem PFC200 als EEBUS "Controllable System" läuft. Er bildet die Use Cases des VDE FNN Hinweises „Schnittstellen der Steuerungseinrichtung“ ab und tauscht Grenzen und Messwerte per Modbus TCP mit der CODESYS-Applikation aus.

| Use Case | Zweck | Laut FNN-Hinweis |
|---|---|---|
| **LPC** Limitation of Power Consumption | Bezug begrenzen (§14a EnWG) | Pflicht |
| **LPP** Limitation of Power Production | Einspeisung begrenzen (§9 EEG) | Pflicht bei Erzeugung |
| **MPC** Monitoring of Power Consumption | Messwerte der Anlage | aktuelle Wirkleistung muss abrufbar sein (4.1.2.3) |
| **MGCP** Monitoring of Grid Connection Point | Messwerte am Netzanschlusspunkt | „zusätzlich vorgesehen“ |

```
 Steuerbox                                    PFC200
 am Smart Meter Gateway      ┌──────────────────────────────────────────────┐
 (Energy Guard,              │  Docker (--network host)                     │
  Monitoring Appliance)      │  ┌───────────────────────┐                   │
          │  SHIP/SPINE      │  │ eebus-bruecke (Go)    │                   │
          └──────────────────┼─►│ LPC, LPP, MPC, MGCP   │                   │
            TLS-WebSocket,   │  │ Zustandsautomaten     │                   │
            mDNS, Port 4712  │  └──────────┬────────────┘                   │
                             │             │ Modbus TCP 127.0.0.1:5502      │
                             │  ┌──────────▼────────────┐                   │
                             │  │ CODESYS: FbEebus*     │◄─► Anlage         │
                             │  │ Watchdog, Ersatzgrenze│    (Last, PV, …)  │
                             │  └───────────────────────┘                   │
                             └──────────────────────────────────────────────┘
```

Aufgabenteilung: Der Go-Dienst kapselt das Protokoll und die Zustandsautomaten (Init, Begrenzt, Failsafe, …) für Bezug und Einspeisung. Die SPS sieht je Richtung nur noch „Begrenzung aktiv ja/nein“ und einen Wert in W und liefert die Messwerte. Zusätzlich überwacht die SPS die Brücke selbst: Steht deren Lebenszeichen, gilt die lokale Ersatzgrenze.

**Wichtig für die Regelung:** Die Grenzen beziehen sich laut FNN-Hinweis (Fußnote 4) auf den **Netzanschlusspunkt**. Die CODESYS-Applikation muss sie am Zähler des Netzanschlusspunkts einhalten. Bei einem EMS ist außerdem die Mindestbezugsleistung nach BNetzA BK6-22-300, Anlage 1, im EMS einzustellen.

## Aufbau des Repos

| Pfad | Inhalt |
|---|---|
| `bruecke/` | Die Brücke (läuft auf dem PFC): EEBUS-Anbindung, Zustandsautomaten, Messwerte, Kopplung, Modbus-Server, Status-UI |
| `codesys/` | CODESYS-Quellen: `FbEebusBruecke`, `FbEebusBegrenzung`, `FbEebusMesswerte`, Strukturen, GVL, Enums, Beispielprogramm. `FbEebusLpc` für bestehende Projekte |
| `testwerkzeuge/steuerbox/` | Test-Steuerbox mit Web-UI: Grenzen für Bezug und Einspeisung senden, Messwerte anzeigen, Heartbeat- und Verbindungsausfall simulieren |
| `testwerkzeuge/spssimulator/` | Modbus-Client, der die CODESYS-Seite spielt: Last, PV, Messwerte |
| `internal/gemeinsam/` | Gemeinsamer Go-Code: Zertifikat, Umgebungsvariablen, Ereignisprotokoll, Web-UI-Grundlagen |
| `third_party/spine-go/` | spine-go mit einem Fix, siehe `PATCH.md` |
| `Dockerfile` | Ein Dockerfile für alle Programme, Auswahl per `--build-arg PROGRAMM=…` |
| `docker-compose.yml` | Lokale Testumgebung: Brücke, Test-Steuerbox, SPS-Simulator |
| `skripte/pfc-images-bauen.sh` | Baut Brücke und Test-Steuerbox für den PFC nach `dist/` |
| `dist/` | gebaute Images zum Verteilen (nicht im Repo) |
| `CLAUDE.md` | Arbeitsstand, offene Punkte und technische Details für die Weiterentwicklung |

Alle Go-Programme liegen in einem Modul. Gebaut wird ausschließlich in Docker (Go 1.24), eine lokale Go-Installation ist nicht nötig.

**eebus-go-Version:** Für den SHIP Pairing Service wird der noch unveröffentlichte Entwicklungsstand von eebus-go und ship-go genutzt, auf feste Commits gepinnt (siehe `go.mod`: eebus-go `8583642861c3` vom 30.09.2026, ship-go `a84426bc3810` vom 28.09.2026). Die Schnittstelle kann sich bis zur Veröffentlichung noch ändern. Vor einem Update die Aufrufe in `bruecke/` und `testwerkzeuge/steuerbox/` abgleichen und die Testszenarien wiederholen.

## Registerlayout (Schnittstellenversion 1, Erweiterung 2)

Mehrwortige Werte High-Word zuerst. Unit-ID beliebig. Gegenüber Version 1 sind nur Register angehängt: Register 1 bleibt `1`, die Erweiterungsversion steht in Register 14. Ältere SPS-Programme (14 Input-, 3 Holding-Register, `FbEebusLpc`) laufen unverändert weiter.

Input-Register (FC04), Brücke → SPS. Bezug (LPC) und Einspeisung (LPP) haben je einen gleich aufgebauten Block, Bezug ab Register 2, Einspeisung ab Register 17:

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
| 0 | Lebenszeichen Brücke (+1 pro Sekunde) |
| 1 | Schnittstellenversion (1) |
| 14 | Erweiterungsversion (2) |
| 15 | Use Cases, die die Brücke anbietet (Bit 0 LPC, 1 LPP, 2 MPC, 3 MGCP) |
| 16 | Use Cases, die die Steuerbox unterstützt (Bits wie Register 15) |
| 29–30 | Failsafe-Mindestdauer in s, gilt für beide Richtungen |

Holding-Register (FC03/FC16), SPS → Brücke. Alle in **einem** FC16-Aufruf schreiben, dann sieht die Brücke einen zusammengehörigen Stand:

| Adr. | Inhalt | Typ, Einheit |
|---|---|---|
| 0 | Lebenszeichen SPS (+1 pro Sekunde) | UINT |
| 1–2 | Nennleistung Bezug (an die Steuerbox), 0 = `NENNLEISTUNG_MAX_W` | UDINT W |
| 3–4 | Nennleistung Erzeugung (LPP), 0 = `NENNLEISTUNG_ERZEUGUNG_MAX_W` | UDINT W |
| 5 | Gültigkeit MPC: Bit 0 P, 1–3 P L1–L3, 4 E Bezug, 5 E Erzeugung, 6–8 I, 9–11 U, 12 f | WORD |
| 6 | Gültigkeit MGCP: Bit 0 P, 1 E Einspeisung, 2 E Bezug, 3–5 I, 6–8 U, 9 f, 10 PV-Faktor | WORD |
| 7 | Anlagenstatus: 0 normal, 1 Störung, 2 Standby | UINT |
| 8–9 | MPC Leistung (Bezug +, Erzeugung −) | DINT W |
| 10–15 | MPC Leistung L1, L2, L3 | 3× DINT W |
| 16–19 | MPC Energie Bezug (Zählerstand) | ULINT Wh |
| 20–23 | MPC Energie Erzeugung (Zählerstand) | ULINT Wh |
| 24–29 | MPC Strom L1, L2, L3 | 3× DINT mA |
| 30–32 | MPC Spannung L1, L2, L3 | 3× UINT 0,1 V |
| 33 | MPC Frequenz | UINT 0,01 Hz |
| 40–41 | MGCP Leistung (Bezug aus dem Netz +, Einspeisung −) | DINT W |
| 42–45 | MGCP Energie Einspeisung | ULINT Wh |
| 46–49 | MGCP Energie Bezug | ULINT Wh |
| 50–55 | MGCP Strom L1, L2, L3 | 3× DINT mA |
| 56–58 | MGCP Spannung L1, L2, L3 | 3× UINT 0,1 V |
| 59 | MGCP Frequenz | UINT 0,01 Hz |
| 60 | MGCP PV-Einspeisebegrenzungsfaktor | UINT 0,1 % |

Ein Messwert ohne Gültigkeitsbit, oder alle Werte bei ausgefallener SPS, gehen als „ungültig“ (ValueState `error`) an die Steuerbox. Energie in 64 Bit, weil 32 Bit Wh an einem größeren Netzanschluss nach wenigen Jahren überlaufen. Strom in mA, weil 0,01 A in 16 Bit nur bis 327 A reicht.

## Bauen und Verteilen

Auf dem Entwicklungsrechner (Docker Desktop, Git Bash) aus dem Repo-Wurzelverzeichnis:

```sh
sh skripte/pfc-images-bauen.sh 0.4
scp dist/eebus-bruecke-0.4.tar.gz root@<pfc-ip>:/home/eebus-bruecke/
```

Das Skript baut Brücke und Test-Steuerbox für ARMv7 und legt beide als `.tar.gz` unter `dist/` ab. `scp` aus Git Bash, nicht aus PowerShell. Als `admin` statt `root` erst nach `/tmp` kopieren und auf dem PFC mit `sudo` verschieben.

Auf dem PFC200 (als root):

```sh
mkdir -p /home/eebus-bruecke
docker load -i /home/eebus-bruecke/eebus-bruecke-0.4.tar.gz

docker rm -f eebus-bruecke    # falls eine ältere Version läuft
docker run -d --name eebus-bruecke \
  --restart unless-stopped \
  --network host \
  --memory 64m \
  -v /home/eebus-bruecke:/data \
  -e GERAET_MARKE=<Firmenkürzel> \
  -e EEBUS_USECASES=lpc,mpc \
  -e NENNLEISTUNG_MAX_W=22000 \
  -e FAILSAFE_GRENZE_W=4200 \
  -e FAILSAFE_MINDESTDAUER=2h \
  -e WEB_PASSWORT='<Passwort für das Status-UI>' \
  eebus-bruecke:0.5
```

Mit Erzeugung (PV, Speicher) zusätzlich `lpp` und die beiden Pflichtwerte dafür, bei einem EMS mit Zähler am Netzanschlusspunkt `mgcp`:

```sh
  -e EEBUS_USECASES=lpc,lpp,mpc,mgcp \
  -e NENNLEISTUNG_ERZEUGUNG_MAX_W=8000 \
  -e FAILSAFE_EINSPEISEGRENZE_W=4800 \
```

Platzhalter in spitzen Klammern samt Klammern ersetzen, die Shell liest `<` sonst als Umleitung. Das Passwort in einfache Anführungszeichen setzen. Nach dem ersten Start die angezeigte SHIP-ID zusätzlich mit `-e SHIP_ID=…` fest eintragen (siehe „Vor der Übergabe an den Messstellenbetreiber“).

`/home/eebus-bruecke` liegt im internen Speicher. Mit SD-Karte stattdessen `/media/sd/eebus-bruecke` verwenden. Das Verzeichnis enthält alles, was die Kopplung ausmacht, und gehört gesichert:

| Datei | Inhalt |
|---|---|
| `zertifikat.pem`, `schluessel.pem` | Zertifikat, daraus SKI und Fingerprint |
| `pairing-secret.txt` | Secret für den SHIP Pairing Service |
| `steuerbox-pairing.json` | per Pairing Service gekoppelte Steuerbox |
| `steuerbox-ski.json` | im UI per SKI gekoppelte Steuerbox (Suchmodus oder „Vertrauen“) |
| `pairing-verlauf.json` | Schutz gegen wiederholte Ankündigungen |
| `failsafe.json` | zuletzt von der Steuerbox vorgegebene Failsafe-Werte |

`--network host` ist nötig, weil mDNS (Multicast) über das Docker-Bridge-Netz nicht zuverlässig funktioniert. Der Modbus-Server bindet trotzdem nur auf 127.0.0.1, ist also aus dem LAN nicht erreichbar. Zum Testen mit einem Modbus-Master auf dem PC `-e MODBUS_URL=tcp://0.0.0.0:5502` setzen. Dann ist der Port ohne Schutz im ganzen LAN offen, danach wieder entfernen.

### Use Cases und Messwerte

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `EEBUS_USECASES` | `lpc` | Angebotene Use Cases, kommagetrennt aus `lpc`, `lpp`, `mpc`, `mgcp`. `lpc` oder `lpp` ist Pflicht. Nur einschalten, was die Anlage liefern kann |
| `NENNLEISTUNG_MAX_W` | 11000 | Nennleistung Bezug, bis die SPS einen Wert schreibt |
| `NENNLEISTUNG_ERZEUGUNG_MAX_W` | – | Nennleistung Erzeugung, **Pflicht mit `lpp`** |
| `FAILSAFE_GRENZE_W` | 4200 | Failsafe-Grenze Bezug (Startwert) |
| `FAILSAFE_EINSPEISEGRENZE_W` | – | Failsafe-Grenze Einspeisung (Startwert), **Pflicht mit `lpp`** |
| `FAILSAFE_MINDESTDAUER` | `2h` | Failsafe-Mindestdauer, gilt für Bezug und Einspeisung gemeinsam |
| `MPC_MESSWERTE` | alle | Angekündigte MPC-Werte: `phasenleistung`, `energie_bezug`, `energie_erzeugung`, `strom`, `spannung`, `frequenz`. Die Gesamtleistung ist immer dabei |
| `MPC_PHASEN` | `abc` | Angeschlossene Phasen, z. B. `a` bei einphasigen Anlagen |
| `MPC_ENTITAET` | `cem` | `cem`: MPC auf der Entität des Energiemanagers (wie LPC). `submeter`: eigene Entität „Unterzähler“, falls eine Steuerbox MPC auf CEM nicht liest |
| `MGCP_MESSWERTE` | `strom,spannung,frequenz` | Zusätzliche MGCP-Werte, dazu `pv_faktor`. Leistung und beide Energien sind immer dabei |
| `MESSWERT_QUELLE` | `measuredValue` | Herkunft der Messwerte: `measuredValue`, `calculatedValue` oder `empiricalValue` |

Welche Werte gerade gültig sind, meldet die SPS über die Gültigkeitsmasken (Holding-Register 5 und 6).

### Weitere Variablen

`EEBUS_PORT` (4712), `MODBUS_URL` (`tcp://127.0.0.1:5502`), `DATENVERZEICHNIS` (`/data`), `GERAET_HERSTELLER`, `GERAET_MARKE`, `GERAET_MODELL`, `GERAET_SERIENNUMMER` (Vorgabe: MAC-Adresse des PFC), `GERAET_HW_REVISION` (Hardware-Revision, wird an die Steuerbox gemeldet). Die Software-Revision ist die Image-Version (`skripte/pfc-images-bauen.sh <Version>`). Zur Kopplung siehe unten.

`EEBUS_DEBUG=an` schreibt das ausführliche Protokoll von eebus-go, ship-go und spine-go nach stdout (`docker logs eebus-bruecke`), nicht ins Ereignisprotokoll. Für die Fehlersuche mit einer neuen Steuerbox, danach wieder ausschalten.

**Failsafe-Werte:** Die `FAILSAFE_*`-Werte sind nur die Startwerte. Schreibt die Steuerbox eigene Werte, merkt die Brücke sie sich in `failsafe.json` und nutzt sie auch nach einem Neustart. Gerade dann, im Zustand „Init“, gelten sie.

**Freigabe von Grenzen:** Die Brücke nimmt eine Grenze nur an, wenn die SPS erreichbar ist (FNN-Hinweis 4.1.2.2: Übernahme *und* Umsetzung bestätigen). Sonst lehnt sie mit „SPS nicht erreichbar“ ab und meldet der Steuerbox den Anlagenstatus „Störung“.

## Status-UI der Brücke

`http://<pfc-ip>:8090`, Anmeldung mit Benutzer `admin` und dem Passwort aus `WEB_PASSWORT`. Die Seite zeigt den Zustand an. Ändern lässt sich nur die Kopplung mit der Steuerbox (Suchmodus, Anfrage annehmen, „Vertrauen“, „Kopplung lösen“), mit `WEB_KOPPLUNG=aus` auch das nicht:

- Ampel mit dem kritischsten Zustand von Bezug und Einspeisung
- je Richtung (LPC, LPP): Zustand, wirksame Grenze, Grenze des Netzbetreibers, Restlaufzeit, Failsafe-Grenze, Nennleistung, letzter Ablehnungsgrund
- EEBUS-Verbindung, Alter des Heartbeats, gemeinsame Failsafe-Mindestdauer
- SPS-Lebenszeichen und Anlagenstatus
- Use Cases: was die Brücke anbietet und was die Steuerbox unterstützt
- Messwerte für MPC und MGCP, ungültige Werte gekennzeichnet
- Steuerbox: gemeldete Daten (Gerät, Software, Entitäten, Use Cases mit Version und Szenarien)
- Kopplung: aktuelle Steuerbox, Suchmodus mit Kopplungsanfragen, SKI, SHIP-ID, Fingerprint, Secret (verdeckt) und QR-Code für den Messstellenbetreiber
- per mDNS gefundene Geräte, Steuerboxen markiert und oben
- die letzten 100 Log-Meldungen

Unter **„Anleitung“** erklärt eine eigene Seite mit Schaubildern den Aufbau, die Use Cases, die Zustände, die Kopplungsverfahren, die PROLAN-Steuerbox und die Fehlersuche.

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `WEB_PASSWORT` | – | **Ohne Passwort startet das UI nicht**, die Brücke selbst läuft normal weiter. |
| `WEB_BENUTZER` | `admin` | Benutzername |
| `WEB_ADRESSE` | `:8090` | Adresse und Port, leer (`WEB_ADRESSE=`) schaltet das UI ab |
| `WEB_KOPPLUNG` | `an` | `aus` macht das UI rein lesend (z. B. nach der Inbetriebnahme) |

Port 8090 statt 8080, weil auf dem PFC die CODESYS-WebVisu oft 8080 belegt. Bei aktiver PFC-Firewall Port 8090 freigeben.

Die Anmeldung läuft über HTTP Basic Auth ohne HTTPS, das Passwort ist im LAN also mitlesbar. Sie schützt vor zufälligem Zugriff, nicht vor gezielten Angriffen.

## Kopplung mit der Steuerbox

Beide Seiten müssen einander vertrauen. Auf Seiten der Steuerbox trägt der Messstellenbetreiber die Brücke ein, bei PROLAN nur aus der Ferne. Auf Seiten der Brücke gibt es drei Wege. Welches Verfahren gilt, legt der Messstellenbetreiber fest. Gekoppelt ist immer genau eine Steuerbox, eine neue Kopplung ersetzt die alte.

**A · SHIP Pairing Service (neues Verfahren, Vorgabe an).** Die Brücke erzeugt beim ersten Start ein zufälliges Secret. Die Statusseite zeigt unter „Kopplung“ einen QR-Code mit SKI, SHIP-ID, SHA-256-Fingerprint und Secret (`SHIP;SKI:…;ID:…;FPH256:…;SPSEC:…;ENDSHIP;`). Der Messstellenbetreiber trägt ihn in die Steuerbox ein. Die Steuerbox kündigt sich dann per mDNS an und beweist per HMAC, dass sie das Secret kennt. Die Brücke vertraut ihr daraufhin automatisch und speichert die Kopplung. Ein Neustart ist nicht nötig. Wird die Steuerbox getauscht, koppelt sich die neue nach 15 Minuten selbst, sofern sie das Secret kennt.

**B · SKI-Verfahren mit Suchmodus.** SKI und SHIP-ID der Brücke an den Messstellenbetreiber geben. Auf der Statusseite „Steuerbox suchen“ drücken: Für 10 Minuten werden Verbindungsversuche unbekannter Geräte nicht abgewiesen, sondern als Kopplungsanfrage angezeigt. Meldet sich die Steuerbox, „Annehmen“. Alternativ bei der gefundenen Steuerbox „Vertrauen“, dann verbindet sich die Brücke selbst. Die Kopplung landet in `steuerbox-ski.json`. Eine automatische Annahme gibt es bewusst nicht (`SetAutoAccept` bleibt aus), sonst könnte jedes Gerät im LAN Grenzen setzen. Ohne Suchmodus meldet die Brücke abgelehnte Versuche unbekannter Geräte einmal im Ereignisprotokoll.

**C · SKI fest per Einstellung.** Den SKI der Steuerbox als `EEBUS_REMOTE_SKI` setzen und den Container neu anlegen. Dann gilt nur diese Steuerbox: Pairing Service, Suchmodus und Kopplung im UI sind aus, eine gespeicherte Kopplung wird ignoriert.

Den Pairing Service mit vertauschten Rollen (Brücke kündigt sich an) gibt es nicht: Das ginge nur, wenn die Steuerbox selbst einen QR-Code mit Secret zeigt.

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `EEBUS_PAIRING_SERVICE` | `an` | `aus` schaltet den Pairing Service ab |
| `EEBUS_REMOTE_SKI` | – | SKI der Steuerbox, fest eingestellt (Verfahren C), schaltet den Pairing Service ab |
| `SHIP_ID` | `<Marke>-<Modell>-<MAC>` | Kennung der Brücke im Netz, z. B. `Demo-PFC200-LPC-Bruecke-0030DE683ADC` |

### SHIP-ID

Die SHIP-ID ist der dauerhafte Name der Brücke im EEBUS-Netz. Sie wird per mDNS verkündet und steht im QR-Code. Über sie findet die Steuerbox die Brücke wieder, und beim Pairing Service richtet die Steuerbox ihre Ankündigung genau an diese ID. Laut SHIP-Spezifikation muss sie **weltweit eindeutig** sein und darf sich **nicht ändern**.

- **Aufbau:** frei wählbarer Text, höchstens 63 Byte, ohne Leerzeichen, Semikolons und Steuerzeichen. Er soll mit einem Herstellerkürzel beginnen, dahinter folgt eine eindeutige Kennung. Die Brücke bildet `<GERAET_MARKE>-<GERAET_MODELL>-<MAC>`, z. B. `Demo-PFC200-LPC-Bruecke-0030DE683ADC`.
- **MAC-Adresse:** Mit `--network host` sieht der Container die echten Schnittstellen des PFC und nimmt bevorzugt `br0` (X1). Das ist die MAC vom Typenschild, sie bleibt bei Neustarts und Updates gleich. Prüfen: `cat /sys/class/net/br0/address` muss zum Ende der SHIP-ID passen. Ohne Host-Netz (z. B. lokal mit Compose) erzeugt Docker eine eigene MAC, die sich ändern kann.
- **IANA-Format:** EEBUS empfiehlt für neue Geräte `i:<IANA-Nummer>_u:<Seriennummer>`. Das ist keine Pflicht, Geräte müssen auch andere Formate akzeptieren. Ohne eigene IANA-Nummer bleibt es beim obigen Format.
- **SHIP-ID und SKI sind verschieden:** SKI und Fingerprint werden aus dem Zertifikat berechnet. Die SHIP-ID ist ein Name, der auch bei einem neuen Zertifikat bleibt.

### Vor der Übergabe an den Messstellenbetreiber

Was danach geändert wird, erzwingt ein neues Pairing. Deshalb vorher:

1. **`GERAET_MARKE` auf ein Kürzel der eigenen Firma setzen** statt `Demo`. Das entspricht der Regel „SHIP-ID beginnt mit dem Herstellerkürzel“, und die Marke erscheint im QR-Code und bei der Steuerbox als Gerätename.
2. **SHIP-ID festschreiben:** Die SHIP-ID von der Statusseite kopieren und mit `-e SHIP_ID=…` im `docker run` eintragen. Danach hängt sie nicht mehr an der MAC, und auch ein Tausch des PFC (mit übertragenem Datenverzeichnis) ändert sie nicht. **Ab der Übergabe SHIP-ID und Datenverzeichnis nicht mehr ändern.**
3. **Datenverzeichnis sichern** (`/home/eebus-bruecke`, siehe Tabelle oben): Zertifikat, Secret und Kopplung.
4. **Netzwerk mit dem Installateur abstimmen**, siehe nächster Abschnitt.
5. **Verfahren klären:** Pairing Service (QR-Code bzw. SKI, SHIP-ID, Fingerprint, Secret übergeben) oder SKI-Verfahren (SKI und SHIP-ID übergeben, an der Brücke per Suchmodus annehmen).
6. **Use Cases festlegen:** `EEBUS_USECASES` passend zur Anlage, die Steuerbox muss sie ebenfalls aktiviert haben.

### Netzwerk: damit sich Brücke und Steuerbox finden

**Vorgabe für den Installateur:** Steuerbox und PFC (X1) ins selbe Netz, **selbes Subnetz** (z. B. beide in 192.168.1.0/24), kein VLAN und kein Router dazwischen. DHCP oder feste Adressen sind egal, solange sie im selben Bereich liegen.

Die Geräte finden sich per **mDNS** (Multicast-DNS, UDP 5353 an 224.0.0.251). Diese Pakete verlassen das eigene Netzsegment nicht, Router leiten sie nicht weiter. Danach läuft die eigentliche Verbindung als verschlüsselter WebSocket per TCP zu der Adresse, die mDNS geliefert hat. Daher braucht es beides: dasselbe Segment zum Finden und gegenseitige IP-Erreichbarkeit zum Verbinden.

| Fall | Finden | Verbinden | Ergebnis |
|---|---|---|---|
| Gleiches Segment, gleiches Subnetz (192.168.1.10 und 192.168.1.20, /24) | ✅ | ✅ | funktioniert |
| Gleiches Segment, verschiedene Subnetze (192.168.1.10 und 192.168.2.20) | ✅ | ❌ nur über Router | praktisch nicht |
| Verschiedene Segmente oder VLANs, über Router verbunden | ❌ ohne mDNS-Repeater | ✅ | nur mit Zusatzaufwand am Router |

| Voraussetzung | Warum |
|---|---|
| **Steuerbox und PFC im selben Netzsegment** (gleiches Subnetz, gleiches VLAN, über Switches verbunden, kein Router dazwischen) | mDNS geht nicht über Router. Getrennte Netze nur mit einem mDNS-Repeater/Reflector am Router |
| **Multicast nicht blockiert** | Verwaltete Switches mit IGMP-Snooping ohne Querier, „Multicast-Filter“ oder WLAN mit Client-Isolation können mDNS verschlucken |
| **PFC-Firewall:** UDP 5353 und TCP 4712 eingehend erlaubt | 4712 ist der EEBUS-Port der Brücke. Die Steuerbox verbindet sich dorthin, oder die Brücke zu ihr |
| **IP-Adressen im selben Bereich** (DHCP oder fest) | Ohne DHCP vergeben EEBUS-Geräte sich laut Spezifikation selbst eine 169.254.x.x-Adresse. Das klappt nur, wenn beide Seiten das tun |

Mit dem Installateur bzw. Messstellenbetreiber klären:

- In welches Netz kommt der LAN-Anschluss der Steuerbox? Am einfachsten in dasselbe Netz wie X1 des PFC.
- Wie werden die IP-Adressen vergeben: DHCP oder fest? Gibt es VLANs?
- Ist ein verwalteter Switch mit Multicast-Filter dazwischen?
- Alternative mit sauberer Trennung: X2 des PFC im WBM als eigene Schnittstelle konfigurieren und die EEBUS-Buchse der Steuerbox (bei PROLAN „ETH1“) direkt an X2 anschließen, mit festem gemeinsamem Subnetz. Die Brücke lauscht dank `--network host` auf allen Schnittstellen. Das ist noch nicht ausprobiert, vorab mit einem Laptop und der Test-Steuerbox direkt an X2 testen. Offen ist dabei auch, ob ship-go IPv6-Link-Local-Adressen (ohne DHCP) korrekt anspricht.

Zur Kontrolle zeigt die Statusseite unter „Per mDNS gefundene EEBUS-Geräte“, ob die Steuerbox gesehen wird. Erscheint sie dort nicht, liegt es am Netz, nicht an der Kopplung.

## PROLAN-Steuerbox (STB-142E)

Die erste echte Steuerbox an der Brücke. Stand der Recherche (Gebrauchsanleitung v1.1, Datenblatt 07/2025, BSI-Zertifikatsliste):

- **Typen:** STB-142 nur mit Relais, **STB-142E** mit aktivierbarer EEBUS-Schnittstelle, beide nach FNN-Lastenheft 1.4. Zertifiziert ist die STB-142E mit HW 3.4 und FW 1.1.3 (TR-03109-5 `BSI-K-TR-0907-2026`, BSZ `BSI-DSZ-BSZ-0025-2026`). Laut Sekundärquellen unterstützt sie LPC, LPP, MPC und MGCP.
- **Anschluss:** EEBUS über die Buchse **ETH1**, die Buchse **CLS** geht zum Smart Meter Gateway. ETH1 dient auch zur Kaskadierung weiterer Steuerboxen.
- **Verplombung:** Aus dem verplombten Bereich dürfen nur Kabel zu EEBUS-Geräten herausgeführt werden. Kann ETH1 kaskadieren, darf die Buchse außerhalb nicht erreichbar sein: Der Messstellenbetreiber muss die Kaskadierung dann abschalten.
- **Kopplung:** Der Messstellenbetreiber (Steuerbox-Administrator) richtet den EEBUS-Partner aus der Ferne ein, vor Ort lässt sich nichts einstellen. Er braucht SKI und SHIP-ID der Brücke, beim Pairing Service zusätzlich Fingerprint und Secret. An der Brücke dann Suchmodus und „Annehmen“.
- **LED „PWR“:** blinkt im Sekundentakt, solange eine eingerichtete EEBUS-Verbindung fehlt.
- **Details** zu EEBUS (Use-Case-Versionen, Heartbeat, IP-Konfiguration von ETH1, Pairing Service) stehen im „Technischen Handbuch“, das es nur im Prolan-Kundenportal gibt.

Fragen an den Messstellenbetreiber vor dem Anschluss: Firmware-Stand (≥ 1.1.3) und EEBUS aktiviert? Welche Use Cases sind aktiv? Kopplung per SKI oder Pairing Service? Wie bekommt ETH1 seine IP-Adresse? Ist die Kaskadierung auf ETH1 abgeschaltet? Welche Failsafe-Werte und welcher Heartbeat-Takt? Zugang zum Technischen Handbuch?

Beim ersten Anschluss hilft die Karte „Steuerbox: gemeldete Daten“ auf der Statusseite: Sie zeigt, welche Use Cases die Box in welcher Version auf welcher Entität meldet. Für mehr Details `EEBUS_DEBUG=an` setzen.

## CODESYS-Seite

Die Quellen liegen als Text unter `codesys/` und werden ins CODESYS-Projekt übernommen.

Gerätebaum:

1. *Ethernet-Adapter → Ethernet* anhängen, Schnittstelle mit der IP des PFC wählen (X1 meist `br0`). Für die Verbindung zu `127.0.0.1` ist die Wahl egal, CODESYS verlangt aber einen Adapter.
2. Darunter *ModbusTCP Master*, **Auto-Reconnect aktivieren**. Sonst gibt CODESYS nach einem Neustart der Brücke auf, und die Brücke zeigt „SPS ausgefallen“.
3. Darunter *ModbusTCP Slave* mit IP `127.0.0.1`, Port `5502`, Unit-ID beliebig.
4. Kanäle wie in `codesys/GvlEebus.st`: FC04 Offset 0 Länge 31 (200 ms) und FC16 Offset 0 Länge 61 (1 s).
5. Im E/A-Abbild die Kanäle als Ganzes auf `GvlEebus.aInputRegister` bzw. `GvlEebus.aHoldingRegister` legen, Buszyklus-Task = Task von `PrgEnergiemanagement`.

Bausteine (Beispiel in `PrgEnergiemanagement.st`):

| Baustein | Aufgabe |
|---|---|
| `FbEebusBruecke` | Lebenszeichen, Versionsprüfung, Verbindung, Use-Case-Masken; schreibt SPS-Lebenszeichen, Nennleistungen und Anlagenstatus (`eAnlagenstatus`) |
| `FbEebusBegrenzung` | eine Richtung, zweimal aufrufen: `iBasis := 2` Bezug, `iBasis := 17` Einspeisung. Ersatzgrenze, wenn die Brücke ausfällt oder den Use Case nicht anbietet |
| `FbEebusMesswerte` | schreibt `ST_EebusMpc` und `ST_EebusMgcp` samt Gültigkeitsbits |
| `FbEebusLpc` | veraltet, nur für bestehende Projekte mit 14/3 Registern |

Die Quellen sind ohne CODESYS-Umgebung geschrieben und noch nicht kompiliert. Beim Übernehmen auf Typfehler achten.

Falls der Gerätebaum Localhost als Ziel nicht akzeptiert, alternativ `FbMbMasterTcp` aus WagoAppPlcModbus mit `sHost := '127.0.0.1'` verwenden. Die Bausteine bleiben dabei unverändert, da sie nur die Register-Arrays sehen.

## Test-Steuerbox

`testwerkzeuge/steuerbox` spielt die Steuerbox des Messstellenbetreibers (Energy Guard für LPC und LPP, Monitoring Appliance für MPC und MGCP). Sie wird über ein Web-UI bedient (Port 8091). Mit `STEUERBOX_USECASES` (Vorgabe `lpc,lpp,mpc,mgcp`) lässt sich eine Steuerbox nachstellen, die nur einen Teil kann.

- **Bezug senden (LPC) und Einspeisung senden (LPP):** Grenze mit Leistung und Dauer, Grenze aufheben, Vorlagen. Failsafe-Grenze je Richtung und Failsafe-Mindestdauer (2–24 h, gemeinsam).
- **Beide Grenzen in einer Nachricht:** wie es andere EEBUS-Stacks tun können, die Brücke muss beide getrennt freigeben.
- **Messwerte:** MPC und MGCP so, wie die Brücke sie meldet, ungültige Werte gekennzeichnet.
- **Störungen simulieren:**
  - Heartbeat stoppen: Die Brücke geht 120 s nach dem letzten Heartbeat in Failsafe. Zurück geht es nur mit Heartbeat *und* einer neuen Grenze.
  - Verbindung kurz unterbrechen: Die Verbindung wird automatisch neu aufgebaut.
  - Trennen: Die Verbindung bleibt getrennt, bis sie wiederhergestellt wird.
- **Werte bei der Brücke:** Grenzen, Failsafe-Werte, Nennleistungen, Anlagenstatus und angebotene Use Cases, so wie die Brücke sie über EEBUS meldet. Die Nennleistungen kommen von der SPS, das prüft also die ganze Kette.
- **Kopplung, wahlweise:** per SHIP Pairing Service (QR-Text der Brücke einfügen, die Steuerbox kündigt sich mit dem Secret an) oder per SKI (bei der gefundenen Brücke „Per SKI koppeln“, an der Brücke per Suchmodus annehmen). Die Kopplung wird im Volume gespeichert.
- **Ereignisse:** Antworten der Brücke (angenommen/abgelehnt), Verbindungswechsel, Pairing.
- **Anleitung:** eigene Seite mit Testaufbau, Kopplung und 17 Testszenarien mit erwartetem Ergebnis.

Der SPS-Simulator rechnet eine kleine Anlage durch: steuerbare Last (`-last`, folgt der Bezugsgrenze), PV (`-pv`, wird bei Einspeisegrenze abgeregelt) und Grundlast (`-grundlast`). Weitere Schalter: `-stoerung` (Anlagenstatus Störung), `-ungueltig-mpc`/`-ungueltig-mgcp` (Gültigkeitsbits löschen), `-zaehlerstart` (Zählerstände über 2³² testen).

Die Steuerbox sendet ihren Heartbeat alle 8 s (`HEARTBEAT_TIMEOUT`, Vorgabe 10 s, minus 2 s), damit die Brücke nach dem Verbinden schnell aus „Init“ kommt. Weitere Variablen: `EEBUS_PORT` (4713), `SHIP_ID` (Vorgabe `Test-Steuerbox-<MAC>`), `WEB_ADRESSE` (`:8091`), `WEB_BENUTZER` (`admin`), `WEB_PASSWORT` (Pflicht), `DATENVERZEICHNIS` (`/data`).

### Lokal auf dem PC

`docker-compose.yml` startet Brücke, Test-Steuerbox und SPS-Simulator im selben Docker-Netz. mDNS funktioniert dort zwischen den Containern.

```sh
docker compose up -d --build
```

| Dienst | Adresse | Anmeldung |
|---|---|---|
| Brücke | http://localhost:8090 | admin / test |
| Test-Steuerbox | http://localhost:8091 | admin / test |
| Modbus der Brücke | localhost:5502 | – |

Einmalig koppeln, am einfachsten per Pairing Service: Im Brücken-UI auf „QR-Text kopieren“ klicken, den Text im Steuerbox-UI unter „Kopplung“ einfügen und „Per Pairing Service koppeln“ wählen.

Alternativ per SKI wie bei PROLAN: Im Steuerbox-UI bei der Brücke „Per SKI koppeln“, im Brücken-UI „Steuerbox suchen“ und die Anfrage annehmen. Oder fest per `.env` im Repo-Wurzelverzeichnis (`STEUERBOX_SKI=<SKI>`) und `docker compose up -d`.

Die Compose-Datei schaltet alle vier Use Cases ein, der Simulator spielt 9 kW Last und 8 kW PV.

Die SHIP-IDs sind in `docker-compose.yml` fest eingetragen, weil sich die MAC eines Containers beim Neuanlegen ändern kann. Zertifikate, Secret und Kopplung liegen in Docker-Volumes und überstehen Neustarts. SPS-Ausfall testen: `docker compose stop spssimulator`. Alles entfernen inklusive Volumes: `docker compose down -v`.

### Auf dem PFC

Für Tests mit der echten CODESYS-Applikation läuft die Test-Steuerbox als zweiter Container auf demselben PFC. Das Image baut `skripte/pfc-images-bauen.sh` mit.

```sh
mkdir -p /home/eebus-steuerbox
docker load -i /home/eebus-steuerbox/eebus-steuerbox-0.4.tar.gz
docker run -d --name eebus-steuerbox \
  --network host \
  --memory 64m \
  -v /home/eebus-steuerbox:/data \
  -e WEB_PASSWORT='<Passwort>' \
  eebus-steuerbox:0.4
```

Danach `http://<pfc-ip>:8091` öffnen und die Brücke per Pairing Service koppeln (QR-Text aus dem Brücken-UI). An der Brücke ist dafür nichts einzustellen.

Nach dem Test die Test-Steuerbox entfernen und ihre Kopplung bei der Brücke löschen, sonst vertraut die Brücke ihr weiter:

```sh
docker rm -f eebus-steuerbox
rm /home/eebus-bruecke/steuerbox-pairing.json /home/eebus-bruecke/failsafe.json
docker restart eebus-bruecke
```

Zertifikat und Secret bleiben dabei erhalten, SKI, Fingerprint und QR-Code ändern sich also nicht. War die Brücke per SKI an die Test-Steuerbox gekoppelt, zusätzlich `EEBUS_REMOTE_SKI` entfernen bzw. auf den SKI der echten Steuerbox setzen.

Das ist auf dem PFC noch nicht ausprobiert. Lokal laufen beide Container im selben Netz problemlos. Auf dem PFC teilen sich beide den Host. Falls sie sich per mDNS nicht finden, gibt das Log der Steuerbox Auskunft.

## Offene Punkte vor dem Produktiveinsatz

- **Unveröffentlichte eebus-go-Version:** Der Pairing Service stammt aus dem Entwicklungsstand von eebus-go/ship-go (siehe oben). Lokal mit der Test-Steuerbox getestet: Kopplung per Pairing Service, per SKI und per Suchmodus, falsches Secret wird abgelehnt, Neustart, Grenzen für Bezug und Einspeisung (auch beide in einer Nachricht), Ablauf, Failsafe-Werte inklusive Speicherung, Heartbeat-Ausfall beider Richtungen, Verbindungsabbrüche, Nennleistungen, Messwerte MPC und MGCP, Ablehnung ohne SPS, Anlagenstatus. **Noch nicht gegen eine echte Steuerbox.**
- **Workarounds für eebus-go/spine-go** (bei einem Update prüfen, ob noch nötig): MGCP kündigt den falschen Akteur an (`bruecke/messwerte.go`), `Set*NominalMax` findet die Kennlinie nicht, wenn MPC auf derselben Entität liegt (`bruecke/begrenzung.go`), Freigabe gleichzeitiger Schreibanfragen (`third_party/spine-go`).
- **MPC auf der CEM-Entität:** Ob die PROLAN-Steuerbox MPC dort liest, ist offen. Sonst `MPC_ENTITAET=submeter`.
- **Verbindungsstatus:** Beim allerersten Test mit dem eebus-go-Beispiel kam nach dem Stoppen der Gegenseite keine Trennungsmeldung. In allen späteren Tests wurde die Trennung sofort gemeldet. Bei der echten Steuerbox beobachten. Für die Grenze ist das unkritisch, dort entscheidet der Heartbeat.
- **Zustandsautomat:** Die Übergänge in `bruecke/begrenzung.go` (Init geht nach 120 s ohne Heartbeat in Failsafe; Failsafe wird nur mit Heartbeat und neuer Grenze oder nach der Mindestdauer verlassen) gegen die Spezifikationen „Limitation of Power Consumption/Production“ und das FNN-Lastenheft Steuerbox 1.4 prüfen. Der FNN-Hinweis nennt keine Update-Raten für Messwerte. Die Brücke sendet bei jeder Änderung, gegebenenfalls ein Totband ergänzen.
- **Zertifizierung:** Diese Brücke ist nicht EEBUS-zertifiziert. Für Pilot- und Eigenanlagen ausreichend, für Serienanlagen vorher mit Netzbetreiber bzw. MSB klären.
- **Docker auf dem PFC200:** Nur ab neueren Firmware-Ständen verfügbar, bei gemischtem Gerätepark vorab je Steuerung prüfen. Docker-Datenverzeichnis wegen begrenztem internem Speicher möglichst auf die SD-Karte legen.
- **mDNS:** Läuft auf dem PFC bereits ein Avahi-Dienst, auf Port-Konflikte an 5353 achten. Netzwerk vorab mit dem Installateur abstimmen (siehe „Netzwerk“).
- **Brücke und Test-Steuerbox gleichzeitig auf dem PFC:** noch nicht ausprobiert, lokal funktioniert es.
- **CODESYS-Bausteine** für Erweiterung 2 noch nicht in CODESYS kompiliert und nicht auf dem PFC getestet.

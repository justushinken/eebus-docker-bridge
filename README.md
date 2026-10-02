# EEBUS-LPC-Brücke für WAGO PFC200

Docker-Container, der auf dem PFC200 als EEBUS "Controllable System" (Use Case LPC, §14a EnWG) läuft und die Leistungsgrenze der Steuerbox per Modbus TCP an die CODESYS-Applikation weitergibt.

```
 Steuerbox (Energy Guard)                     PFC200
 am Smart Meter Gateway      ┌──────────────────────────────────────────────┐
          │                  │  Docker (--network host)                     │
          │  SHIP/SPINE      │  ┌───────────────────────┐                   │
          └──────────────────┼─►│ eebus-bruecke (Go)    │                   │
            TLS-WebSocket,   │  │ eebus-go, cs/lpc      │                   │
            mDNS, Port 4712  │  │ LPC-Zustandsautomat   │                   │
                             │  └──────────┬────────────┘                   │
                             │             │ Modbus TCP 127.0.0.1:5502      │
                             │  ┌──────────▼────────────┐                   │
                             │  │ CODESYS: FbEebusLpc   │──► Verbraucher    │
                             │  │ Watchdog, Ersatzgrenze│                   │
                             │  └───────────────────────┘                   │
                             └──────────────────────────────────────────────┘
```

Aufgabenteilung: Der Go-Dienst kapselt das Protokoll und den LPC-Zustandsautomaten (Init, Begrenzt, Failsafe, …). Die SPS sieht nur noch "Begrenzung aktiv ja/nein" und einen Wert in W. Zusätzlich überwacht die SPS die Brücke selbst: Steht deren Lebenszeichen, gilt die lokale Ersatzgrenze.

## Aufbau des Repos

| Pfad | Inhalt |
|---|---|
| `bruecke/` | Die Brücke (läuft auf dem PFC): EEBUS-Anbindung, LPC-Zustandsautomat, Modbus-Server, Status-UI |
| `codesys/` | CODESYS-Quellen: `FbEebusLpc`, GVL, Enums, Beispielprogramm |
| `testwerkzeuge/steuerbox/` | Test-Steuerbox mit Web-UI: Grenzen senden, Heartbeat- und Verbindungsausfall simulieren |
| `testwerkzeuge/spssimulator/` | Modbus-Client, der die CODESYS-Seite spielt |
| `internal/gemeinsam/` | Gemeinsamer Go-Code: Zertifikat, Umgebungsvariablen, Ereignisprotokoll, Web-UI-Grundlagen |
| `Dockerfile` | Ein Dockerfile für alle Programme, Auswahl per `--build-arg PROGRAMM=…` |
| `docker-compose.yml` | Lokale Testumgebung: Brücke, Test-Steuerbox, SPS-Simulator |
| `skripte/pfc-images-bauen.sh` | Baut Brücke und Test-Steuerbox für den PFC nach `dist/` |
| `dist/` | gebaute Images zum Verteilen (nicht im Repo) |

Alle Go-Programme liegen in einem Modul. Abhängigkeiten sind in `go.mod`/`go.sum` gepinnt (eebus-go v0.7.0, modbus v1.6.4). Gebaut wird ausschließlich in Docker, eine lokale Go-Installation ist nicht nötig.

## Registerlayout (Schnittstellenversion 1)

32-Bit-Werte als zwei Register, High-Word zuerst. Unit-ID beliebig.

Input-Register (FC04), Brücke → SPS:

| Adr. | Inhalt | Einheit |
|---|---|---|
| 0 | Lebenszeichen Brücke (+1 pro Sekunde) | – |
| 1 | Schnittstellenversion | – |
| 2 | LPC-Zustand (0 Init, 1 Unbegrenzt/gesteuert, 2 Begrenzt, 3 Failsafe, 4 Unbegrenzt/autonom) | – |
| 3 | Verbindung (0 kein Partner, 1 getrennt, 2 verbunden) | – |
| 4–5 | wirksame Leistungsgrenze | W |
| 6 | Begrenzung aktiv (0/1) | – |
| 7–8 | letzte Grenze des Netzbetreibers (Diagnose) | W |
| 9–10 | Restlaufzeit der Grenze, 0 = unbefristet | s |
| 11–12 | Failsafe-Grenze (Diagnose) | W |
| 13 | Sekunden seit letztem Heartbeat, 65535 = nie | s |

Holding-Register (FC03/FC16), SPS → Brücke:

| Adr. | Inhalt | Einheit |
|---|---|---|
| 0 | Lebenszeichen SPS (+1 pro Sekunde) | – |
| 1–2 | Nennleistung max. der Anlage (wird an die Steuerbox gemeldet) | W |

## Bauen und Verteilen

Auf dem Entwicklungsrechner (Docker Desktop, Git Bash) aus dem Repo-Wurzelverzeichnis:

```sh
sh skripte/pfc-images-bauen.sh 0.3
scp dist/eebus-bruecke-0.3.tar.gz root@<pfc-ip>:/home/eebus-bruecke/
```

Das Skript baut Brücke und Test-Steuerbox für ARMv7 und legt beide als `.tar.gz` unter `dist/` ab. `scp` aus Git Bash, nicht aus PowerShell. Als `admin` statt `root` erst nach `/tmp` kopieren und auf dem PFC mit `sudo` verschieben.

Auf dem PFC200 (als root):

```sh
mkdir -p /home/eebus-bruecke
docker load -i /home/eebus-bruecke/eebus-bruecke-0.3.tar.gz

docker rm -f eebus-bruecke    # falls eine ältere Version läuft
docker run -d --name eebus-bruecke \
  --restart unless-stopped \
  --network host \
  --memory 64m \
  -v /home/eebus-bruecke:/data \
  -e EEBUS_REMOTE_SKI=<SKI der Steuerbox> \
  -e GERAET_SERIENNUMMER=<eindeutig je Anlage> \
  -e NENNLEISTUNG_MAX_W=22000 \
  -e FAILSAFE_GRENZE_W=4200 \
  -e FAILSAFE_MINDESTDAUER=2h \
  -e WEB_PASSWORT='<Passwort für das Status-UI>' \
  eebus-bruecke:0.3
```

Platzhalter in spitzen Klammern samt Klammern ersetzen, die Shell liest `<` sonst als Umleitung. Das Passwort in einfache Anführungszeichen setzen.

`/home/eebus-bruecke` liegt im internen Speicher. Mit SD-Karte stattdessen `/media/sd/eebus-bruecke` verwenden. In beiden Fällen `zertifikat.pem` und `schluessel.pem` auf dem PC sichern.

`--network host` ist nötig, weil mDNS (Multicast) über das Docker-Bridge-Netz nicht zuverlässig funktioniert. Der Modbus-Server bindet trotzdem nur auf 127.0.0.1, ist also aus dem LAN nicht erreichbar. Zum Testen mit einem Modbus-Master auf dem PC `-e MODBUS_URL=tcp://0.0.0.0:5502` setzen. Dann ist der Port ohne Schutz im ganzen LAN offen, danach wieder entfernen.

Weitere Variablen: `EEBUS_PORT` (4712), `MODBUS_URL` (`tcp://127.0.0.1:5502`), `DATENVERZEICHNIS` (`/data`), `GERAET_HERSTELLER`, `GERAET_MARKE`, `GERAET_MODELL`.

## Status-UI der Brücke

`http://<pfc-ip>:8090`, Anmeldung mit Benutzer `admin` und dem Passwort aus `WEB_PASSWORT`. Die Seite zeigt nur an, ändern lässt sich darüber nichts:

- Ampel mit LPC-Zustand und wirksamer Grenze
- Grenze des Netzbetreibers, Restlaufzeit, Failsafe-Werte
- EEBUS-Verbindung, Alter des Heartbeats, eigener SKI zum Kopieren
- per mDNS gefundene Geräte mit SKI, als Hilfe beim Pairing
- SPS-Lebenszeichen, gemeldete Nennleistung
- die letzten 100 Log-Meldungen

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `WEB_PASSWORT` | – | **Ohne Passwort startet das UI nicht**, die Brücke selbst läuft normal weiter. |
| `WEB_BENUTZER` | `admin` | Benutzername |
| `WEB_ADRESSE` | `:8090` | Adresse und Port, leer (`WEB_ADRESSE=`) schaltet das UI ab |

Port 8090 statt 8080, weil auf dem PFC die CODESYS-WebVisu oft 8080 belegt. Bei aktiver PFC-Firewall Port 8090 freigeben.

Die Anmeldung läuft über HTTP Basic Auth ohne HTTPS, das Passwort ist im LAN also mitlesbar. Sie schützt vor zufälligem Zugriff, nicht vor gezielten Angriffen.

## Pairing mit der Steuerbox

1. Container ohne `EEBUS_REMOTE_SKI` starten. Status-UI bzw. `docker logs eebus-bruecke` zeigen den eigenen SKI und alle per mDNS gefundenen EEBUS-Geräte mit deren SKI.
2. Eigenen SKI an den Messstellenbetreiber bzw. Installateur der Steuerbox geben, SKI der Steuerbox notieren.
3. Container mit `EEBUS_REMOTE_SKI` neu anlegen. Das Zertifikat liegt im Volume und bleibt erhalten, der SKI ändert sich also nicht.

Das Volume `/data` gehört gesichert: Geht es verloren, entsteht ein neues Zertifikat mit neuem SKI und das Pairing muss wiederholt werden.

## CODESYS-Seite

Die Quellen liegen als Text unter `codesys/` und werden ins CODESYS-Projekt übernommen.

Gerätebaum:

1. *Ethernet-Adapter → Ethernet* anhängen, Schnittstelle mit der IP des PFC wählen (X1 meist `br0`). Für die Verbindung zu `127.0.0.1` ist die Wahl egal, CODESYS verlangt aber einen Adapter.
2. Darunter *ModbusTCP Master*, **Auto-Reconnect aktivieren**. Sonst gibt CODESYS nach einem Neustart der Brücke auf, und die Brücke zeigt „SPS ausgefallen“.
3. Darunter *ModbusTCP Slave* mit IP `127.0.0.1`, Port `5502`, Unit-ID beliebig.
4. Kanäle wie in `codesys/GvlEebus.st`: FC04 Offset 0 Länge 14 (200 ms) und FC16 Offset 0 Länge 3 (1 s).
5. Im E/A-Abbild die Kanäle als Ganzes auf `GvlEebus.aInputRegister` bzw. `GvlEebus.aHoldingRegister` legen, Buszyklus-Task = Task von `PrgEnergiemanagement`.

Falls der Gerätebaum Localhost als Ziel nicht akzeptiert, alternativ `FbMbMasterTcp` aus WagoAppPlcModbus mit `sHost := '127.0.0.1'` verwenden. `FbEebusLpc` bleibt dabei unverändert, da er nur die Register-Arrays sieht.

## Test-Steuerbox

`testwerkzeuge/steuerbox` spielt die Steuerbox des Messstellenbetreibers (EEBUS Energy Guard). Sie wird über ein Web-UI bedient (Port 8091):

- **Senden:** Grenze mit Leistung und Dauer, Grenze aufheben, Vorlagen für typische Werte. Failsafe-Grenze und Failsafe-Mindestdauer (2–24 h).
- **Störungen simulieren:**
  - Heartbeat stoppen: Die Brücke geht 120 s nach dem letzten Heartbeat in Failsafe. Zurück geht es nur mit Heartbeat *und* einer neuen Grenze.
  - Verbindung kurz unterbrechen: Die Verbindung wird automatisch neu aufgebaut.
  - Trennen: Die Verbindung bleibt getrennt, bis sie wiederhergestellt wird.
- **Werte bei der Brücke:** Grenze, Failsafe-Werte und die Nennleistung, so wie die Brücke sie über EEBUS meldet. Die Nennleistung kommt von der SPS, das prüft also die ganze Kette.
- **Kopplung:** eigener SKI zum Kopieren, per mDNS gefundene Geräte, Koppeln per Klick. Die Kopplung wird im Volume gespeichert.
- **Ereignisse:** Antworten der Brücke (angenommen/abgelehnt), Verbindungswechsel, Pairing.

Die Steuerbox sendet ihren Heartbeat alle 8 s (`HEARTBEAT_TIMEOUT`, Vorgabe 10 s, minus 2 s), damit die Brücke nach dem Verbinden schnell aus „Init“ kommt. Weitere Variablen: `EEBUS_PORT` (4713), `WEB_ADRESSE` (`:8091`), `WEB_BENUTZER` (`admin`), `WEB_PASSWORT` (Pflicht), `DATENVERZEICHNIS` (`/data`).

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

Einmalig koppeln:

1. Im Steuerbox-UI unter „Kopplung“ den eigenen SKI kopieren.
2. Im Repo-Wurzelverzeichnis eine Datei `.env` anlegen mit `STEUERBOX_SKI=<SKI>` und `docker compose up -d` wiederholen. Damit vertraut die Brücke der Steuerbox.
3. Im Steuerbox-UI bei der gefundenen Brücke auf „Koppeln“ klicken.

Zertifikate und Kopplung liegen in Docker-Volumes und überstehen Neustarts. SPS-Ausfall testen: `docker compose stop spssimulator`. Alles entfernen inklusive Volumes: `docker compose down -v`.

### Auf dem PFC

Für Tests mit der echten CODESYS-Applikation läuft die Test-Steuerbox als zweiter Container auf demselben PFC. Das Image baut `skripte/pfc-images-bauen.sh` mit.

```sh
mkdir -p /home/eebus-steuerbox
docker load -i /home/eebus-steuerbox/eebus-steuerbox-0.3.tar.gz
docker run -d --name eebus-steuerbox \
  --network host \
  --memory 64m \
  -v /home/eebus-steuerbox:/data \
  -e WEB_PASSWORT='<Passwort>' \
  eebus-steuerbox:0.3
```

Danach `http://<pfc-ip>:8091` öffnen, den SKI der Steuerbox als `EEBUS_REMOTE_SKI` bei der Brücke eintragen (Container neu anlegen) und im Steuerbox-UI die Brücke koppeln. Nach dem Test `docker rm -f eebus-steuerbox` und die Brücke wieder mit dem SKI der echten Steuerbox anlegen.

Das ist auf dem PFC noch nicht ausprobiert. Lokal laufen beide Container im selben Netz problemlos. Auf dem PFC teilen sich beide den Host. Falls sie sich per mDNS nicht finden, gibt das Log der Steuerbox Auskunft.

## Offene Punkte vor dem Produktiveinsatz

- **eebus-go-Version:** Kompiliert und lokal mit der Test-Steuerbox getestet: Pairing, Grenze, Ablauf, Failsafe-Werte, Heartbeat-Ausfall, Verbindungsabbrüche, Nennleistung. Noch nicht gegen eine echte Steuerbox.
- **Fehler in eebus-go v0.7.0:** Nach einer Trennung bleibt beim Energy Guard die alte Entität in `RemoteEntitiesScenarios()`, Schreibzugriffe gehen danach ins Leere. Die Test-Steuerbox umgeht das (siehe `ziel()` in `testwerkzeuge/steuerbox/steuerbox.go`). Die Brücke ist als Controllable System nicht betroffen. Bei einem Update von eebus-go prüfen, ob der Fehler behoben ist.
- **Verbindungsstatus:** Beim allerersten Test mit dem eebus-go-Beispiel kam nach dem Stoppen der Gegenseite kein `RemoteSKIDisconnected`. In allen späteren Tests (Container-Stopp, kurze Unterbrechung, Trennen) wurde die Trennung sofort gemeldet. Bei der echten Steuerbox beobachten. Für die Grenze ist das unkritisch, dort entscheidet der Heartbeat.
- **Zustandsautomat:** Die Übergänge in `bruecke/bruecke.go` (insbesondere Init und Verlassen von Failsafe) gegen die aktuelle Spezifikation "EEBUS UC Limitation of Power Consumption" und das FNN-Lastenheft Steuerbox prüfen.
- **Zertifizierung:** Diese Brücke ist nicht EEBUS-zertifiziert. Für Pilot- und Eigenanlagen ausreichend, für Serienanlagen vorher mit Netzbetreiber bzw. MSB klären.
- **Docker auf dem PFC200:** Nur ab neueren Firmware-Ständen verfügbar, bei gemischtem Gerätepark vorab je Steuerung prüfen. Docker-Datenverzeichnis wegen begrenztem internem Speicher möglichst auf die SD-Karte legen.
- **mDNS:** Läuft auf dem PFC bereits ein Avahi-Dienst, auf Port-Konflikte an 5353 achten.
- **Erweiterung:** MPC (Messwerte an die Steuerbox) und LPP (Einspeisebegrenzung) lassen sich nach gleichem Muster ergänzen. Dafür ist die Schnittstellenversion zu erhöhen.

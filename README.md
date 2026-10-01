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
| `main.go`, `bruecke.go`, `modbus.go` | Go-Dienst: EEBUS-Anbindung, LPC-Zustandsautomat, Modbus-Server |
| `Dockerfile` | Image für PFC200 (ARMv7) oder PC (amd64) |
| `Codesys/` | CODESYS-Quellen: `FbEebusLpc`, GVL, Enums, Beispielprogramm |
| `test/controlbox/` | Test-Steuerbox aus eebus-go als Docker-Image |
| `test/spssimulator/` | Modbus-Client, der die SPS-Seite spielt |
| `dist/` | gebaute Images zum Verteilen (nicht im Repo) |

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

Auf dem Entwicklungsrechner (Docker mit buildx):

Abhängigkeiten sind in `go.mod`/`go.sum` gepinnt (eebus-go v0.7.0, modbus v1.6.4).

```sh
docker buildx build --platform linux/arm/v7 -t eebus-bruecke:0.1 --load .
mkdir -p dist && docker save eebus-bruecke:0.1 | gzip > dist/eebus-bruecke-0.1.tar.gz
scp dist/eebus-bruecke-0.1.tar.gz root@<pfc-ip>:/home/eebus-bruecke/
```

`scp` aus Git Bash, nicht aus PowerShell. Als `admin` statt `root` erst nach `/tmp` kopieren und auf dem PFC mit `sudo` verschieben.

Auf dem PFC200 (als root):

```sh
mkdir -p /home/eebus-bruecke
docker load -i /home/eebus-bruecke/eebus-bruecke-0.1.tar.gz

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
  eebus-bruecke:0.1
```

`/home/eebus-bruecke` liegt im internen Speicher. Mit SD-Karte stattdessen `/media/sd/eebus-bruecke` verwenden. In beiden Fällen `zertifikat.pem` und `schluessel.pem` auf dem PC sichern.

`--network host` ist nötig, weil mDNS (Multicast) über das Docker-Bridge-Netz nicht zuverlässig funktioniert. Der Modbus-Server bindet trotzdem nur auf 127.0.0.1, ist also aus dem LAN nicht erreichbar. Zum Testen mit einem Modbus-Master auf dem PC `-e MODBUS_URL=tcp://0.0.0.0:5502` setzen. Dann ist der Port ohne Schutz im ganzen LAN offen, danach wieder entfernen.

Weitere Variablen: `EEBUS_PORT` (4712), `MODBUS_URL` (`tcp://127.0.0.1:5502`), `DATENVERZEICHNIS` (`/data`), `GERAET_HERSTELLER`, `GERAET_MARKE`, `GERAET_MODELL`.

## Pairing mit der Steuerbox

1. Container ohne `EEBUS_REMOTE_SKI` starten. `docker logs eebus-bruecke` zeigt den eigenen SKI und alle per mDNS gefundenen EEBUS-Geräte mit deren SKI.
2. Eigenen SKI an den Messstellenbetreiber bzw. Installateur der Steuerbox geben, SKI der Steuerbox notieren.
3. Container mit `EEBUS_REMOTE_SKI` neu anlegen. Das Zertifikat liegt im Volume und bleibt erhalten, der SKI ändert sich also nicht.

Das Volume `/data` gehört gesichert: Geht es verloren, entsteht ein neues Zertifikat mit neuem SKI und das Pairing muss wiederholt werden.

## CODESYS-Seite

Die Quellen liegen als Text unter `Codesys/` und werden ins CODESYS-Projekt übernommen.

Gerätebaum:

1. *Ethernet-Adapter → Ethernet* anhängen, Schnittstelle mit der IP des PFC wählen (X1 meist `br0`). Für die Verbindung zu `127.0.0.1` ist die Wahl egal, CODESYS verlangt aber einen Adapter.
2. Darunter *ModbusTCP Master*, **Auto-Reconnect aktivieren**, da die Brücke nach einem Neustart oft später bereit ist als die SPS.
3. Darunter *ModbusTCP Slave* mit IP `127.0.0.1`, Port `5502`, Unit-ID beliebig.
4. Kanäle wie in `Codesys/GvlEebus.st`: FC04 Offset 0 Länge 14 (200 ms) und FC16 Offset 0 Länge 3 (1 s).
5. Im E/A-Abbild die Kanäle als Ganzes auf `GvlEebus.aInputRegister` bzw. `GvlEebus.aHoldingRegister` legen, Buszyklus-Task = Task von `PrgEnergiemanagement`.

Falls der Gerätebaum Localhost als Ziel nicht akzeptiert, alternativ `FbMbMasterTcp` aus WagoAppPlcModbus mit `sHost := '127.0.0.1'` verwenden. `FbEebusLpc` bleibt dabei unverändert, da er nur die Register-Arrays sieht.

## Testen ohne echte Steuerbox

eebus-go enthält unter `cmd/controlbox` ein Beispiel, das die Gegenseite (Energy Guard) spielt: 5 s nach dem Verbinden sendet es eine Grenze von 7000 W für 2 Minuten, Heartbeat alle ~58 s. `test/controlbox/Dockerfile` baut es als Image, `test/spssimulator` spielt die CODESYS-Seite.

Komplett auf dem PC (Docker Desktop, Git Bash), ohne PFC und ohne Go-Installation:

```sh
# Images bauen (Brücke fuer amd64, ohne --platform)
docker build -t eebus-bruecke:dev .
docker build -t eebus-controlbox:dev test/controlbox

# Einmalig: Zertifikat der Test-Steuerbox erzeugen und ihren SKI ermitteln
docker run --rm eebus-controlbox:dev 4713 > cb.txt
awk '/BEGIN CERT/,/END CERT/' cb.txt > test/controlbox/zertifikat.pem
awk '/BEGIN EC/,/END EC/' cb.txt > test/controlbox/schluessel.pem && rm cb.txt
openssl x509 -in test/controlbox/zertifikat.pem -noout -ext subjectKeyIdentifier
#   -> Doppelpunkte entfernen, klein schreiben = SKI der Steuerbox

# Brücke starten, eigener SKI steht im Log ("Eigener SKI: ...")
docker network create eebus-test
MSYS_NO_PATHCONV=1 docker run -d --name bruecke --network eebus-test \
  -v "$PWD/test/data:/data" -e EEBUS_REMOTE_SKI=<SKI Steuerbox> eebus-bruecke:dev
docker logs bruecke

# Steuerbox starten
MSYS_NO_PATHCONV=1 docker run -d --name steuerbox --network eebus-test \
  -v "$PWD/test/controlbox:/cert:ro" eebus-controlbox:dev \
  4713 <SKI Brücke> /cert/zertifikat.pem /cert/schluessel.pem

# SPS simulieren: Register live ansehen (Strg+C beendet)
MSYS_NO_PATHCONV=1 docker run --rm -it --network container:bruecke \
  -v "$PWD:/src" -w /src golang:1.23 go run ./test/spssimulator -nennleistung 22000
```

Erwarteter Ablauf in `docker logs -f bruecke`: `Steuerbox verbunden` → `Neue Grenze: 7000 W` → mit dem ersten Heartbeat `Init -> Begrenzt` → nach 2 min `Begrenzt -> Unbegrenzt/gesteuert`. `docker stop steuerbox` führt 120 s nach dem letzten Heartbeat zu `Failsafe`, `docker start steuerbox` wieder zurück nach `Begrenzt`.

Im Container-Netz funktioniert mDNS zwischen den Containern. Gegen eine echte Steuerbox im LAN braucht es dagegen `--network host`, was mit Docker Desktop unter Windows nicht zuverlässig geht, also dafür den PFC oder einen Linux-Rechner nehmen.

## Offene Punkte vor dem Produktiveinsatz

- **eebus-go-Version:** Kompiliert und lokal gegen `cmd/controlbox` aus eebus-go v0.7.0 getestet (Pairing, Grenze, Ablauf, Failsafe, Rückkehr). Noch nicht gegen eine echte Steuerbox.
- **Verbindungsstatus:** Beim Stoppen der Test-Steuerbox kam kein `RemoteSKIDisconnected`, Register 3 blieb auf „verbunden“. Prüfen, ob eebus-go die Trennung erst verzögert meldet oder gar nicht.
- **Zustandsautomat:** Die Übergänge in `bruecke.go` (insbesondere Init und Verlassen von Failsafe) gegen die aktuelle Spezifikation "EEBUS UC Limitation of Power Consumption" und das FNN-Lastenheft Steuerbox prüfen.
- **Zertifizierung:** Diese Brücke ist nicht EEBUS-zertifiziert. Für Pilot- und Eigenanlagen ausreichend, für Serienanlagen vorher mit Netzbetreiber bzw. MSB klären.
- **Docker auf dem PFC200:** Nur ab neueren Firmware-Ständen verfügbar, bei gemischtem Gerätepark vorab je Steuerung prüfen. Docker-Datenverzeichnis wegen begrenztem internem Speicher möglichst auf die SD-Karte legen.
- **mDNS:** Läuft auf dem PFC bereits ein Avahi-Dienst, auf Port-Konflikte an 5353 achten.
- **Erweiterung:** MPC (Messwerte an die Steuerbox) und LPP (Einspeisebegrenzung) lassen sich nach gleichem Muster ergänzen. Dafür ist die Schnittstellenversion zu erhöhen.

# EEBUS-Gateway

Verbindet die **Steuerbox des Messstellenbetreibers** per EEBUS mit einer **CODESYS-Steuerung**. Das Gateway läuft als Docker-Container, z. B. auf einem WAGO PFC200, und tritt gegenüber der Steuerbox als EEBUS „Controllable System“ auf. Grenzen und Messwerte tauscht es per Modbus TCP mit der CODESYS-Applikation aus.

Abgedeckt sind die Use Cases des VDE FNN Hinweises „Schnittstellen der Steuerungseinrichtung“:

| Use Case | Zweck | Laut FNN-Hinweis |
|---|---|---|
| **LPC** Limitation of Power Consumption | Bezug begrenzen (§14a EnWG) | Pflicht |
| **LPP** Limitation of Power Production | Einspeisung begrenzen (§9 EEG) | Pflicht bei Erzeugung |
| **MPC** Monitoring of Power Consumption | Messwerte der Anlage | aktuelle Wirkleistung muss abrufbar sein |
| **MGCP** Monitoring of Grid Connection Point | Messwerte am Netzanschlusspunkt | zusätzlich vorgesehen |

```
 Steuerbox                                    Steuerung (z. B. PFC200)
 am Smart Meter Gateway      ┌──────────────────────────────────────────────┐
          │  EEBUS           │  Docker (--network host)                     │
          │  (SHIP/SPINE,    │  ┌───────────────────────┐                   │
          └──mDNS, TCP 4712)─┼─►│ eebus-gateway         │◄── Status-UI :8090│
                             │  │ LPC, LPP, MPC, MGCP   │                   │
                             │  └──────────┬────────────┘                   │
                             │             │ Modbus TCP 127.0.0.1:5502      │
                             │  ┌──────────▼────────────┐                   │
                             │  │ CODESYS: FbEebus*     │◄─► Anlage         │
                             │  └───────────────────────┘    (Last, PV, …)  │
                             └──────────────────────────────────────────────┘
```

**Aufgabenteilung:** Das Gateway kapselt das Protokoll und die Zustandsautomaten (Init, Begrenzt, Failsafe, …). Die SPS sieht je Richtung nur „Begrenzung aktiv ja/nein“ und einen Wert in W und liefert die Messwerte. Fällt das Gateway aus, greift in der SPS eine lokale Ersatzgrenze.

**Wichtig für die Regelung:** Die Grenzen gelten laut FNN-Hinweis am **Netzanschlusspunkt**. Die CODESYS-Applikation muss sie am Zähler des Netzanschlusspunkts einhalten.

## Schnellstart

Voraussetzung: eine Steuerung mit Docker (PFC200: Firmware mit Docker-Unterstützung) und auf dem Entwicklungsrechner Docker. Go muss nicht installiert sein.

**1. Image bauen** (Repo-Wurzel, Git Bash):

```sh
sh skripte/pfc-images-bauen.sh 1.0
ssh root@<pfc-ip> mkdir -p /home/eebus-gateway
scp dist/eebus-gateway-1.0.tar.gz root@<pfc-ip>:/home/eebus-gateway/
```

**2. Auf der Steuerung starten** (als root):

```sh
docker load -i /home/eebus-gateway/eebus-gateway-1.0.tar.gz
docker run -d --name eebus-gateway \
  --restart unless-stopped --network host --memory 64m \
  -v /home/eebus-gateway:/data \
  -e GERAET_MARKE=<Firmenkürzel> \
  -e EEBUS_USECASES=lpc,mpc \
  -e NENNLEISTUNG_MAX_W=22000 \
  -e FAILSAFE_GRENZE_W=4200 \
  -e WEB_PASSWORT='<Passwort>' \
  eebus-gateway:latest
```

Mit Erzeugung (PV, Speicher) zusätzlich `lpp` sowie `NENNLEISTUNG_ERZEUGUNG_MAX_W` und `FAILSAFE_EINSPEISEGRENZE_W`. Hängt die Steuerbox in einem eigenen Netz an X2, zusätzlich `-e EEBUS_SCHNITTSTELLEN=br1` ([Netzwerk](docs/kopplung.md#getrenntes-netz-an-x2)). Wer Compose hat, nimmt stattdessen die Vorlage [docker-compose.yml](docker-compose.yml).

**3. Status-UI öffnen:** `http://<pfc-ip>:8090`, Benutzer `admin`. Ausdrücklich `http://` eintippen, eine HTTPS-Verbindung gibt es nicht.

**4. Mit der Steuerbox koppeln:** Im Reiter „Kopplung“ stehen die Daten für den Messstellenbetreiber (QR-Code bzw. SKI und SHIP-ID). Er trägt das Gateway in der Steuerbox ein. Beim Pairing Service ist danach nichts mehr zu tun. Beim SKI-Verfahren in der Geräteliste bei der Steuerbox „Vertrauen“ wählen. Steht dort „verbunden“, ist die Kopplung fertig.

**5. CODESYS anbinden:** Modbus-Master auf `127.0.0.1:5502`, Bausteine aus [codesys/](codesys/README.md).

Vor der Übergabe an den Messstellenbetreiber die SHIP-ID festschreiben, siehe [Kopplung](docs/kopplung.md#vor-der-übergabe-an-den-messstellenbetreiber).

## Weitere Doku

| Thema | Datei |
|---|---|
| Installation, Update, alle Variablen, Datenverzeichnis, Status-UI | [docs/betrieb.md](docs/betrieb.md) |
| Kopplung, SHIP-ID, Netzwerk, Fehlersuche, Steuerboxen (Swistec, PROLAN) | [docs/kopplung.md](docs/kopplung.md) |
| Modbus-Registerlayout und CODESYS-Bausteine | [codesys/README.md](codesys/README.md) |
| Test-Steuerbox, SPS-Simulator, lokale Testumgebung | [testwerkzeuge/README.md](testwerkzeuge/README.md) |
| Aufbau des Codes, eebus-go-Version, offene Punkte | [docs/entwicklung.md](docs/entwicklung.md) |

Im UI erklärt außerdem die Seite „Anleitung“ Aufbau, Zustände, Kopplung und Fehlersuche mit Schaubildern.

## Stand und Einschränkungen

- Lokal mit der Test-Steuerbox in allen Szenarien getestet. **Im Betrieb mit einer Swistec SwisBox** (LPC, seit 20.10.2026 auf einem PFC200), Erkenntnisse in [docs/kopplung.md](docs/kopplung.md#swistec-swisbox). Mit einer PROLAN-Steuerbox noch nicht getestet.
- Nutzt einen unveröffentlichten Entwicklungsstand von eebus-go/ship-go (für den SHIP Pairing Service), auf feste Commits gepinnt.
- Das Gateway ist **nicht EEBUS-zertifiziert**. Für Pilot- und Eigenanlagen gedacht, für Serienanlagen vorher mit Netzbetreiber bzw. Messstellenbetreiber klären.
- Die CODESYS-Bausteine der aktuellen Registerversion sind noch nicht in CODESYS kompiliert.
- Offene fachliche Punkte (Failsafe-Verhalten, Grenze nach Neustart) stehen in [docs/entwicklung.md](docs/entwicklung.md#offene-punkte-vor-dem-produktiveinsatz).

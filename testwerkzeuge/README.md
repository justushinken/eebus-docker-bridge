# Testwerkzeuge

Zum Ausprobieren und Testen des Gateways ohne echte Steuerbox und ohne CODESYS. Nicht für den Produktivbetrieb.

| Programm | Rolle |
|---|---|
| `steuerbox/` | Test-Steuerbox mit Web-UI (Port 8091): spielt die Steuerbox des Messstellenbetreibers |
| `spssimulator/` | Modbus-Client, der die CODESYS-Seite spielt: Last, PV, Messwerte |
| `docker-compose.yml` | Testumgebung: Gateway, Test-Steuerbox und SPS-Simulator zusammen |

## Lokal auf dem PC

Aus der Repo-Wurzel:

```sh
docker compose -f testwerkzeuge/docker-compose.yml up -d --build
```

| Dienst | Adresse | Anmeldung |
|---|---|---|
| Gateway | http://localhost:8090 | admin / test |
| Test-Steuerbox | http://localhost:8091 | admin / test |
| Modbus des Gateways | localhost:5502 | – |

Alle drei laufen im selben Docker-Netz, mDNS funktioniert dort zwischen den Containern. Die Compose-Datei schaltet alle vier Use Cases ein, der Simulator spielt 9 kW steuerbare Last und 8 kW PV.

Einmalig koppeln, am einfachsten per Pairing Service: Im Gateway-UI unter „Kopplung“ auf „QR-Text kopieren“ klicken, den Text im Steuerbox-UI unter „Kopplung“ einfügen und „Per Pairing Service koppeln“ wählen.

Alternativ per SKI wie bei PROLAN: Im Steuerbox-UI beim Gateway „Per SKI koppeln“, im Gateway-UI bei der Steuerbox „Vertrauen“. Oder fest per `.env` neben der Compose-Datei (`testwerkzeuge/.env` mit `STEUERBOX_SKI=<SKI>`).

Die SHIP-IDs sind in der Compose-Datei fest eingetragen, weil sich die MAC eines Containers beim Neuanlegen ändern kann. Zertifikate, Secret und Kopplung liegen in Docker-Volumes (Projekt `eebus-test`) und überstehen Neustarts.

```sh
docker compose -f testwerkzeuge/docker-compose.yml stop spssimulator   # SPS-Ausfall testen
docker compose -f testwerkzeuge/docker-compose.yml down -v             # alles entfernen inkl. Volumes
```

## Test-Steuerbox

Spielt die Steuerbox des Messstellenbetreibers (Energy Guard für LPC und LPP, Monitoring Appliance für MPC und MGCP). Mit `STEUERBOX_USECASES` (Vorgabe `lpc,lpp,mpc,mgcp`) lässt sich eine Steuerbox nachstellen, die nur einen Teil kann.

- **Bezug senden (LPC) und Einspeisung senden (LPP):** Grenze mit Leistung und Dauer, Grenze aufheben, Vorlagen. Failsafe-Grenze je Richtung und Failsafe-Mindestdauer (2–24 h, gemeinsam).
- **Beide Grenzen in einer Nachricht:** wie es andere EEBUS-Stacks tun können, das Gateway muss beide getrennt freigeben.
- **Messwerte:** MPC und MGCP so, wie das Gateway sie meldet, ungültige Werte gekennzeichnet.
- **Störungen simulieren:**
  - Heartbeat stoppen: Das Gateway geht 120 s nach dem letzten Heartbeat in Failsafe. Zurück geht es nur mit Heartbeat *und* einer neuen Grenze.
  - Verbindung kurz unterbrechen: Die Verbindung wird automatisch neu aufgebaut.
  - Trennen: Die Verbindung bleibt getrennt, bis sie wiederhergestellt wird.
- **Werte beim Gateway:** Grenzen, Failsafe-Werte, Nennleistungen, Anlagenstatus und angebotene Use Cases, so wie das Gateway sie über EEBUS meldet. Die Nennleistungen kommen von der SPS, das prüft also die ganze Kette.
- **Kopplung, wahlweise:** per SHIP Pairing Service (QR-Text des Gateways einfügen) oder per SKI („Per SKI koppeln“). Die Kopplung wird im Datenverzeichnis gespeichert.
- **Reiter:** „Steuern“ (Grenzen, Failsafe, Störungen, Ereignisse), „Gateway“ (Werte beim Gateway, Messwerte), „Kopplung“.
- **Anleitung:** eigene Seite mit Testaufbau, Kopplung und 17 Testszenarien mit erwartetem Ergebnis.

Die Steuerbox sendet ihren Heartbeat alle 8 s (`HEARTBEAT_TIMEOUT`, Vorgabe 10 s, minus 2 s), damit das Gateway nach dem Verbinden schnell aus „Init“ kommt. Weitere Variablen: `EEBUS_PORT` (4713), `SHIP_ID` (Vorgabe `Test-Steuerbox-<MAC>`), `WEB_ADRESSE` (`:8091`), `WEB_BENUTZER` (`admin`), `WEB_PASSWORT` (Pflicht), `DATENVERZEICHNIS` (`/data`).

Die Bedienung ohne Browser geht auch über die API, z. B. `POST :8091/api/grenze` mit `{"wertW":7000,"dauerS":120,"aktiv":true}` (Basic Auth, `Content-Type: application/json`).

## SPS-Simulator

Rechnet eine kleine Anlage durch: steuerbare Last (`-last`, folgt der Bezugsgrenze), PV (`-pv`, wird bei Einspeisegrenze abgeregelt) und Grundlast (`-grundlast`). Weitere Schalter: `-url` (Modbus-Server des Gateways), `-nennleistung`, `-nennleistung-erzeugung`, `-stoerung` (Anlagenstatus Störung), `-ungueltig-mpc`/`-ungueltig-mgcp` (Gültigkeitsbits löschen), `-zaehlerstart` (Zählerstände über 2³² testen).

Mit anderen Schaltern statt des Compose-Dienstes:

```sh
docker compose -f testwerkzeuge/docker-compose.yml stop spssimulator
docker compose -f testwerkzeuge/docker-compose.yml run -d --name simtest spssimulator -url tcp://gateway:5502 -stoerung -ungueltig-mpc 1
```

## Test-Steuerbox als Windows-Programm gegen ein Gateway im LAN

Docker Desktop kann das nicht: Die Container laufen in einer eigenen VM, mDNS kommt nicht ins LAN, `--network host` gibt es dort nicht. Stattdessen die Test-Steuerbox als `.exe` bauen und direkt starten (PowerShell, Repo-Wurzel):

```powershell
docker run --rm -v "${PWD}:/src" -v eebus-gomod:/go/pkg/mod -w /src -e GOOS=windows -e CGO_ENABLED=0 golang:1.24-alpine go build -o dist/steuerbox.exe ./testwerkzeuge/steuerbox
mkdir dist\steuerbox-daten -Force
$env:WEB_PASSWORT = "test"; $env:DATENVERZEICHNIS = "$PWD\dist\steuerbox-daten"
.\dist\steuerbox.exe
```

- **Netzwerkprofil des PCs auf „Privat“ stellen** und beim Firewall-Dialog den Zugriff erlauben. Im Profil „Öffentlich“ blockiert die Windows-Firewall eingehende mDNS-Antworten: Das Gateway findet die Steuerbox dann zwar (Pairing Service geht), die Liste „Per mDNS gefunden“ der Steuerbox bleibt aber leer. Im WLAN kann außerdem der Access Point Multicast zum Kabelnetz filtern, dann ein LAN-Kabel nehmen.
- Danach funktionieren alle Verfahren wie mit Compose. Den SKI des Gateways kann man in der Test-Steuerbox auch von Hand eintragen.

## Test-Steuerbox auf dem PFC

Für Tests mit der echten CODESYS-Applikation läuft die Test-Steuerbox als zweiter Container auf demselben PFC. Das Image baut `skripte/pfc-images-bauen.sh` mit.

```sh
mkdir -p /home/eebus-steuerbox
docker load -i /home/eebus-steuerbox/eebus-steuerbox-0.5.tar.gz
docker run -d --name eebus-steuerbox \
  --network host \
  --memory 64m \
  -v /home/eebus-steuerbox:/data \
  -e WEB_PASSWORT='<Passwort>' \
  eebus-steuerbox:latest
```

Danach `http://<pfc-ip>:8091` öffnen und das Gateway per Pairing Service koppeln (QR-Text aus dem Gateway-UI). Am Gateway ist dafür nichts einzustellen.

Nach dem Test die Test-Steuerbox entfernen und ihre Kopplung beim Gateway löschen, sonst vertraut das Gateway ihr weiter:

```sh
docker rm -f eebus-steuerbox
rm /home/eebus-gateway/steuerbox-pairing.json /home/eebus-gateway/failsafe.json
docker restart eebus-gateway
```

Zertifikat und Secret bleiben dabei erhalten, SKI, Fingerprint und QR-Code ändern sich also nicht. War das Gateway per SKI an die Test-Steuerbox gekoppelt, stattdessen im UI „Kopplung lösen“ bzw. `EEBUS_REMOTE_SKI` entfernen.

Gateway und Test-Steuerbox gleichzeitig auf dem PFC sind noch nicht ausprobiert. Lokal laufen beide im selben Netz problemlos. Falls sie sich per mDNS nicht finden, gibt das Log der Steuerbox Auskunft.

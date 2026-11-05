# Betrieb

Installation, Update, Konfiguration und Status-UI des EEBUS-Gateways. Der Schnellstart steht im [README](../README.md).

## Bauen und Verteilen

Die Versionsnummer gibt es nur beim Bauen: Sie ist das Argument des Skripts, steht im Image-Namen und wird als Software-Revision an die Steuerbox gemeldet (Fußzeile des UI). Lokal mit Compose gebaute Images heißen `dev`.

Auf dem Entwicklungsrechner (Docker Desktop) aus dem Repo-Wurzelverzeichnis, in **Git Bash**:

```sh
sh skripte/pfc-images-bauen.sh 1.0
ssh root@<pfc-ip> mkdir -p /home/eebus-gateway
scp dist/eebus-gateway-1.0.tar.gz root@<pfc-ip>:/home/eebus-gateway/
```

In PowerShell gibt es kein `sh`, dort das von Git mitgelieferte aufrufen:

```powershell
& "C:\Program Files\Git\bin\sh.exe" skripte/pfc-images-bauen.sh 1.0
```

Das Skript baut Gateway und Test-Steuerbox für ARMv7 und legt beide als `.tar.gz` unter `dist/` ab. `scp` aus Git Bash, nicht aus PowerShell. Als `admin` statt `root` erst nach `/tmp` kopieren und auf dem PFC mit `sudo` verschieben.

Auf dem PFC200 (als root):

```sh
docker load -i /home/eebus-gateway/eebus-gateway-1.0.tar.gz

docker rm -f eebus-gateway    # falls eine ältere Version läuft
docker run -d --name eebus-gateway \
  --restart unless-stopped \
  --network host \
  --memory 64m \
  -v /home/eebus-gateway:/data \
  -e GERAET_MARKE=<Firmenkürzel> \
  -e EEBUS_USECASES=lpc,mpc \
  -e NENNLEISTUNG_MAX_W=22000 \
  -e FAILSAFE_GRENZE_W=4200 \
  -e FAILSAFE_MINDESTDAUER=2h \
  -e WEB_PASSWORT='<Passwort für das Status-UI>' \
  eebus-gateway:latest
```

Mit Erzeugung (PV, Speicher) zusätzlich `lpp` und die beiden Pflichtwerte dafür, bei einem Zähler am Netzanschlusspunkt `mgcp`:

```sh
  -e EEBUS_USECASES=lpc,lpp,mpc,mgcp \
  -e NENNLEISTUNG_ERZEUGUNG_MAX_W=8000 \
  -e FAILSAFE_EINSPEISEGRENZE_W=4800 \
```

Platzhalter in spitzen Klammern samt Klammern ersetzen, die Shell liest `<` sonst als Umleitung. Das Passwort in einfache Anführungszeichen setzen. Nach dem ersten Start die angezeigte SHIP-ID zusätzlich mit `-e SHIP_ID=…` fest eintragen (siehe [Kopplung](kopplung.md#vor-der-übergabe-an-den-messstellenbetreiber)).

Auf anderen Linux-Rechnern mit Compose gilt dasselbe über die Vorlage [docker-compose.yml](../docker-compose.yml). Docker Desktop (Windows/Mac) eignet sich nicht für den Betrieb, weil `--network host` und damit mDNS dort nicht funktionieren.

**Update und Rollback:** Das Archiv enthält das Image unter zwei Namen, `eebus-gateway:1.0` und `eebus-gateway:latest`. `docker load` setzt `latest` also auf die zuletzt geladene Version. Ein laufender Container wechselt dadurch nicht von selbst: Für ein Update `docker rm -f eebus-gateway` und denselben `docker run` erneut ausführen. Welche Version läuft, zeigt die Fußzeile des UI. Zurück auf eine ältere Version: im `docker run` statt `latest` die Nummer angeben, z. B. `eebus-gateway:0.6` (muss geladen sein, `docker images eebus-gateway`).

**Umstieg von der „EEBUS-Brücke“** (Versionen bis 0.4, Container `eebus-bruecke`):

```sh
docker rm -f eebus-bruecke
mv /home/eebus-bruecke /home/eebus-gateway
# dann docker load und docker run wie oben
```

Zertifikat, SKI, Fingerprint, Secret und Kopplung bleiben erhalten. Die SHIP-ID ändert sich, wenn sie nicht per `SHIP_ID` festgeschrieben war (aus `…-PFC200-LPC-Bruecke-…` wird `…-EEBUS-Gateway-…`). Dann die alte SHIP-ID per `SHIP_ID` weiterverwenden oder neu koppeln.

## Datenverzeichnis

`/home/eebus-gateway` liegt im internen Speicher. Mit SD-Karte stattdessen `/media/sd/eebus-gateway` verwenden. Das Verzeichnis enthält alles, was die Kopplung ausmacht, und gehört gesichert:

| Datei | Inhalt |
|---|---|
| `zertifikat.pem`, `schluessel.pem` | Zertifikat, daraus SKI und Fingerprint |
| `pairing-secret.txt` | Secret für den SHIP Pairing Service |
| `steuerbox-pairing.json` | per Pairing Service gekoppelte Steuerbox |
| `steuerbox-ski.json` | im UI per SKI gekoppelte Steuerbox („Vertrauen“ oder Suchmodus) |
| `pairing-verlauf.json` | Schutz gegen wiederholte Ankündigungen |
| `failsafe.json` | zuletzt von der Steuerbox vorgegebene Failsafe-Werte |
| `einstellungen.json` | im UI geänderte Einstellungen (Vorrang vor der Env) |

`--network host` ist nötig, weil mDNS (Multicast) über das Docker-Bridge-Netz nicht zuverlässig funktioniert. Der Modbus-Server bindet trotzdem nur auf 127.0.0.1, ist also aus dem LAN nicht erreichbar. Zum Testen mit einem Modbus-Master auf dem PC `-e MODBUS_URL=tcp://0.0.0.0:5502` setzen. Dann ist der Port ohne Schutz im ganzen LAN offen, danach wieder entfernen.

## Variablen

### Use Cases und Messwerte

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `EEBUS_USECASES` | `lpc` | Angebotene Use Cases, kommagetrennt aus `lpc`, `lpp`, `mpc`, `mgcp`. `lpc` oder `lpp` ist Pflicht. Nur einschalten, was die Anlage liefern kann |
| `NENNLEISTUNG_MAX_W` | 11000 | Nennleistung Bezug, bis die SPS einen Wert schreibt |
| `NENNLEISTUNG_ERZEUGUNG_MAX_W` | – | Nennleistung Erzeugung, **Pflicht mit `lpp`** |
| `FAILSAFE_GRENZE_W` | 4200 | Failsafe-Grenze Bezug (Startwert) |
| `FAILSAFE_EINSPEISEGRENZE_W` | – | Failsafe-Grenze Einspeisung (Startwert), **Pflicht mit `lpp`** |
| `FAILSAFE_MINDESTDAUER` | `2h` | Failsafe-Mindestdauer, gilt für Bezug und Einspeisung gemeinsam |
| `MPC_ENTITAET` | `cem` | `cem`: MPC auf der Entität des Energiemanagers (wie LPC). `submeter`: eigene Entität „Unterzähler“, falls eine Steuerbox MPC auf CEM nicht liest |
| `MESSWERT_QUELLE` | `measuredValue` | Herkunft der Messwerte: `measuredValue`, `calculatedValue` oder `empiricalValue` |

Welche Werte gerade gültig sind, meldet die SPS über die Gültigkeitsmasken (Holding-Register 5 und 6, siehe [codesys/README.md](../codesys/README.md)).

**Failsafe-Werte:** Die `FAILSAFE_*`-Werte sind nur die Startwerte. Schreibt die Steuerbox eigene Werte, merkt sich das Gateway sie in `failsafe.json` und nutzt sie auch nach einem Neustart. Gerade dann, im Zustand „Init“, gelten sie.

**Freigabe von Grenzen:** Das Gateway nimmt eine Grenze nur an, wenn die SPS erreichbar ist (FNN-Hinweis 4.1.2.2: Übernahme *und* Umsetzung bestätigen). Sonst lehnt es mit „SPS nicht erreichbar“ ab und meldet der Steuerbox den Anlagenstatus „Störung“.

### Kopplung

`EEBUS_PAIRING_SERVICE`, `EEBUS_REMOTE_SKI` und `SHIP_ID`, siehe [Kopplung](kopplung.md#variablen).

### Web-UI

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `WEB_PASSWORT` | – | **Ohne Passwort startet das UI nicht**, das Gateway selbst läuft normal weiter. |
| `WEB_BENUTZER` | `admin` | Benutzername |
| `WEB_ADRESSE` | `:8090` | Adresse und Port, leer (`WEB_ADRESSE=`) schaltet das UI ab |
| `WEB_AENDERUNGEN` | `an` | `aus` macht das UI rein lesend: keine Kopplung, keine Einstellungen, kein Neustart (z. B. nach der Inbetriebnahme) |

Port 8090 statt 8080, weil auf dem PFC die CODESYS-WebVisu oft 8080 belegt. Bei aktiver PFC-Firewall Port 8090 freigeben.

### Weitere

`EEBUS_SCHNITTSTELLEN` (leer = alle): mDNS nur auf diesen Netzwerkschnittstellen, kommagetrennt, z. B. `br1`. Nötig, wenn die Steuerbox in einem eigenen Netz an X2 hängt, siehe [Kopplung](kopplung.md#getrenntes-netz-an-x2). Die SHIP-ID (MAC) hängt nicht davon ab.

`EEBUS_PORT` (4712), `MODBUS_URL` (`tcp://127.0.0.1:5502`), `DATENVERZEICHNIS` (`/data`), `GERAET_HERSTELLER`, `GERAET_MARKE` (`Demo`), `GERAET_MODELL` (`EEBUS-Gateway`), `GERAET_SERIENNUMMER` (Vorgabe: MAC-Adresse), `GERAET_HW_REVISION` (wird an die Steuerbox gemeldet). Die Software-Revision ist die Image-Version.

`EEBUS_DEBUG=an` schreibt das ausführliche Protokoll von eebus-go, ship-go und spine-go nach stdout (`docker logs eebus-gateway`), nicht ins Ereignisprotokoll. Für die Fehlersuche mit einer neuen Steuerbox, danach wieder ausschalten. Gescheiterte Verbindungsversuche wertet das Gateway auch ohne `EEBUS_DEBUG` aus (Reiter „Diagnose“, „Verbindungsversuche“). Die Zeiten in `docker logs` sind UTC, das UI zeigt Ortszeit.

## Status-UI

`http://<pfc-ip>:8090`, Anmeldung mit Benutzer `admin` und dem Passwort aus `WEB_PASSWORT`. Oben steht immer je Richtung eine Ampel (Bezug, bei eingeschaltetem LPP daneben Einspeisung), darunter der Verbindungsstatus von Steuerbox und SPS. Was beide Richtungen betrifft, etwa der Countdown bis Failsafe nach Verlust der Steuerbox, steht im Feld der Steuerbox. Darunter folgen Reiter, direkt aufrufbar, z. B. `…:8090/#kopplung`:

- **Übersicht** (Betrieb): je Richtung Zustand, wirksame Grenze, Grenze des Netzbetreibers, Restlaufzeit, Failsafe-Grenze, Nennleistung und letzter Ablehnungsgrund; Heartbeat, Failsafe-Mindestdauer; SPS-Lebenszeichen und Anlagenstatus; Messwerte MPC und MGCP.
- **Kopplung** (Inbetriebnahme): per mDNS gefundene Geräte mit „Vertrauen“, gekoppelte Steuerbox und „Kopplung lösen“, einklappbarer Suchmodus, Daten für den Messstellenbetreiber (SKI, SHIP-ID, Fingerprint, Secret, QR-Code).
- **Diagnose:** Use Cases von Gateway und Steuerbox, letzte Verbindungsversuche in beide Richtungen (TCP und TLS, mit Grund), von der Steuerbox gemeldete Daten (Gerät, Software, Entitäten, Use-Case-Versionen), die letzten 100 Log-Meldungen.
- **Konfiguration:** alle Einstellungen mit Variablenname und Herkunft, änderbar (siehe unten).
- **Anleitung:** eigene Seite mit Schaubildern zu Aufbau, Zuständen, Kopplung, Netzwerk, Steuerboxen und Fehlersuche.

Die Anmeldung läuft über HTTP Basic Auth ohne HTTPS, das Passwort ist im LAN also mitlesbar. Sie schützt vor zufälligem Zugriff, nicht vor gezielten Angriffen.

### Einstellungen im UI ändern

Use Cases, Nennleistungen, Failsafe-Startwerte, MPC-Entität, Messwert-Herkunft, Pairing Service, SHIP-ID, Gerätedaten und `EEBUS_DEBUG` lassen sich im Reiter „Konfiguration“ ändern:

1. Werte ändern und „Speichern“. Das Gateway prüft die ganze Konfiguration wie beim Start (z. B. braucht `lpp` beide Pflichtwerte) und schreibt die Änderungen nach `einstellungen.json`.
2. „Jetzt neu starten“: Das Gateway trennt die Steuerbox sauber und startet sich im laufenden Container neu (etwa 10 s). Docker, PFC und CODESYS laufen weiter.

**Vorrang:** im UI geändert > Env (`docker run -e`) > Vorgabe. Jede Zeile zeigt, woher ihr Wert kommt. „Zurücksetzen“ löscht die Änderung aus dem UI, dann gilt wieder die Env. `einstellungen.json` bleibt bei einem Update erhalten. Passen die gespeicherten Werte nicht mehr zur Env, startet das Gateway mit der Env allein und meldet das im Ereignisprotokoll.

Nur per Env änderbar: Ports und Adressen, `DATENVERZEICHNIS`, Web-Zugang (`WEB_*`, sonst sperrt man sich aus), `GERAET_SERIENNUMMER` und `EEBUS_REMOTE_SKI`.

**Achtung beim Neustart während einer Begrenzung:** Das Gateway vergisst die Grenze des Netzbetreibers. Bis zum ersten Heartbeat gilt die Failsafe-Grenze, danach ist der Bezug unbegrenzt, bis die Steuerbox erneut eine Grenze schreibt. Gleiches gilt nach einem Stromausfall. Ob eine echte Steuerbox die Grenze nach dem Wiederverbinden neu schreibt, ist offen (siehe [offene Punkte](entwicklung.md#offene-punkte-vor-dem-produktiveinsatz)).

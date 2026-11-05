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

## Mehrere Steuerboxen

Ein Gateway bedient genau eine Steuerbox: Bei LPC und LPP gehört zu einem Controllable System genau ein Energy Guard. Gibt es mehrere steuerbare Verbrauchseinrichtungen mit **eigenen Zählpunkten** und je einer Steuerbox, läuft auf dem PFC **ein Gateway-Container je Steuerbox**. Jede Instanz ist für ihre Steuerbox ein eigenes EEBUS-Gerät mit eigenem Zertifikat, SKI und eigener SHIP-ID. Die SPS spricht jede über einen eigenen Modbus-Port an.

Hängen alle Einrichtungen an einem Zählpunkt, ist eine Steuerbox über das EMS (die SPS) meist die bessere Lösung: Der Netzbetreiber gibt eine gemeinsame Grenze vor, die SPS verteilt sie. Das vorher mit Netzbetreiber bzw. Messstellenbetreiber klären.

### Was `GATEWAY_INSTANZ` einstellt

Die Instanznummer ist die einzige Einstellung, die sich zwischen den Instanzen unterscheiden muss. Aus ihr ergibt das Gateway selbst:

| | Instanz 1 (Vorgabe) | Instanz 2 | Instanz 3 | Instanz 4 |
|---|---|---|---|---|
| `GATEWAY_INSTANZ` | weglassen oder `1` | `2` | `3` | `4` |
| EEBUS-Port | 4712 | 4713 | 4714 | 4715 |
| Modbus (für CODESYS) | 127.0.0.1:5502 | 127.0.0.1:5503 | 127.0.0.1:5504 | 127.0.0.1:5505 |
| Web-UI | :8090 | :8092 | :8093 | :8094 |
| Seriennummer | MAC | MAC-2 | MAC-3 | MAC-4 |
| SHIP-ID (ohne `SHIP_ID`) | `<Marke>-EEBUS-Gateway-<MAC>` | `…-<MAC>-2` | `…-<MAC>-3` | `…-<MAC>-4` |

Erlaubt ist 1 bis 20. Ausdrücklich gesetzte Variablen (`EEBUS_PORT`, `MODBUS_URL`, `WEB_ADRESSE`, `GERAET_SERIENNUMMER`) haben Vorrang. Container-Name und Datenverzeichnis legt der `docker run` fest, also ebenfalls je Instanz eigene.

**Warum die Seriennummer zählt:** Alle Container auf dem PFC sehen dieselbe MAC. Aus Marke, Modell und Seriennummer bildet eebus-go den Namen in der mDNS-Ankündigung. Bei gleicher Seriennummer kündigen sich zwei Instanzen unter demselben Namen an, und die Steuerboxen verwechseln sie. Eine eigene `SHIP_ID` allein hilft dagegen nicht. `GATEWAY_INSTANZ` hängt die Nummer deshalb selbst an. Sieht ein Gateway trotzdem ein fremdes Gerät mit seiner eigenen Kennung (mDNS-Name oder SHIP-ID), zeigt die Statusseite oben einen roten Hinweis „Achtung: … kündigt sich unter derselben Kennung an“, und im Ereignisprotokoll steht eine WARNUNG.

### Schritt für Schritt auf dem PFC

Alle Befehle als `root` auf dem PFC. Für jede Instanz kommt der `docker run` in ein kleines Startskript im eigenen Datenverzeichnis. Damit liegt der Befehl bei der Sicherung dabei, und ein Update ist ein Aufruf je Instanz.

**1. Image laden** (einmal für alle Instanzen, Archiv wie unter [Bauen und Verteilen](#bauen-und-verteilen) kopiert):

```sh
docker load -i /home/eebus-gateway/eebus-gateway-1.0.tar.gz
docker images eebus-gateway
```

**2. Datenverzeichnisse anlegen:**

```sh
mkdir -p /home/eebus-gateway /home/eebus-gateway-2 /home/eebus-gateway-3 /home/eebus-gateway-4
```

**3. Startskripte schreiben.** Je Instanz einen Block kopieren und vorher anpassen: Firmenkürzel, Passwort und die Werte der jeweiligen Einrichtung (Use Cases, Nennleistung, Failsafe). Hängen die Steuerboxen nicht über einen Switch an X2, sondern im Netz von X1, die Zeile `EEBUS_SCHNITTSTELLEN` entfernen (siehe [Kopplung](kopplung.md#mehrere-steuerboxen)). Der Text zwischen `<<'EOF'` und `EOF` wird unverändert in die Datei geschrieben.

Instanz 1 (läuft schon ein Gateway, dessen bisherige Werte übernehmen, insbesondere `SHIP_ID` und `EEBUS_REMOTE_SKI`):

```sh
cat > /home/eebus-gateway/start.sh <<'EOF'
docker rm -f eebus-gateway 2>/dev/null
docker run -d --name eebus-gateway \
  --restart unless-stopped \
  --network host \
  --memory 64m \
  -v /home/eebus-gateway:/data \
  -e GATEWAY_INSTANZ=1 \
  -e GERAET_MARKE=<Firmenkürzel> \
  -e EEBUS_SCHNITTSTELLEN=br1 \
  -e EEBUS_USECASES=lpc \
  -e NENNLEISTUNG_MAX_W=11000 \
  -e FAILSAFE_GRENZE_W=4200 \
  -e FAILSAFE_MINDESTDAUER=2h \
  -e WEB_PASSWORT='<Passwort für das Status-UI>' \
  eebus-gateway:latest
EOF
```

Instanz 2:

```sh
cat > /home/eebus-gateway-2/start.sh <<'EOF'
docker rm -f eebus-gateway-2 2>/dev/null
docker run -d --name eebus-gateway-2 \
  --restart unless-stopped \
  --network host \
  --memory 64m \
  -v /home/eebus-gateway-2:/data \
  -e GATEWAY_INSTANZ=2 \
  -e GERAET_MARKE=<Firmenkürzel> \
  -e EEBUS_SCHNITTSTELLEN=br1 \
  -e EEBUS_USECASES=lpc \
  -e NENNLEISTUNG_MAX_W=11000 \
  -e FAILSAFE_GRENZE_W=4200 \
  -e FAILSAFE_MINDESTDAUER=2h \
  -e WEB_PASSWORT='<Passwort für das Status-UI>' \
  eebus-gateway:latest
EOF
```

Instanz 3:

```sh
cat > /home/eebus-gateway-3/start.sh <<'EOF'
docker rm -f eebus-gateway-3 2>/dev/null
docker run -d --name eebus-gateway-3 \
  --restart unless-stopped \
  --network host \
  --memory 64m \
  -v /home/eebus-gateway-3:/data \
  -e GATEWAY_INSTANZ=3 \
  -e GERAET_MARKE=<Firmenkürzel> \
  -e EEBUS_SCHNITTSTELLEN=br1 \
  -e EEBUS_USECASES=lpc \
  -e NENNLEISTUNG_MAX_W=11000 \
  -e FAILSAFE_GRENZE_W=4200 \
  -e FAILSAFE_MINDESTDAUER=2h \
  -e WEB_PASSWORT='<Passwort für das Status-UI>' \
  eebus-gateway:latest
EOF
```

Instanz 4:

```sh
cat > /home/eebus-gateway-4/start.sh <<'EOF'
docker rm -f eebus-gateway-4 2>/dev/null
docker run -d --name eebus-gateway-4 \
  --restart unless-stopped \
  --network host \
  --memory 64m \
  -v /home/eebus-gateway-4:/data \
  -e GATEWAY_INSTANZ=4 \
  -e GERAET_MARKE=<Firmenkürzel> \
  -e EEBUS_SCHNITTSTELLEN=br1 \
  -e EEBUS_USECASES=lpc \
  -e NENNLEISTUNG_MAX_W=11000 \
  -e FAILSAFE_GRENZE_W=4200 \
  -e FAILSAFE_MINDESTDAUER=2h \
  -e WEB_PASSWORT='<Passwort für das Status-UI>' \
  eebus-gateway:latest
EOF
```

Statt `cat > … <<'EOF'` geht auch ein Editor (`vi /home/eebus-gateway-2/start.sh`). Platzhalter in spitzen Klammern samt Klammern ersetzen.

**4. Alle Instanzen starten:**

```sh
sh /home/eebus-gateway/start.sh
sh /home/eebus-gateway-2/start.sh
sh /home/eebus-gateway-3/start.sh
sh /home/eebus-gateway-4/start.sh
```

**5. Prüfen:**

```sh
docker ps --filter name=eebus-gateway
docker logs eebus-gateway-2 2>&1 | head -20
```

`docker ps` zeigt alle vier als „Up“. Im Log von Instanz 2 bis 4 steht „Gateway-Instanz 2: EEBUS-Port 4713, Modbus tcp://127.0.0.1:5503, Web-UI :8092, Seriennummer …-2“. Bricht ein Container gleich wieder ab, zeigt `docker logs` den Grund, meist ein belegter Port (gleiche `GATEWAY_INSTANZ` zweimal) oder ein Tippfehler in einer Variable. Die Status-UIs: `http://<pfc-ip>:8090`, `:8092`, `:8093`, `:8094`. Die Seriennummer steht oben rechts, bei Instanz 2 bis 4 mit „· Instanz n“, der Browser-Tab heißt „EEBUS-Gateway n“. Oben darf kein roter Hinweis „Achtung: …“ stehen.

**6. Koppeln:** Je Instanz im Reiter „Kopplung“ die Daten für den Messstellenbetreiber ablesen (SKI und SHIP-ID bzw. QR-Code) und genau der Steuerbox dieser Einrichtung zuordnen. Danach wie bei einer Instanz koppeln ([Kopplung](kopplung.md)).

**7. Festschreiben:** Nach der Kopplung in jedem Startskript `-e SHIP_ID=<SHIP-ID aus dem UI>` und `-e EEBUS_REMOTE_SKI=<SKI der Steuerbox>` ergänzen (z. B. mit `vi`) und das Skript erneut ausführen. Kopplung und Zertifikat bleiben dabei erhalten.

**Update** (neues Image, alle Instanzen):

```sh
docker load -i /home/eebus-gateway/eebus-gateway-1.1.tar.gz
for s in /home/eebus-gateway*/start.sh; do sh "$s"; done
docker ps --filter name=eebus-gateway
```

**Eine Instanz entfernen:**

```sh
docker rm -f eebus-gateway-4
# Datenverzeichnis erst löschen, wenn die Steuerbox nicht mehr gebraucht wird (Zertifikat und Kopplung gehen verloren):
# rm -r /home/eebus-gateway-4
```

**Sichern:** alle Datenverzeichnisse samt Startskripten, z. B. auf den PC (Git Bash): `scp -r root@<pfc-ip>:/home/eebus-gateway* .`

### Hinweise

- **Speicher:** Ein Gateway braucht gut 10–15 MB, vier Instanzen mit je `--memory 64m` passen auf den PFC200. Kontrolle: `docker stats --no-stream`.
- **Firewall:** Ist die PFC-Firewall aktiv, zusätzlich zu 4712 und 8090 die Ports 4713–4715 und 8092–8094 (TCP) freigeben. UDP 5353 für mDNS teilen sich alle Instanzen.
- **Netz:** Alle Steuerboxen im selben Netz, Einzelheiten unter [Kopplung](kopplung.md#mehrere-steuerboxen).
- **CODESYS:** je Instanz ein eigener Modbus-Slave auf Port 5502–5505, siehe [codesys/README.md](../codesys/README.md#mehrere-gateways).
- **Compose:** Auf dem PFC gibt es kein Docker Compose, deshalb die Startskripte. Auf anderen Linux-Rechnern geht dasselbe mit der Vorlage [docker-compose.yml](../docker-compose.yml) (auskommentierte zweite Instanz).
- **Ausprobieren** auf dem PC mit zwei Paaren aus Gateway und Test-Steuerbox: [testwerkzeuge/README.md](../testwerkzeuge/README.md#zwei-steuerboxen).

**Zuordnung festhalten**, für den Messstellenbetreiber und für die SPS:

| Instanz | Einrichtung | Zählpunkt | SHIP-ID des Gateways | SKI des Gateways | Steuerbox (SKI) | Modbus-Port | Web-UI |
|---|---|---|---|---|---|---|---|
| 1 | z. B. Wärmepumpe | … | … | … | … | 5502 | 8090 |
| 2 | | | | | | 5503 | 8092 |
| 3 | | | | | | 5504 | 8093 |
| 4 | | | | | | 5505 | 8094 |

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

`GATEWAY_INSTANZ` (1): Nummer bei mehreren Gateways auf einer Steuerung, setzt Ports und Seriennummer (siehe [Mehrere Steuerboxen](#mehrere-steuerboxen)). `EEBUS_PORT` (4712), `MODBUS_URL` (`tcp://127.0.0.1:5502`), `DATENVERZEICHNIS` (`/data`), `GERAET_HERSTELLER`, `GERAET_MARKE` (`Demo`), `GERAET_MODELL` (`EEBUS-Gateway`), `GERAET_SERIENNUMMER` (Vorgabe: MAC-Adresse), `GERAET_HW_REVISION` (wird an die Steuerbox gemeldet). Die Software-Revision ist die Image-Version.

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

Nur per Env änderbar: `GATEWAY_INSTANZ`, Ports und Adressen, `DATENVERZEICHNIS`, Web-Zugang (`WEB_*`, sonst sperrt man sich aus), `GERAET_SERIENNUMMER` und `EEBUS_REMOTE_SKI`.

**Achtung beim Neustart während einer Begrenzung:** Das Gateway vergisst die Grenze des Netzbetreibers. Bis zum ersten Heartbeat gilt die Failsafe-Grenze, danach ist der Bezug unbegrenzt, bis die Steuerbox erneut eine Grenze schreibt. Gleiches gilt nach einem Stromausfall. Ob eine echte Steuerbox die Grenze nach dem Wiederverbinden neu schreibt, ist offen (siehe [offene Punkte](entwicklung.md#offene-punkte-vor-dem-produktiveinsatz)).

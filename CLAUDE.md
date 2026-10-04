# Projektkontext: EEBUS-Brücke für WAGO PFC200

Docker-Container auf dem WAGO PFC200: tritt als EEBUS „Controllable System“ gegenüber der Steuerbox des Messstellenbetreibers auf und bildet die Use Cases des VDE FNN Hinweises „Schnittstellen der Steuerungseinrichtung“ ab: LPC (Bezug begrenzen, §14a EnWG), LPP (Einspeisung begrenzen, §9 EEG), MPC (Messwerte Anlage), MGCP (Messwerte Netzanschlusspunkt). Austausch mit der CODESYS-Applikation per Modbus TCP (127.0.0.1:5502). Details für Anwender stehen im README. Diese Datei ist für die Weiterarbeit.

## Stand (Oktober 2026)

- **Branch `dev`** (früher `ship-pairing`, noch nicht nach `main` übernommen):
  - SHIP Pairing Service, Anleitungsseiten, gespeicherte Failsafe-Werte, SHIP-ID aus der MAC.
  - **Neu:** alle vier FNN-Use-Cases (per `EEBUS_USECASES` wählbar, Vorgabe `lpc`), Suchmodus und Kopplung von der Brücke aus, Diagnose der Gegenstelle, Anlagenstatus und Revisionen, Freigabe nur bei erreichbarer SPS, Modbus-Erweiterung 2, neue CODESYS-Bausteine.
  - `main` = Stand 0.3 (nur SKI-Verfahren, eebus-go v0.7.0).
- **Lokal mit der Test-Steuerbox getestet** (alle Szenarien der Steuerbox-Anleitung, Stand 03.10.2026), **noch nicht gegen eine echte Steuerbox, nicht auf dem PFC**.
- **CODESYS-Bausteine** der Erweiterung 2 sind ohne CODESYS geschrieben, nicht kompiliert.
- **Auf dem PFC läuft 0.2** (nur Status-UI), gestartet mit `GERAET_SERIENNUMMER=0001`. Beim Update diese Zeile weglassen, dann kommt die SHIP-ID aus der MAC. Images 0.4 unter `dist/` sind veraltet, als nächstes 0.5 bauen.
- **Erste echte Steuerbox: PROLAN STB-142E** (EEBUS über Buchse ETH1, Kopplung richtet der Messstellenbetreiber aus der Ferne ein). Recherche und Fragen an den MSB stehen im README, Abschnitt „PROLAN-Steuerbox“.

## Nächste Schritte / offen

1. Fragen an den Messstellenbetreiber klären (README, „PROLAN-Steuerbox“): FW ≥ 1.1.3, aktive Use Cases, Kopplungsverfahren, IP-Vergabe an ETH1, Kaskadierung aus, Failsafe-Werte, Technisches Handbuch.
2. 0.5 bauen und auf dem PFC testen, auch Brücke + Test-Steuerbox gleichzeitig. CODESYS-Bausteine übernehmen und kompilieren.
3. Netzwerk: Direktkabel ETH1 → X2 vorab mit Laptop und Test-Steuerbox an X2 testen (inkl. IPv6-Link-Local ohne DHCP).
4. Beim ersten Kontakt mit der PROLAN-Box: Karte „Steuerbox: gemeldete Daten“ auswerten (Use-Case-Versionen, Entitäten), ggf. `EEBUS_DEBUG=an`. Prüfen, ob sie MPC auf der CEM-Entität liest, sonst `MPC_ENTITAET=submeter`.
5. Vor Übergabe: `GERAET_MARKE` auf Firmenkürzel, SHIP-ID mit `SHIP_ID=…` festschreiben (README, „Vor der Übergabe“).
6. Zustandsautomat (`bruecke/begrenzung.go`, `takt`) gegen die Spezifikationen LPC/LPP und FNN-Lastenheft 1.4 prüfen. Update-Rate der Messwerte klären (sendet bei jeder Änderung).
7. Fehler upstream melden: spine-go `ApproveOrDenyWrite` (third_party/spine-go/PATCH.md, Issue-Text vorbereitet 04.10.2026), eebus-go `gcp/mgcp` Akteur, `cs/lpc`/`cs/lpp` `Set*NominalMax` mit festen IDs 0/0.
8. `dev` nach `main` übernehmen, wenn der PFC-Test passt.

## Umgebung und Werkzeuge

- **PFC200:** IP 192.168.111.74 (X1), SSH als `root`, Datenverzeichnis `/home/eebus-bruecke` (keine SD-Karte), Hostname `PFC200V3-683ADC`. Docker läuft. CODESYS: ModbusTCP Master mit Auto-Reconnect (war anfangs aus, Ursache für „SPS ausgefallen“).
- **Entwicklungsrechner:** Windows, **kein Go installiert**, alles läuft in Docker Desktop (20.10, Compose v2.10). Bash-Befehle in Git Bash. Bei `docker run -v` mit Windows-Pfaden `MSYS_NO_PATHCONV=1` voranstellen.
- **Laufwerk C: ist knapp** (war einmal ganz voll, Docker lief dann nur noch lesend, zuletzt 1,1 GB frei). Docker-Festplatte liegt auf C:. Vor großen Builds `docker system df` prüfen, alter Build-Cache: `docker builder prune -f --filter until=168h`. Für Go-Befehle `golang:1.24-alpine` statt des großen Images nehmen, Modul-Cache im Volume `eebus-gomod`.
- Downloads beim Docker-Build brechen gelegentlich ab (`tls: bad record MAC`, TLS-Timeouts): vorübergehend, Build wiederholen. Zuerst nur `docker compose build bruecke`, die anderen nutzen dann die Modul-Schicht mit.
- Im LAN läuft eine fremde „EnbilityNet Devices-App“ (SKI c1fded…), die per mDNS auftaucht und sich verbinden will. Callbacks müssen deshalb prüfen, ob ein Ereignis den eigenen Partner betrifft (`gemeinsam.GleicheIdentitaet`).
- Python unter Windows schreibt im Textmodus CRLF: beim Bearbeiten per Skript `newline=''` oder Binärmodus verwenden.
- Quellen der gepinnten Module zum Nachlesen: `docker run … golang:1.24-alpine sh -c 'cp -r /go/pkg/mod/github.com/enbility /out/'` in ein Scratch-Verzeichnis.

## Häufige Befehle

```sh
# Prüfen und bauen (ohne lokales Go)
MSYS_NO_PATHCONV=1 docker run --rm -v "D:/01_Software/eebus-docker-bridge:/src" -v eebus-gomod:/go/pkg/mod -w /src golang:1.24-alpine sh -c 'gofmt -l . ; go vet ./...'

# Lokale Testumgebung (alle vier Use Cases): Brücke :8090, Steuerbox :8091 (admin/test), Modbus :5502
docker compose up -d --build
docker compose down -v          # inkl. Volumes (Zertifikate, Kopplung)

# PFC-Images (ARMv7) nach dist/, Version landet als Software-Revision in der Brücke
sh skripte/pfc-images-bauen.sh 0.5
```

Testen ohne Browser über die APIs, z. B.:
```sh
QR=$(curl -s -u admin:test localhost:8090/api/status | python -c "import json,sys; print(json.load(sys.stdin)['kennung']['qrText'])")
# Steuerbox koppeln: POST :8091/api/kopplung {"verfahren":"pairing","qrText":...} oder {"verfahren":"ski","ski":...}
# Brücke: POST :8090/api/suchmodus {an}, /api/kopplungsanfrage {ski,annehmen}, /api/kopplung {ski}, DELETE /api/kopplung
curl -s -u admin:test -X POST -H "Content-Type: application/json" -d '{"wertW":7000,"dauerS":120,"aktiv":true}' localhost:8091/api/grenze
# weitere Steuerbox: /api/einspeisegrenze {wertW,dauerS,aktiv}, /api/grenzen-gemeinsam {bezugW,einspeisungW,dauerS},
#   /api/failsafe und /api/failsafe-einspeisung {grenzeW,mindestdauerS}, /api/heartbeat {an}, /api/verbindung {aktion: kurz|trennen|wiederherstellen}
# Simulator mit Störung/ungültigen Werten: docker compose stop spssimulator; docker compose run -d --name simtest spssimulator -url tcp://bruecke:5502 -stoerung -ungueltig-mpc 1
```
Aktionen verlangen `Content-Type: application/json` (CSRF-Schutz). Screenshots der UIs mit Edge headless (`--headless=new --screenshot`, mind. ~490 px breit; für 375 px die Seite in einen iframe legen).

## Aufbau

- `bruecke/`:
  - `main.go`: Start, Entitäten, Use Cases in fester Reihenfolge. `konfiguration.go`: Env lesen und prüfen.
  - `bruecke.go`: Zustand, LPC/LPP-Ereignisse, Freigaben, Takt, Failsafe-Speicherung.
  - `begrenzung.go`: Zustandsautomat je Richtung, Adapter für cs/lpc und cs/lpp, Nennleistungs-Workaround.
  - `kopplung.go`: ship-go-Callbacks, Suchmodus, Kopplungsanfragen, eine Steuerbox (Änderungen seriell über `kopplungMu`).
  - `messwerte.go`: MPC/MGCP einrichten, MGCP-Akteur-Workaround. Jeder Messwert ist eine Zeile der Tabelle `messgroesse` (Register, Gültigkeitsbit, Update-Funktion); Senden und UI laufen nur darüber.
  - `gegenstelle.go`: Diagnose der verbundenen Steuerbox.
  - `anlage.go`: Revisionen, Anlagenstatus.
  - `modbus.go`: Registerlayout.
  - `web.go` + `web/`: Status-UI, Aktionen, Anleitung.
- `testwerkzeuge/steuerbox/`: Steuerbox-Simulator (eg/lpc, eg/lpp, ma/mpc, ma/mgcp) mit UI. `senden.go`: Grenzen und Failsafe-Werte senden. `messwerte.go` liest MPC selbst, weil ma/mpc keine CEM-Entität akzeptiert. `testwerkzeuge/spssimulator/`: Modbus-Client mit Anlagenmodell.
- `internal/gemeinsam/`:
  - Zertifikat, Env, Ereignisprotokoll (hängt am Standard-Log), Web-UI mit Basic Auth und `Aktion[T]`.
  - Gemeinsames CSS (`stil.css`, wird per `<!--STIL-->` inline eingesetzt).
  - Kopplung/Secret/QR-Parser, `NormalisiereSki`, `GleicheIdentitaet`, MAC-Kennung, `EebusLog` (Debug nach stdout).
  - `SchreibeDatei`: alle Dateien im Datenverzeichnis atomar schreiben (temporäre Datei + umbenennen).
- `third_party/spine-go/`: gepatchte Kopie, per `replace` in `go.mod` (siehe `PATCH.md`).
- `codesys/`: `FbEebusBruecke`, `FbEebusBegrenzung` (je Richtung), `FbEebusMesswerte`, `ST_EebusMpc/Mgcp`, `eAnlagenstatus`, `FuEebusRegister`, `FbEebusLpc` (veraltet). Nicht ins Repo-Build eingebunden.
- Ein `Dockerfile` für alle Programme (`--build-arg PROGRAMM=…`, `VERSION=…`), Laufzeit-Image `scratch`.

## Modbus-Schnittstelle

- **Versionsregel:** Register 1 = Schnittstellenversion (1), nur bei inkompatiblen Änderungen erhöhen. Neue Register nur anhängen und dann `ErweiterungsVersion` (Register 14, jetzt 2) erhöhen. `FbEebusLpc` prüft Register 1 und läuft deshalb weiter.
- Input 0–30: LPC-Block ab 2, LPP-Block ab 17 (gleicher Aufbau, Offsets `blk*`), 14 Erweiterung, 15/16 Use-Case-Masken (lokal / Steuerbox), 29–30 Failsafe-Mindestdauer.
- Holding 0–60: 0 Lebenszeichen, 1–2/3–4 Nennleistungen, 5/6 Gültigkeitsmasken, 7 Anlagenstatus, 8–33 MPC, 40–60 MGCP. Die SPS schreibt alles in einem FC16.
- Tabellen im README.

## Konventionen

- Bezeichner, Kommentare, Logs auf Deutsch. **In Go-Code und Log-Meldungen Umlaute als ae/oe/ue/ss**, in HTML/README echte Umlaute.
- Zugriff auf Zustand nur unter `mu`. Aufrufe in den EEBUS-Stack immer außerhalb von `mu` (Verklemmungsgefahr mit Callbacks). Aus ship-go-Callbacks heraus nicht direkt in den Stack zurückrufen (`ersetzeKopplung` nutzt dafür eine Goroutine).
- Log nur bei echten Änderungen (Verbindung, Pairing-Endzustände, Failsafe-Werte, neu gefundene Geräte, Ablehnungsgründe), damit das Ereignisprotokoll (100 Zeilen) lesbar bleibt.
- Werte aus dem LAN (mDNS-Gerätenamen, Daten der Steuerbox) im UI nur per `textContent`, nie `innerHTML`.
- **Genau eine Steuerbox:** `EEBUS_REMOTE_SKI` schaltet Pairing Service und UI-Kopplung ab, sonst ersetzt jede neue Kopplung die alte. Deshalb müssen LPC/LPP-Ereignisse nicht nach Absender gefiltert werden.
- Brücken-UI ist nur lesend **bis auf Kopplung und Suchmodus** (Entscheidung des Nutzers 10/2026), abschaltbar mit `WEB_KOPPLUNG=aus`. Basic Auth (`WEB_PASSWORT` Pflicht, sonst UI aus), Aktionen nur mit JSON-Content-Type. Kein `SetAutoAccept`.
- Commits mit `Co-Authored-By`-Zeile. Gearbeitet wird auf `dev`.

## eebus-go: wichtige Details

- Gepinnt auf Entwicklungsstände: eebus-go `8583642861c3` (30.09.2026), ship-go `a84426bc3810` (28.09.2026, enthält SKI-Vierergruppen im QR-Code), spine-go `0104ce40c885` als gepatchte Kopie. Unveröffentlicht, API kann sich ändern.
- Neue API ggü. v0.7.0: `ServiceIdentity` statt SKI-Strings, `RegisterRemoteService`, Callbacks `RemoteServiceConnected`, `VisibleRemoteMdnsServicesUpdated`, `ServiceAutoTrusted/-Failed/-Removed`. `Start()`/`AddUseCase()` liefern Fehler.
- **Kopplungsanfragen:** Mit `UserIsAbleToApproveOrCancelPairingRequests(true)` hält ship-go eingehende Verbindungen unbekannter Geräte an (`ServicePairingDetailUpdate` mit `ReceivedPairingRequest`, Wartezeit 60 s mit Verlängerung). `RegisterRemoteService` nimmt an, `CancelPairing` lehnt ab. Ohne das lehnt ship-go sofort ab.
- CS/LPC und CS/LPP verlangen Freigaben: `LimitWriteApprovalRequired` und `ConfigurationWriteApprovalRequired`.
- **LPC und LPP auf derselben CEM-Entität** (wie eebus-go `examples/hems`):
  - Die `FailsafeDurationMinimum` ist ein gemeinsamer Schlüssel.
  - Konfigurations-Writes kommen in beiden Use Cases an und müssen in beiden freigegeben werden, mit derselben Prüfung. Grenzwert-Writes gibt der fremde Use Case selbst frei.
  - Dadurch haben LoadControl und DeviceConfiguration je zwei Freigabe-Callbacks, und dabei verlor spine-go bei schnell aufeinanderfolgenden Writes Freigaben (Bug, gepatcht in `third_party/spine-go`).
- **MPC** liegt per Vorgabe auf der CEM-Entität und wird vor LPC/LPP angemeldet (LPC verweist auf dessen ACPowerTotal). Dann findet `Set*NominalMax` (feste IDs 0/0) die Kennlinie nicht: Die Brücke setzt sie selbst über den Typ (`setzeNennleistung`, EMS = contractual).
- **MGCP** braucht eine eigene Entität `GridConnectionPointOfPremises`. `gcp/mgcp` setzt den Akteur falsch auf MonitoringAppliance, korrigiert über `mgcp.UseCaseActor` vor `AddUseCase`.
- **Messwerte:** Updates sind partiell, deshalb den ValueState immer mitschicken (`normal`/`error`). Sonst bleibt ein früheres `error` bei der Steuerbox stehen. eebus-go vergibt die Phasen-IDs in Map-Reihenfolge.
- Pairing Service: Brücke = Listener (`PairingModeListener` + 16-Byte-Secret), Steuerbox = Announcer. Der Announcer muss die Gegenseite vorher per `RegisterRemoteService` vertrauen. ship-go hält Auto-Trust nur im Speicher, die Brücke speichert ihn in `steuerbox-pairing.json`, UI-Kopplungen in `steuerbox-ski.json`. Eine Pairing-Kopplung kennt oft keinen SKI, nur Fingerprint und SHIP-ID.
- Nach Neustart lehnt ship-go die weiterlaufende Ankündigung einer schon gekoppelten Steuerbox als „replay attack“ ab: harmlos, Meldung wird unterdrückt. Ändert die Steuerbox ihre SHIP-ID, kommt „SHIP ID mismatch“: Kopplung lösen und neu koppeln.
- **Zustandsautomat (seit 10/2026):** Init geht nach 120 s ohne Heartbeat in **Failsafe** (vorher Unbegrenzt/autonom), damit die gespeicherte Failsafe-Grenze nach einem Neustart ohne Steuerbox mindestens die Mindestdauer gilt. Gegen die Spezifikation noch prüfen (Schritt 6).
- Failsafe-Werte der Steuerbox müssen einen Neustart überstehen (gelten im Zustand Init): `failsafe.json` (mit `einspeisegrenzeW` für LPP), haben Vorrang vor `FAILSAFE_*`.

## Partner-Info

Der Partner (Messstellenbetreiber-Seite) kündigte an: Künftig braucht die EEBUS-Kopplung statt nur SKI eine **SHIP-ID, einen SHA-256-Fingerprint und ein Secret**. Das ist der „SHIP Pairing Service 1.0.0“ und ist umgesetzt (Branch `dev`). Firma hat keine IANA-Nummer, daher SHIP-ID-Format `<Marke>-<Modell>-<MAC>`. Erste Steuerbox wird eine PROLAN STB-142E.

# Projektkontext: EEBUS-Gateway

Docker-Container, betrieben auf dem WAGO PFC200: tritt als EEBUS „Controllable System“ gegenüber der Steuerbox des Messstellenbetreibers auf und bildet die Use Cases des VDE FNN Hinweises „Schnittstellen der Steuerungseinrichtung“ ab: LPC (Bezug begrenzen, §14a EnWG), LPP (Einspeisung begrenzen, §9 EEG), MPC (Messwerte Anlage), MGCP (Messwerte Netzanschlusspunkt). Austausch mit der CODESYS-Applikation per Modbus TCP (127.0.0.1:5502). Hieß bis 08.10.2026 „EEBUS-Brücke“ (Ordner `bruecke/`, Container `eebus-bruecke`).

Doku für Anwender: `README.md` (Schnellstart, kurz halten), `docs/betrieb.md` (Installation, Variablen, UI), `docs/kopplung.md` (Kopplung, SHIP-ID, Netzwerk, Fehlersuche, Swistec, PROLAN), `codesys/README.md` (Registerlayout, Bausteine), `testwerkzeuge/README.md` (Test-Steuerbox, Simulator, Testumgebung), `docs/entwicklung.md` (Repo-Aufbau, Workarounds, offene Punkte). Diese Datei ist für die Weiterarbeit.

## Stand: Version 1.0 (20.10.2026)

- **`main` = 1.0** (Tag `v1.0`), `dev` ist darin aufgegangen. Enthält: SHIP Pairing Service, alle vier FNN-Use-Cases (`EEBUS_USECASES`, Vorgabe `lpc`), Suchmodus und Kopplung im UI, Diagnose der Gegenstelle, Anlagenstatus, Freigabe nur bei erreichbarer SPS, Modbus-Erweiterung 2, `EEBUS_SCHNITTSTELLEN`, Karte „Verbindungsversuche“.
- **Im Betrieb mit der Swistec SwisBox** seit 20.10.2026 auf dem PFC (Gateway 0.6, danach 1.0): Kopplung per SKI, Grenze 11 000 W, Heartbeat. Erkenntnisse in docs/kopplung.md, „Swistec SwisBox“. Kurz: Box ist reiner Client (eigener Port RST), Ursache der anfänglichen Funkstille war die mDNS-Ankündigung (X1-Adresse vorn, dann br1 ohne IP). Box meldet LPC+LPP 1.0.0, keine Messwerte, keine Herstellerdaten über SPINE, schreibt die Grenze nach dem Verbinden 10× in 12 s.
- **PFC-Konfiguration** (laut Anleitung vom 20.10.2026 umgezogen auf Container `eebus-gateway`, Daten `/home/eebus-gateway`): `GERAET_MARKE=ecg`, `SHIP_ID=ecg-PFC200-LPC-Bruecke-0030DE683ADC` (bleibt so, steht beim MSB), `EEBUS_USECASES=lpc`, `EEBUS_REMOTE_SKI=d4e4c4aeb6082d84bc5b09ecd679e0edd6241a5c` (SwisBox, SHIP-ID `GSWIC026090004`), `EEBUS_SCHNITTSTELLEN=br1`, `NENNLEISTUNG_MAX_W=11000`, `FAILSAFE_GRENZE_W=11000`, `FAILSAFE_MINDESTDAUER=2h`. Im UI gesetzt (einstellungen.json): `EEBUS_PAIRING_SERVICE=aus`, `EEBUS_DEBUG` (für den Betrieb aus). SKI des Gateways `d3b6409e4b5f…`. Netz: X1 = br0 192.168.111.74, X2 = br1 192.168.1.74/24 direkt an ETH1 der Box (192.168.1.100). Kein avahi-daemon auf dem PFC, der D-Bus-Mount ist überflüssig. Die SPS meldet die Nennleistung mit 22 000 W.
- **CODESYS-Bausteine** der Erweiterung 2 sind ohne CODESYS geschrieben, nicht kompiliert.
- PROLAN STB-142E bleibt dokumentiert (docs/kopplung.md), noch nicht getestet.

## Nächste Schritte / offen

1. Mit der SwisBox noch nicht erprobt: Grenzwechsel und Ablauf, Failsafe bei Verbindungsverlust, ob die Box Failsafe-Werte schreibt (`failsafe.json` vorhanden?), Verhalten nach Neustart des Gateways (schreibt die Box die Grenze neu? Laut Beobachtung ja, direkt nach dem Verbinden). LPP meldet die Box, wird aber nicht genutzt.
2. CODESYS-Bausteine übernehmen und kompilieren. Nennleistung der SPS (22 000 W) mit der Anlage abgleichen.
3. Netzwerk: IPv6-Link-Local ohne IPv4 an X2 noch ungetestet.
4. Vor Übergabe weiterer Anlagen: `GERAET_MARKE` auf Firmenkürzel, SHIP-ID mit `SHIP_ID=…` festschreiben (docs/kopplung.md, „Vor der Übergabe“).
5. Zustandsautomat (`gateway/begrenzung.go`, `takt`) gegen die Spezifikationen LPC/LPP und FNN-Lastenheft 1.4 prüfen. Konkret: Ohne Steuerbox geht Failsafe nach der Mindestdauer in „Unbegrenzt/autonom“ (Anlage frei). Ist das FNN-konform, oder muss die Failsafe-Grenze bleiben? (Vom Nutzer am 04.10.2026 für später vorgemerkt.) Update-Rate der Messwerte klären (sendet bei jeder Änderung).
6. **Neustart/Stromausfall während einer Begrenzung:** Die aktive Grenze geht verloren (Init → Failsafe-Grenze, nach Heartbeat Unbegrenzt). Die Test-Steuerbox schreibt sie nicht neu, die SwisBox schreibt sie direkt nach dem Verbinden (20.10.2026). Bei anderen Boxen prüfen, sonst Grenze mit Ablaufzeit sichern.
7. Fehler upstream melden: spine-go `ApproveOrDenyWrite` (third_party/spine-go/PATCH.md, Issue-Text vorbereitet 04.10.2026), eebus-go `gcp/mgcp` Akteur, `cs/lpc`/`cs/lpp` `Set*NominalMax` mit festen IDs 0/0, enbility/zeroconf kündigt alle Adressen auf jeder Schnittstelle an.

## Umgebung und Werkzeuge

- **PFC200:** IP 192.168.111.74 (X1), SSH als `root` (nur mit Passwort, Claude kommt nicht selbst drauf, liest aber das UI über HTTP), Datenverzeichnis `/home/eebus-gateway` (keine SD-Karte), Hostname `PFC200V3-683ADC`. Docker läuft. CODESYS: ModbusTCP Master mit Auto-Reconnect (war anfangs aus, Ursache für „SPS ausgefallen“).
- **Entwicklungsrechner:** Windows, **kein Go installiert**, alles läuft in Docker Desktop (20.10, Compose v2.10). Bash-Befehle in Git Bash. Bei `docker run -v` mit Windows-Pfaden `MSYS_NO_PATHCONV=1` voranstellen.
- **Laufwerk C:** war einmal ganz voll (Docker lief dann nur noch lesend), am 20.10.2026 88 GB frei. Docker-Festplatte liegt auf C:. Vor großen Builds `docker system df` prüfen, alter Build-Cache: `docker builder prune -f --filter until=168h`. Für Go-Befehle `golang:1.24-alpine` statt des großen Images nehmen, Modul-Cache im Volume `eebus-gomod`.
- Downloads beim Docker-Build brechen gelegentlich ab (`tls: bad record MAC`, TLS-Timeouts): vorübergehend, Build wiederholen. Zuerst nur `docker compose -f testwerkzeuge/docker-compose.yml build gateway`, die anderen nutzen dann die Modul-Schicht mit.
- Im LAN läuft eine fremde „EnbilityNet Devices-App“ (SKI c1fded…), die per mDNS auftaucht und sich verbinden will. Callbacks müssen deshalb prüfen, ob ein Ereignis den eigenen Partner betrifft (`gemeinsam.GleicheIdentitaet`).
- **Repo:** GitHub `justushinken/eebus-docker-bridge`, wird vom Nutzer in `eebus-gateway` umbenannt (danach `git remote set-url`). Der lokale Ordner heißt weiter `eebus-docker-bridge`.
- Python unter Windows schreibt im Textmodus CRLF: beim Bearbeiten per Skript `newline=''` oder Binärmodus verwenden.
- Quellen der gepinnten Module zum Nachlesen: `docker run … golang:1.24-alpine sh -c 'cp -r /go/pkg/mod/github.com/enbility /out/'` in ein Scratch-Verzeichnis.

## Häufige Befehle

```sh
# Prüfen und bauen (ohne lokales Go)
MSYS_NO_PATHCONV=1 docker run --rm -v "D:/01_Software/eebus-docker-bridge:/src" -v eebus-gomod:/go/pkg/mod -w /src golang:1.24-alpine sh -c 'gofmt -l . ; go vet ./...'

# Lokale Testumgebung (alle vier Use Cases, Projekt eebus-test): Gateway :8090, Steuerbox :8091 (admin/test), Modbus :5502
docker compose -f testwerkzeuge/docker-compose.yml up -d --build
docker compose -f testwerkzeuge/docker-compose.yml down -v   # inkl. Volumes (Zertifikate, Kopplung)
# docker-compose.yml in der Wurzel ist nur die Betriebsvorlage (Host-Netz, läuft nicht auf Docker Desktop)

# PFC-Images (ARMv7) nach dist/, Version landet als Software-Revision im Gateway
sh skripte/pfc-images-bauen.sh 1.0
```

Testen ohne Browser über die APIs, z. B.:
```sh
QR=$(curl -s -u admin:test localhost:8090/api/status | python -c "import json,sys; print(json.load(sys.stdin)['kennung']['qrText'])")
# Steuerbox koppeln: POST :8091/api/kopplung {"verfahren":"pairing","qrText":...} oder {"verfahren":"ski","ski":...}
# Gateway: POST :8090/api/suchmodus {an}, /api/kopplungsanfrage {ski,annehmen}, /api/kopplung {ski}, DELETE /api/kopplung
curl -s -u admin:test -X POST -H "Content-Type: application/json" -d '{"wertW":7000,"dauerS":120,"aktiv":true}' localhost:8091/api/grenze
# weitere Steuerbox: /api/einspeisegrenze {wertW,dauerS,aktiv}, /api/grenzen-gemeinsam {bezugW,einspeisungW,dauerS},
#   /api/failsafe und /api/failsafe-einspeisung {grenzeW,mindestdauerS}, /api/heartbeat {an}, /api/verbindung {aktion: kurz|trennen|wiederherstellen}
# Simulator mit Störung/ungültigen Werten: docker compose -f testwerkzeuge/docker-compose.yml stop spssimulator; docker compose -f testwerkzeuge/docker-compose.yml run -d --name simtest spssimulator -url tcp://gateway:5502 -stoerung -ungueltig-mpc 1
```
Aktionen verlangen `Content-Type: application/json` (CSRF-Schutz). Screenshots der UIs mit Edge headless (`--headless=new --screenshot`, mind. ~490 px breit; für 375 px die Seite in einen iframe legen).

## Aufbau

- `gateway/` (Typ `Gateway`, Empfänger heißt weiter `b`):
  - `main.go`: Start, Entitäten, Use Cases in fester Reihenfolge. `konfiguration.go`: Env lesen und prüfen (`leseKonfiguration(gemeinsam.Umgebung)`), Anzeige für das UI.
  - `einstellungen.go`: im UI änderbare Variablen (`einstellbar`), `einstellungen.json` hat Vorrang vor der Env (`mitEinstellungen`). Neustart aus dem UI per `syscall.Exec` auf sich selbst, unabhängig von der Docker-Restart-Policy.
  - `gateway.go`: Zustand, LPC/LPP-Ereignisse, Freigaben, Takt, Failsafe-Speicherung.
  - `begrenzung.go`: Zustandsautomat je Richtung, Adapter für cs/lpc und cs/lpp, Nennleistungs-Workaround.
  - `kopplung.go`: ship-go-Callbacks, Suchmodus, Kopplungsanfragen, eine Steuerbox (Änderungen seriell über `kopplungMu`).
  - `messwerte.go`: MPC/MGCP einrichten, MGCP-Akteur-Workaround. Jeder Messwert ist eine Zeile der Tabelle `messgroesse` (Register, Gültigkeitsbit, Update-Funktion); Senden und UI laufen nur darüber.
  - `gegenstelle.go`: Diagnose der verbundenen Steuerbox.
  - `verbindungsversuche.go`: letzter TCP/TLS-Versuch je Richtung aus dem ship-go-Log (`EebusLog`-Beobachter) und den TLS-Fehlern im Standard-Log (Filter des Ereignisprotokolls). Eigene Sperre, unter ihr nie loggen. `netz.go`: Schnittstellen und Avahi beim Start prüfen.
  - `anlage.go`: Revisionen, Anlagenstatus.
  - `modbus.go`: Registerlayout.
  - `web.go` + `web/`: Status-UI, Aktionen, Anleitung. `GET /api/konfiguration` liefert `Konfiguration.Anzeige()` (ohne Passwort).
  - UIs (Gateway und Test-Steuerbox) mit Reitern: Links `href="#name" data-reiter="name"`, Inhalte `data-reiter-inhalt="name"`, umgeschaltet über `location.hash`. Element-IDs dürfen nicht wie ein Reiter heißen, sonst springt der Browser beim Hash dorthin.
- `testwerkzeuge/steuerbox/`: Steuerbox-Simulator (eg/lpc, eg/lpp, ma/mpc, ma/mgcp) mit UI. `senden.go`: Grenzen und Failsafe-Werte senden. `messwerte.go` liest MPC selbst, weil ma/mpc keine CEM-Entität akzeptiert. `testwerkzeuge/spssimulator/`: Modbus-Client mit Anlagenmodell.
- `internal/gemeinsam/`:
  - Zertifikat, Env, Ereignisprotokoll (hängt am Standard-Log), Web-UI mit Basic Auth und `Aktion[T]`.
  - Gemeinsames CSS (`stil.css`, wird per `<!--STIL-->` inline eingesetzt).
  - Kopplung/Secret/QR-Parser, `NormalisiereSki`, `GleicheIdentitaet`, MAC-Kennung, `EebusLog` (Debug nach stdout).
  - `SchreibeDatei`: alle Dateien im Datenverzeichnis atomar schreiben (temporäre Datei + umbenennen).
- `third_party/spine-go/`: gepatchte Kopie, per `replace` in `go.mod` (siehe `PATCH.md`).
- `codesys/`: `FbEebusGateway`, `FbEebusBegrenzung` (je Richtung), `FbEebusMesswerte`, `ST_EebusMpc/Mgcp`, `eAnlagenstatus`, `FuEebusRegister`, `FbEebusLpc` (veraltet). Nicht ins Repo-Build eingebunden.
- Ein `Dockerfile` für alle Programme (`--build-arg PROGRAMM=…`, `VERSION=…`), Laufzeit-Image `scratch`.

## Modbus-Schnittstelle

- **Versionsregel:** Register 1 = Schnittstellenversion (1), nur bei inkompatiblen Änderungen erhöhen. Neue Register nur anhängen und dann `ErweiterungsVersion` (Register 14, jetzt 2) erhöhen. `FbEebusLpc` prüft Register 1 und läuft deshalb weiter.
- Input 0–30: LPC-Block ab 2, LPP-Block ab 17 (gleicher Aufbau, Offsets `blk*`), 14 Erweiterung, 15/16 Use-Case-Masken (Gateway / Steuerbox, in CODESYS `wUseCasesGateway`/`wUseCasesSteuerbox`), 29–30 Failsafe-Mindestdauer.
- Holding 0–27: 0 Lebenszeichen, 1–2/3–4 Nennleistungen, 5/6 Gültigkeitsmasken (je 3 Bits), 7 Anlagenstatus, 8–17 MPC, 18–27 MGCP (je Leistung DINT, Energie Bezug/Erzeugung bzw. Einspeisung/Bezug ULINT). Die SPS schreibt alles in einem FC16.
- Messwerte bewusst nur Leistung und Energie (Entscheidung 10/2026): reicht für FNN und die Pflicht-Szenarien. Strom, Spannung, Frequenz, Phasen, PV-Faktor bei Bedarf hinten anhängen.
- Tabellen in `codesys/README.md`.

## Konventionen

- Bezeichner, Kommentare, Logs auf Deutsch. **In Go-Code und Log-Meldungen Umlaute als ae/oe/ue/ss**, in HTML/README echte Umlaute. „das Gateway“ (Neutrum), Pronomen „es/sein“.
- Zugriff auf Zustand nur unter `mu`. Aufrufe in den EEBUS-Stack immer außerhalb von `mu` (Verklemmungsgefahr mit Callbacks). Aus ship-go-Callbacks heraus nicht direkt in den Stack zurückrufen (`ersetzeKopplung` nutzt dafür eine Goroutine).
- Log nur bei echten Änderungen (Verbindung, Pairing-Endzustände, Failsafe-Werte, neu gefundene Geräte, Ablehnungsgründe), damit das Ereignisprotokoll (100 Zeilen) lesbar bleibt.
- Werte aus dem LAN (mDNS-Gerätenamen, Daten der Steuerbox) im UI nur per `textContent`, nie `innerHTML`.
- **Genau eine Steuerbox:** `EEBUS_REMOTE_SKI` schaltet Pairing Service und UI-Kopplung ab, sonst ersetzt jede neue Kopplung die alte. Deshalb müssen LPC/LPP-Ereignisse nicht nach Absender gefiltert werden.
- Gateway-UI ändert nur **Kopplung, Suchmodus und Einstellungen samt Neustart** (Entscheidungen des Nutzers 10/2026), alles abschaltbar mit `WEB_AENDERUNGEN=aus`. Basic Auth (`WEB_PASSWORT` Pflicht, sonst UI aus), Aktionen nur mit JSON-Content-Type. Kein `SetAutoAccept`.
- Commits mit `Co-Authored-By`-Zeile. Gearbeitet wird auf `dev`, Versionen kommen nach `main` mit Tag `vX.Y`.

## eebus-go: wichtige Details

- Gepinnt auf Entwicklungsstände: eebus-go `8583642861c3` (30.09.2026), ship-go `a84426bc3810` (28.09.2026, enthält SKI-Vierergruppen im QR-Code), spine-go `0104ce40c885` als gepatchte Kopie. Unveröffentlicht, API kann sich ändern.
- Neue API ggü. v0.7.0: `ServiceIdentity` statt SKI-Strings, `RegisterRemoteService`, Callbacks `RemoteServiceConnected`, `VisibleRemoteMdnsServicesUpdated`, `ServiceAutoTrusted/-Failed/-Removed`. `Start()`/`AddUseCase()` liefern Fehler.
- **Kopplungsanfragen:** Mit `UserIsAbleToApproveOrCancelPairingRequests(true)` hält ship-go eingehende Verbindungen unbekannter Geräte an (`ServicePairingDetailUpdate` mit `ReceivedPairingRequest`, Wartezeit 60 s mit Verlängerung). `RegisterRemoteService` nimmt an, `CancelPairing` lehnt ab. Ohne das lehnt ship-go sofort ab.
- CS/LPC und CS/LPP verlangen Freigaben: `LimitWriteApprovalRequired` und `ConfigurationWriteApprovalRequired`.
- **LPC und LPP auf derselben CEM-Entität** (wie eebus-go `examples/hems`):
  - Die `FailsafeDurationMinimum` ist ein gemeinsamer Schlüssel.
  - Konfigurations-Writes kommen in beiden Use Cases an und müssen in beiden freigegeben werden, mit derselben Prüfung. Grenzwert-Writes gibt der fremde Use Case selbst frei.
  - Dadurch haben LoadControl und DeviceConfiguration je zwei Freigabe-Callbacks, und dabei verlor spine-go bei schnell aufeinanderfolgenden Writes Freigaben (Bug, gepatcht in `third_party/spine-go`).
- **MPC** liegt per Vorgabe auf der CEM-Entität und wird vor LPC/LPP angemeldet (LPC verweist auf dessen ACPowerTotal). Dann findet `Set*NominalMax` (feste IDs 0/0) die Kennlinie nicht: Das Gateway setzt sie selbst über den Typ (`setzeNennleistung`, EMS = contractual).
- **MGCP** braucht eine eigene Entität `GridConnectionPointOfPremises`. `gcp/mgcp` setzt den Akteur falsch auf MonitoringAppliance, korrigiert über `mgcp.UseCaseActor` vor `AddUseCase`.
- **Messwerte:** Updates sind partiell, deshalb den ValueState immer mitschicken (`normal`/`error`). Sonst bleibt ein früheres `error` bei der Steuerbox stehen. eebus-go vergibt die Phasen-IDs in Map-Reihenfolge.
- Pairing Service: Gateway = Listener (`PairingModeListener` + 16-Byte-Secret), Steuerbox = Announcer. Der Announcer muss die Gegenseite vorher per `RegisterRemoteService` vertrauen. ship-go hält Auto-Trust nur im Speicher, das Gateway speichert ihn in `steuerbox-pairing.json`, UI-Kopplungen in `steuerbox-ski.json`. Eine Pairing-Kopplung kennt oft keinen SKI, nur Fingerprint und SHIP-ID.
- Nach Neustart lehnt ship-go die weiterlaufende Ankündigung einer schon gekoppelten Steuerbox als „replay attack“ ab: harmlos, Meldung wird unterdrückt. Ändert die Steuerbox ihre SHIP-ID, kommt „SHIP ID mismatch“: Kopplung lösen und neu koppeln.
- **Zustandsautomat (seit 10/2026):** Init geht nach 120 s ohne Heartbeat in **Failsafe** (vorher Unbegrenzt/autonom), damit die gespeicherte Failsafe-Grenze nach einem Neustart ohne Steuerbox mindestens die Mindestdauer gilt. Gegen die Spezifikation noch prüfen (Schritt 6).
- Failsafe-Werte der Steuerbox müssen einen Neustart überstehen (gelten im Zustand Init): `failsafe.json` (mit `einspeisegrenzeW` für LPP), haben Vorrang vor `FAILSAFE_*`.

## Partner-Info

Der Partner (Messstellenbetreiber-Seite) kündigte an: Künftig braucht die EEBUS-Kopplung statt nur SKI eine **SHIP-ID, einen SHA-256-Fingerprint und ein Secret**. Das ist der „SHIP Pairing Service 1.0.0“ und ist umgesetzt (Branch `dev`). Firma hat keine IANA-Nummer, daher SHIP-ID-Format `<Marke>-<Modell>-<MAC>` (Vorgabe-Modell `EEBUS-Gateway`). Erste Steuerbox wird eine PROLAN STB-142E.

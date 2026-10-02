# Projektkontext: EEBUS-LPC-Brücke für WAGO PFC200

Docker-Container auf dem WAGO PFC200: nimmt als EEBUS „Controllable System“ (Use Case LPC, §14a EnWG) Leistungsgrenzen der Steuerbox des Messstellenbetreibers entgegen und gibt sie per Modbus TCP (127.0.0.1:5502) an die CODESYS-Applikation weiter. Details für Anwender stehen im README. Diese Datei ist für die Weiterarbeit.

## Stand (Oktober 2026)

- **Branch `ship-pairing`** (noch nicht nach `main` übernommen): SHIP Pairing Service, Kopplung wahlweise per SKI oder Pairing Service, Anleitungsseiten, gespeicherte Failsafe-Werte, SHIP-ID aus der MAC. `main` = Stand 0.3 (nur SKI-Verfahren, eebus-go v0.7.0).
- **Images 0.4** für den PFC liegen unter `dist/` (`eebus-bruecke-0.4.tar.gz`, `eebus-steuerbox-0.4.tar.gz`), **noch nicht auf dem PFC getestet**.
- **Auf dem PFC läuft 0.2** (nur Status-UI), gestartet mit `GERAET_SERIENNUMMER=0001`. Beim Update auf 0.4 diese Zeile weglassen, dann kommt die SHIP-ID aus der MAC.
- Alles lokal mit der Test-Steuerbox getestet, **noch nicht gegen eine echte Steuerbox**.

## Nächste Schritte / offen

1. Vor Übergabe an den Messstellenbetreiber: `GERAET_MARKE` auf Firmenkürzel statt `Demo` setzen, danach SHIP-ID mit `SHIP_ID=…` festschreiben und nicht mehr ändern (README, „Vor der Übergabe an den Messstellenbetreiber“).
2. 0.4 auf dem PFC einspielen und testen, auch Brücke + Test-Steuerbox gleichzeitig auf dem PFC (bisher nur lokal getestet).
3. Netzwerk mit Installateur abstimmen: Steuerbox und PFC im selben Segment, Multicast erlaubt, UDP 5353 / TCP 4712 (README, „Netzwerk“). Idee: X2 separat für die Steuerbox (ungetestet).
4. Mit dem Partner klären: ab wann Pairing Service, welche Spezifikationsversion, ob er den QR-Code (`SHIP;SKI:…;ID:…;FPH256:…;SPSEC:…;ENDSHIP;`) oder die Einzelwerte will, und wer das Secret erzeugt (wir tun es, so sieht es ship-go vor).
5. `ship-pairing` nach `main` übernehmen, wenn der PFC-Test passt.
6. Zustandsautomat (`bruecke/bruecke.go`, `Takt`) gegen LPC-Spezifikation und FNN-Lastenheft prüfen.

## Umgebung und Werkzeuge

- **PFC200:** IP 192.168.111.74 (X1), SSH als `root`, Datenverzeichnis `/home/eebus-bruecke` (keine SD-Karte), Hostname `PFC200V3-683ADC`. Docker läuft. CODESYS: ModbusTCP Master mit Auto-Reconnect (war anfangs aus, Ursache für „SPS ausgefallen“).
- **Entwicklungsrechner:** Windows, **kein Go installiert**, alles läuft in Docker Desktop (20.10, Compose v2.10). Bash-Befehle in Git Bash. Bei `docker run -v` mit Windows-Pfaden `MSYS_NO_PATHCONV=1` voranstellen.
- **Laufwerk C: ist knapp** (war einmal ganz voll, Docker lief dann nur noch lesend). Docker-Festplatte liegt auf C:. Für Go-Befehle `golang:1.24-alpine` statt des großen Images nehmen, Modul-Cache im Volume `eebus-gomod`.
- Downloads beim Docker-Build brachen einmal mit `tls: bad record MAC` ab: war vorübergehend, Build wiederholen.

## Häufige Befehle

```sh
# Prüfen und bauen (ohne lokales Go)
MSYS_NO_PATHCONV=1 docker run --rm -v "D:/01_Software/eebus-docker-bridge:/src" -v eebus-gomod:/go/pkg/mod -w /src golang:1.24-alpine sh -c 'gofmt -l . ; go vet ./...'

# Lokale Testumgebung: Brücke :8090, Steuerbox :8091 (admin/test), Modbus :5502
docker compose up -d --build
docker compose down -v          # inkl. Volumes (Zertifikate, Kopplung)

# PFC-Images (ARMv7) nach dist/
sh skripte/pfc-images-bauen.sh 0.5
```

Testen ohne Browser über die API der Steuerbox, z. B.:
```sh
QR=$(curl -s -u admin:test localhost:8090/api/status | python -c "import json,sys; print(json.load(sys.stdin)['kennung']['qrText'])")
# koppeln: POST /api/kopplung {"verfahren":"pairing","qrText":...} oder {"verfahren":"ski","ski":...}
curl -s -u admin:test -X POST -H "Content-Type: application/json" -d '{"wertW":7000,"dauerS":120,"aktiv":true}' localhost:8091/api/grenze
# weitere: /api/failsafe {grenzeW,mindestdauerS}, /api/heartbeat {an}, /api/verbindung {aktion: kurz|trennen|wiederherstellen}
```
Aktionen verlangen `Content-Type: application/json` (CSRF-Schutz). Screenshots der UIs mit Edge headless (`--headless=new --screenshot`, mind. ~490 px breit; für 375 px die Seite in einen iframe legen).

## Aufbau

- `bruecke/`: `main.go` (Konfiguration aus Env, Start), `bruecke.go` (Zustandsautomat, EEBUS-Callbacks, Freigaben, Failsafe-Speicherung), `modbus.go` (Registerlayout, Schnittstellenversion 1), `web.go` + `web/` (Status-UI, Anleitung, QR-Bild).
- `testwerkzeuge/steuerbox/`: Energy-Guard-Simulator mit UI. `testwerkzeuge/spssimulator/`: Modbus-Client als CODESYS-Ersatz.
- `internal/gemeinsam/`: Zertifikat, Env, Ereignisprotokoll (hängt am Standard-Log), Web-UI mit Basic Auth, gemeinsames CSS (`stil.css`, wird per `<!--STIL-->` inline eingesetzt), Kopplung/Secret/QR-Parser, MAC-Kennung.
- `codesys/`: `FbEebusLpc` u. a., nicht ins Repo-Build eingebunden.
- Ein `Dockerfile` für alle Programme (`--build-arg PROGRAMM=…`), Laufzeit-Image `scratch`.

## Konventionen

- Bezeichner, Kommentare, Logs auf Deutsch. **In Go-Code und Log-Meldungen Umlaute als ae/oe/ue/ss**, in HTML/README echte Umlaute.
- Zugriff auf Zustand nur unter `mu`. Aufrufe in den EEBUS-Stack immer außerhalb von `mu` (Verklemmungsgefahr mit Callbacks).
- Log nur bei echten Änderungen (Verbindung, Pairing-Endzustände, Failsafe-Werte, neu gefundene Geräte), damit das Ereignisprotokoll (100 Zeilen) lesbar bleibt.
- Werte aus dem LAN (mDNS-Gerätenamen) im UI nur per `textContent`, nie `innerHTML`.
- Brücken-UI ist bewusst nur lesend (Entscheidung des Nutzers), geschützt per Basic Auth (`WEB_PASSWORT` Pflicht, sonst UI aus).
- Commits mit `Co-Authored-By`-Zeile, Feature-Arbeit auf eigenem Branch.

## eebus-go: wichtige Details

- Gepinnt auf Entwicklungsstände: eebus-go `8583642861c3` (30.09.2026), ship-go `a84426bc3810` (28.09.2026, enthält SKI-Vierergruppen im QR-Code). Unveröffentlicht, API kann sich ändern.
- Neue API ggü. v0.7.0: `ServiceIdentity` statt SKI-Strings, `RegisterRemoteService`, Callbacks `RemoteServiceConnected`, `VisibleRemoteMdnsServicesUpdated`, `ServiceAutoTrusted/-Failed/-Removed`. `Start()`/`AddUseCase()` liefern Fehler. Kein `AllowWaitingForTrust` mehr.
- CS/LPC verlangt Freigaben: `LimitWriteApprovalRequired` und `ConfigurationWriteApprovalRequired` (Failsafe-Werte, Bereich 2–24 h).
- Pairing Service: Brücke = Listener (`PairingModeListener` + 16-Byte-Secret), Steuerbox = Announcer. Der Announcer muss die Gegenseite vorher per `RegisterRemoteService` vertrauen. ship-go hält Auto-Trust nur im Speicher, die Brücke speichert ihn in `steuerbox-pairing.json` und meldet ihn beim Start wieder an.
- Nach Neustart lehnt ship-go die weiterlaufende Ankündigung einer schon gekoppelten Steuerbox als „replay attack“ ab: harmlos, Meldung wird unterdrückt.
- In v0.7.0 blieb nach Trennung eine veraltete Entität in `RemoteEntitiesScenarios()`, im Entwicklungsstand behoben.
- Failsafe-Werte der Steuerbox müssen einen Neustart überstehen (gelten im Zustand Init): `failsafe.json`, haben Vorrang vor `FAILSAFE_*`.

## Partner-Info

Der Partner (Messstellenbetreiber-Seite) kündigte an: Künftig braucht die EEBUS-Kopplung statt nur SKI eine **SHIP-ID, einen SHA-256-Fingerprint und ein Secret**. Das ist der „SHIP Pairing Service 1.0.0“ und ist umgesetzt (Branch `ship-pairing`). Firma hat keine IANA-Nummer, daher SHIP-ID-Format `<Marke>-<Modell>-<MAC>`.

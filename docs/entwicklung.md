# Entwicklung

Für alle, die am Gateway selbst arbeiten. Arbeitsstand und Hintergründe für die Weiterentwicklung stehen außerdem in [CLAUDE.md](../CLAUDE.md).

## Aufbau des Repos

| Pfad | Inhalt |
|---|---|
| `gateway/` | Das Gateway: EEBUS-Anbindung, Zustandsautomaten, Messwerte, Kopplung, Modbus-Server, Status-UI (`web/`) |
| `codesys/` | CODESYS-Quellen und Registerlayout, siehe [codesys/README.md](../codesys/README.md) |
| `testwerkzeuge/` | Test-Steuerbox, SPS-Simulator und Testumgebung, siehe [testwerkzeuge/README.md](../testwerkzeuge/README.md) |
| `internal/gemeinsam/` | Gemeinsamer Go-Code: Zertifikat, Umgebungsvariablen, Ereignisprotokoll, Web-UI-Grundlagen, Kopplung, QR-Code |
| `third_party/spine-go/` | spine-go mit einem Fix, siehe `PATCH.md` |
| `Dockerfile` | Ein Dockerfile für alle Programme, Auswahl per `--build-arg PROGRAMM=…` |
| `docker-compose.yml` | Betriebsvorlage nur für das Gateway |
| `skripte/pfc-images-bauen.sh` | Baut Gateway und Test-Steuerbox für den PFC (ARMv7) nach `dist/` |
| `dist/` | gebaute Images zum Verteilen (nicht im Repo) |

## Bauen ohne lokales Go

Alle Go-Programme liegen in einem Modul (`eebus-gateway`). Gebaut wird ausschließlich in Docker (Go 1.24), das Laufzeit-Image ist `scratch`.

```sh
# Formatierung und go vet (Git Bash; MSYS_NO_PATHCONV verhindert die Pfadumwandlung)
MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD:/src" -v eebus-gomod:/go/pkg/mod -w /src golang:1.24-alpine sh -c 'gofmt -l . ; go vet ./...'

# Lokale Testumgebung
docker compose -f testwerkzeuge/docker-compose.yml up -d --build

# Images für den PFC, die Version landet als Software-Revision im Gateway
sh skripte/pfc-images-bauen.sh 0.5
```

## eebus-go-Version

Für den SHIP Pairing Service wird der noch unveröffentlichte Entwicklungsstand von eebus-go und ship-go genutzt, auf feste Commits gepinnt (siehe `go.mod`: eebus-go `8583642861c3` vom 30.09.2026, ship-go `a84426bc3810` vom 28.09.2026, spine-go als gepatchte Kopie). Die Schnittstelle kann sich bis zur Veröffentlichung noch ändern. Vor einem Update die Aufrufe in `gateway/` und `testwerkzeuge/steuerbox/` abgleichen und die Testszenarien der Test-Steuerbox wiederholen.

**Workarounds** (bei einem Update prüfen, ob noch nötig):

- MGCP kündigt den falschen Akteur an (`gateway/messwerte.go`).
- `Set*NominalMax` findet die Kennlinie nicht, wenn MPC auf derselben Entität liegt (`gateway/begrenzung.go`).
- spine-go verliert Freigaben bei schnell aufeinanderfolgenden Schreibanfragen (`third_party/spine-go/PATCH.md`).

## Offene Punkte vor dem Produktiveinsatz

- **Echte Steuerbox:** Lokal mit der Test-Steuerbox getestet: Kopplung per Pairing Service, per SKI und per Suchmodus, falsches Secret wird abgelehnt, Neustart, Grenzen für Bezug und Einspeisung (auch beide in einer Nachricht), Ablauf, Failsafe-Werte inklusive Speicherung, Heartbeat-Ausfall beider Richtungen, Verbindungsabbrüche, Nennleistungen, Messwerte MPC und MGCP, Ablehnung ohne SPS, Anlagenstatus. **Noch nicht gegen eine echte Steuerbox.**
- **Begrenzung nach Neustart oder Stromausfall:** Das Gateway speichert die aktive Grenze des Netzbetreibers nicht. Nach dem Start gilt die Failsafe-Grenze, nach dem ersten Heartbeat ist der Bezug unbegrenzt, bis die Steuerbox erneut eine Grenze schreibt. Klären, ob eine echte Steuerbox das nach dem Wiederverbinden tut.
- **Zustandsautomat:** Die Übergänge in `gateway/begrenzung.go` (Init geht nach 120 s ohne Heartbeat in Failsafe; Failsafe wird nur mit Heartbeat und neuer Grenze oder nach der Mindestdauer verlassen) gegen die Spezifikationen „Limitation of Power Consumption/Production“ und das FNN-Lastenheft Steuerbox 1.4 prüfen. Der FNN-Hinweis nennt keine Update-Raten für Messwerte. Das Gateway sendet bei jeder Änderung, gegebenenfalls ein Totband ergänzen.
- **MPC auf der CEM-Entität:** Ob die PROLAN-Steuerbox MPC dort liest, ist offen. Sonst `MPC_ENTITAET=submeter`.
- **Verbindungsstatus:** Beim allerersten Test mit dem eebus-go-Beispiel kam nach dem Stoppen der Gegenseite keine Trennungsmeldung, in allen späteren Tests sofort. Bei der echten Steuerbox beobachten. Für die Grenze ist das unkritisch, dort entscheidet der Heartbeat.
- **Zertifizierung:** Das Gateway ist nicht EEBUS-zertifiziert. Für Pilot- und Eigenanlagen ausreichend, für Serienanlagen vorher mit Netzbetreiber bzw. Messstellenbetreiber klären.
- **Docker auf dem PFC200:** Nur ab neueren Firmware-Ständen verfügbar, bei gemischtem Gerätepark vorab je Steuerung prüfen. Docker-Datenverzeichnis wegen begrenztem internem Speicher möglichst auf die SD-Karte legen.
- **CODESYS-Bausteine** der Erweiterung 2 noch nicht in CODESYS kompiliert und nicht auf dem PFC getestet.

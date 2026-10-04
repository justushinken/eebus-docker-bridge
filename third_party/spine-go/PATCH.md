# spine-go mit Patch

Kopie von `github.com/enbility/spine-go` im Stand `v0.7.1-0.20260520153416-0104ce40c885`
(der von eebus-go `8583642861c3` verwendete Stand), ohne Tests und Doku. Die
Mocks sind dabei, weil `go mod tidy` sie für die Tests von eebus-go auflöst.
Mit `gofmt` formatiert, sonst unverändert bis auf die Stelle unten.
Eingebunden über `replace` in `go.mod`. Lizenz: MIT, siehe `LICENSE`.

## Geänderte Stelle

`spine/feature_local.go`, `FeatureLocal.ApproveOrDenyWrite`: Hat ein Server-Feature
mehrere Freigabe-Callbacks, zählt spine-go die Freigaben je Schreibanfrage
(msgCounter) in einer Map je SKI. Das Original legt diese Map bei jeder noch nicht
gezählten msgCounter neu an und verwirft damit die Zählstände anderer gleichzeitig
offener Anfragen. Folge: Zwei schnell aufeinanderfolgende Schreibanfragen der
Steuerbox werden nie vollständig freigegeben und laufen in den Timeout
("write not approved in time by application").

Das betrifft die Brücke, sobald LPC und LPP auf derselben Entität laufen: Dann
haben LoadControl und DeviceConfiguration je zwei Callbacks. Beobachtet beim
Senden von Failsafe-Grenze und Failsafe-Mindestdauer direkt hintereinander.

## Entfernen

Sobald spine-go den Fehler behebt (Stand 03.10.2026 auch auf `dev` noch vorhanden):
`replace`-Zeile in `go.mod` und diesen Ordner löschen, `COPY third_party` im
Dockerfile entfernen. Fehler upstream melden: https://github.com/enbility/spine-go

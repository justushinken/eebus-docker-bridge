# Beispiel-Steuerbox (Energy Guard) aus eebus-go, nur fuer lokale Tests.
# Sendet nach dem Verbinden eine LPC-Grenze von 7000 W fuer 2 Minuten.
FROM golang:1.23-alpine AS bau
RUN CGO_ENABLED=0 go install github.com/enbility/eebus-go/cmd/controlbox@v0.7.0

FROM alpine:3.20
COPY --from=bau /go/bin/controlbox /usr/local/bin/controlbox
ENTRYPOINT ["controlbox"]

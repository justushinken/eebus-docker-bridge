# Ein Dockerfile fuer alle Programme im Repo, Auswahl per PROGRAMM.
# Go-Kompilat fuer die Zielplattform, Laufzeit-Image "scratch".
#
# Bruecke fuer den PFC200 (ARMv7):
#   docker buildx build --platform linux/arm/v7 -t eebus-bruecke:0.3 --load .
# Test-Steuerbox:
#   docker buildx build --platform linux/arm/v7 --build-arg PROGRAMM=testwerkzeuge/steuerbox -t eebus-steuerbox:0.3 --load .
# Fuer lokale Tests auf dem PC (amd64) ohne --platform bauen, oder docker compose verwenden.

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS bau
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG PROGRAMM=bruecke
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY bruecke ./bruecke
COPY testwerkzeuge ./testwerkzeuge
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /programm ./$PROGRAMM

FROM scratch
COPY --from=bau /programm /programm
# Zertifikat und Schluessel liegen hier, unbedingt als Volume einbinden
VOLUME ["/data"]
ENTRYPOINT ["/programm"]

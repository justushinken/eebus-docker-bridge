# Ein Dockerfile fuer alle Programme im Repo, Auswahl per PROGRAMM.
# Go-Kompilat fuer die Zielplattform, Laufzeit-Image "scratch".
#
# Bruecke fuer den PFC200 (ARMv7):
#   docker buildx build --platform linux/arm/v7 -t eebus-bruecke:0.4 --load .
# Test-Steuerbox:
#   docker buildx build --platform linux/arm/v7 --build-arg PROGRAMM=testwerkzeuge/steuerbox -t eebus-steuerbox:0.4 --load .
# Fuer lokale Tests auf dem PC (amd64) ohne --platform bauen, oder docker compose verwenden.

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS bau
ARG TARGETOS TARGETARCH TARGETVARIANT
WORKDIR /src
COPY go.mod go.sum ./
# gepatchtes spine-go (replace in go.mod), siehe third_party/spine-go/PATCH.md
COPY third_party ./third_party
RUN go mod download
# Erst nach dem Download: So teilen sich alle Programme die Modul-Schicht im Cache.
ARG PROGRAMM=bruecke
ARG VERSION=dev
COPY internal ./internal
COPY bruecke ./bruecke
COPY testwerkzeuge ./testwerkzeuge
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X main.Version=$VERSION" -o /programm ./$PROGRAMM

FROM scratch
COPY --from=bau /programm /programm
# Zertifikat und Schluessel liegen hier, unbedingt als Volume einbinden
VOLUME ["/data"]
ENTRYPOINT ["/programm"]

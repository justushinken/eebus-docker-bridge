# Mehrstufiger Build: Go-Kompilat fuer die Zielplattform, Laufzeit-Image "scratch".
# Fuer den PFC200 (ARMv7) auf dem Entwicklungsrechner:
#   docker buildx build --platform linux/arm/v7 -t eebus-bruecke:0.1 --load .
# Fuer lokale Tests auf dem PC (amd64) ohne --platform bauen.

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS bau
ARG TARGETOS TARGETARCH TARGETVARIANT
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /eebus-bruecke .

FROM scratch
COPY --from=bau /eebus-bruecke /eebus-bruecke
# Zertifikat und Schluessel liegen hier, unbedingt als Volume einbinden
VOLUME ["/data"]
ENTRYPOINT ["/eebus-bruecke"]

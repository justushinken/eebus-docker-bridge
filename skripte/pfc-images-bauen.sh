#!/bin/sh
# Baut Bruecke und Test-Steuerbox fuer den PFC200 (ARMv7) und legt sie als
# .tar.gz unter dist/ ab. Aus dem Repo-Wurzelverzeichnis aufrufen:
#
#   sh skripte/pfc-images-bauen.sh 0.5                                    (Git Bash)
#   & "C:\Program Files\Git\bin\sh.exe" skripte/pfc-images-bauen.sh 0.5   (PowerShell)
#
# Danach z. B.: scp dist/eebus-bruecke-0.5.tar.gz root@<pfc-ip>:/home/eebus-bruecke/
set -e

version=${1:?Version angeben, z. B. 0.5}
mkdir -p dist

baue() {
	name=$1
	programm=$2
	echo "== $name:$version ($programm)"
	docker buildx build --platform linux/arm/v7 --build-arg PROGRAMM="$programm" --build-arg VERSION="$version" \
		-t "$name:$version" -t "$name:latest" --load .
	# Beide Namen ins Archiv: docker load setzt latest dann auf diese Version
	docker save "$name:$version" "$name:latest" | gzip > "dist/$name-$version.tar.gz"
}

baue eebus-bruecke bruecke
baue eebus-steuerbox testwerkzeuge/steuerbox

ls -l dist/*-"$version".tar.gz

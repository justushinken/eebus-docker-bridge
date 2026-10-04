#!/bin/sh
# Baut Bruecke und Test-Steuerbox fuer den PFC200 (ARMv7) und legt sie als
# .tar.gz unter dist/ ab. Aus dem Repo-Wurzelverzeichnis aufrufen:
#
#   sh skripte/pfc-images-bauen.sh 0.3
#
# Danach z. B.: scp dist/eebus-bruecke-0.3.tar.gz root@<pfc-ip>:/home/eebus-bruecke/
set -e

version=${1:?Version angeben, z. B. 0.3}
mkdir -p dist

baue() {
	name=$1
	programm=$2
	echo "== $name:$version ($programm)"
	docker buildx build --platform linux/arm/v7 --build-arg PROGRAMM="$programm" --build-arg VERSION="$version" \
		-t "$name:$version" --load .
	docker save "$name:$version" | gzip > "dist/$name-$version.tar.gz"
}

baue eebus-bruecke bruecke
baue eebus-steuerbox testwerkzeuge/steuerbox

ls -l dist/*-"$version".tar.gz

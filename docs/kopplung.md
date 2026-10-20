# Kopplung mit der Steuerbox

Gateway und Steuerbox müssen einander vertrauen, **jede Seite trägt die andere ein**. Auf Seiten der Steuerbox macht das der Messstellenbetreiber, bei PROLAN nur aus der Ferne. Welches Verfahren gilt, legt der Messstellenbetreiber fest. Gekoppelt ist immer genau eine Steuerbox, eine neue Kopplung ersetzt die alte.

## Verfahren

**A · SHIP Pairing Service (neues Verfahren, Vorgabe an).** Das Gateway erzeugt beim ersten Start ein zufälliges Secret. Die Statusseite zeigt unter „Kopplung“ einen QR-Code mit SKI, SHIP-ID, SHA-256-Fingerprint und Secret (`SHIP;SKI:…;ID:…;FPH256:…;SPSEC:…;ENDSHIP;`). Der Messstellenbetreiber trägt ihn in die Steuerbox ein. Die Steuerbox kündigt sich dann per mDNS an und beweist per HMAC, dass sie das Secret kennt. Das Gateway vertraut ihr daraufhin automatisch und speichert die Kopplung. Ein Neustart ist nicht nötig. Wird die Steuerbox getauscht, koppelt sich die neue nach 15 Minuten selbst, sofern sie das Secret kennt. Das Secret wie einen Schlüssel behandeln und nur dem Messstellenbetreiber geben.

**B · SKI-Verfahren.** SKI und SHIP-ID des Gateways an den Messstellenbetreiber geben. Auf der Statusseite unter „Per mDNS gefundene EEBUS-Geräte“ bei der Steuerbox „Vertrauen“ wählen. Die Reihenfolge ist egal. Die Verbindung kommt, sobald beide Seiten eingetragen sind. Bis dahin steht dort „vertraut – wartet auf Steuerbox“, danach „verbunden“. Die Kopplung landet in `steuerbox-ski.json`.

Taucht die Steuerbox nicht per mDNS auf, den **Suchmodus** aufklappen und „Steuerbox suchen“ drücken: Für 10 Minuten werden Verbindungsversuche unbekannter Geräte nicht abgewiesen, sondern als Kopplungsanfrage angezeigt. Meldet sich die Steuerbox, „Annehmen“. Das klappt auch, wenn das Gateway die Steuerbox nicht sieht, die Steuerbox das Gateway aber erreicht. Ohne Suchmodus meldet das Gateway abgelehnte Versuche unbekannter Geräte einmal im Ereignisprotokoll. Eine automatische Annahme gibt es bewusst nicht, sonst könnte jedes Gerät im LAN Grenzen setzen.

**C · SKI fest per Einstellung.** Den SKI der Steuerbox als `EEBUS_REMOTE_SKI` setzen und den Container neu anlegen. Dann gilt nur diese Steuerbox: Pairing Service, Suchmodus und Kopplung im UI sind aus, eine gespeicherte Kopplung wird ignoriert.

Den Pairing Service mit vertauschten Rollen (Gateway kündigt sich an) gibt es nicht: Das ginge nur, wenn die Steuerbox selbst einen QR-Code mit Secret zeigt.

## Variablen

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `EEBUS_PAIRING_SERVICE` | `an` | `aus` schaltet den Pairing Service ab |
| `EEBUS_REMOTE_SKI` | – | SKI der Steuerbox, fest eingestellt (Verfahren C), schaltet den Pairing Service ab |
| `SHIP_ID` | `<Marke>-<Modell>-<MAC>` | Kennung des Gateways im Netz, z. B. `Demo-EEBUS-Gateway-0030DE683ADC` |

## SHIP-ID

Die SHIP-ID ist der dauerhafte Name des Gateways im EEBUS-Netz. Sie wird per mDNS verkündet und steht im QR-Code. Über sie findet die Steuerbox das Gateway wieder, und beim Pairing Service richtet die Steuerbox ihre Ankündigung genau an diese ID. Laut SHIP-Spezifikation muss sie **weltweit eindeutig** sein und darf sich **nicht ändern**.

- **Aufbau:** frei wählbarer Text, höchstens 63 Byte, ohne Leerzeichen, Semikolons und Steuerzeichen. Er soll mit einem Herstellerkürzel beginnen, dahinter folgt eine eindeutige Kennung. Das Gateway bildet `<GERAET_MARKE>-<GERAET_MODELL>-<MAC>`, z. B. `Demo-EEBUS-Gateway-0030DE683ADC`.
- **MAC-Adresse:** Mit `--network host` sieht der Container die echten Schnittstellen und nimmt auf dem PFC bevorzugt `br0` (X1). Das ist die MAC vom Typenschild, sie bleibt bei Neustarts und Updates gleich. Prüfen: `cat /sys/class/net/br0/address` muss zum Ende der SHIP-ID passen. Ohne Host-Netz (z. B. lokal mit Compose) erzeugt Docker eine eigene MAC, die sich ändern kann.
- **IANA-Format:** EEBUS empfiehlt für neue Geräte `i:<IANA-Nummer>_u:<Seriennummer>`. Das ist keine Pflicht, Geräte müssen auch andere Formate akzeptieren.
- **SHIP-ID und SKI sind verschieden:** SKI und Fingerprint werden aus dem Zertifikat berechnet. Die SHIP-ID ist ein Name, der auch bei einem neuen Zertifikat bleibt.

## Vor der Übergabe an den Messstellenbetreiber

Was danach geändert wird, erzwingt ein neues Pairing. Deshalb vorher:

1. **`GERAET_MARKE` auf ein Kürzel der eigenen Firma setzen** statt `Demo`. Die Marke erscheint in der SHIP-ID, im QR-Code und bei der Steuerbox als Gerätename.
2. **SHIP-ID festschreiben:** Die SHIP-ID von der Statusseite kopieren und mit `-e SHIP_ID=…` im `docker run` eintragen. Danach hängt sie nicht mehr an der MAC, und auch ein Tausch der Steuerung (mit übertragenem Datenverzeichnis) ändert sie nicht. **Ab der Übergabe SHIP-ID und Datenverzeichnis nicht mehr ändern.**
3. **Datenverzeichnis sichern** (siehe [Betrieb](betrieb.md#datenverzeichnis)): Zertifikat, Secret und Kopplung.
4. **Netzwerk mit dem Installateur abstimmen**, siehe unten.
5. **Verfahren klären:** Pairing Service (QR-Code übergeben) oder SKI-Verfahren (SKI und SHIP-ID übergeben, am Gateway „Vertrauen“).
6. **Use Cases festlegen:** `EEBUS_USECASES` passend zur Anlage, die Steuerbox muss sie ebenfalls aktiviert haben.

## Netzwerk

**Vorgabe für den Installateur:** Steuerbox und Steuerung (beim PFC X1) ins selbe Netz, **selbes Subnetz** (z. B. beide in 192.168.1.0/24), kein VLAN und kein Router dazwischen. DHCP oder feste Adressen sind egal, solange sie im selben Bereich liegen.

Die Geräte finden sich per **mDNS** (Multicast-DNS, UDP 5353 an 224.0.0.251). Diese Pakete verlassen das eigene Netzsegment nicht. Danach läuft die eigentliche Verbindung als verschlüsselter WebSocket per TCP zu der Adresse, die mDNS geliefert hat. Daher braucht es beides: dasselbe Segment zum Finden und gegenseitige IP-Erreichbarkeit zum Verbinden.

| Fall | Finden | Verbinden | Ergebnis |
|---|---|---|---|
| Gleiches Segment, gleiches Subnetz (192.168.1.10 und 192.168.1.20, /24) | ✅ | ✅ | funktioniert |
| Gleiches Segment, verschiedene Subnetze (192.168.1.10 und 192.168.2.20) | ✅ | ❌ nur über Router | praktisch nicht |
| Verschiedene Segmente oder VLANs, über Router verbunden | ❌ ohne mDNS-Repeater | ✅ | nur mit Zusatzaufwand am Router |

| Voraussetzung | Warum |
|---|---|
| **Multicast nicht blockiert** | Verwaltete Switches mit IGMP-Snooping ohne Querier, „Multicast-Filter“ oder WLAN mit Client-Isolation können mDNS verschlucken |
| **Firewall der Steuerung:** UDP 5353 und TCP 4712 eingehend erlaubt | 4712 ist der EEBUS-Port des Gateways. Die Steuerbox verbindet sich dorthin, oder das Gateway zu ihr |
| **IP-Adressen im selben Bereich** (DHCP oder fest) | Ohne DHCP vergeben EEBUS-Geräte sich laut Spezifikation selbst eine 169.254.x.x-Adresse. Das klappt nur, wenn beide Seiten das tun |

Läuft auf der Steuerung ein Avahi-Daemon, nutzt das Gateway ihn, wenn der Container `-v /var/run/dbus:/var/run/dbus` hat. Avahi kündigt je Schnittstelle nur deren eigene Adresse an. Ohne Avahi (auf dem PFC bisher der Fall) nutzt das Gateway sein eingebautes Zeroconf, das ebenfalls UDP 5353 belegt. Den Grund nennt die Startmeldung „mDNS über Zeroconf, Avahi nicht nutzbar: …“.

Mit dem Installateur bzw. Messstellenbetreiber klären:

- In welches Netz kommt der LAN-Anschluss der Steuerbox? Am einfachsten in dasselbe Netz wie X1 des PFC.
- Wie werden die IP-Adressen vergeben: DHCP oder fest? Gibt es VLANs?
- Ist ein verwalteter Switch mit Multicast-Filter dazwischen?
- Alternative mit sauberer Trennung: Steuerbox direkt an X2, siehe nächster Abschnitt.

Zur Kontrolle zeigt die Statusseite unter „Per mDNS gefundene EEBUS-Geräte“, ob die Steuerbox gesehen wird. Die Liste wird laufend aktualisiert, eine neue Steuerbox erscheint spätestens nach etwa einer Minute. Erscheint sie dort nicht, liegt es am Netz, nicht an der Kopplung.

### Getrenntes Netz an X2

Die EEBUS-Buchse der Steuerbox (bei PROLAN und Swistec „ETH1“) kommt per Kabel direkt an X2 des PFC. X2 wird im WBM als eigene Schnittstelle eingerichtet (nicht als Switch mit X1), auf dem PFC heißt sie dann `br1`. Die Adresse von X2 muss im Subnetz der Steuerbox liegen, z. B. Steuerbox 192.168.1.100/24, X2 192.168.1.74/24. Ein Gateway (Router) ist nicht nötig.

Dazu **`EEBUS_SCHNITTSTELLEN=br1`** setzen. Ohne diese Einstellung kündigt das Gateway auf jeder Schnittstelle alle seine Adressen an, auch die von X1 und docker0 (172.17.0.1). Die Steuerbox bekommt dann über X2 zuerst die Adresse von X1 und erreicht sie nicht. Grund: Ohne Avahi-Daemon auf dem PFC nutzt ship-go das eingebaute Zeroconf, und das sammelt die Adressen aller Schnittstellen in einer Liste. Mit `EEBUS_SCHNITTSTELLEN` sucht und kündigt das Gateway nur auf diesen Schnittstellen an. Der EEBUS-Port 4712 und das Web-UI bleiben auf allen Schnittstellen erreichbar. Beim Start stehen im Ereignisprotokoll die Zeilen „mDNS nur auf br1 (…)“ und „mDNS über Zeroconf“ bzw. „über den Avahi-Daemon“, jeweils mit dem Grund.

Am 20.10.2026 mit der Swistec SwisBox an X2 aufgebaut: Ohne `EEBUS_SCHNITTSTELLEN` stand in der Ankündigung die unerreichbare Adresse von X1 vorn. Mit `EEBUS_SCHNITTSTELLEN` (ab 0.6) ist das auf dem PFC noch nicht getestet, lokal schon. Noch offen ist, ob ship-go IPv6-Link-Local-Adressen (ohne IPv4) korrekt anspricht.

### Fehlersuche bei der Verbindung

Der Reiter „Diagnose“ zeigt unter **„Verbindungsversuche“** den letzten Versuch in beide Richtungen, auch ohne `EEBUS_DEBUG`. Die Statusleiste nennt bei „Getrennt“ den letzten Fehler. Im Ereignisprotokoll steht jeder neue Fehler einmal, also erst wieder, wenn sich Adresse oder Grund ändern.

| Anzeige | Bedeutung |
|---|---|
| Port geschlossen (TCP-RST) | Die Steuerbox ist im Netz erreichbar, nimmt auf dem per mDNS angekündigten Port aber keine Verbindung an. Liegt an der Steuerbox: EEBUS-Dienst aus oder nicht freigeschaltet. |
| keine Antwort | Adresse aus einem anderen Netz, Firewall oder Kabel. Subnetz und `EEBUS_SCHNITTSTELLEN` prüfen. |
| nicht erreichbar | Keine Antwort auf ARP oder keine Route ins Netz der Adresse. |
| TLS fehlgeschlagen | Meist vertraut eine Seite dem Zertifikat (SKI) der anderen nicht. „EOF“ ist meist nur eine Portprüfung, z. B. `Test-NetConnection`. |
| Noch kein Versuch | Das Gateway hat die gekoppelte Steuerbox per mDNS nicht gefunden. Ob die Steuerbox es versucht, steht unter „Steuerbox → Gateway“. |

Eingehend kennt das Gateway die Adresse der Gegenstelle nur bei einem TLS-Fehler, nach erfolgreichem TLS nur deren SKI.

Befehle auf dem PFC (als root):

```sh
docker logs -f eebus-gateway                              # Zeiten in UTC, das UI zeigt Ortszeit
tcpdump -l -ni br1 -vv udp port 5353                      # mDNS-Ankündigungen beider Seiten
tcpdump -l -ni br1 'host 192.168.1.100 and tcp'           # Verbindungsversuche von und zur Steuerbox
ip -4 addr; netstat -tlnp | grep 4712                     # Adressen, lauscht das Gateway?
```

Vom Laptop im selben Netz: `Test-NetConnection 192.168.1.74 -Port 4712` (PowerShell) prüft, ob der EEBUS-Port des Gateways erreichbar ist. Das Gateway meldet den Versuch als „TLS fehlgeschlagen (EOF)“. „bad udp cksum“ im tcpdump ist harmlos (Checksum-Offload). Die Meldung `lookup <SHIP-ID>.local. on [::1]:53` aus `EEBUS_DEBUG` ist ebenfalls harmlos: Den Namen aus dem mDNS löst der Container nicht auf, ship-go verbindet sich danach über die IP. Seit 0.6 unterdrückt das Gateway diese Zeile.

## Swistec SwisBox

Die erste echte Steuerbox am Gateway (Inbetriebnahme ab 20.10.2026):

- **Anschluss:** EEBUS über die Buchse **ETH1**. In der ersten Anlage mit fester IP 192.168.1.100/24 ohne Gateway, direkt an X2 des PFC (siehe [Getrenntes Netz an X2](#getrenntes-netz-an-x2)).
- **Kopplung:** derzeit **nur SKI-Verfahren**, kein SHIP Pairing Service. Am Gateway `EEBUS_PAIRING_SERVICE=aus` und `EEBUS_REMOTE_SKI=<SKI der Box>` setzen oder die Box im UI „Vertrauen“. Der Messstellenbetreiber trägt den SKI des Gateways in seine Konfiguration ein (FNN-XML, `ski1`).
- **SHIP-Port dynamisch:** Die Box wählt den Port bei jedem Neustart ihres Dienstes neu (beobachtet 51555, 51658, 54375) und kündigt ihn per mDNS an. Eine Freigabe für einen festen Port auf ihrer Seite gibt es also nicht.
- **Konfiguration des Messstellenbetreibers** (erste Anlage): CLS2, nur Use Case LPC, Failsafe 11 000 W für 7200 s, Softstart in Stufen 0/3300/6600/11 000 W zu je 60 s. Passend dazu am Gateway `EEBUS_USECASES=lpc`, `FAILSAFE_GRENZE_W=11000`, `FAILSAFE_MINDESTDAUER=2h`.
- **Stand 20.10.2026:** Die Box kündigt sich per mDNS an, setzt aber jede Verbindung auf ihrem SHIP-Port sofort zurück (TCP-RST, auch nach Neustart der Box) und baut selbst keine Verbindung zum Gateway auf. Die Seite des Gateways ist geprüft: SKI stabil, Box vertraut, Port 4712 von außen erreichbar, Ankündigung im richtigen Netz. Die Anfrage an Messstellenbetreiber und Swistec läuft: Ist die signierte Konfiguration mit `ski1` aktiv und EEBUS auf ETH1 freigeschaltet? Arbeitet die Box als Client oder Server? Warum ist der angekündigte Port geschlossen? Gibt es ein Log der Box?

## PROLAN-Steuerbox (STB-142E)

Die erste echte Steuerbox am Gateway. Stand der Recherche (Gebrauchsanleitung v1.1, Datenblatt 07/2025, BSI-Zertifikatsliste):

- **Typen:** STB-142 nur mit Relais, **STB-142E** mit aktivierbarer EEBUS-Schnittstelle, beide nach FNN-Lastenheft 1.4. Zertifiziert ist die STB-142E mit HW 3.4 und FW 1.1.3 (TR-03109-5 `BSI-K-TR-0907-2026`, BSZ `BSI-DSZ-BSZ-0025-2026`). Laut Sekundärquellen unterstützt sie LPC, LPP, MPC und MGCP.
- **Anschluss:** EEBUS über die Buchse **ETH1**, die Buchse **CLS** geht zum Smart Meter Gateway. ETH1 dient auch zur Kaskadierung weiterer Steuerboxen.
- **Verplombung:** Aus dem verplombten Bereich dürfen nur Kabel zu EEBUS-Geräten herausgeführt werden. Kann ETH1 kaskadieren, darf die Buchse außerhalb nicht erreichbar sein: Der Messstellenbetreiber muss die Kaskadierung dann abschalten.
- **Kopplung:** Der Messstellenbetreiber (Steuerbox-Administrator) richtet den EEBUS-Partner aus der Ferne ein, vor Ort lässt sich nichts einstellen. Er braucht SKI und SHIP-ID des Gateways, beim Pairing Service zusätzlich Fingerprint und Secret (am einfachsten den QR-Code).
- **LED „PWR“:** blinkt im Sekundentakt, solange eine eingerichtete EEBUS-Verbindung fehlt.
- **Details** zu EEBUS (Use-Case-Versionen, Heartbeat, IP-Konfiguration von ETH1, Pairing Service) stehen im „Technischen Handbuch“, das es nur im Prolan-Kundenportal gibt.

Fragen an den Messstellenbetreiber vor dem Anschluss: Firmware-Stand (≥ 1.1.3) und EEBUS aktiviert? Welche Use Cases sind aktiv? Kopplung per SKI oder Pairing Service? Wie bekommt ETH1 seine IP-Adresse? Ist die Kaskadierung auf ETH1 abgeschaltet? Welche Failsafe-Werte und welcher Heartbeat-Takt? Zugang zum Technischen Handbuch?

Beim ersten Anschluss hilft die Karte „Steuerbox: gemeldete Daten“ im Reiter „Diagnose“: Sie zeigt, welche Use Cases die Box in welcher Version auf welcher Entität meldet. Liest die Box MPC nicht, `MPC_ENTITAET=submeter` versuchen. Für mehr Details `EEBUS_DEBUG=an` setzen.

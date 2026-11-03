### goat-cli — German (de). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = kein Profil für Kontext `{ $context }`
error-off-mesh-summary = außerhalb des ZTOL — nicht mit dem Mesh verbunden
error-router-unreachable-summary = Zenoh-Router unter `{ $endpoint }` ist nicht erreichbar
error-cert-expired-summary = mTLS-Zertifikat abgelaufen oder vom Router abgelehnt
error-portal-unreachable-summary = Portal-API unter `{ $endpoint }` ist nicht erreichbar
error-token-invalid-summary = Portal hat das Bearer-Token abgelehnt
error-topic-conflict-summary = Topic `{ $topic }` existiert bereits
error-bundle-invalid-summary = Überprüfung der Bundle-Signatur oder des Ablaufs fehlgeschlagen

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = Router-Endpunkt fehlerhaft
doctor-step1-endpoint-malformed-hint = erwartet `<proto>/<host>:<port>`; erhalten `{ $endpoint }`
doctor-step1-ok-summary = TCP-Verbindung zum Router-Host { $host }:{ $port } erfolgreich
doctor-step1-conn-failed-summary = TCP-Verbindung zum Router-Host fehlgeschlagen
doctor-step1-conn-failed-hint = { $err } — netbird starten (`sudo netbird up --management-url <mgmt> --setup-key <key>`); `netbird status` prüfen
doctor-step1-timeout-summary = Zeitüberschreitung bei TCP-Verbindung zum Router-Host
doctor-step1-timeout-hint = Router-Host { $host }:{ $port } in { $ms }ms nicht erreichbar — wahrscheinlich außerhalb des Mesh
doctor-step2-endpoint-malformed-hint = Bundle-Aussteller korrigieren (--goat-cli-router-endpoint muss `<proto>/<host>:<port>` sein)
doctor-step2-ca-missing-summary = CA-Roots-PEM fehlt oder nicht lesbar
doctor-step2-ca-missing-hint = { $err } — `goat profile init <bundle.cbor>` erneut ausführen
doctor-step2-ok-summary = Router { $host }:{ $port } akzeptiert TCP (TLS in Schritt 3 validiert)
doctor-step2-refused-summary = Router-TCP abgelehnt
doctor-step2-refused-hint = { $err } — Router ist möglicherweise ausgefallen; Zenoh-Bereitstellung auf dem mgmt-host prüfen
doctor-step2-timeout-summary = Zeitüberschreitung bei Router-TCP
doctor-step2-timeout-hint = Router { $host }:{ $port } in { $ms }ms nicht erreichbar
doctor-step3-config-failed-summary = Erstellung der zenoh-Konfiguration fehlgeschlagen
doctor-step3-config-failed-hint = ungültiger Endpunkt `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = Erstellung der zenoh-TLS-Konfiguration fehlgeschlagen
doctor-step3-tls-config-failed-hint = { $err } — wahrscheinlich zenoh-Versionsabweichung; Abhängigkeiten prüfen
doctor-step3-ok-summary = mTLS-Sitzung geöffnet — Zertifikat von Router-ACL akzeptiert
doctor-step3-open-failed-summary = Öffnen der zenoh-Sitzung fehlgeschlagen
doctor-step3-open-failed-hint = { $err } — Zertifikat ist möglicherweise abgelaufen (`goat profile show` ausführen) oder von der Router-ACL abgelehnt (URI-SAN-Konflikt)
doctor-step3-timeout-summary = Zeitüberschreitung beim Öffnen der zenoh-Sitzung
doctor-step3-timeout-hint = Router hat den Handshake nicht in { $secs }s abgeschlossen — Zeitabweichung, falsches Zertifikat oder Überlastung
doctor-step4-ok-summary = Portal lieferte { $n } Organisationszeile(n)
doctor-step4-token-rejected-summary = Portal hat das Bearer-Token abgelehnt
doctor-step4-token-rejected-hint = HTTP { $status } — Token abgelaufen oder widerrufen; über operator-onboard.yml neu einbinden
doctor-step4-request-failed-summary = Portal-Anfrage fehlgeschlagen
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = Zeitüberschreitung bei Portal-Anfrage
doctor-step4-timeout-hint = keine Antwort in { $secs }s — prüfen, ob { $endpoint } aus dem Mesh erreichbar ist
doctor-skipped-summary = übersprungen — vorheriger Schritt fehlgeschlagen (mit --all erzwingen)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Einheitliche Operator-Befehlszeile für das goat-Daten-Fabric.
help-long-about = goat — operatororientiertes CLI, das panoscope-core zu einem Werkzeug vereint: Profil initialisieren/wiederverwenden, ZTOL-bewusster Erreichbarkeits-doctor, Organisations-portal durchsuchen, topic-CRUD, Pub/Sub, stethoscope-Delegation, ein interaktiver Befehls-Composer und Substrat-Kaskaden-Narration.
help-flag-profile-dir = Das Profilverzeichnis überschreiben (Standard: $GOAT_PROFILE_DIR oder ~/.goat).
help-flag-context = Den aktiven Kontext überschreiben (Standard: $GOAT_CONTEXT-Umgebung, dann current_context in config.toml, dann "default").
help-flag-narrate = Die Substrat-Kaskade anzeigen, die ein mutierender Befehl auslöst.
help-flag-verbose = Ausführliche Debug-Ausgabe aktivieren (oder RUST_LOG=goat=debug setzen).
help-flag-color = Farbmodus für die Terminalausgabe (auto|always|never).
help-flag-lang = Sprache für menschenlesbaren Text (z. B. en, uk). Fällt zurück auf GOAT_LANG, dann die OS-Locale, dann Englisch.
help-cmd-profile = Das lokale goat-Profil verwalten (mTLS-Zertifikat + portal-Token + Endpunktsatz).
help-cmd-context = Kontexte verwalten (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Einen schrittweisen Erreichbarkeits-Prober gegen das Fabric ausführen.
help-cmd-doctor-long = Schrittweiser Erreichbarkeits-Prober gegen das Fabric: ZTOL-Mitgliedschaft → Zenoh-Router → Zertifikatsakzeptanz → portal. Jeder fehlgeschlagene Schritt gibt einen spezifischen Behebungshinweis aus.
help-cmd-vendor = Im portal registrierte Organisationen durchsuchen.
help-cmd-topic = topics auflisten, anzeigen, erstellen oder löschen.
help-cmd-pub = Eine Nutzlast an einen topic-Schlüssel veröffentlichen.
help-cmd-sub = Einen topic-Schlüsselausdruck abonnieren.
help-cmd-inspect = Eine diagnostische Verdikt-Anfrage an stethoscope delegieren.
help-cmd-ui = Interaktiver zusammengeführter Auswähler: die deklarierte ⊕ live-Liste durchsuchen und zur Diagnose-/Tail-Lens auf dem ausgewählten Ziel springen.
help-cmd-compose = Interaktiver Befehlsbaukasten als TUI.
help-cmd-narrate = Substrat-Kaskaden-Reflexion über jüngste Aktivität. Noch nicht verfügbar (der alltägliche Weg ist --narrate auf einem mutierenden Befehl).
help-cmd-completion = Shell-Vervollständigungsskript nach stdout ausgeben.
help-cmd-explain = Die Bedeutung + Behebung eines Fehlercodes im eingebetteten `goat-cli-error-codes.md`-Dokument nachschlagen (cargo --explain-Muster).
help-cmd-mock = Synthetische Stichproben an einen topic veröffentlichen. Standard: triviales hello-world-JSON mit 1Hz. `--format <.fds>` + `--message <Name>` übergeben, um stattdessen operatorbereitgestelltes protobuf zu kodieren.
help-cmd-status = Zustandssynthese: ZTOL-Erreichbarkeit + portal /api/health + lokaler Zertifikatsablauf, drei glyph-Zeilen oder ein --json-Objekt.
help-cmd-services = Jeden gehosteten Dienst in `ops/serve/services.yaml` prüfen und eine glyph-Zeile pro Dienst rendern (oder ein --json-Objekt).
help-cmd-client = Den lokalen goat-clientd-daemon über seinen JSON-RPC-IPC-Socket (`~/.goat-client/goat-clientd.sock`) umhüllen: status / up / down / bundle import.
help-cmd-plugin = PATH-basierte Erweiterungserkennung (kubectl-Stil). Jede `goat-<name>`-ausführbare Datei im PATH ist als `goat <name>` aufrufbar; `goat plugin list` zählt installierte Plugins auf, `goat plugin which <name>` löst den Pfad auf.

# goat status / inspect / --narrate runtime prose (ADR 1024).
# Contract tokens + backticked keys + glyphs → — reproduced verbatim.
status-ztol-malformed = Router-Endpunkt fehlerhaft: `{ $endpoint }`
status-ztol-refused = TCP zu Router { $host }:{ $port } abgelehnt: { $err }
status-ztol-unreachable = außerhalb des Mesh (Router { $host }:{ $port } in { $ms }ms nicht erreichbar)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health fehlgeschlagen: { $err }
status-portal-timeout = portal /api/health Zeitüberschreitung nach { $secs }s
status-cert-read-failed = Lesen des Zertifikats fehlgeschlagen: { $err }
status-cert-pem-parse-failed = PEM-Parsen des Zertifikats fehlgeschlagen: { $err }
status-cert-wrong-tag = erster PEM-Block ist { $tag }, erwartet CERTIFICATE
status-cert-x509-parse-failed = x509-Parsen fehlgeschlagen: { $err }
status-cert-expired = Zertifikat ABGELAUFEN bei unix={ $not_after }, jetzt={ $now }
status-cert-valid = Zertifikat gültig; läuft in { $days_left }d ab (unix={ $not_after })
inspect-finding-label = Befund ({ $severity }): { $label } — { $detail }
inspect-remedy-label = Behebung: { $title } — { $detail }
inspect-no-outcomes = (keine Ergebnisse — Ziel traf auf keine Prüfungen zu)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh-put akzeptiert auf `{ $key }` ({ $bytes } Bytes)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = Kaskade abgeschlossen — { $summary } ({ $n } { $n ->
        [one] Folge
       *[other] Folgen
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Erstkonfigurationsassistent: Sandbox oder echt wählen, Funktion nachweisen, auf den tutor verweisen.
help-cmd-tutor = Eine selbstbestimmte, praktische Tour durch das Fabric (im Stil von vimtutor; fortsetzbar).
help-cmd-docs = Die goat-Dokumentation durchsuchen oder die vollständige Befehlsreferenz ausgeben (--markdown).
help-cmd-hello = Ein Hallo durchgängig durch das Fabric hin und zurück senden (Sandbox- oder echte Umgebung).
flourish-all-green = 🐐 alles grün — Fabric nominal

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = Nur-Operator-Aktion — keine Berechtigung (`goat admin` ist für Nicht-Admins wirkungslos)
help-flag-narrate-json = Gibt die Substrat-Kaskade als NDJSON aus (ein Objekt pro Konsequenz) für Incident-Logs und Scripting. Impliziert --narrate.
help-cmd-admin = Nur-Operator-Administration: Organisations-Lebenszyklus, Onboarding/Widerruf/Rotation, Mesh-Peer-Trennung, Geräte- und Operator-Tier-Provisionierung, LAN-Triage. Autorisiert durch Operator-Credentials (serverseitig via Portal-/NetBird-Token-Scope); wirkungslos für Nicht-Admins — ein nicht autorisierter Aufruf ändert nichts und schlägt fehl mit `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = Profil für Kontext `{ $context }` existiert, ist für diesen Benutzer aber nicht lesbar (Zugriff verweigert)
error-portal-bad-response-summary = Portal hat geantwortet, aber dieser goat-Build konnte die Antwort nicht parsen
doctor-step0-ok-summary = lokales Setup ist bereit für Netzwerk-Probes
doctor-step0-warn-summary = lokales Setup enthält Prüfungen, die nicht bestätigt werden konnten
doctor-step0-fail-summary = lokales Setup braucht Aufmerksamkeit
doctor-step0-profile-absent-summary = kein goat-Profil für Kontext `{ $context }`
doctor-step0-profile-new-hint = `goat context ls` prüfen; bei Ersteinrichtung `goat ca add <bundle-ca.pem>` ausführen und den Fingerprint mit dem deines Operators vergleichen, dann `goat profile init <bundle.cbor>`; `goat doctor` erneut ausführen
doctor-step0-profile-absent-hint = `goat context ls` prüfen; vorhandenen Kontext mit `goat context use <name>` wählen oder `goat profile init <bundle.cbor>` für diesen Kontext ausführen; `goat doctor` erneut ausführen
doctor-step0-profile-denied-summary = goat-Profil unter { $path } ist nicht lesbar
doctor-step0-profile-denied-hint = Profil-Besitzer korrigieren oder als besitzender Benutzer ausführen; vorhandenes Profil nicht neu initialisieren
doctor-step0-profile-incomplete-summary = goat-Profildatei { $file } fehlt oder ist nicht lesbar
doctor-step0-profile-incomplete-hint = die genannte Datei aus dem ausgestellten Bundle wiederherstellen oder diesen Kontext mit `goat profile rotate <bundle.cbor>` ersetzen; `goat doctor` erneut ausführen
doctor-step0-profile-invalid-summary = goat-Profil hat nicht lesbare oder fehlende Endpunkte
doctor-step0-profile-empty-token-summary = goat-Profil hat ein leeres Portal-Token
doctor-step0-profile-ok-summary = goat-Kontext `{ $context }` hat lesbares Profilmaterial
doctor-step0-client-absent-summary = kein unterstützter Mesh-Client hat geantwortet
doctor-step0-client-absent-hint = den von deinem goatnet bereitgestellten Mesh-Client installieren, dessen Daemon starten, dann `goat doctor` erneut ausführen
doctor-step0-client-stopped-summary = installierter Mesh-Client hat keinen brauchbaren Status geliefert
doctor-step0-client-stopped-hint = `netbird status -d` oder `goat client status --detail` prüfen; installierten Daemon starten (`sudo netbird service start` bei NetBird), dann `goat doctor` erneut ausführen
doctor-step0-netbird-ok-summary = NetBird-Daemon hat geantwortet (gemeldetes Profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd hat geantwortet (Zustand `{ $state }`)
doctor-step0-management-connected-summary = NetBird meldet Management verbunden
doctor-step0-management-disconnected-summary = NetBird meldet Management getrennt
doctor-step0-management-disconnected-hint = gemeldete Management-URL und `netbird status -d` prüfen; ein bestehender Tunnel kann bei Management-Ausfall weiter funktionieren
doctor-step0-management-unknown-summary = konfigurierter Management-Endpunkt konnte lokal nicht ermittelt werden
doctor-step0-management-unknown-hint = `netbird status -d` oder `goat client diagnostics` prüfen; doctor errät keine Management-URL
doctor-step0-tunnel-down-summary = NetBird meldet keine Mesh-Adresse
doctor-step0-tunnel-down-hint = `netbird status -d` ausführen; falls registriert und der Daemon antwortet, `goat heal` ausführen, danach `goat doctor` erneut starten
doctor-step0-tunnel-address-summary = NetBird meldet Mesh-Adresse { $address }; als Nächstes wird die Router-Erreichbarkeit geprüft
doctor-step0-goat-client-no-bundle-summary = goat-clientd hat kein importiertes Bundle
doctor-step0-goat-client-no-bundle-hint = `goat client bundle import <bundle.cbor>` mit dem Client-Enrollment-Bundle ausführen, Client verbinden, dann `goat doctor` erneut starten
doctor-step0-goat-client-connected-summary = goat-clientd meldet einen verbundenen Tunnel; als Nächstes wird die Router-Erreichbarkeit geprüft
doctor-step0-goat-client-outer-disabled-summary = Äußerer Tunnel von goat-clientd ist im netbird-only-Modus deaktiviert
doctor-step0-goat-client-down-summary = goat-clientd meldet Tunnelstatus `{ $state }`
doctor-step0-goat-client-down-hint = `goat client diagnostics` ausführen, danach `goat client up`, falls das Bundle geladen ist; `goat doctor` erneut starten
doctor-step0-goat-client-inner-ok-summary = goat-clientd meldet inneres Mesh verbunden
doctor-step0-goat-client-inner-disabled-summary = Inneres Mesh von goat-clientd ist im wg-cp0-only-Modus deaktiviert
doctor-step0-goat-client-inner-down-summary = goat-clientd meldet Status des inneren Mesh `{ $state }`
doctor-step0-goat-client-inner-down-hint = `goat client diagnostics` ausführen, um das innere Mesh zu prüfen; nach Wiederherstellung `goat doctor` erneut starten
doctor-step0-management-reachable-summary = Management-Endpunkt antwortete mit HTTP { $status }
doctor-step0-management-auth-summary = Management-HTTPS-Endpunkt ist erreichbar (HTTP { $status }: für diese Prüfung ist eine Autorisierung nötig)
doctor-step0-management-unexpected-summary = Management-Endpunkt lieferte unerwartetes HTTP { $status }
doctor-step0-management-unexpected-hint = konfigurierte Management-URL prüfen und den goatnet-Operator bitten, diese Antwort zu prüfen
doctor-step0-management-unreachable-summary = Konfigurierter Management-Endpunkt nicht erreichbar: { $error }
doctor-step0-management-unreachable-hint = DNS und Netzwerkzugriff auf die vom Client gemeldete URL prüfen; ein bestehender Tunnel kann trotzdem funktionieren
doctor-step0-clock-ok-summary = Lokale Uhr weicht höchstens { $seconds } Sekunden von der Management-HTTP-Zeit ab
doctor-step0-clock-skew-summary = Lokale Uhr weicht um { $seconds } Sekunden von der Management-HTTP-Zeit ab
doctor-step0-clock-skew-hint = Gerätezeit synchronisieren, dann `goat doctor` erneut starten; Zertifikatsprüfung kann bei falscher Uhr fehlschlagen
doctor-step0-clock-unknown-summary = Zeitabweichung konnte nicht aus einem Management-HTTP-Date-Header ermittelt werden
doctor-step0-clock-unknown-hint = Zeitsynchronisation des Geräts prüfen, falls TLS- oder Zertifikatsprüfungen fehlschlagen
error-clock-skew-summary = die Geräteuhr weicht zu stark von der Managementzeit ab
error-doctor-inconclusive-summary = doctor hat einen Fehler ohne nachgewiesene Ursache beobachtet
error-portal-forbidden-summary = das Portal hat diesem Konto den Zugriff auf die geprüfte API verweigert
doctor-step4-forbidden-summary = Portal lieferte HTTP 403 für dieses Profil
doctor-step4-forbidden-hint = diese Zugangsdaten haben das Portal erreicht, haben aber keinen Zugriff auf die geprüfte API; `goat profile show` prüfen und den goatnet-Operator bitten, die Freigabe zu verifizieren
doctor-step4-decode-summary = Portal antwortete mit einer Antwort, die dieses goat nicht lesen kann
doctor-step4-decode-hint = { $err } — goat auf die von diesem goatnet bereitgestellte Version aktualisieren, dann `goat doctor` erneut ausführen
doctor-step1-control-only-summary = nur mit dem Control-Server verbunden — kein Tunnel zu einem der { $total } Peers
doctor-step1-control-only-hint = dieses Netzwerk lässt TCP 443 nach außen, blockiert aber die Mesh-Datenports; daher ist nichts im Mesh (auch keine Anmeldeseiten) von diesem Gerät erreichbar. Netzwerkteam bitten, ausgehendes TCP und UDP zu { $relays } sowie UDP 3478 zu { $stun } freizugeben; direktes UDP zwischen Geräten wird ebenfalls empfohlen. Zur Prüfung einem Smartphone-Hotspot beitreten und `netbird status` ausführen: Peers sollten sich dann verbinden.
doctor-offmesh-dashboard-hint = Relay-Flotte (externer Messpunkt): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokales Zertifikat, passender Schlüssel und CA-Bundle lassen sich parsen; Zertifikat liegt im Gültigkeitszeitraum
doctor-step2-material-failed-summary = lokales TLS-Material { $file } konnte nicht verwendet werden
doctor-step2-material-failed-hint = { $err } — genannte Datei prüfen; aus dem ausgestellten Bundle wiederherstellen oder das Profil rotieren
doctor-step2-material-invalid-hint = { $file } ist kein gültiges X.509-PEM-Zertifikat; ausgestelltes Material wiederherstellen oder `goat profile rotate <bundle.cbor>` ausführen
doctor-step2-key-invalid-hint = mtls.key.pem ist ungültig oder passt nicht zu mtls.cert.pem; ausgestelltes Material wiederherstellen oder `goat profile rotate <bundle.cbor>` ausführen
doctor-step2-cert-time-summary = lokales Zertifikat liegt außerhalb seines Gültigkeitszeitraums
doctor-step2-cert-time-hint = `goat profile show` ausführen; oben das Uhr-Ergebnis prüfen, dann beim Operator ein neues Bundle anfordern und bei abgelaufenem Zertifikat `goat profile rotate <new-bundle.cbor>` ausführen
doctor-skipped-no-endpoint-summary = übersprungen — in profile.toml fehlt { $field }
doctor-skipped-no-profile-summary = übersprungen — keine lesbaren Profil-Endpunkte für diesen Kontext
doctor-skipped-no-portal-token-summary = übersprungen — kein lesbares Portal-Token für diesen Kontext
status-ztol-tcp-ok = Router-Port { $host }:{ $port } akzeptiert TCP (nur Port — siehe Session)
status-session-ok = Zenoh-mTLS-Session in { $ms }ms geöffnet (Router hat dieses Zertifikat akzeptiert)
status-session-skipped = Session nicht versucht (außerhalb des Mesh; siehe ztol)
status-session-material-missing = Zenoh-Session nicht versucht: { $detail }
status-session-config-failed = Zenoh-Session-Konfiguration fehlgeschlagen: { $err }
status-session-failed = Zenoh-Session nicht geöffnet: { $err } — `goat doctor` ausführen
status-session-timeout = Zenoh-Session nicht geöffnet in { $secs }s — Router-Port nimmt TCP an, aber keine Session kommt zustande (Router hängt, Zertifikat abgelehnt oder Zeitabweichung); `goat doctor` ausführen
status-names-live = Namen live (Mesh-DNS hat geantwortet)
status-names-ip-only = Namen nicht geprüft (Profil-Endpunkte sind IP-Literale)
status-names-snapshot = Namen laut signiertem Snapshot Serial { $serial }, Alter { $age } (Mesh-DNS nicht erreichbar)
status-names-snapshot-aging = Namen laut signiertem Snapshot Serial { $serial }, Alter { $age } — VERALTET (Mesh-DNS nicht erreichbar; goat-coredns reparieren, danach aktualisieren sich die Namen automatisch)
status-names-snapshot-daemon = Namen laut signiertem Snapshot Serial { $serial }, Alter { $age } — Daemon-Store (Mesh-DNS nicht erreichbar)
status-names-snapshot-daemon-aging = Namen laut signiertem Snapshot Serial { $serial }, Alter { $age } — Daemon-Store, VERALTET (Mesh-DNS nicht erreichbar; goat-coredns reparieren, danach aktualisieren sich die Namen automatisch)
status-names-failed = Mesh-DNS nicht erreichbar und Snapshot-Fallback nicht verfügbar — { $reason }
status-pqc-on = WireGuard-PQC (Rosenpass) AN — { $qr }/{ $connected } verbundene Peers quantenresistent
status-pqc-off = WireGuard-PQC (Rosenpass) AUS — klassische WireGuard-Handshakes
status-pqc-unknown = WireGuard-PQC unbekannt — { $reason }
status-device-none = geräteweiter Namensschutz AUS — kein goat-clientd-Namensspeicher (goat-Befehle haben weiterhin Fallback; aktivieren gemäß name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd-Namensspeicher vorhanden (Serial { $serial }), aber sein Forwarder antwortet NICHT auf { $addr } — Daemon down oder Namens-Subsystem hängt
status-device-active = geräteweiter Namensschutz AKTIV — goat-clientd steht vor dem OS-Mesh-DNS unter { $addr } (Store-Serial { $serial }, { $grade })
status-device-not-fronted = goat-clientd-Forwarder läuft unter { $addr } (Store-Serial { $serial }, { $grade }) — OS nicht vorgeschaltet; Browser nutzen direktes Mesh-DNS
status-device-fronting-unknown = goat-clientd-Forwarder läuft unter { $addr } (Store-Serial { $serial }, { $grade }) — Vorschaltstatus des OS auf dieser Plattform nicht erkennbar
status-names-observed = Namen NICHT KANONISCH (ad hoc): beobachteter Eintrag, Alter { $age } — nicht im signierten Snapshot (Mesh-DNS nicht erreichbar; Eintrag kodifizieren)
status-names-selfclaim = Namen über verifizierten SELF-CLAIM, Alter { $age } — vom eigenen Anchor des Namens signiert (Mesh-DNS nicht erreichbar)
doctor-names-note = über Snapshot Serial { $serial }, Alter { $age }
doctor-names-selfclaim-note = über verifizierten SELF-CLAIM, Alter { $age } — vom eigenen Anchor des Namens signiert
doctor-names-selfclaim-hint = Mesh-DNS nicht erreichbar — dieser Schritt wurde über einen verifizierten SELF-CLAIM aufgelöst (Alter { $age }): Der eigene Anchor des Namens hat diese Bindung signiert, und die peer_bindings des gecachten Snapshots autorisieren sie. Signiert, aber nicht live; Mesh-DNS reparieren, um das zu beheben.
doctor-names-observed-note = über BEOBACHTETEN (ad hoc) Eintrag, Alter { $age } — nicht kanonisch
doctor-names-observed-hint = Mesh-DNS nicht erreichbar — dieser Schritt wurde über einen NICHT KANONISCHEN beobachteten Eintrag aufgelöst (Alter { $age }): Der Name existiert nur live und wurde nie in die Site-Registry kodifiziert. Kodifizieren (group_vars-Registry → Generator → neu signieren), sonst läuft er ab; Mesh-DNS reparieren, um das zu beheben.
doctor-names-hint = Mesh-DNS nicht erreichbar — dieser Schritt wurde über den signierten Namens-Snapshot aufgelöst (Serial { $serial }, Alter { $age }). Das Fabric bleibt über Fallback-Namen erreichbar; Mesh-DNS reparieren (goat-coredns auf dem Mgmt-Host), um das zu beheben.
storm-brake-plan-header = Sturmbremse-Plan (ADR 1081 Layer 4 — manuell):
storm-brake-plan-target = Mgmt-Host { $user }@{ $host }  (Kontext { $context })
storm-brake-step-flush = Signal-Container `{ $container }` neu starten (verwirft Zombie-Streams)
storm-brake-step-tighten = L2-Limiter auf `{ $container }` drosseln (Rate-Limit halbieren, Neg-Cache-TTL erhöhen) — { $window } Min., Rücknahme durch Operator; übersprungen, wenn L2 nicht deployed ist
storm-brake-step-shed = nicht essenzielle Publisher-Unit `{ $unit }` pausieren
storm-brake-announce-note = ein OperatorActionV1 mit den Ergebnissen je Schritt in den Audit-Trail schreiben
storm-brake-dryrun-note = Probelauf — nichts wurde ausgeführt.
storm-brake-triggers-header = Auslösen, wenn das Host-Net-Band von relay-health Folgendes zeigt:
storm-brake-trigger-futility = Signaling-Futility-Ratio >= 0.5 bei >= 100 Msgs/Min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = Uplink-Verlust >= 5 % über 10 Min. anhaltend (wan.loss_pct)
storm-brake-triggers-band-url = Host-Band: relay-health.{ $site }.<zone> (siehe docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Hinweis: Ein Signal-Flush allein stellt das Mesh NICHT wieder her. Layer 1-2 (Client-Backoff + Signal-Shedding) sind die Abhilfe; die Bremse verschafft nur Erholungszeit.
storm-brake-confirm = Sturmbremse auf { $user }@{ $host } auslösen? Startet den Signal-Container neu und pausiert deklarierte Publisher. [y/N]
storm-brake-aborted = Sturmbremse abgebrochen (keine Bestätigung)
storm-brake-executing = löse Sturmbremse auf { $user }@{ $host } aus ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = fehlgeschlagen
storm-brake-outcome-skipped = übersprungen
storm-brake-summary = Sturmbremse abgeschlossen: { $ok } ok, { $failed } fehlgeschlagen, { $skipped } übersprungen
storm-brake-flush-failed = FLUSH fehlgeschlagen — der Signal-Container wurde nicht neu gestartet, daher ist keine Mesh-Erholung zu erwarten: { $reason }
storm-brake-mgmt-unresolved = Mgmt-Host nicht aufgelöst — --mgmt-host übergeben, mgmt_ssh_host in der profile.toml des Kontexts setzen oder GOAT_MGMT_SSH_HOST exportieren
storm-brake-audit-note = Operator-Aktion im Audit-Trail erfasst (Akteur { $actor })

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
storm-brake-shed-none = (keine Abwurfeinheiten deklariert — storm_brake_shed_units in profile.toml setzen oder --shed-unit übergeben)

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = das Portal antwortete HTTP 404 für `{ $path }`: dort nicht registriert
error-portal-rate-limited-summary = das Portal antwortete HTTP 429 für `{ $path }`: zu viele Anfragen, später erneut versuchen
error-portal-server-error-summary = das Portal antwortete HTTP { $status } für `{ $path }`: ein Fehler auf der Seite des Portals
error-portal-refused-summary = das Portal antwortete HTTP { $status } für `{ $path }`: Anfrage abgelehnt

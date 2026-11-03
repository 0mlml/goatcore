### goat-cli — Luxembourgish (lb). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = kee Profil fir de Kontext `{ $context }`
error-off-mesh-summary = ausserhalb vum ZTOL — net mam Mesh verbonnen
error-router-unreachable-summary = den Zenoh-Router op `{ $endpoint }` ass net erreechbar
error-cert-expired-summary = mTLS-Zertifikat ofgelaf oder vum Router refuséiert
error-portal-unreachable-summary = d'Portal-API op `{ $endpoint }` ass net erreechbar
error-token-invalid-summary = de Portal huet den Bearer-Token refuséiert
error-topic-conflict-summary = den Topic `{ $topic }` existéiert schonn
error-bundle-invalid-summary = Iwwerpréiwung vun der Bundle-Ënnerschrëft oder dem Oflaf feelgeschloen

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Eenheetlech Operateur-Kommandozeil fir d'goat-Datefabrik.
help-long-about = goat — Operateur-CLI deen panoscope-core an een eenzegt Tool zesummesetzt: Profil-Initialiséierung/-Erneierung, ZTOL-bewossten doctor fir Erreechbarkeet, Organisatiouns-Portal-Browsen, topic CRUD, pub/sub, stethoscope-Delegéierung, en interaktive Kommando-Composer an d'Narratioun vun der Substrat-Kaskad.
help-flag-profile-dir = D'Profil-Verzeechnes iwwerschreiwen (Standard: $GOAT_PROFILE_DIR oder ~/.goat).
help-flag-context = De aktive Kontext iwwerschreiwen (Standard: $GOAT_CONTEXT Ëmgéigsvariabel, dann current_context an config.toml, dann "default").
help-flag-narrate = D'Substrat-Kaskad weisen, déi e mutéierend Kommando ausléist.
help-flag-verbose = Detailléiert Debug-Ausgab aktivéieren (oder RUST_LOG=goat=debug setzen).
help-flag-color = Faarfmodus fir d'Terminal-Ausgab (auto|always|never).
help-flag-lang = Sprooch fir mënschlech-liesbaren Text (z.B. en, uk). Fält op GOAT_LANG zréck, dann d'OS-Lokal, dann Englesch.
help-cmd-profile = Dat lokaalt goat-Profil verwalten (mTLS-Zertifikat + Portal-Token + Endpunkt-Set).
help-cmd-context = Kontexter verwalten (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = E gestaffelten Erreechbarkeets-Test géint d'Fabrik lafe loossen.
help-cmd-doctor-long = Gestaffelten Erreechbarkeets-Test géint d'Fabrik: ZTOL-Memberschaft → Zenoh-Router → Zertifikat-Akzeptatioun → Portal. All gescheitert Schrëtt gëtt e spezifeschen Ofhëllef-Hiweis eraus.
help-cmd-vendor = Organisatioune browsen, déi am Portal registréiert sinn.
help-cmd-topic = Topic-en oplëschten, weisen, erstellen oder läschen.
help-cmd-pub = E Payload op e topic-Schlëssel publizéieren.
help-cmd-sub = Op en topic-Schlësselausdrock abonnéieren.
help-cmd-inspect = Eng Ufro fir en Diagnos-Uerteel un stethoscope delegéieren.
help-cmd-ui = Interaktive zesummegefaaste Picker: déi declared ⊕ live Lëscht browsen an op d'Diagnose / Tail-Lëns um gewielten Zil sprangen.
help-cmd-compose = Interaktiven Kommando-Builder-TUI.
help-cmd-narrate = Substrat-Kaskad-Reflexioun iwwer rezent Aktivitéit. Nach net verfügbar (den alldeegleche Wee ass --narrate op engem mutéierende Kommando).
help-cmd-completion = Shell-Vervollstännegungsskript op stdout drécken.
help-cmd-explain = D'Bedeitung vun engem Feelercode + Ofhëllef am agebauten `goat-cli-error-codes.md` Dokument nokucken (cargo --explain Muster).
help-cmd-mock = Synthetesch Echantillonen op en topic publizéieren. Standard: trivialt hello-world JSON op 1Hz. Gitt `--format <.fds>` + `--message <Name>` fir amplaz operateur-geliwwert protobuf ze kodéieren.
help-cmd-status = Gesondheets-Synthees: ZTOL-Erreechbarkeet + portal /api/health + lokaalt Zertifikat-Oflaf, dräi glyph-Linnen oder een --json Objet.
help-cmd-services = All gehoste Service an `ops/serve/services.yaml` ofteschten an eng glyph-Linn pro Service uweisen (oder een --json Objet).
help-cmd-client = De lokale goat-clientd daemon iwwer säi JSON-RPC IPC-Socket (`~/.goat-client/goat-clientd.sock`) ëmschloen: status / up / down / bundle import.
help-cmd-plugin = PATH-baséiert Extensioun-Entdeckung (kubectl-Stil). All `goat-<name>` Programm um PATH ass als `goat <name>` opruffbar; `goat plugin list` zielt installéiert Plugins op, `goat plugin which <name>` léist de Wee op.

# goat status / inspect / --narrate runtime prose (ADR 1024).
# Contract tokens + backticked keys + glyphs → — reproduced verbatim.
status-ztol-malformed = Router-Endpunkt falsch geformt: `{ $endpoint }`
status-ztol-refused = TCP zum Router { $host }:{ $port } refuséiert: { $err }
status-ztol-unreachable = ausserhalb vum Mesh (Router { $host }:{ $port } net erreechbar an { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health feelgeschloen: { $err }
status-portal-timeout = portal /api/health Zäitiwwerschreidung no { $secs }s
status-cert-read-failed = Liesen vum Zertifikat feelgeschloen: { $err }
status-cert-pem-parse-failed = PEM-Parse vum Zertifikat feelgeschloen: { $err }
status-cert-wrong-tag = éischte PEM-Block ass { $tag }, erwaart CERTIFICATE
status-cert-x509-parse-failed = x509-Parse feelgeschloen: { $err }
status-cert-expired = Zertifikat OFGELAF bei unix={ $not_after }, elo={ $now }
status-cert-valid = Zertifikat gülteg; leeft an { $days_left }d of (unix={ $not_after })
inspect-finding-label = Befund ({ $severity }): { $label } — { $detail }
inspect-remedy-label = Ofhëllef: { $title } — { $detail }
inspect-no-outcomes = (keng Resultater — d'Zil huet op keng Prüfungen gepasst)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh-put akzeptéiert op `{ $key }` ({ $bytes } Bytes)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = Kaskad ofgeschloss — { $summary } ({ $n } { $n ->
        [one] Konsequenz
       *[other] Konsequenzen
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Assistent fir d'éischt Kéier: Sandbox oder echt wielen, beweisen datt et funktionéiert, op den tutor weisen.
help-cmd-tutor = Eng selbstgesteiert, praktesch Tour duerch de Fabric (vimtutor-Stil; fortsetzbar).
help-cmd-docs = Duerch d'goat-Dokumentatioun browsen oder déi voll Kommando-Referenz erausginn (--markdown).
help-cmd-hello = E hello vun Enn zu Enn duerch de Fabric hin an zréck schécken (Sandbox- oder echt Ëmgéigung).
flourish-all-green = 🐐 alles gréng — Fabric nominal

# machine-translated additions (2026-06-10) — review per i18n-provenance.toml
doctor-endpoint-malformed-summary = Router-Endpunkt fehlerhaft
doctor-step1-endpoint-malformed-hint = erwaart `<proto>/<host>:<port>`; kritt `{ $endpoint }`
doctor-step1-ok-summary = TCP-Verbindung zum Router-Host { $host }:{ $port } gelongen
doctor-step1-conn-failed-summary = TCP-Verbindung zum Router-Host fehlgeschloen
doctor-step1-conn-failed-hint = { $err } — netbird starten (`sudo netbird up --management-url <mgmt> --setup-key <key>`); `netbird status` iwwerpréiwen
doctor-step1-timeout-summary = TCP-Verbindung zum Router-Host ofgelaf
doctor-step1-timeout-hint = Router-Host { $host }:{ $port } net erreechbar no { $ms }ms — wahrscheinlech net am Mesh
doctor-step2-endpoint-malformed-hint = Bundle-Issuer korrigéieren (--goat-cli-router-endpoint muss `<proto>/<host>:<port>` sinn)
doctor-step2-ca-missing-summary = CA-Roots-PEM feelt oder net liisbar
doctor-step2-ca-missing-hint = { $err } — `goat profile init <bundle.cbor>` nei ausféieren
doctor-step2-ok-summary = Router { $host }:{ $port } akzeptéiert TCP (TLS gëtt a Schrëtt 3 validéiert)
doctor-step2-refused-summary = Router TCP ofgewisen
doctor-step2-refused-hint = { $err } — Router ass eventuell down; Zenoh-Deploy op dem mgmt-Host iwwerpréiwen
doctor-step2-timeout-summary = Router TCP ofgelaf
doctor-step2-timeout-hint = Router { $host }:{ $port } net erreechbar no { $ms }ms
doctor-step3-config-failed-summary = Zenoh-Config-Build fehlgeschloen
doctor-step3-config-failed-hint = ongültegen Endpunkt `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = Zenoh-TLS-Config-Build fehlgeschloen
doctor-step3-tls-config-failed-hint = { $err } — wahrscheinlech Zenoh-Versions-Drift; Ofhängegkeeten iwwerpréiwen
doctor-step3-ok-summary = mTLS-Sessioun opgemaach — Zertifikat vum Router-ACL akzeptéiert
doctor-step3-open-failed-summary = Zenoh-Sessioun konnt net opgemaach ginn
doctor-step3-open-failed-hint = { $err } — Zertifikat eventuell ofgelaf (run `goat profile show`) oder vum Router-ACL refuséiert (URI-SAN Ongläichheet)
doctor-step3-timeout-summary = Zenoh-Sessioun ofgelaf
doctor-step3-timeout-hint = Router huet den Handshake net fäerdeggestallt no { $secs }s — Auer-Offset, falscht Zertifikat oder Congestion
doctor-step4-ok-summary = Portal huet { $n } Organisatiouns-Zeil(en) zréckginn
doctor-step4-token-rejected-summary = Portal huet Bearer-Token ofgewisen
doctor-step4-token-rejected-hint = HTTP { $status } — Token ofgelaf oder widerruffen; iwwer operator-onboard.yml nei onboarden
doctor-step4-request-failed-summary = Portal-Ufro fehlgeschloen
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = Portal-Ufro ofgelaf
doctor-step4-timeout-hint = keng Äntwert no { $secs }s — iwwerpréiwen ob { $endpoint } vum Mesh aus erreechbar ass
doctor-skipped-summary = iwwersprong — viregehenden Schrëtt fehlgeschloen (mat --all nei ausféieren fir ze forcéieren)

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = nëmme fir Operateuren — Dir sidd net autoriséiert (`goat admin` ass inaktiv fir Net-Adminen)
help-flag-narrate-json = Substrate-Cascade als NDJSON ausginn (ee Objet pro Konsequenz) fir Tëschefall-Logbicher + Scripting. Implizéiert --narrate.
help-cmd-admin = Nëmme fir Operateuren: Organisatiouns-Liewenszyklus, Onboarding/Réckzuch/Rotatioun, Mesh-Peer-Trennung, Apparat + Operateur-Tier-Provisionéierung, LAN-Triagen. Autoriséiert duerch Är Operateur-Umeldungsinformatiounen (Server-säiteg iwwer Portal/NetBird Token Scope); inaktiv fir Net-Adminen — en net-autoriséierten Opruff ännert näischt a schléit fehl mat `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = de Profil fir de Kontext `{ $context }` existéiert, ass awer fir dëse Benotzer net liesbar (Permissioun verweigert)
error-portal-bad-response-summary = de Portal huet geäntwert, awer dëse goat-Build konnt d'Äntwert net parsen
doctor-step0-ok-summary = d'lokal Setup ass prett fir Netzwierk-Tester
doctor-step0-warn-summary = d'lokal Setup huet Kontrollen, déi net konnte bestätegt ginn
doctor-step0-fail-summary = d'lokal Setup brauch Opmierksamkeet
doctor-step0-profile-absent-summary = kee goat-Profil fir de Kontext `{ $context }`
doctor-step0-profile-new-hint = kuckt `goat context ls`; wann dëst Är éischt Setup ass, féiert `goat ca add <bundle-ca.pem>` aus a vergläicht säin Fingerprint mat deem vun Ärem Operateur, dann `goat profile init <bundle.cbor>`; `goat doctor` nach eng Kéier ausféieren
doctor-step0-profile-absent-hint = kuckt `goat context ls`; wielt e bestehende Kontext mat `goat context use <name>`, oder féiert `goat profile init <bundle.cbor>` fir dëse Kontext aus; `goat doctor` nach eng Kéier ausféieren
doctor-step0-profile-denied-summary = de goat-Profil op { $path } ass net liesbar
doctor-step0-profile-denied-hint = Besëtzer vum Profil korrigéieren oder als besëtzende Benotzer ausféieren; e bestehende Profil net nei initialiséieren
doctor-step0-profile-incomplete-summary = d'goat-Profil-Datei { $file } feelt oder ass net liesbar
doctor-step0-profile-incomplete-hint = d'genannt Datei aus dem ausgestallte Bundle restauréieren, oder mat `goat profile rotate <bundle.cbor>` dëse Kontext ersetzen; `goat doctor` nach eng Kéier ausféieren
doctor-step0-profile-invalid-summary = de goat-Profil huet net liesbar oder feelend Endpunkten
doctor-step0-profile-empty-token-summary = de goat-Profil huet en eidele Portal-Token
doctor-step0-profile-ok-summary = de goat-Kontext `{ $context }` huet liesbart Profil-Material
doctor-step0-client-absent-summary = kee ënnerstëtzte Mesh-Client huet geäntwert
doctor-step0-client-absent-hint = de Mesh-Client vun Ärem goatnet installéieren, säin Daemon starten, dann `goat doctor` nach eng Kéier ausféieren
doctor-step0-client-stopped-summary = den installéierte Mesh-Client huet keen notzbare Status zréckginn
doctor-step0-client-stopped-hint = kuckt `netbird status -d` oder `goat client status --detail`; den installéierten Daemon starten (`sudo netbird service start` fir NetBird), dann `goat doctor` nach eng Kéier ausféieren
doctor-step0-netbird-ok-summary = den NetBird-Daemon huet geäntwert (gemellte Profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd huet geäntwert (Zoustand `{ $state }`)
doctor-step0-management-connected-summary = NetBird mellt Management verbonnen
doctor-step0-management-disconnected-summary = NetBird mellt Management getrennt
doctor-step0-management-disconnected-hint = gemellten Management-URL an `netbird status -d` kontrolléieren; en opgebauten Tunnel ka bei engem Management-Ausfall weiderhin funktionéieren
doctor-step0-management-unknown-summary = de konfiguréierte Management-Endpunkt konnt lokal net festgestallt ginn
doctor-step0-management-unknown-hint = kuck `netbird status -d` oder `goat client diagnostics` no; doctor probéiert net, eng Management-URL ze roden
doctor-step0-tunnel-down-summary = NetBird mellt keng Mesh-Adress
doctor-step0-tunnel-down-hint = féier `netbird status -d` aus; wann ageschriwwen an den Daemon äntwert, féier `goat heal` aus, dann `goat doctor` nach eng Kéier
doctor-step0-tunnel-address-summary = NetBird mellt d'Mesh-Adress { $address }; d'Erreechbarkeet vum Router gëtt als Nächst gepréift
doctor-step0-goat-client-no-bundle-summary = goat-clientd huet kee Bundle importéiert
doctor-step0-goat-client-no-bundle-hint = féier `goat client bundle import <bundle.cbor>` mam Client-Enrollment-Bundle aus, verbann de Client, dann `goat doctor` nach eng Kéier
doctor-step0-goat-client-connected-summary = goat-clientd mellt en verbonnenen Tunnel; d'Erreechbarkeet vum Router gëtt als Nächst gepréift
doctor-step0-goat-client-outer-disabled-summary = den äusseren Tunnel vu goat-clientd ass am netbird-only-Modus ausgeschalt
doctor-step0-goat-client-down-summary = goat-clientd mellt Tunnel-Zoustand `{ $state }`
doctor-step0-goat-client-down-hint = féier `goat client diagnostics` aus, dann `goat client up` wann de Bundle gelueden ass; dann `goat doctor` nach eng Kéier
doctor-step0-goat-client-inner-ok-summary = goat-clientd mellt säin banneschte Mesh als verbonnen
doctor-step0-goat-client-inner-disabled-summary = dee banneschte Mesh vu goat-clientd ass am wg-cp0-only-Modus ausgeschalt
doctor-step0-goat-client-inner-down-summary = goat-clientd mellt banneschte Mesh-Zoustand `{ $state }`
doctor-step0-goat-client-inner-down-hint = féier `goat client diagnostics` aus fir de banneschte Mesh nozekucken, dann `goat doctor` nach eng Kéier nodeems en erëmhiergestallt ass
doctor-step0-management-reachable-summary = Management-Endpunkt huet mat HTTP { $status } geäntwert
doctor-step0-management-auth-summary = Management-HTTPS-Endpunkt ass erreechbar (HTTP { $status } verlaangt eng Autorisatioun fir dës Sond)
doctor-step0-management-unexpected-summary = Management-Endpunkt huet en onerwaarten HTTP { $status } zréckginn
doctor-step0-management-unexpected-hint = kuck déi konfiguréiert Management-URL no a frot däin goatnet-Operateur, dës Äntwert ze préifen
doctor-step0-management-unreachable-summary = de konfiguréierte Management-Endpunkt war net erreechbar: { $error }
doctor-step0-management-unreachable-hint = préif DNS an Netzzougang zu der URL, déi de Client mellt; en existenten Tunnel kann trotzdem nach funktionéieren
doctor-step0-clock-ok-summary = lokal Auer ass bannent { $seconds } Sekonne vun der HTTP-Zäit vum Management
doctor-step0-clock-skew-summary = lokal Auer ënnerscheet sech ëm { $seconds } Sekonne vun der HTTP-Zäit vum Management
doctor-step0-clock-skew-hint = synchroniséier d'Auer vum Apparat, dann `goat doctor` nach eng Kéier; d'Zertifikatsvalidéierung kann feelschloen, wann d'Auer falsch geet
doctor-step0-clock-unknown-summary = d'Auer-Ofwäichung konnt net aus engem HTTP-Date-Header vum Management gemoss ginn
doctor-step0-clock-unknown-hint = préif d'Zäitsynchronisatioun vum Apparat, wann TLS- oder Zertifikatskontrollen feelschloen
error-clock-skew-summary = d'Auer vum Apparat ass ze wäit vun der Management-Zäit ewech
error-doctor-inconclusive-summary = doctor huet e Feeler observéiert, ouni bewisen Ursaach
error-portal-forbidden-summary = de Portal huet dësem Kont den Zougang zur getester API verweigert
doctor-step4-forbidden-summary = de Portal huet HTTP 403 fir dëse Profil zréckginn
doctor-step4-forbidden-hint = dës Zougangsdate hunn de Portal erreecht, hunn awer keen Zougang zur getester API; kuckt `goat profile show` a frot Ären goatnet-Operateur, de Grant ze verifizéieren
doctor-step4-decode-summary = de Portal huet eng Äntwert geschéckt, déi dëse goat net liese kann
doctor-step4-decode-hint = { $err } — aktualiséier goat op d'Versioun, déi dëst goatnet bereetstellt, a start duerno `goat doctor` nei
doctor-step1-control-only-summary = nëmmen mam Kontrollserver verbonnen — kee Tunnel zu engem vun { $total } Peeren
doctor-step1-control-only-hint = dëst Netz léisst TCP 443 erausgoen, blockéiert awer d'Mesh-Dateports, dofir ass näischt um Mesh (Umeldesäite abegraff) vun dësem Apparat aus erreechbar. Frot d'Netzwierk-Team, ausgoend TCP an UDP op { $relays } an UDP 3478 op { $stun } z'erlaben; direkt UDP tëscht Apparater gëtt och recommandéiert. Fir et ze bestätegen, verbann dech mat engem Handy-Hotspot a féier `netbird status` aus: d'Peere sollten dann connectéieren.
doctor-offmesh-dashboard-hint = Relay-Flott (externe Standpunkt): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokalt Zertifikat, passende Schlëssel a CA-Bundle sinn liesbar; d'Zertifikat ass banne vu sengem Gültegkeetsintervall
doctor-step2-material-failed-summary = lokaalt TLS-Material { $file } konnt net benotzt ginn
doctor-step2-material-failed-hint = { $err } — kontrolléier déi genannt Datei; stell se aus dem ausgestallte Bundle erëm hier oder rotéier de Profil
doctor-step2-material-invalid-hint = { $file } ass kee gülteg X.509-PEM-Zertifikat; stell dat ausgestallt Material erëm hier oder féier `goat profile rotate <bundle.cbor>` aus
doctor-step2-key-invalid-hint = mtls.key.pem ass ongülteg oder passt net zu mtls.cert.pem; stell dat ausgestallt Material erëm hier oder féier `goat profile rotate <bundle.cbor>` aus
doctor-step2-cert-time-summary = lokalt Zertifikat ass baussent sengem Gültegkeetsintervall
doctor-step2-cert-time-hint = féier `goat profile show` aus; kuck d'Auer-Resultat uewendriwwer, frot dann Ären Operateur no engem neie Bundle a féier `goat profile rotate <new-bundle.cbor>` aus, wann d'Zertifikat ofgelaf ass
doctor-skipped-no-endpoint-summary = iwwersprong — an profile.toml feelt { $field }
doctor-skipped-no-profile-summary = iwwersprong — keng liesbar Profil-Endpunkte fir dëse Kontext
doctor-skipped-no-portal-token-summary = iwwersprong — kee liesbaren Portal-Token fir dëse Kontext
status-ztol-tcp-ok = Router-Port { $host }:{ $port } hëlt TCP un (nëmmen de Port — kuck Session)
status-session-ok = zenoh-mTLS-Session an { $ms }ms opgemaach (Router huet dëst Zertifikat ugeholl)
status-session-skipped = Session net probéiert (off mesh; kuck ztol)
status-session-material-missing = zenoh-Session net probéiert: { $detail }
status-session-config-failed = zenoh-Session-Konfiguratioun ass feelgeschloen: { $err }
status-session-failed = zenoh-Session ass net opgaangen: { $err } — `goat doctor` ausféieren
status-session-timeout = zenoh-Session ass net an { $secs }s opgaangen — de Router-Port hëlt TCP un, mä keng Session kënnt zoustan (Router blockéiert, Zertifikat refuséiert oder Auer-Ofwäichung); `goat doctor` ausféieren
status-names-live = Nimm live (Mesh-DNS huet geäntwert)
status-names-ip-only = Nimm net getest (Profil-Endpunkte si IP-Literaler)
status-names-snapshot = Nimm aus dem signéierte Snapshot, Serial { $serial }, Alter { $age } (Mesh-DNS net erreechbar)
status-names-snapshot-aging = Nimm aus dem signéierte Snapshot, Serial { $serial }, Alter { $age } — VERALT (Mesh-DNS net erreechbar; goat-coredns reparéieren, dann aktualiséieren d'Nimm automatesch)
status-names-snapshot-daemon = Nimm aus dem signéierte Snapshot, Serial { $serial }, Alter { $age } — Daemon-Store (Mesh-DNS net erreechbar)
status-names-snapshot-daemon-aging = Nimm aus dem signéierte Snapshot, Serial { $serial }, Alter { $age } — Daemon-Store, VERALT (Mesh-DNS net erreechbar; goat-coredns reparéieren, dann aktualiséieren d'Nimm automatesch)
status-names-failed = Mesh-DNS net erreechbar a Snapshot-Fallback net disponibel — { $reason }
status-pqc-on = wireguard PQC (rosenpass) AN — { $qr }/{ $connected } verbonne Peeren quantenresistent
status-pqc-off = wireguard PQC (rosenpass) AUS — klassesch wireguard-Handshakes
status-pqc-unknown = wireguard PQC onbekannt — { $reason }
status-device-none = Apparatweite Nummschutz AUS — kee goat-clientd-Numm-Store (goat-Commandë hunn nach de Fallback; aktivéieren no name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd-Numm-Store do (Serial { $serial }), mä säi Forwarder äntwert NET op { $addr } — Daemon down oder Nimm-Subsystem blockéiert
status-device-active = Apparatweite Nummschutz AKTIV — goat-clientd virum OS-Mesh-DNS op { $addr } (Store-Serial { $serial }, { $grade })
status-device-not-fronted = goat-clientd-Forwarder ass up op { $addr } (Store-Serial { $serial }, { $grade }) — OS net virgeschalt; Browser notzen direkt de Mesh-DNS
status-device-fronting-unknown = goat-clientd-Forwarder ass up op { $addr } (Store-Serial { $serial }, { $grade }) — OS-Virschaltungsstatus op dëser Plattform net erkennbar
status-names-observed = Nimm NET-KANONESCH (ad hoc): observéierten Eintrag, Alter { $age } — net am signéierte Snapshot (Mesh-DNS net erreechbar; dësen Eintrag kodifizéieren)
status-names-selfclaim = Nimm iwwer verifizéiert SELF-CLAIM, Alter { $age } — vum eegenen Anker vum Numm signéiert (Mesh-DNS net erreechbar)
doctor-names-note = iwwer Snapshot-Serial { $serial }, Alter { $age }
doctor-names-selfclaim-note = iwwer verifizéiert SELF-CLAIM, Alter { $age } — vum eegenen Anker vum Numm signéiert
doctor-names-selfclaim-hint = Mesh-DNS ass net erreechbar — dëse Schrëtt gouf iwwer e verifizéierte SELF-CLAIM opgeléist (Alter { $age }): den eegenen Anker vum Numm huet dës Bindung signéiert an d'peer_bindings vum gecachte Snapshot autoriséieren se. Signéiert, mä net live; Mesh-DNS reparéieren, fir dat ewechzekréien.
doctor-names-observed-note = iwwer OBSERVÉIERTEN (ad hoc) Eintrag, Alter { $age } — net-kanonesch
doctor-names-observed-hint = Mesh-DNS ass net erreechbar — dëse Schrëtt gouf iwwer en NET-KANONESCHEN observéierten Eintrag opgeléist (Alter { $age }): den Numm gëtt et nëmme live, en assni an de Site-Registry kodifizéiert ginn. Kodifizéieren (group_vars-Registry → Generator → nei signéieren), soss leeft en of; Mesh-DNS reparéieren, fir dat ewechzekréien.
doctor-names-hint = Mesh-DNS ass net erreechbar — dëse Schrëtt gouf iwwer den signéierte Nimm-Snapshot opgeléist (Serial { $serial }, Alter { $age }). D'Fabric bleift iwwer d'Fallback-Nimm erreechbar; Mesh-DNS (goat-coredns um mgmt-Host) reparéieren, fir dat ewechzekréien.
storm-brake-plan-header = Storm-Brems-Plang (ADR 1081 Layer 4 — manuell):
storm-brake-plan-target = Mgmt-Host { $user }@{ $host }  (Kontext { $context })
storm-brake-step-flush = Signal-Container `{ $container }` nei starten (verwerft Zombie-Streams)
storm-brake-step-tighten = L2-Limiter op `{ $container }` drosselen (Rate-Limit halbéieren, Neg-Cache-TTL erhéijen) — { $window } Min., vum Operateur zréckgesat; iwwersprong, wann L2 net deployt ass
storm-brake-step-shed = net-essentiell Publisher-Unit `{ $unit }` pauséieren
storm-brake-shed-none = (keng Shed-Units deklaréiert — storm_brake_shed_units an der profile.toml setzen oder --shed-unit matginn)
storm-brake-announce-note = eng OperatorActionV1 mat de Resultater pro Schrëtt an den Audit-Trail schreiwen
storm-brake-dryrun-note = Probelaf — näischt gouf ausgefouert.
storm-brake-triggers-header = Ausléisen, wann de Relay-Health-Host-Net-Band weist:
storm-brake-trigger-futility = Signaling-Futility-Ratio >= 0.5 bei >= 100 Msgs/Min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = Uplink-Verlust >= 5% 10 Min. laang (wan.loss_pct)
storm-brake-triggers-band-url = Host-Band: relay-health.{ $site }.<zone> (kuck docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Note: E Signal-Flush eleng stellt de Mesh NET erëm hier. Layers 1-2 (Client-Backoff + Signal-Shedding) sinn d'Heelmëttel; d'Brems kaaft Zäit fir d'Erhuelung.
storm-brake-confirm = Storm-Brems géint { $user }@{ $host } ausléisen? Start de Signal-Container nei a pauséiert deklaréiert Publisher. [y/N]
storm-brake-aborted = Storm-Brems ofgebrach (keng Confirmatioun)
storm-brake-executing = Storm-Brems gëtt géint { $user }@{ $host } ausgeléist ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = feelgeschloen
storm-brake-outcome-skipped = iwwersprong
storm-brake-summary = Storm-Brems fäerdeg: { $ok } ok, { $failed } feelgeschloen, { $skipped } iwwersprong
storm-brake-flush-failed = FLUSH feelgeschloen — de Signal-Container ass net nei gestart, dofir gëtt keng Mesh-Erhuelung erwaart: { $reason }
storm-brake-mgmt-unresolved = Mgmt-Host net opgeléist — --mgmt-host matginn, mgmt_ssh_host an der profile.toml vum Kontext setzen oder GOAT_MGMT_SSH_HOST exportéieren
storm-brake-audit-note = Operateur-Aktioun an den Audit-Trail opgeholl (Akteur { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = de Portal huet HTTP 404 fir `{ $path }` geäntwert: do net registréiert
error-portal-rate-limited-summary = de Portal huet HTTP 429 fir `{ $path }` geäntwert: ze vill Ufroen, méi spéit nach eng Kéier probéieren
error-portal-server-error-summary = de Portal huet HTTP { $status } fir `{ $path }` geäntwert: e Feeler op der Säit vum Portal
error-portal-refused-summary = de Portal huet HTTP { $status } fir `{ $path }` geäntwert: Ufro refuséiert

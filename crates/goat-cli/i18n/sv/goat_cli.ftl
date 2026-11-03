### goat-cli — Swedish (sv). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = ingen profil för kontexten `{ $context }`
error-off-mesh-summary = utanför ZTOL — inte ansluten till mesh
error-router-unreachable-summary = Zenoh-routern på `{ $endpoint }` kan inte nås
error-cert-expired-summary = mTLS-certifikatet har gått ut eller avvisades av routern
error-portal-unreachable-summary = portal-API:et på `{ $endpoint }` kan inte nås
error-token-invalid-summary = portalen avvisade Bearer-token
error-topic-conflict-summary = topic `{ $topic }` finns redan
error-bundle-invalid-summary = verifiering av bundle-signatur eller utgång misslyckades

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = routerns slutpunkt är felaktigt utformad
doctor-step1-endpoint-malformed-hint = förväntade `<proto>/<host>:<port>`; fick `{ $endpoint }`
doctor-step1-ok-summary = TCP-anslutning till routervärden { $host }:{ $port } lyckades
doctor-step1-conn-failed-summary = TCP-anslutning till routervärden misslyckades
doctor-step1-conn-failed-hint = { $err } — starta netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); kontrollera `netbird status`
doctor-step1-timeout-summary = tidsgräns för TCP-anslutning till routervärden överskreds
doctor-step1-timeout-hint = routervärden { $host }:{ $port } kunde inte nås på { $ms }ms — troligen utanför mesh
doctor-step2-endpoint-malformed-hint = korrigera bundle-utfärdaren (--goat-cli-router-endpoint måste vara `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = CA-rötter-PEM saknas eller går inte att läsa
doctor-step2-ca-missing-hint = { $err } — kör `goat profile init <bundle.cbor>` igen
doctor-step2-ok-summary = router { $host }:{ $port } accepterar TCP (TLS validerat i steg 3)
doctor-step2-refused-summary = routerns TCP avvisades
doctor-step2-refused-hint = { $err } — routern kan vara nere; kontrollera Zenoh-distributionen på mgmt-host
doctor-step2-timeout-summary = tidsgräns för routerns TCP överskreds
doctor-step2-timeout-hint = router { $host }:{ $port } kunde inte nås på { $ms }ms
doctor-step3-config-failed-summary = bygget av zenoh-konfigurationen misslyckades
doctor-step3-config-failed-hint = ogiltig slutpunkt `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = bygget av zenoh-TLS-konfigurationen misslyckades
doctor-step3-tls-config-failed-hint = { $err } — troligen zenoh-versionsavvikelse; kontrollera att beroendena matchar
doctor-step3-ok-summary = mTLS-session öppnad — certifikat accepterat av routerns ACL
doctor-step3-open-failed-summary = öppning av zenoh-session misslyckades
doctor-step3-open-failed-hint = { $err } — certifikatet kan ha gått ut (kör `goat profile show`) eller avvisats av routerns ACL (URI-SAN-avvikelse)
doctor-step3-timeout-summary = tidsgräns för öppning av zenoh-session överskreds
doctor-step3-timeout-hint = routern slutförde inte handskakningen på { $secs }s — klockavvikelse, fel certifikat eller överbelastning
doctor-step4-ok-summary = portalen returnerade { $n } organisationsrad(er)
doctor-step4-token-rejected-summary = portalen avvisade Bearer-token
doctor-step4-token-rejected-hint = HTTP { $status } — token utgånget eller återkallat; gör om onboarding via operator-onboard.yml
doctor-step4-request-failed-summary = portalbegäran misslyckades
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = tidsgräns för portalbegäran överskreds
doctor-step4-timeout-hint = inget svar på { $secs }s — kontrollera att { $endpoint } kan nås från mesh
doctor-skipped-summary = hoppade över — föregående steg misslyckades (kör igen med --all för att tvinga)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Enhetlig operatörskommandorad för goat-data fabric.
help-long-about = goat — operatörsinriktad CLI som komponerar panoscope-core till ett verktyg: profilinitiering/-återanvändning, ZTOL-medveten nåbarhets-doctor, bläddring i organisations-portal, topic-CRUD, pub/sub, stethoscope-delegering, en interaktiv kommando-composer och substratkaskadberättelse.
help-flag-profile-dir = Åsidosätt profilkatalogen (standard: $GOAT_PROFILE_DIR eller ~/.goat).
help-flag-context = Åsidosätt den aktiva kontexten (standard: $GOAT_CONTEXT-miljö, sedan current_context i config.toml, sedan "default").
help-flag-narrate = Visa substratkaskaden som ett muterande kommando utlöser.
help-flag-verbose = Aktivera utförlig debug-utdata (eller sätt RUST_LOG=goat=debug).
help-flag-color = Färgläge för terminalutdata (auto|always|never).
help-flag-lang = Språk för människovänd text (t.ex. en, uk). Faller tillbaka på GOAT_LANG, sedan OS-lokalen, sedan engelska.
help-cmd-profile = Hantera den lokala goat-profilen (mTLS-certifikat + portal-token + endpunktsuppsättning).
help-cmd-context = Hantera kontexter (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Kör en stegvis nåbarhetsprober mot fabric.
help-cmd-doctor-long = Stegvis nåbarhetsprober mot fabric: ZTOL-medlemskap → Zenoh-router → certifikatacceptans → portal. Varje misslyckat steg avger en specifik åtgärdsledtråd.
help-cmd-vendor = Bläddra bland organisationer registrerade i portalen.
help-cmd-topic = Lista, visa, skapa eller ta bort topics.
help-cmd-pub = Publicera en nyttolast till en topic-nyckel.
help-cmd-sub = Prenumerera på ett topic-nyckeluttryck.
help-cmd-inspect = Delegera en diagnostisk utlåtandeförfrågan till stethoscope.
help-cmd-ui = Interaktiv sammanslagen väljare: bläddra i den deklarerade ⊕ live-listan och hoppa till Diagnose-/Tail-linsen på det valda målet.
help-cmd-compose = Interaktiv kommandobyggar-TUI.
help-cmd-narrate = Substratkaskadreflektion över senaste aktivitet. Inte tillgängligt ännu (den vardagliga vägen är --narrate på ett muterande kommando).
help-cmd-completion = Skriv ut skalkompletteringsskript till stdout.
help-cmd-explain = Slå upp en felkods betydelse + åtgärd i det inbäddade `goat-cli-error-codes.md`-dokumentet (cargo --explain-mönster).
help-cmd-mock = Publicera syntetiska prover till en topic. Standard: trivial hello-world-JSON vid 1Hz. Ange `--format <.fds>` + `--message <Name>` för att i stället koda operatörstillhandahållen protobuf.
help-cmd-status = Hälsosyntes: ZTOL-nåbarhet + portal /api/health + lokal certifikatutgång, tre glyph-rader eller ett --json-objekt.
help-cmd-services = Sondera varje hostad tjänst i `ops/serve/services.yaml` och rendera en glyph-rad per tjänst (eller ett --json-objekt).
help-cmd-client = Omslut den lokala goat-clientd-daemon via dess JSON-RPC-IPC-socket (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH-baserad tilläggsupptäckt (kubectl-stil). Varje `goat-<name>`-körbar fil på PATH kan anropas som `goat <name>`; `goat plugin list` räknar upp installerade plugins, `goat plugin which <name>` löser sökvägen.

# goat status / inspect / --narrate runtime prose (ADR 1024).
# Contract tokens + backticked keys + glyphs → — reproduced verbatim.
status-ztol-malformed = routerns slutpunkt är felaktigt utformad: `{ $endpoint }`
status-ztol-refused = TCP avvisades till router { $host }:{ $port }: { $err }
status-ztol-unreachable = utanför mesh (router { $host }:{ $port } kan inte nås på { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health misslyckades: { $err }
status-portal-timeout = portal /api/health överskred tidsgränsen på { $secs }s
status-cert-read-failed = läsning av certifikat misslyckades: { $err }
status-cert-pem-parse-failed = PEM-parsning av certifikat misslyckades: { $err }
status-cert-wrong-tag = första PEM-blocket är { $tag }, förväntade CERTIFICATE
status-cert-x509-parse-failed = x509-parsning misslyckades: { $err }
status-cert-expired = certifikat HAR GÅTT UT vid unix={ $not_after }, nu={ $now }
status-cert-valid = certifikat giltigt; går ut om { $days_left }d (unix={ $not_after })
inspect-finding-label = fynd ({ $severity }): { $label } — { $detail }
inspect-remedy-label = åtgärd: { $title } — { $detail }
inspect-no-outcomes = (inga utfall — målet matchade inga kontroller)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh-put accepterad på `{ $key }` ({ $bytes } byte)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskaden lade sig — { $summary } ({ $n } { $n ->
        [one] konsekvens
       *[other] konsekvenser
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Guide vid första körning: välj sandbox eller riktig, bevisa att det fungerar, peka på tutor.
help-cmd-tutor = En självgående, praktisk rundtur i fabric (vimtutor-stil; kan återupptas).
help-cmd-docs = Bläddra i goat-dokumentationen, eller skriv ut den fullständiga kommandoreferensen (--markdown).
help-cmd-hello = Skicka en hälsning fram och tillbaka genom fabric från ände till ände (sandbox- eller riktig miljö).
flourish-all-green = 🐐 allt grönt — fabric nominell

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = endast operatörsåtgärd — du är inte behörig (`goat admin` är inaktivt för icke-administratörer)
help-flag-narrate-json = Skicka ut substratskaskaden som NDJSON (ett objekt per konsekvens) för incidentloggar + skriptning. Aktiverar --narrate.
help-cmd-admin = Operatörsexklusiv administration: org-livscykel, onboarding/återkallning/rotation, mesh-peer-avskärning, enhet + operatörsnivåprovisionering, LAN-triage. Auktoriseras av dina operatörsuppgifter (serversidan via portal/NetBird-tokenomfång); inaktivt för icke-administratörer — ett obehörigt anrop ändrar ingenting och misslyckas med `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profilen för kontexten `{ $context }` finns men är inte läsbar för den här användaren (åtkomst nekad)
error-portal-bad-response-summary = portalen svarade men den här goat-builden kunde inte tolka svaret
doctor-step0-ok-summary = lokal konfiguration är redo för nätverkstester
doctor-step0-warn-summary = lokal konfiguration har kontroller som inte kunde bekräftas
doctor-step0-fail-summary = lokal konfiguration kräver åtgärd
doctor-step0-profile-absent-summary = ingen goat-profil för kontexten `{ $context }`
doctor-step0-profile-new-hint = kontrollera `goat context ls`; om det är din första konfiguration, kör `goat ca add <bundle-ca.pem>` och jämför dess fingeravtryck med din operatörs, sedan `goat profile init <bundle.cbor>`; kör `goat doctor` igen
doctor-step0-profile-absent-hint = kontrollera `goat context ls`; välj en befintlig kontext med `goat context use <name>`, eller kör `goat profile init <bundle.cbor>` för den här kontexten; kör `goat doctor` igen
doctor-step0-profile-denied-summary = goat-profilen på { $path } är oläsbar
doctor-step0-profile-denied-hint = åtgärda profilens ägarskap eller kör som ägande användare; initiera inte om en befintlig profil
doctor-step0-profile-incomplete-summary = goat-profilfilen { $file } saknas eller är oläsbar
doctor-step0-profile-incomplete-hint = återställ den angivna filen från det utfärdade paketet, eller använd `goat profile rotate <bundle.cbor>` för att ersätta den här kontexten; kör `goat doctor` igen
doctor-step0-profile-invalid-summary = goat-profilen har oläsbara eller saknade endpoints
doctor-step0-profile-empty-token-summary = goat-profilen har en tom portaltoken
doctor-step0-profile-ok-summary = goat-kontexten `{ $context }` har läsbart profilmaterial
doctor-step0-client-absent-summary = ingen mesh-klient som stöds svarade
doctor-step0-client-absent-hint = installera mesh-klienten som tillhandahålls av ditt goatnet, starta dess daemon och kör sedan `goat doctor` igen
doctor-step0-client-stopped-summary = installerad mesh-klient returnerade ingen användbar status
doctor-step0-client-stopped-hint = kontrollera `netbird status -d` eller `goat client status --detail`; starta den installerade daemonen (`sudo netbird service start` för NetBird) och kör sedan `goat doctor` igen
doctor-step0-netbird-ok-summary = NetBird-daemonen svarade (rapporterad profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd svarade (tillstånd `{ $state }`)
doctor-step0-management-connected-summary = NetBird rapporterar att management är ansluten
doctor-step0-management-disconnected-summary = NetBird rapporterar att management är frånkopplad
doctor-step0-management-disconnected-hint = kontrollera den rapporterade management-URL:en och `netbird status -d`; en etablerad tunnel kan fortfarande fungera under ett management-avbrott
doctor-step0-management-unknown-summary = konfigurerad management-endpoint kunde inte fastställas lokalt
doctor-step0-management-unknown-hint = inspektera `netbird status -d` eller `goat client diagnostics`; doctor gissar inte någon hanterings-URL
doctor-step0-tunnel-down-summary = NetBird rapporterar ingen mesh-adress
doctor-step0-tunnel-down-hint = kör `netbird status -d`; om enheten är registrerad och daemonen svarar, kör `goat heal` och sedan `goat doctor` igen
doctor-step0-tunnel-address-summary = NetBird rapporterar mesh-adress { $address }; routerns nåbarhet kontrolleras härnäst
doctor-step0-goat-client-no-bundle-summary = goat-clientd har inget importerat paket
doctor-step0-goat-client-no-bundle-hint = kör `goat client bundle import <bundle.cbor>` med klientens registreringspaket, anslut klienten och kör sedan `goat doctor` igen
doctor-step0-goat-client-connected-summary = goat-clientd rapporterar en ansluten tunnel; routerns nåbarhet kontrolleras härnäst
doctor-step0-goat-client-outer-disabled-summary = goat-clientds yttre tunnel är inaktiverad i netbird-only-läge
doctor-step0-goat-client-down-summary = goat-clientd rapporterar tunneltillstånd `{ $state }`
doctor-step0-goat-client-down-hint = kör `goat client diagnostics` och sedan `goat client up` om paketet är laddat; kör `goat doctor` igen
doctor-step0-goat-client-inner-ok-summary = goat-clientd rapporterar att dess inre mesh är ansluten
doctor-step0-goat-client-inner-disabled-summary = goat-clientds inre mesh är inaktiverad i wg-cp0-only-läge
doctor-step0-goat-client-inner-down-summary = goat-clientd rapporterar inre mesh-tillstånd `{ $state }`
doctor-step0-goat-client-inner-down-hint = kör `goat client diagnostics` för att inspektera den inre meshen och kör sedan `goat doctor` igen när den är återställd
doctor-step0-management-reachable-summary = hanteringsslutpunkten svarade med HTTP { $status }
doctor-step0-management-auth-summary = hanterings-HTTPS-slutpunkten är nåbar (HTTP { $status } kräver auktorisering för den här sonderingen)
doctor-step0-management-unexpected-summary = hanteringsslutpunkten returnerade oväntad HTTP { $status }
doctor-step0-management-unexpected-hint = inspektera den konfigurerade hanterings-URL:en och be din goatnet-operatör kontrollera svaret
doctor-step0-management-unreachable-summary = den konfigurerade hanteringsslutpunkten kunde inte nås: { $error }
doctor-step0-management-unreachable-hint = kontrollera DNS och nätverksåtkomst till URL:en som klienten rapporterar; en befintlig tunnel kan fortfarande fungera
doctor-step0-clock-ok-summary = lokal klocka ligger inom { $seconds } sekunder från hanteringens HTTP-tid
doctor-step0-clock-skew-summary = lokal klocka avviker från hanteringens HTTP-tid med { $seconds } sekunder
doctor-step0-clock-skew-hint = synkronisera enhetens klocka och kör sedan `goat doctor` igen; certifikatvalidering kan misslyckas medan klockan är fel
doctor-step0-clock-unknown-summary = klockavvikelsen kunde inte mätas från någon Date-header i hanterings-HTTP
doctor-step0-clock-unknown-hint = kontrollera enhetens tidssynkronisering om TLS- eller certifikatkontroller misslyckas
error-clock-skew-summary = enhetens klocka avviker för mycket från hanteringstiden
error-doctor-inconclusive-summary = doctor observerade ett fel utan bevisad orsak
error-portal-forbidden-summary = portalen nekade det här kontot åtkomst till det testade API:et
doctor-step4-forbidden-summary = portalen svarade med HTTP 403 för den här profilen
doctor-step4-forbidden-hint = den här inloggningsuppgiften nådde portalen men saknar åtkomst till det testade API:et; kontrollera `goat profile show` och be din goatnet-operatör verifiera behörigheten
doctor-step4-decode-summary = portalen svarade med ett svar som den här goat-versionen inte kan läsa
doctor-step4-decode-hint = { $err } — uppdatera goat till den version som goatnet serverar och kör sedan `goat doctor` igen
doctor-step1-control-only-summary = ansluten endast till kontrollservern — ingen tunnel till någon av { $total } peers
doctor-step1-control-only-hint = det här nätverket släpper ut TCP 443 men blockerar mesh-dataportarna, så inget på meshen (inklusive inloggningssidor) går att nå från den här enheten. Be nätverksteamet tillåta utgående TCP och UDP till { $relays }, och UDP 3478 till { $stun }; direkt UDP mellan enheter rekommenderas också. För att bekräfta: anslut till en telefons surfzon och kör `netbird status`: peers ska då ansluta.
doctor-offmesh-dashboard-hint = relay-flotta (externt perspektiv): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokalt certifikat, matchande nyckel och CA-paket kan tolkas; certifikatet är inom sin giltighetstid
doctor-step2-material-failed-summary = lokalt TLS-material { $file } kunde inte användas
doctor-step2-material-failed-hint = { $err } — undersök den angivna filen; återställ den från det utfärdade paketet eller rotera profilen
doctor-step2-material-invalid-hint = { $file } är inte ett giltigt X.509 PEM-certifikat; återställ det utfärdade materialet eller kör `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem är ogiltig eller matchar inte mtls.cert.pem; återställ det utfärdade materialet eller kör `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokalt certifikat är utanför sin giltighetstid
doctor-step2-cert-time-hint = kör `goat profile show`; kontrollera klockresultatet ovan, be sedan operatören om ett nytt paket och kör `goat profile rotate <new-bundle.cbor>` om certifikatet har gått ut
doctor-skipped-no-endpoint-summary = hoppades över — profile.toml saknar { $field }
doctor-skipped-no-profile-summary = hoppades över — inga läsbara profilendpoints för det här sammanhanget
doctor-skipped-no-portal-token-summary = hoppades över — ingen läsbar portaltoken för det här sammanhanget
status-ztol-tcp-ok = routerport { $host }:{ $port } accepterar TCP (endast port — se session)
status-session-ok = zenoh mTLS-session öppnades på { $ms }ms (routern godkände det här certifikatet)
status-session-skipped = session försöktes inte (utanför meshen; se ztol)
status-session-material-missing = zenoh-session försöktes inte: { $detail }
status-session-config-failed = zenoh-sessionskonfiguration misslyckades: { $err }
status-session-failed = zenoh-sessionen öppnades inte: { $err } — kör `goat doctor`
status-session-timeout = zenoh-sessionen öppnades inte inom { $secs }s — routerporten tar emot TCP men ingen session slutförs (routern hänger sig, certifikatet avvisat eller klockskillnad); kör `goat doctor`
status-names-live = namn live (mesh-DNS svarade)
status-names-ip-only = namn ej testade (profilens endpoints är IP-adresser)
status-names-snapshot = namn enligt signerad ögonblicksbild, serienummer { $serial }, ålder { $age } (mesh-DNS onåbar)
status-names-snapshot-aging = namn enligt signerad ögonblicksbild, serienummer { $serial }, ålder { $age } — FÖRÅLDRAD (mesh-DNS onåbar; åtgärda goat-coredns, så uppdateras namnen automatiskt)
status-names-snapshot-daemon = namn enligt signerad ögonblicksbild, serienummer { $serial }, ålder { $age } — daemonens lager (mesh-DNS onåbar)
status-names-snapshot-daemon-aging = namn enligt signerad ögonblicksbild, serienummer { $serial }, ålder { $age } — daemonens lager, FÖRÅLDRAD (mesh-DNS onåbar; åtgärda goat-coredns, så uppdateras namnen automatiskt)
status-names-failed = mesh-DNS onåbar och reserv via ögonblicksbild saknas — { $reason }
status-pqc-on = wireguard PQC (rosenpass) PÅ — { $qr }/{ $connected } anslutna peers kvantsäkra
status-pqc-off = wireguard PQC (rosenpass) AV — klassiska wireguard-handskakningar
status-pqc-unknown = wireguard PQC okänt — { $reason }
status-device-none = namnskydd för hela enheten AV — inget namnlager från goat-clientd (goat-kommandon har ändå reserv; aktivera enligt name-fallback-resolver.md §9.2)
status-device-broken = namnlager från goat-clientd finns (serienummer { $serial }) men dess vidarebefordrare svarar INTE på { $addr } — daemonen nere eller namndelsystemet hänger sig
status-device-active = namnskydd för hela enheten AKTIVT — goat-clientd framför OS:ets mesh-DNS på { $addr } (lagrets serienummer { $serial }, { $grade })
status-device-not-fronted = goat-clientd-vidarebefordrare uppe på { $addr } (lagrets serienummer { $serial }, { $grade }) — OS inte framför; webbläsare använder mesh-DNS direkt
status-device-fronting-unknown = goat-clientd-vidarebefordrare uppe på { $addr } (lagrets serienummer { $serial }, { $grade }) — OS-fronting kan inte avgöras på denna plattform
status-names-observed = namn ICKE-KANONISKA (ad hoc): observerad post, ålder { $age } — finns inte i signerad ögonblicksbild (mesh-DNS onåbar; kodifiera denna post)
status-names-selfclaim = namn via verifierat SJÄLVANSPRÅK, ålder { $age } — signerat av namnets eget anchor (mesh-DNS onåbar)
doctor-names-note = via ögonblicksbild med serienummer { $serial }, ålder { $age }
doctor-names-selfclaim-note = via verifierat SJÄLVANSPRÅK, ålder { $age } — signerat av namnets eget anchor
doctor-names-selfclaim-hint = mesh-DNS är onåbar — detta steg löstes via ett verifierat SJÄLVANSPRÅK (ålder { $age }): namnets eget anchor signerade bindningen och den cachade ögonblicksbildens peer_bindings godkänner den. Signerat men inte live; åtgärda mesh-DNS för att få bort detta.
doctor-names-observed-note = via OBSERVERAD (ad hoc) post, ålder { $age } — icke-kanonisk
doctor-names-observed-hint = mesh-DNS är onåbar — detta steg löstes via en ICKE-KANONISK observerad post (ålder { $age }): namnet är endast live och har aldrig kodifierats i sitt register. Kodifiera det (group_vars registry → generator → omsignering) annars åldras det bort; åtgärda mesh-DNS för att få bort detta.
doctor-names-hint = mesh-DNS är onåbar — detta steg löstes via den signerade namnögonblicksbilden (serienummer { $serial }, ålder { $age }). Fabriken förblir nåbar via reservnamn; åtgärda mesh-DNS (goat-coredns på mgmt-värden) för att få bort detta.
storm-brake-plan-header = Stormbromsplan (ADR 1081 lager 4 — manuell):
storm-brake-plan-target = mgmt-värd { $user }@{ $host }  (kontext { $context })
storm-brake-step-flush = starta om signalcontainern `{ $container }` (släpper zombieströmmar)
storm-brake-step-tighten = strypa L2-begränsaren på `{ $container }` (halvera rate-limit, höj neg-cache TTL) — { $window } min, återställs av operatör; hoppas över om L2 inte är driftsatt
storm-brake-step-shed = pausa icke-väsentlig publiceringsenhet `{ $unit }`
storm-brake-shed-none = (inga avlastningsenheter angivna — sätt storm_brake_shed_units i profile.toml eller ange --shed-unit)
storm-brake-announce-note = skicka en OperatorActionV1 till granskningsloggen med utfall per steg
storm-brake-dryrun-note = torrkörning — inget kördes.
storm-brake-triggers-header = Utlös när relay-health-bandet för värdnät visar:
storm-brake-trigger-futility = signaleringens resultatlöshetskvot >= 0,5 vid >= 100 meddelanden/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = upplänkförlust >= 5 % under 10 min i följd (wan.loss_pct)
storm-brake-triggers-band-url = värdband: relay-health.{ $site }.<zone> (se docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Obs: enbart en signalspolning återställer INTE mesh-nätet. Lager 1–2 (klient-backoff + signalavlastning) är botemedlet; bromsen köper återhämtningstid.
storm-brake-confirm = Utlösa stormbromsen mot { $user }@{ $host }? Startar om signalcontainern och pausar angivna publicerare. [y/N]
storm-brake-aborted = stormbromsen avbruten (ingen bekräftelse)
storm-brake-executing = utlöser stormbromsen mot { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = misslyckades
storm-brake-outcome-skipped = överhoppad
storm-brake-summary = stormbroms klar: { $ok } ok, { $failed } misslyckade, { $skipped } överhoppade
storm-brake-flush-failed = FLUSH misslyckades — signalcontainern startade inte om, så ingen mesh-återhämtning väntas: { $reason }
storm-brake-mgmt-unresolved = mgmt-värd kunde inte lösas — ange --mgmt-host, sätt mgmt_ssh_host i kontextens profile.toml eller exportera GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operatörsåtgärd registrerad i granskningsloggen (aktör { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portalen svarade HTTP 404 för `{ $path }`: inte registrerat där
error-portal-rate-limited-summary = portalen svarade HTTP 429 för `{ $path }`: för många förfrågningar, försök igen senare
error-portal-server-error-summary = portalen svarade HTTP { $status } för `{ $path }`: ett fel på portalens sida
error-portal-refused-summary = portalen svarade HTTP { $status } för `{ $path }`: begäran avvisades

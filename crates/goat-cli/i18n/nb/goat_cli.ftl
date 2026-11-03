### goat-cli — Norwegian Bokmål (nb). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = ingen profil for konteksten `{ $context }`
error-off-mesh-summary = utenfor ZTOL — ikke koblet til mesh
error-router-unreachable-summary = Zenoh-ruteren på `{ $endpoint }` er ikke tilgjengelig
error-cert-expired-summary = mTLS-sertifikat utløpt eller avvist av ruteren
error-portal-unreachable-summary = portal-API på `{ $endpoint }` er ikke tilgjengelig
error-token-invalid-summary = portalen avviste Bearer-tokenet
error-topic-conflict-summary = topic `{ $topic }` finnes allerede
error-bundle-invalid-summary = verifisering av bundle-signatur eller utløp mislyktes

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = ruterens endepunkt er feil utformet
doctor-step1-endpoint-malformed-hint = forventet `<proto>/<host>:<port>`; mottok `{ $endpoint }`
doctor-step1-ok-summary = TCP-tilkobling til ruterverten { $host }:{ $port } vellykket
doctor-step1-conn-failed-summary = TCP-tilkobling til ruterverten mislyktes
doctor-step1-conn-failed-hint = { $err } — start netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); sjekk `netbird status`
doctor-step1-timeout-summary = tidsavbrudd for TCP-tilkobling til ruterverten
doctor-step1-timeout-hint = ruterverten { $host }:{ $port } er utilgjengelig på { $ms }ms — sannsynligvis utenfor mesh
doctor-step2-endpoint-malformed-hint = rett opp bundle-utstederen (--goat-cli-router-endpoint må være `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = CA-røtter-PEM mangler eller er uleselig
doctor-step2-ca-missing-hint = { $err } — kjør `goat profile init <bundle.cbor>` på nytt
doctor-step2-ok-summary = ruter { $host }:{ $port } godtar TCP (TLS validert i trinn 3)
doctor-step2-refused-summary = ruterens TCP avvist
doctor-step2-refused-hint = { $err } — ruteren er kanskje nede; sjekk Zenoh-distribusjonen på mgmt-host
doctor-step2-timeout-summary = tidsavbrudd for ruterens TCP
doctor-step2-timeout-hint = ruter { $host }:{ $port } er utilgjengelig på { $ms }ms
doctor-step3-config-failed-summary = bygging av zenoh-konfigurasjon mislyktes
doctor-step3-config-failed-hint = ugyldig endepunkt `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = bygging av zenoh-TLS-konfigurasjon mislyktes
doctor-step3-tls-config-failed-hint = { $err } — sannsynligvis zenoh-versjonsavvik; sjekk at avhengighetene samsvarer
doctor-step3-ok-summary = mTLS-økt åpnet — sertifikat godtatt av ruterens ACL
doctor-step3-open-failed-summary = åpning av zenoh-økt mislyktes
doctor-step3-open-failed-hint = { $err } — sertifikatet er kanskje utløpt (kjør `goat profile show`) eller avvist av ruterens ACL (URI-SAN-avvik)
doctor-step3-timeout-summary = tidsavbrudd for åpning av zenoh-økt
doctor-step3-timeout-hint = ruteren fullførte ikke håndtrykket på { $secs }s — tidsforskyvning, feil sertifikat eller overbelastning
doctor-step4-ok-summary = portalen returnerte { $n } organisasjonsrad(er)
doctor-step4-token-rejected-summary = portalen avviste Bearer-tokenet
doctor-step4-token-rejected-hint = HTTP { $status } — token utløpt eller tilbakekalt; gjenta onboarding via operator-onboard.yml
doctor-step4-request-failed-summary = portalforespørsel mislyktes
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = tidsavbrudd for portalforespørsel
doctor-step4-timeout-hint = ingen respons på { $secs }s — sjekk at { $endpoint } er tilgjengelig fra mesh
doctor-skipped-summary = hoppet over — forrige trinn mislyktes (kjør på nytt med --all for å tvinge)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Enhetlig operatørkommandolinje for goat-data fabric.
help-long-about = goat — operatørrettet CLI som setter sammen panoscope-core til ett verktøy: profilinitialisering/-gjenbruk, ZTOL-bevisst tilgjengelighets-doctor, bla i organisasjons-portal, topic-CRUD, pub/sub, stethoscope-delegering, en interaktiv kommando-composer og substrat-kaskadefortelling.
help-flag-profile-dir = Overstyr profilkatalogen (standard: $GOAT_PROFILE_DIR eller ~/.goat).
help-flag-context = Overstyr den aktive konteksten (standard: $GOAT_CONTEXT-miljø, deretter current_context i config.toml, deretter "default").
help-flag-narrate = Vis substratkaskaden som en muterende kommando utløser.
help-flag-verbose = Aktiver detaljert debug-utdata (eller sett RUST_LOG=goat=debug).
help-flag-color = Fargemodus for terminalutdata (auto|always|never).
help-flag-lang = Språk for menneskerettet tekst (f.eks. en, uk). Faller tilbake på GOAT_LANG, deretter OS-lokalen, deretter engelsk.
help-cmd-profile = Administrer den lokale goat-profilen (mTLS-sertifikat + portal-token + endepunktssett).
help-cmd-context = Administrer kontekster (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Kjør en trinnvis tilgjengelighetsprober mot fabric.
help-cmd-doctor-long = Trinnvis tilgjengelighetsprober mot fabric: ZTOL-medlemskap → Zenoh-router → sertifikatgodkjenning → portal. Hvert mislykket trinn gir et spesifikt utbedringshint.
help-cmd-vendor = Bla gjennom organisasjoner registrert i portalen.
help-cmd-topic = List opp, vis, opprett eller slett topics.
help-cmd-pub = Publiser en nyttelast til en topic-nøkkel.
help-cmd-sub = Abonner på et topic-nøkkeluttrykk.
help-cmd-inspect = Deleger en diagnostisk kjennelsesforespørsel til stethoscope.
help-cmd-ui = Interaktiv sammenslått velger: bla i den deklarerte ⊕ live-listen og hopp til Diagnose-/Tail-linsen på det valgte målet.
help-cmd-compose = Interaktiv kommandobygger-TUI.
help-cmd-narrate = Substrat-kaskaderefleksjon over nylig aktivitet. Ikke tilgjengelig ennå (den daglige veien er --narrate på en muterende kommando).
help-cmd-completion = Skriv ut skallfullføringsskript til stdout.
help-cmd-explain = Slå opp en feilkodes betydning + utbedring i det innebygde `goat-cli-error-codes.md`-dokumentet (cargo --explain-mønster).
help-cmd-mock = Publiser syntetiske prøver til en topic. Standard: triviell hello-world-JSON ved 1Hz. Angi `--format <.fds>` + `--message <Name>` for å i stedet kode operatørlevert protobuf.
help-cmd-status = Helsesyntese: ZTOL-tilgjengelighet + portal /api/health + lokal sertifikatutløp, tre glyph-linjer eller ett --json-objekt.
help-cmd-services = Sonder hver hostet tjeneste i `ops/serve/services.yaml` og gjengi én glyph-linje per tjeneste (eller ett --json-objekt).
help-cmd-client = Omslutt den lokale goat-clientd-daemonen via dens JSON-RPC-IPC-socket (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH-basert utvidelsesoppdagelse (kubectl-stil). Enhver `goat-<name>`-kjørbar fil på PATH kan kalles som `goat <name>`; `goat plugin list` teller opp installerte plugins, `goat plugin which <name>` løser stien.

# goat status / inspect / --narrate runtime prose (ADR 1024).
# Contract tokens + backticked keys + glyphs → — reproduced verbatim.
status-ztol-malformed = ruterens endepunkt er feil utformet: `{ $endpoint }`
status-ztol-refused = TCP avvist til ruter { $host }:{ $port }: { $err }
status-ztol-unreachable = utenfor mesh (ruter { $host }:{ $port } utilgjengelig på { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health mislyktes: { $err }
status-portal-timeout = portal /api/health fikk tidsavbrudd på { $secs }s
status-cert-read-failed = lesing av sertifikat mislyktes: { $err }
status-cert-pem-parse-failed = PEM-parsing av sertifikat mislyktes: { $err }
status-cert-wrong-tag = første PEM-blokk er { $tag }, forventet CERTIFICATE
status-cert-x509-parse-failed = x509-parsing mislyktes: { $err }
status-cert-expired = sertifikat UTLØPT ved unix={ $not_after }, nå={ $now }
status-cert-valid = sertifikat gyldig; utløper om { $days_left }d (unix={ $not_after })
inspect-finding-label = funn ({ $severity }): { $label } — { $detail }
inspect-remedy-label = utbedring: { $title } — { $detail }
inspect-no-outcomes = (ingen utfall — målet samsvarte med ingen sjekker)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh-put godtatt på `{ $key }` ({ $bytes } byte)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskade falt til ro — { $summary } ({ $n } { $n ->
        [one] konsekvens
       *[other] konsekvenser
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Veiviser ved første kjøring: velg sandbox eller ekte, bevis at det virker, pek på tutor.
help-cmd-tutor = En selvstyrt, praktisk omvisning i fabric (vimtutor-stil; kan gjenopptas).
help-cmd-docs = Bla gjennom goat-dokumentasjonen, eller skriv ut den fullstendige kommandoreferansen (--markdown).
help-cmd-hello = Send en hilsen frem og tilbake gjennom fabric ende-til-ende (sandbox- eller ekte miljø).
flourish-all-green = 🐐 alt grønt — fabric nominell

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = kun-operatør-handling — du er ikke autorisert (`goat admin` er inaktiv for ikke-administratorer)
help-flag-narrate-json = Send substratets kaskadefølger som NDJSON (ett objekt per konsekvens) for hendelselogger + skripting. Impliserer --narrate.
help-cmd-admin = Kun-operatør-administrasjon: organisasjonslivssyklus, onboarding/tilbakekalling/rotasjon, mesh-peer-kutt, enhets- og operatørnivå-klargjøring, LAN-triage. Autorisert av operatørlegitimasjonen din (tjenerside via portal/NetBird-tokenomfang); inaktiv for ikke-administratorer — et uautorisert kall endrer ingenting og feiler med `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profilen for kontekst `{ $context }` finnes, men kan ikke leses av denne brukeren (tilgang nektet)
error-portal-bad-response-summary = portalen svarte, men denne goat-versjonen kunne ikke tolke svaret
doctor-step0-ok-summary = lokalt oppsett er klart for nettverksprober
doctor-step0-warn-summary = lokalt oppsett har kontroller som ikke kunne bekreftes
doctor-step0-fail-summary = lokalt oppsett må følges opp
doctor-step0-profile-absent-summary = ingen goat-profil for kontekst `{ $context }`
doctor-step0-profile-new-hint = sjekk `goat context ls`; hvis dette er første oppsett, kjør `goat ca add <bundle-ca.pem>` og sammenlign fingeravtrykket med operatørens, deretter `goat profile init <bundle.cbor>`; kjør `goat doctor` på nytt
doctor-step0-profile-absent-hint = sjekk `goat context ls`; velg en eksisterende kontekst med `goat context use <name>`, eller kjør `goat profile init <bundle.cbor>` for denne konteksten; kjør `goat doctor` på nytt
doctor-step0-profile-denied-summary = goat-profilen på { $path } kan ikke leses
doctor-step0-profile-denied-hint = rett eierskapet til profilen eller kjør som eieren; ikke initialiser en eksisterende profil på nytt
doctor-step0-profile-incomplete-summary = goat-profilfilen { $file } mangler eller kan ikke leses
doctor-step0-profile-incomplete-hint = gjenopprett filen det gjelder fra den utstedte bundelen, eller bruk `goat profile rotate <bundle.cbor>` for å erstatte denne konteksten; kjør `goat doctor` på nytt
doctor-step0-profile-invalid-summary = goat-profilen har uleselige eller manglende endepunkter
doctor-step0-profile-empty-token-summary = goat-profilen har et tomt portal-token
doctor-step0-profile-ok-summary = goat-kontekst `{ $context }` har lesbart profilmateriale
doctor-step0-client-absent-summary = ingen støttet mesh-klient svarte
doctor-step0-client-absent-hint = installer mesh-klienten som goatnet-et ditt leverer, start daemonen, og kjør `goat doctor` på nytt
doctor-step0-client-stopped-summary = installert mesh-klient ga ikke brukbar status
doctor-step0-client-stopped-hint = sjekk `netbird status -d` eller `goat client status --detail`; start den installerte daemonen (`sudo netbird service start` for NetBird), og kjør `goat doctor` på nytt
doctor-step0-netbird-ok-summary = NetBird-daemonen svarte (rapportert profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd svarte (tilstand `{ $state }`)
doctor-step0-management-connected-summary = NetBird rapporterer at management er tilkoblet
doctor-step0-management-disconnected-summary = NetBird rapporterer at management er frakoblet
doctor-step0-management-disconnected-hint = sjekk den rapporterte management-URL-en og `netbird status -d`; en etablert tunnel kan fortsatt fungere under et management-avbrudd
doctor-step0-management-unknown-summary = konfigurert management-endepunkt kunne ikke fastslås lokalt
doctor-step0-management-unknown-hint = undersøk `netbird status -d` eller `goat client diagnostics`; doctor gjetter ikke en management-URL
doctor-step0-tunnel-down-summary = NetBird melder ingen mesh-adresse
doctor-step0-tunnel-down-hint = kjør `netbird status -d`; hvis enhetsregistrert og daemonen svarer, kjør `goat heal`, deretter `goat doctor` på nytt
doctor-step0-tunnel-address-summary = NetBird melder mesh-adresse { $address }; ruterens tilgjengelighet sjekkes neste
doctor-step0-goat-client-no-bundle-summary = goat-clientd har ingen importert bundle
doctor-step0-goat-client-no-bundle-hint = kjør `goat client bundle import <bundle.cbor>` med klientens registreringsbundle, koble til klienten, og kjør deretter `goat doctor` på nytt
doctor-step0-goat-client-connected-summary = goat-clientd melder tilkoblet tunnel; ruterens tilgjengelighet sjekkes neste
doctor-step0-goat-client-outer-disabled-summary = goat-clientd ytre tunnel er deaktivert i netbird-only-modus
doctor-step0-goat-client-down-summary = goat-clientd melder tunnelstatus `{ $state }`
doctor-step0-goat-client-down-hint = kjør `goat client diagnostics`, deretter `goat client up` hvis bundlen er lastet; kjør `goat doctor` på nytt
doctor-step0-goat-client-inner-ok-summary = goat-clientd melder indre mesh tilkoblet
doctor-step0-goat-client-inner-disabled-summary = goat-clientd indre mesh er deaktivert i wg-cp0-only-modus
doctor-step0-goat-client-inner-down-summary = goat-clientd melder indre mesh-status `{ $state }`
doctor-step0-goat-client-inner-down-hint = kjør `goat client diagnostics` for å undersøke indre mesh, og kjør `goat doctor` på nytt etter at det er gjenopprettet
doctor-step0-management-reachable-summary = management-endepunktet svarte med HTTP { $status }
doctor-step0-management-auth-summary = management-HTTPS-endepunktet er tilgjengelig (HTTP { $status } krever autorisasjon for denne proben)
doctor-step0-management-unexpected-summary = management-endepunktet returnerte uventet HTTP { $status }
doctor-step0-management-unexpected-hint = kontroller konfigurert management-URL og be goatnet-operatøren sjekke dette svaret
doctor-step0-management-unreachable-summary = konfigurert management-endepunkt kunne ikke nås: { $error }
doctor-step0-management-unreachable-hint = sjekk DNS og nettverkstilgang til URL-en klienten melder; en eksisterende tunnel kan fortsatt fungere
doctor-step0-clock-ok-summary = lokal klokke er innenfor { $seconds } sekunder av management-HTTP-tid
doctor-step0-clock-skew-summary = lokal klokke avviker fra management-HTTP-tid med { $seconds } sekunder
doctor-step0-clock-skew-hint = synkroniser enhetens klokke, og kjør `goat doctor` på nytt; sertifikatvalidering kan feile mens klokken er feil
doctor-step0-clock-unknown-summary = klokkeavvik kunne ikke måles fra en management-HTTP-Date-header
doctor-step0-clock-unknown-hint = sjekk enhetens tidssynkronisering hvis TLS- eller sertifikatkontroller feiler
error-clock-skew-summary = enhetens klokke avviker for mye fra administrasjonstiden
error-doctor-inconclusive-summary = doctor observerte en feil uten påvist årsak
error-portal-forbidden-summary = portalen nektet denne kontoen tilgang til det testede API-et
doctor-step4-forbidden-summary = portalen returnerte HTTP 403 for denne profilen
doctor-step4-forbidden-hint = denne legitimasjonen nådde portalen, men mangler tilgang til det testede API-et; sjekk `goat profile show` og be goatnet-operatøren din verifisere tilgangen
doctor-step4-decode-summary = portalen svarte med et svar denne goat-versjonen ikke kan lese
doctor-step4-decode-hint = { $err } — oppdater goat til versjonen dette goatnet kjører, og kjør `goat doctor` på nytt
doctor-step1-control-only-summary = koblet til kun kontrollserveren — ingen tunnel til noen av { $total } peers
doctor-step1-control-only-hint = dette nettverket slipper ut TCP 443, men blokkerer mesh-dataportene, så ingenting på meshet (inkludert påloggingssider) er tilgjengelig fra denne enheten. Be nettverksteamet tillate utgående TCP og UDP til { $relays }, og UDP 3478 til { $stun }; direkte UDP mellom enheter anbefales også. For å bekrefte: koble til en mobil hotspot og kjør `netbird status`: peers skal da koble til.
doctor-offmesh-dashboard-hint = relay-flåte (eksternt målepunkt): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokalt sertifikat, tilhørende nøkkel og CA-bunt kan tolkes; sertifikatet er innenfor gyldighetsperioden
doctor-step2-material-failed-summary = lokalt TLS-materiale { $file } kunne ikke brukes
doctor-step2-material-failed-hint = { $err } — undersøk den angitte filen; gjenopprett den fra den utstedte bunten eller roter profilen
doctor-step2-material-invalid-hint = { $file } er ikke et gyldig X.509 PEM-sertifikat; gjenopprett det utstedte materialet eller kjør `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem er ugyldig eller samsvarer ikke med mtls.cert.pem; gjenopprett det utstedte materialet eller kjør `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokalt sertifikat er utenfor gyldighetsperioden
doctor-step2-cert-time-hint = kjør `goat profile show`; sjekk klokkeresultatet ovenfor, og be operatøren om en ny bunt og kjør `goat profile rotate <new-bundle.cbor>` hvis sertifikatet er utløpt
doctor-skipped-no-endpoint-summary = hoppet over — profile.toml mangler { $field }
doctor-skipped-no-profile-summary = hoppet over — ingen lesbare profilendepunkter for denne konteksten
doctor-skipped-no-portal-token-summary = hoppet over — ingen lesbar portaltoken for denne konteksten
status-ztol-tcp-ok = ruterport { $host }:{ $port } godtar TCP (kun port — se session)
status-session-ok = zenoh mTLS-sesjon åpnet på { $ms } ms (ruteren godtok dette sertifikatet)
status-session-skipped = sesjon ikke forsøkt (utenfor meshet; se ztol)
status-session-material-missing = zenoh-sesjon ikke forsøkt: { $detail }
status-session-config-failed = zenoh-sesjonskonfigurasjon feilet: { $err }
status-session-failed = zenoh-sesjonen ble ikke åpnet: { $err } — kjør `goat doctor`
status-session-timeout = zenoh-sesjonen ble ikke åpnet innen { $secs }s — ruterporten tar imot TCP, men ingen sesjon fullføres (ruteren er hengt, sertifikatet avvist eller klokkeavvik); kjør `goat doctor`
status-names-live = navn live (mesh-DNS svarte)
status-names-ip-only = navn ikke testet (profilens endepunkter er IP-adresser)
status-names-snapshot = navn fra signert snapshot, serienummer { $serial }, alder { $age } (mesh-DNS utilgjengelig)
status-names-snapshot-aging = navn fra signert snapshot, serienummer { $serial }, alder { $age } — FORELDET (mesh-DNS utilgjengelig; fiks goat-coredns, så oppdateres navnene automatisk)
status-names-snapshot-daemon = navn fra signert snapshot, serienummer { $serial }, alder { $age } — daemon-lager (mesh-DNS utilgjengelig)
status-names-snapshot-daemon-aging = navn fra signert snapshot, serienummer { $serial }, alder { $age } — daemon-lager, FORELDET (mesh-DNS utilgjengelig; fiks goat-coredns, så oppdateres navnene automatisk)
status-names-failed = mesh-DNS utilgjengelig og snapshot-fallback ikke tilgjengelig — { $reason }
status-pqc-on = wireguard PQC (rosenpass) PÅ — { $qr }/{ $connected } tilkoblede peers er kvantesikre
status-pqc-off = wireguard PQC (rosenpass) AV — klassiske wireguard-håndtrykk
status-pqc-unknown = wireguard PQC ukjent — { $reason }
status-device-none = enhetsdekkende navnebeskyttelse AV — ingen goat-clientd-navnelager (goat-kommandoer har fortsatt fallback; aktiver iht. name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd-navnelager finnes (serienummer { $serial }), men forwarderen svarer IKKE på { $addr } — daemonen er nede eller navnedelsystemet er hengt
status-device-active = enhetsdekkende navnebeskyttelse AKTIV — goat-clientd foran OS-ets mesh-DNS på { $addr } (lagerets serienummer { $serial }, { $grade })
status-device-not-fronted = goat-clientd-forwarder oppe på { $addr } (lagerets serienummer { $serial }, { $grade }) — OS ikke satt bak den; nettlesere bruker direkte mesh-DNS
status-device-fronting-unknown = goat-clientd-forwarder oppe på { $addr } (lagerets serienummer { $serial }, { $grade }) — OS-ets fronting-tilstand kan ikke oppdages på denne plattformen
status-names-observed = navn IKKE-KANONISKE (ad hoc): observert oppføring, alder { $age } — ikke i signert snapshot (mesh-DNS utilgjengelig; kodifiser denne oppføringen)
status-names-selfclaim = navn via verifisert SELVERKLÆRING, alder { $age } — signert av navnets eget anker (mesh-DNS utilgjengelig)
doctor-names-note = via snapshot, serienummer { $serial }, alder { $age }
doctor-names-selfclaim-note = via verifisert SELVERKLÆRING, alder { $age } — signert av navnets eget anker
doctor-names-selfclaim-hint = mesh-DNS er utilgjengelig — dette steget ble løst via en verifisert SELVERKLÆRING (alder { $age }): navnets eget anker signerte bindingen, og peer_bindings i det bufrede snapshotet godkjenner den. Signert, men ikke live; fiks mesh-DNS for å fjerne dette.
doctor-names-observed-note = via OBSERVERT (ad hoc) oppføring, alder { $age } — ikke-kanonisk
doctor-names-observed-hint = mesh-DNS er utilgjengelig — dette steget ble løst via en IKKE-KANONISK observert oppføring (alder { $age }): navnet er kun live og aldri kodifisert i site-registeret. Kodifiser det (group_vars-register → generator → re-signering), ellers foreldes det; fiks mesh-DNS for å fjerne dette.
doctor-names-hint = mesh-DNS er utilgjengelig — dette steget ble løst via det signerte navnesnapshotet (serienummer { $serial }, alder { $age }). Fabrikken forblir nåbar på fallback-navn; fiks mesh-DNS (goat-coredns på mgmt-verten) for å fjerne dette.
storm-brake-plan-header = Stormbremse-plan (ADR 1081 lag 4 — manuell):
storm-brake-plan-target = mgmt-vert { $user }@{ $host }  (kontekst { $context })
storm-brake-step-flush = start signalcontaineren `{ $container }` på nytt (kutter zombie-strømmer)
storm-brake-step-tighten = stram inn L2-begrenser på `{ $container }` (halver rate-limit, øker neg-cache-TTL) — { $window } min, tilbakestilles av operatør; hoppes over hvis L2 ikke er utplassert
storm-brake-step-shed = pause for ikke-essensiell publisher-enhet `{ $unit }`
storm-brake-shed-none = (ingen shed-enheter deklarert — sett storm_brake_shed_units i profile.toml eller bruk --shed-unit)
storm-brake-announce-note = send én OperatorActionV1 til revisjonssporet med utfall per steg
storm-brake-dryrun-note = tørrkjøring — ingenting ble utført.
storm-brake-triggers-header = Utløs når relay-health-båndet for vertsnettet viser:
storm-brake-trigger-futility = signaleringens nytteløshetsrate >= 0.5 ved >= 100 meld/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = opplinktap >= 5 % vedvarende i 10 min (wan.loss_pct)
storm-brake-triggers-band-url = vertsbånd: relay-health.{ $site }.<zone> (se docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Merk: en signal-flush alene gjenoppretter IKKE meshet. Lag 1-2 (klient-backoff + signal-shedding) er løsningen; bremsen kjøper tid til gjenoppretting.
storm-brake-confirm = Utløse stormbremsen mot { $user }@{ $host }? Starter signalcontaineren på nytt og pauser deklarerte publishere. [y/N]
storm-brake-aborted = stormbremse avbrutt (ingen bekreftelse)
storm-brake-executing = utløser stormbremse mot { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = feilet
storm-brake-outcome-skipped = hoppet over
storm-brake-summary = stormbremse fullført: { $ok } ok, { $failed } feilet, { $skipped } hoppet over
storm-brake-flush-failed = FLUSH feilet — signalcontaineren startet ikke på nytt, så mesh-gjenoppretting er ikke forventet: { $reason }
storm-brake-mgmt-unresolved = mgmt-vert ikke løst — bruk --mgmt-host, sett mgmt_ssh_host i kontekstens profile.toml, eller eksporter GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operatørhandling logget i revisjonssporet (aktør { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portalen svarte HTTP 404 for `{ $path }`: ikke registrert der
error-portal-rate-limited-summary = portalen svarte HTTP 429 for `{ $path }`: for mange forespørsler, prøv igjen senere
error-portal-server-error-summary = portalen svarte HTTP { $status } for `{ $path }`: en feil på portalens side
error-portal-refused-summary = portalen svarte HTTP { $status } for `{ $path }`: forespørselen ble avvist

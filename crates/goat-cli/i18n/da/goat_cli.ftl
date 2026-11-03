### goat-cli — Danish (da). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = ingen profil for konteksten `{ $context }`
error-off-mesh-summary = uden for ZTOL — ikke forbundet til mesh
error-router-unreachable-summary = Zenoh-routeren på `{ $endpoint }` kan ikke nås
error-cert-expired-summary = mTLS-certifikat udløbet eller afvist af routeren
error-portal-unreachable-summary = portal-API'en på `{ $endpoint }` kan ikke nås
error-token-invalid-summary = portalen afviste Bearer-tokenet
error-topic-conflict-summary = topic `{ $topic }` findes allerede
error-bundle-invalid-summary = verificering af bundle-signatur eller udløb mislykkedes

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = routerens slutpunkt er forkert udformet
doctor-step1-endpoint-malformed-hint = forventede `<proto>/<host>:<port>`; modtog `{ $endpoint }`
doctor-step1-ok-summary = TCP-forbindelse til routerværten { $host }:{ $port } lykkedes
doctor-step1-conn-failed-summary = TCP-forbindelse til routerværten mislykkedes
doctor-step1-conn-failed-hint = { $err } — start netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); tjek `netbird status`
doctor-step1-timeout-summary = tidsudløb for TCP-forbindelse til routerværten
doctor-step1-timeout-hint = routerværten { $host }:{ $port } kan ikke nås på { $ms }ms — sandsynligvis uden for mesh
doctor-step2-endpoint-malformed-hint = ret bundle-udstederen (--goat-cli-router-endpoint skal være `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = CA-rødder-PEM mangler eller kan ikke læses
doctor-step2-ca-missing-hint = { $err } — kør `goat profile init <bundle.cbor>` igen
doctor-step2-ok-summary = router { $host }:{ $port } accepterer TCP (TLS valideret i trin 3)
doctor-step2-refused-summary = routerens TCP afvist
doctor-step2-refused-hint = { $err } — routeren er muligvis nede; tjek Zenoh-udrulningen på mgmt-host
doctor-step2-timeout-summary = tidsudløb for routerens TCP
doctor-step2-timeout-hint = router { $host }:{ $port } kan ikke nås på { $ms }ms
doctor-step3-config-failed-summary = opbygning af zenoh-konfiguration mislykkedes
doctor-step3-config-failed-hint = ugyldigt slutpunkt `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = opbygning af zenoh-TLS-konfiguration mislykkedes
doctor-step3-tls-config-failed-hint = { $err } — sandsynligvis zenoh-versionsafvigelse; tjek at afhængighederne matcher
doctor-step3-ok-summary = mTLS-session åbnet — certifikat accepteret af routerens ACL
doctor-step3-open-failed-summary = åbning af zenoh-session mislykkedes
doctor-step3-open-failed-hint = { $err } — certifikatet er muligvis udløbet (kør `goat profile show`) eller afvist af routerens ACL (URI-SAN-uoverensstemmelse)
doctor-step3-timeout-summary = tidsudløb for åbning af zenoh-session
doctor-step3-timeout-hint = routeren fuldførte ikke håndtrykket på { $secs }s — tidsforskydning, forkert certifikat eller overbelastning
doctor-step4-ok-summary = portalen returnerede { $n } organisationsrække(r)
doctor-step4-token-rejected-summary = portalen afviste Bearer-tokenet
doctor-step4-token-rejected-hint = HTTP { $status } — token udløbet eller tilbagekaldt; gentag onboarding via operator-onboard.yml
doctor-step4-request-failed-summary = portalanmodning mislykkedes
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = tidsudløb for portalanmodning
doctor-step4-timeout-hint = intet svar på { $secs }s — tjek at { $endpoint } kan nås fra mesh
doctor-skipped-summary = sprunget over — forrige trin mislykkedes (kør igen med --all for at gennemtvinge)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Samlet operatørkommandolinje til goat-data fabric.
help-long-about = goat — operatørvendt CLI, der samler panoscope-core i ét værktøj: profilinitialisering/-genbrug, ZTOL-bevidst tilgængeligheds-doctor, gennemsyn af organisations-portal, topic-CRUD, pub/sub, stethoscope-delegering, en interaktiv kommando-composer og substrat-kaskadefortælling.
help-flag-profile-dir = Tilsidesæt profilmappen (standard: $GOAT_PROFILE_DIR eller ~/.goat).
help-flag-context = Tilsidesæt den aktive kontekst (standard: $GOAT_CONTEXT-miljø, derefter current_context i config.toml, derefter "default").
help-flag-narrate = Vis den substratkaskade, som en muterende kommando udløser.
help-flag-verbose = Aktivér detaljeret debug-output (eller sæt RUST_LOG=goat=debug).
help-flag-color = Farvetilstand for terminaloutput (auto|always|never).
help-flag-lang = Sprog til menneskevendt tekst (f.eks. en, uk). Falder tilbage til GOAT_LANG, derefter OS-lokaliteten, derefter engelsk.
help-cmd-profile = Administrér den lokale goat-profil (mTLS-certifikat + portal-token + endepunktssæt).
help-cmd-context = Administrér kontekster (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Kør en trinvis tilgængelighedsprober mod fabric.
help-cmd-doctor-long = Trinvis tilgængelighedsprober mod fabric: ZTOL-medlemskab → Zenoh-router → certifikataccept → portal. Hvert mislykket trin udsender et specifikt afhjælpningstip.
help-cmd-vendor = Gennemse organisationer registreret i portalen.
help-cmd-topic = Vis, fremvis, opret eller slet topics.
help-cmd-pub = Udgiv en nyttelast til en topic-nøgle.
help-cmd-sub = Abonnér på et topic-nøgleudtryk.
help-cmd-inspect = Delegér en diagnostisk kendelsesanmodning til stethoscope.
help-cmd-ui = Interaktiv samlet vælger: gennemse den deklarerede ⊕ live-liste og hop til Diagnose-/Tail-linsen på det valgte mål.
help-cmd-compose = Interaktiv kommandobygger-TUI.
help-cmd-narrate = Substrat-kaskaderefleksion over nylig aktivitet. Endnu ikke tilgængelig (den daglige sti er --narrate på en muterende kommando).
help-cmd-completion = Udskriv shell-fuldførelsesscript til stdout.
help-cmd-explain = Slå en fejlkodes betydning + afhjælpning op i det indlejrede `goat-cli-error-codes.md`-dokument (cargo --explain-mønster).
help-cmd-mock = Udgiv syntetiske prøver til et topic. Standard: triviel hello-world-JSON ved 1Hz. Angiv `--format <.fds>` + `--message <Name>` for i stedet at kode operatørleveret protobuf.
help-cmd-status = Sundhedssyntese: ZTOL-tilgængelighed + portal /api/health + lokal certifikatudløb, tre glyph-linjer eller ét --json-objekt.
help-cmd-services = Test hver hostet tjeneste i `ops/serve/services.yaml` og gengiv én glyph-linje pr. tjeneste (eller ét --json-objekt).
help-cmd-client = Indkapsl den lokale goat-clientd-daemon via dens JSON-RPC-IPC-socket (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH-baseret udvidelsesopdagelse (kubectl-stil). Enhver `goat-<name>`-eksekverbar på PATH kan kaldes som `goat <name>`; `goat plugin list` opregner installerede plugins, `goat plugin which <name>` opløser stien.

# goat status / inspect / --narrate runtime prose (ADR 1024).
# Contract tokens + backticked keys + glyphs → — reproduced verbatim.
status-ztol-malformed = routerens slutpunkt er forkert udformet: `{ $endpoint }`
status-ztol-refused = TCP afvist til router { $host }:{ $port }: { $err }
status-ztol-unreachable = uden for mesh (router { $host }:{ $port } kan ikke nås på { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health mislykkedes: { $err }
status-portal-timeout = portal /api/health fik tidsudløb på { $secs }s
status-cert-read-failed = læsning af certifikat mislykkedes: { $err }
status-cert-pem-parse-failed = PEM-parsing af certifikat mislykkedes: { $err }
status-cert-wrong-tag = første PEM-blok er { $tag }, forventede CERTIFICATE
status-cert-x509-parse-failed = x509-parsing mislykkedes: { $err }
status-cert-expired = certifikat UDLØBET ved unix={ $not_after }, nu={ $now }
status-cert-valid = certifikat gyldigt; udløber om { $days_left }d (unix={ $not_after })
inspect-finding-label = fund ({ $severity }): { $label } — { $detail }
inspect-remedy-label = afhjælpning: { $title } — { $detail }
inspect-no-outcomes = (ingen resultater — målet matchede ingen tjek)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh-put accepteret på `{ $key }` ({ $bytes } bytes)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskade faldet til ro — { $summary } ({ $n } { $n ->
        [one] konsekvens
       *[other] konsekvenser
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Førstegangs-guide: vælg sandbox eller rigtig, bevis at det virker, peg på tutor.
help-cmd-tutor = En selvstyret, praktisk rundtur i fabric (vimtutor-stil; kan genoptages).
help-cmd-docs = Gennemse goat-dokumentationen, eller udsend den fulde kommandoreference (--markdown).
help-cmd-hello = Send et hello frem og tilbage gennem fabric fra ende til ende (sandbox- eller rigtigt miljø).
flourish-all-green = 🐐 alt grønt — fabric nominel

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = kun-operatør-handling — du er ikke autoriseret (`goat admin` er inaktiv for ikke-administratorer)
help-flag-narrate-json = Udsend substrat-kaskaden som NDJSON (ét objekt pr. konsekvens) til hændelseslogge + scripting. Medfører --narrate.
help-cmd-admin = Kun-operatør-administration: organisations-livscyklus, onboard/revoke/rotate, mesh-peer-afskæring, device + operatør-niveau-klargøring, LAN-triage. Autoriseret af dine operatørlegitimationsoplysninger (server-side via portal/NetBird token-omfang); inaktiv for ikke-administratorer — et uautoriseret kald ændrer ingenting og fejler med `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profilen for kontekst `{ $context }` findes, men kan ikke læses af denne bruger (adgang nægtet)
error-portal-bad-response-summary = portalen svarede, men denne goat-build kunne ikke tolke svaret
doctor-step0-ok-summary = lokal opsætning er klar til netværksprober
doctor-step0-warn-summary = lokal opsætning har kontroller, der ikke kunne bekræftes
doctor-step0-fail-summary = lokal opsætning kræver opmærksomhed
doctor-step0-profile-absent-summary = ingen goat-profil for kontekst `{ $context }`
doctor-step0-profile-new-hint = tjek `goat context ls`; hvis det er din første opsætning, kør `goat ca add <bundle-ca.pem>` og sammenlign fingeraftrykket med din operatørs, derefter `goat profile init <bundle.cbor>`; kør `goat doctor` igen
doctor-step0-profile-absent-hint = tjek `goat context ls`; vælg en eksisterende kontekst med `goat context use <name>`, eller kør `goat profile init <bundle.cbor>` for denne kontekst; kør `goat doctor` igen
doctor-step0-profile-denied-summary = goat-profilen på { $path } kan ikke læses
doctor-step0-profile-denied-hint = ret profilens ejerskab eller kør som den ejende bruger; geninitialisér ikke en eksisterende profil
doctor-step0-profile-incomplete-summary = goat-profilfilen { $file } mangler eller kan ikke læses
doctor-step0-profile-incomplete-hint = gendan den angivne fil fra det udstedte bundle, eller brug `goat profile rotate <bundle.cbor>` til at erstatte denne kontekst; kør `goat doctor` igen
doctor-step0-profile-invalid-summary = goat-profilen har endepunkter, der er ulæselige eller mangler
doctor-step0-profile-empty-token-summary = goat-profilen har et tomt portal-token
doctor-step0-profile-ok-summary = goat-kontekst `{ $context }` har læsbart profilmateriale
doctor-step0-client-absent-summary = ingen understøttet mesh-klient svarede
doctor-step0-client-absent-hint = installér den mesh-klient, dit goatnet leverer, start dens daemon, og kør derefter `goat doctor` igen
doctor-step0-client-stopped-summary = den installerede mesh-klient returnerede ingen brugbar status
doctor-step0-client-stopped-hint = tjek `netbird status -d` eller `goat client status --detail`; start den installerede daemon (`sudo netbird service start` for NetBird), og kør derefter `goat doctor` igen
doctor-step0-netbird-ok-summary = NetBird-daemonen svarede (rapporteret profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd svarede (tilstand `{ $state }`)
doctor-step0-management-connected-summary = NetBird rapporterer, at management er forbundet
doctor-step0-management-disconnected-summary = NetBird rapporterer, at management er afbrudt
doctor-step0-management-disconnected-hint = tjek den rapporterede management-URL og `netbird status -d`; en etableret tunnel kan stadig virke under et management-udfald
doctor-step0-management-unknown-summary = det konfigurerede management-endepunkt kunne ikke etableres lokalt
doctor-step0-management-unknown-hint = undersøg `netbird status -d` eller `goat client diagnostics`; doctor gætter ikke på en management-URL
doctor-step0-tunnel-down-summary = NetBird rapporterer ingen mesh-adresse
doctor-step0-tunnel-down-hint = kør `netbird status -d`; hvis enheden er tilmeldt, og dæmonen svarer, kør `goat heal` og derefter `goat doctor` igen
doctor-step0-tunnel-address-summary = NetBird rapporterer mesh-adressen { $address }; router-forbindelse kontrolleres næste gang
doctor-step0-goat-client-no-bundle-summary = goat-clientd har ingen importeret bundle
doctor-step0-goat-client-no-bundle-hint = kør `goat client bundle import <bundle.cbor>` med klientens tilmeldingsbundle, forbind klienten, og kør derefter `goat doctor` igen
doctor-step0-goat-client-connected-summary = goat-clientd rapporterer en forbundet tunnel; router-forbindelse kontrolleres næste gang
doctor-step0-goat-client-outer-disabled-summary = goat-clientd's ydre tunnel er deaktiveret i netbird-only-tilstand
doctor-step0-goat-client-down-summary = goat-clientd rapporterer tunneltilstand `{ $state }`
doctor-step0-goat-client-down-hint = kør `goat client diagnostics`, derefter `goat client up` hvis bundlen er indlæst; kør `goat doctor` igen
doctor-step0-goat-client-inner-ok-summary = goat-clientd rapporterer, at dets indre mesh er forbundet
doctor-step0-goat-client-inner-disabled-summary = goat-clientd's indre mesh er deaktiveret i wg-cp0-only-tilstand
doctor-step0-goat-client-inner-down-summary = goat-clientd rapporterer indre mesh-tilstand `{ $state }`
doctor-step0-goat-client-inner-down-hint = kør `goat client diagnostics` for at undersøge det indre mesh, og kør `goat doctor` igen, når det er genoprettet
doctor-step0-management-reachable-summary = management-endpoint svarede med HTTP { $status }
doctor-step0-management-auth-summary = management-HTTPS-endpoint kan nås (HTTP { $status } kræver autorisation for denne probe)
doctor-step0-management-unexpected-summary = management-endpoint returnerede uventet HTTP { $status }
doctor-step0-management-unexpected-hint = undersøg den konfigurerede management-URL, og bed din goatnet-operatør om at kontrollere dette svar
doctor-step0-management-unreachable-summary = den konfigurerede management-endpoint kunne ikke nås: { $error }
doctor-step0-management-unreachable-hint = kontroller DNS og netværksadgang til den URL, klienten rapporterer; en eksisterende tunnel kan stadig fungere
doctor-step0-clock-ok-summary = lokalt ur er inden for { $seconds } sekunder af management-HTTP-tiden
doctor-step0-clock-skew-summary = lokalt ur afviger fra management-HTTP-tiden med { $seconds } sekunder
doctor-step0-clock-skew-hint = synkroniser enhedens ur, og kør derefter `goat doctor` igen; certifikatvalidering kan fejle, mens uret er forkert
doctor-step0-clock-unknown-summary = urafvigelsen kunne ikke måles fra en management-HTTP-Date-header
doctor-step0-clock-unknown-hint = kontroller enhedens tidssynkronisering, hvis TLS- eller certifikatkontrol fejler
error-clock-skew-summary = enhedens ur afviger for meget fra managementtiden
error-doctor-inconclusive-summary = doctor observerede en fejl uden en påvist årsag
error-portal-forbidden-summary = portalen nægtede denne konto adgang til det undersøgte API
doctor-step4-forbidden-summary = portalen returnerede HTTP 403 for denne profil
doctor-step4-forbidden-hint = dette credential nåede portalen, men har ikke adgang til det undersøgte API; tjek `goat profile show` og bed din goatnet-operatør om at verificere tildelingen
doctor-step4-decode-summary = portalen svarede med et svar, som denne goat ikke kan læse
doctor-step4-decode-hint = { $err } — opdater goat til den version, som denne goatnet leverer, og kør `goat doctor` igen
doctor-step1-control-only-summary = forbundet til kontrolserveren alene — ingen tunnel til nogen af { $total } peers
doctor-step1-control-only-hint = dette netværk lukker TCP 443 ud, men blokerer mesh-dataportene, så intet på meshet (inkl. login-sider) kan nås fra denne enhed. Bed netværksteamet om at tillade udgående TCP og UDP til { $relays } samt UDP 3478 til { $stun }; direkte UDP mellem enheder anbefales også. For at bekræfte: tilslut en telefons hotspot og kør `netbird status`: peers bør så kunne forbinde.
doctor-offmesh-dashboard-hint = relay-flåde (eksternt udsigtspunkt): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokalt certifikat, tilhørende nøgle og CA-bundle kan parses; certifikatet er inden for sin gyldighedsperiode
doctor-step2-material-failed-summary = lokalt TLS-materiale { $file } kunne ikke bruges
doctor-step2-material-failed-hint = { $err } — undersøg den nævnte fil; gendan den fra det udstedte bundle, eller roter profilen
doctor-step2-material-invalid-hint = { $file } er ikke et gyldigt X.509 PEM-certifikat; gendan det udstedte materiale eller kør `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem er ugyldig eller passer ikke til mtls.cert.pem; gendan det udstedte materiale eller kør `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokalt certifikat er uden for sin gyldighedsperiode
doctor-step2-cert-time-hint = kør `goat profile show`; tjek urresultatet ovenfor, bed derefter din operatør om et nyt bundle og kør `goat profile rotate <new-bundle.cbor>`, hvis certifikatet er udløbet
doctor-skipped-no-endpoint-summary = sprunget over — profile.toml mangler { $field }
doctor-skipped-no-profile-summary = sprunget over — ingen læsbare profilendepunkter for denne kontekst
doctor-skipped-no-portal-token-summary = sprunget over — intet læsbart portal-token for denne kontekst
status-ztol-tcp-ok = routerport { $host }:{ $port } accepterer TCP (kun port — se session)
status-session-ok = zenoh mTLS-session åbnet på { $ms }ms (router accepterede dette certifikat)
status-session-skipped = session ikke forsøgt (uden for mesh; se ztol)
status-session-material-missing = zenoh-session ikke forsøgt: { $detail }
status-session-config-failed = zenoh-sessionskonfiguration mislykkedes: { $err }
status-session-failed = zenoh-session kunne ikke åbnes: { $err } — kør `goat doctor`
status-session-timeout = zenoh-session kunne ikke åbnes på { $secs }s — routerporten accepterer TCP, men ingen session gennemføres (router hængt, certifikat afvist eller urskævhed); kør `goat doctor`
status-names-live = navne live (mesh-DNS svarede)
status-names-ip-only = navne ikke afprøvet (profilens endpoints er IP-literaler)
status-names-snapshot = navne pr. signeret snapshot, serienr. { $serial }, alder { $age } (mesh-DNS utilgængelig)
status-names-snapshot-aging = navne pr. signeret snapshot, serienr. { $serial }, alder { $age } — FORÆLDET (mesh-DNS utilgængelig; ret goat-coredns, så opdateres navne automatisk)
status-names-snapshot-daemon = navne pr. signeret snapshot, serienr. { $serial }, alder { $age } — daemon-lager (mesh-DNS utilgængelig)
status-names-snapshot-daemon-aging = navne pr. signeret snapshot, serienr. { $serial }, alder { $age } — daemon-lager, FORÆLDET (mesh-DNS utilgængelig; ret goat-coredns, så opdateres navne automatisk)
status-names-failed = mesh-DNS utilgængelig og snapshot-fallback ikke tilgængelig — { $reason }
status-pqc-on = wireguard PQC (rosenpass) TIL — { $qr }/{ $connected } forbundne peers kvanteresistente
status-pqc-off = wireguard PQC (rosenpass) FRA — klassiske wireguard-handshakes
status-pqc-unknown = wireguard PQC ukendt — { $reason }
status-device-none = navnebeskyttelse for hele enheden FRA — intet goat-clientd-navnelager (goat-kommandoer har stadig fallback; aktivér iht. name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd-navnelager findes (serienr. { $serial }), men dets forwarder svarer IKKE på { $addr } — daemon nede eller navnesubsystem hængt
status-device-active = navnebeskyttelse for hele enheden AKTIV — goat-clientd foran OS' mesh-DNS på { $addr } (lagerserienr. { $serial }, { $grade })
status-device-not-fronted = goat-clientd-forwarder oppe på { $addr } (lagerserienr. { $serial }, { $grade }) — OS ikke fronted; browsere bruger direkte mesh-DNS
status-device-fronting-unknown = goat-clientd-forwarder oppe på { $addr } (lagerserienr. { $serial }, { $grade }) — OS' fronting-tilstand kan ikke registreres på denne platform
status-names-observed = navne IKKE-KANONISKE (ad hoc): observeret record, alder { $age } — ikke i signeret snapshot (mesh-DNS utilgængelig; kodificér denne record)
status-names-selfclaim = navne via verificeret SELVKRAV, alder { $age } — signeret af navnets eget anker (mesh-DNS utilgængelig)
doctor-names-note = via snapshot, serienr. { $serial }, alder { $age }
doctor-names-selfclaim-note = via verificeret SELVKRAV, alder { $age } — signeret af navnets eget anker
doctor-names-selfclaim-hint = mesh-DNS er utilgængelig — dette trin blev opløst via et verificeret SELVKRAV (alder { $age }): navnets eget anker signerede denne binding, og den cachede snapshots peer_bindings godkender den. Signeret, men ikke live; ret mesh-DNS for at fjerne dette.
doctor-names-observed-note = via OBSERVERET (ad hoc) record, alder { $age } — ikke-kanonisk
doctor-names-observed-hint = mesh-DNS er utilgængelig — dette trin blev opløst via en IKKE-KANONISK observeret record (alder { $age }): navnet er kun live og aldrig kodificeret i sitets register. Kodificér det (group_vars-register → generator → gensignér), ellers udløber det; ret mesh-DNS for at fjerne dette.
doctor-names-hint = mesh-DNS er utilgængelig — dette trin blev opløst via det signerede navnesnapshot (serienr. { $serial }, alder { $age }). Fabric forbliver tilgængeligt på fallback-navne; ret mesh-DNS (goat-coredns på mgmt-værten) for at fjerne dette.
storm-brake-plan-header = Stormbremseplan (ADR 1081 lag 4 — manuel):
storm-brake-plan-target = mgmt-vært { $user }@{ $host }  (kontekst { $context })
storm-brake-step-flush = genstart signal-container `{ $container }` (dropper zombie-streams)
storm-brake-step-tighten = begræns L2-limiter på `{ $container }` (halvér rate-limit, hæv neg-cache-TTL) — { $window } min, tilbageføres af operatør; springes over hvis L2 ikke er udrullet
storm-brake-step-shed = sæt ikke-essentiel publisher-enhed `{ $unit }` på pause
storm-brake-shed-none = (ingen shed-enheder angivet — sæt storm_brake_shed_units i profile.toml eller angiv --shed-unit)
storm-brake-announce-note = send én OperatorActionV1 til revisionssporet med resultat pr. trin
storm-brake-dryrun-note = prøvekørsel — intet blev udført.
storm-brake-triggers-header = Udløs når relay-health host-net-båndet viser:
storm-brake-trigger-futility = ratio for resultatløs signalering >= 0.5 ved >= 100 msgs/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = uplink-tab >= 5% vedvarende i 10 min (wan.loss_pct)
storm-brake-triggers-band-url = host-bånd: relay-health.{ $site }.<zone> (se docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Bemærk: en signal-flush alene genopretter IKKE meshet. Lag 1-2 (klient-backoff + signal-shedding) er kuren; bremsen køber tid til genopretning.
storm-brake-confirm = Udløs stormbremsen mod { $user }@{ $host }? Genstarter signal-containeren og sætter angivne publishers på pause. [y/N]
storm-brake-aborted = stormbremse afbrudt (ingen bekræftelse)
storm-brake-executing = udløser stormbremse mod { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = fejlet
storm-brake-outcome-skipped = sprunget over
storm-brake-summary = stormbremse fuldført: { $ok } ok, { $failed } fejlet, { $skipped } sprunget over
storm-brake-flush-failed = FLUSH fejlede — signal-containeren blev ikke genstartet, så mesh-genopretning forventes ikke: { $reason }
storm-brake-mgmt-unresolved = mgmt-vært ikke fundet — angiv --mgmt-host, sæt mgmt_ssh_host i kontekstens profile.toml, eller eksportér GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operatørhandling registreret i revisionssporet (aktør { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portalen svarede HTTP 404 for `{ $path }`: ikke registreret der
error-portal-rate-limited-summary = portalen svarede HTTP 429 for `{ $path }`: for mange forespørgsler, prøv igen senere
error-portal-server-error-summary = portalen svarede HTTP { $status } for `{ $path }`: en fejl på portalens side
error-portal-refused-summary = portalen svarede HTTP { $status } for `{ $path }`: anmodningen blev afvist

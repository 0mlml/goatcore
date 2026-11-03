### goat-cli — Dutch (nl). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = geen profiel voor context `{ $context }`
error-off-mesh-summary = buiten de ZTOL — niet verbonden met de mesh
error-router-unreachable-summary = Zenoh-router op `{ $endpoint }` is onbereikbaar
error-cert-expired-summary = mTLS-certificaat verlopen of geweigerd door de router
error-portal-unreachable-summary = portal-API op `{ $endpoint }` is onbereikbaar
error-token-invalid-summary = portal heeft het Bearer-token geweigerd
error-topic-conflict-summary = topic `{ $topic }` bestaat al
error-bundle-invalid-summary = verificatie van bundle-handtekening of vervaldatum mislukt

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = router-eindpunt onjuist opgemaakt
doctor-step1-endpoint-malformed-hint = verwacht `<proto>/<host>:<port>`; ontvangen `{ $endpoint }`
doctor-step1-ok-summary = TCP-verbinding met router-host { $host }:{ $port } geslaagd
doctor-step1-conn-failed-summary = TCP-verbinding met router-host mislukt
doctor-step1-conn-failed-hint = { $err } — start netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); controleer `netbird status`
doctor-step1-timeout-summary = time-out bij TCP-verbinding met router-host
doctor-step1-timeout-hint = router-host { $host }:{ $port } onbereikbaar in { $ms }ms — waarschijnlijk buiten de mesh
doctor-step2-endpoint-malformed-hint = corrigeer de bundle-uitgever (--goat-cli-router-endpoint moet `<proto>/<host>:<port>` zijn)
doctor-step2-ca-missing-summary = CA-roots-PEM ontbreekt of onleesbaar
doctor-step2-ca-missing-hint = { $err } — voer `goat profile init <bundle.cbor>` opnieuw uit
doctor-step2-ok-summary = router { $host }:{ $port } accepteert TCP (TLS gevalideerd in stap 3)
doctor-step2-refused-summary = router-TCP geweigerd
doctor-step2-refused-hint = { $err } — router is mogelijk offline; controleer de Zenoh-implementatie op de mgmt-host
doctor-step2-timeout-summary = time-out bij router-TCP
doctor-step2-timeout-hint = router { $host }:{ $port } onbereikbaar in { $ms }ms
doctor-step3-config-failed-summary = opbouwen van zenoh-configuratie mislukt
doctor-step3-config-failed-hint = ongeldig eindpunt `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = opbouwen van zenoh-TLS-configuratie mislukt
doctor-step3-tls-config-failed-hint = { $err } — waarschijnlijk zenoh-versieafwijking; controleer of de afhankelijkheden overeenkomen
doctor-step3-ok-summary = mTLS-sessie geopend — certificaat geaccepteerd door router-ACL
doctor-step3-open-failed-summary = openen van zenoh-sessie mislukt
doctor-step3-open-failed-hint = { $err } — certificaat is mogelijk verlopen (voer `goat profile show` uit) of geweigerd door de router-ACL (URI-SAN-mismatch)
doctor-step3-timeout-summary = time-out bij openen van zenoh-sessie
doctor-step3-timeout-hint = router voltooide de handshake niet in { $secs }s — klokafwijking, verkeerd certificaat of congestie
doctor-step4-ok-summary = portal gaf { $n } organisatierij(en) terug
doctor-step4-token-rejected-summary = portal heeft het Bearer-token geweigerd
doctor-step4-token-rejected-hint = HTTP { $status } — token verlopen of ingetrokken; opnieuw onboarden via operator-onboard.yml
doctor-step4-request-failed-summary = portalverzoek mislukt
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = time-out bij portalverzoek
doctor-step4-timeout-hint = geen reactie in { $secs }s — controleer of { $endpoint } bereikbaar is vanuit de mesh
doctor-skipped-summary = overgeslagen — vorige stap mislukt (voer opnieuw uit met --all om te forceren)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Uniforme operator-opdrachtregel voor het goat-datafabric.
help-long-about = goat — operatorgerichte CLI die panoscope-core tot één tool samenstelt: profiel initialiseren/hergebruiken, ZTOL-bewuste bereikbaarheids-doctor, organisatie-portal doorbladeren, topic-CRUD, pub/sub, stethoscope-delegatie, een interactieve opdracht-composer en substraat-cascadeverhaal.
help-flag-profile-dir = De profielmap overschrijven (standaard: $GOAT_PROFILE_DIR of ~/.goat).
help-flag-context = De actieve context overschrijven (standaard: $GOAT_CONTEXT-omgeving, dan current_context in config.toml, dan "default").
help-flag-narrate = De substraatcascade tonen die een muterende opdracht in gang zet.
help-flag-verbose = Uitgebreide debug-uitvoer inschakelen (of RUST_LOG=goat=debug instellen).
help-flag-color = Kleurmodus voor terminaluitvoer (auto|always|never).
help-flag-lang = Taal voor mensgerichte tekst (bijv. en, uk). Valt terug op GOAT_LANG, dan de OS-locale, dan Engels.
help-cmd-profile = Het lokale goat-profiel beheren (mTLS-certificaat + portal-token + endpointset).
help-cmd-context = Contexten beheren (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Een stapsgewijze bereikbaarheidsprober tegen het fabric uitvoeren.
help-cmd-doctor-long = Stapsgewijze bereikbaarheidsprober tegen het fabric: ZTOL-lidmaatschap → Zenoh-router → certificaataanvaarding → portal. Elke mislukte stap geeft een specifieke herstelhint.
help-cmd-vendor = In het portal geregistreerde organisaties doorbladeren.
help-cmd-topic = topics weergeven, tonen, aanmaken of verwijderen.
help-cmd-pub = Een payload naar een topic-sleutel publiceren.
help-cmd-sub = Inschrijven op een topic-sleutelexpressie.
help-cmd-inspect = Een diagnostisch oordeelsverzoek delegeren aan stethoscope.
help-cmd-ui = Interactieve samengevoegde kiezer: de gedeclareerde ⊕ live-lijst doorbladeren en naar de Diagnose-/Tail-lens op het geselecteerde doel springen.
help-cmd-compose = Interactieve opdrachtbouwer-TUI.
help-cmd-narrate = Substraat-cascadereflectie over recente activiteit. Nog niet beschikbaar (het alledaagse pad is --narrate op een muterende opdracht).
help-cmd-completion = Shell-aanvullingsscript naar stdout afdrukken.
help-cmd-explain = De betekenis + het herstel van een foutcode opzoeken in het ingebedde `goat-cli-error-codes.md`-document (cargo --explain-patroon).
help-cmd-mock = Synthetische monsters naar een topic publiceren. Standaard: triviale hello-world-JSON op 1Hz. Geef `--format <.fds>` + `--message <Name>` door om in plaats daarvan door de operator aangeleverde protobuf te coderen.
help-cmd-status = Gezondheidssynthese: ZTOL-bereikbaarheid + portal /api/health + lokaal certificaatverloop, drie glyph-regels of één --json-object.
help-cmd-services = Elke gehoste service in `ops/serve/services.yaml` testen en één glyph-regel per service renderen (of één --json-object).
help-cmd-client = De lokale goat-clientd-daemon omwikkelen via zijn JSON-RPC-IPC-socket (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH-gebaseerde extensiedetectie (kubectl-stijl). Elk `goat-<name>`-uitvoerbaar bestand op PATH is aanroepbaar als `goat <name>`; `goat plugin list` somt geïnstalleerde plugins op, `goat plugin which <name>` lost het pad op.

# goat status / inspect / --narrate runtime prose (ADR 1024).
# Contract tokens + backticked keys + glyphs → — reproduced verbatim.
status-ztol-malformed = router-eindpunt onjuist opgemaakt: `{ $endpoint }`
status-ztol-refused = TCP geweigerd naar router { $host }:{ $port }: { $err }
status-ztol-unreachable = buiten de mesh (router { $host }:{ $port } onbereikbaar in { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health mislukt: { $err }
status-portal-timeout = portal /api/health time-out na { $secs }s
status-cert-read-failed = lezen van certificaat mislukt: { $err }
status-cert-pem-parse-failed = PEM-parsing van certificaat mislukt: { $err }
status-cert-wrong-tag = eerste PEM-blok is { $tag }, verwacht CERTIFICATE
status-cert-x509-parse-failed = x509-parsing mislukt: { $err }
status-cert-expired = certificaat VERLOPEN op unix={ $not_after }, nu={ $now }
status-cert-valid = certificaat geldig; verloopt over { $days_left }d (unix={ $not_after })
inspect-finding-label = bevinding ({ $severity }): { $label } — { $detail }
inspect-remedy-label = oplossing: { $title } — { $detail }
inspect-no-outcomes = (geen uitkomsten — doel kwam met geen enkele controle overeen)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh-put geaccepteerd op `{ $key }` ({ $bytes } bytes)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = cascade bezonken — { $summary } ({ $n } { $n ->
        [one] gevolg
       *[other] gevolgen
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Wizard bij eerste gebruik: kies sandbox of echt, bewijs dat het werkt, verwijs naar de tutor.
help-cmd-tutor = Een zelfgestuurde, praktische rondleiding door het fabric (vimtutor-stijl; hervatbaar).
help-cmd-docs = Blader door de goat-documentatie of geef de volledige opdrachtreferentie uit (--markdown).
help-cmd-hello = Stuur een hallo end-to-end heen en terug door het fabric (sandbox- of echte omgeving).
flourish-all-green = 🐐 alles groen — fabric nominaal

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = alleen voor operators — je bent niet bevoegd (`goat admin` is inactief voor niet-admins)
help-flag-narrate-json = Geef de substraatcascade als NDJSON (één object per consequentie) voor incidentlogs en scripting. Impliceert --narrate.
help-cmd-admin = Beheer exclusief voor operators: organisatie-lifecycle, onboarden/intrekken/roteren, mesh-peer afsluiten, apparaat- en operatorprovisioning, LAN-triage. Geautoriseerd via jouw operatorcredentials (server-side via portal/NetBird-tokenscope); inactief voor niet-admins — een onbevoegde aanroep wijzigt niets en mislukt met `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = het profiel voor context `{ $context }` bestaat, maar is niet leesbaar voor deze gebruiker (toegang geweigerd)
error-portal-bad-response-summary = portal antwoordde, maar deze goat-build kon het antwoord niet parsen
doctor-step0-ok-summary = lokale setup is klaar voor netwerkprobes
doctor-step0-warn-summary = lokale setup heeft controles die niet bevestigd konden worden
doctor-step0-fail-summary = lokale setup vereist aandacht
doctor-step0-profile-absent-summary = geen goat-profiel voor context `{ $context }`
doctor-step0-profile-new-hint = controleer `goat context ls`; bij een eerste setup: voer `goat ca add <bundle-ca.pem>` uit en vergelijk de vingerafdruk met die van je operator, daarna `goat profile init <bundle.cbor>`; draai `goat doctor` opnieuw
doctor-step0-profile-absent-hint = controleer `goat context ls`; kies een bestaande context met `goat context use <name>`, of voer `goat profile init <bundle.cbor>` uit voor deze context; draai `goat doctor` opnieuw
doctor-step0-profile-denied-summary = goat-profiel op { $path } is onleesbaar
doctor-step0-profile-denied-hint = herstel de eigenaar van het profiel of draai als de eigenaar; initialiseer een bestaand profiel niet opnieuw
doctor-step0-profile-incomplete-summary = goat-profielbestand { $file } ontbreekt of is onleesbaar
doctor-step0-profile-incomplete-hint = herstel het genoemde bestand uit de uitgegeven bundle, of gebruik `goat profile rotate <bundle.cbor>` om deze context te vervangen; draai `goat doctor` opnieuw
doctor-step0-profile-invalid-summary = goat-profiel heeft onleesbare of ontbrekende endpoints
doctor-step0-profile-empty-token-summary = goat-profiel heeft een leeg portaltoken
doctor-step0-profile-ok-summary = goat-context `{ $context }` heeft leesbaar profielmateriaal
doctor-step0-client-absent-summary = geen ondersteunde mesh-client reageerde
doctor-step0-client-absent-hint = installeer de mesh-client van je goatnet, start de daemon en draai `goat doctor` opnieuw
doctor-step0-client-stopped-summary = geïnstalleerde mesh-client gaf geen bruikbare status terug
doctor-step0-client-stopped-hint = controleer `netbird status -d` of `goat client status --detail`; start de geïnstalleerde daemon (`sudo netbird service start` voor NetBird) en draai `goat doctor` opnieuw
doctor-step0-netbird-ok-summary = NetBird-daemon antwoordde (gemeld profiel `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd antwoordde (status `{ $state }`)
doctor-step0-management-connected-summary = NetBird meldt management verbonden
doctor-step0-management-disconnected-summary = NetBird meldt management verbroken
doctor-step0-management-disconnected-hint = controleer de gemelde management-URL en `netbird status -d`; een bestaande tunnel kan tijdens een management-storing nog werken
doctor-step0-management-unknown-summary = geconfigureerd management-endpoint kon lokaal niet worden vastgesteld
doctor-step0-management-unknown-hint = controleer `netbird status -d` of `goat client diagnostics`; doctor raadt geen management-URL
doctor-step0-tunnel-down-summary = NetBird meldt geen mesh-adres
doctor-step0-tunnel-down-hint = voer `netbird status -d` uit; als het apparaat is aangemeld en de daemon reageert, voer `goat heal` uit en daarna opnieuw `goat doctor`
doctor-step0-tunnel-address-summary = NetBird meldt mesh-adres { $address }; hierna wordt de bereikbaarheid van de router gecontroleerd
doctor-step0-goat-client-no-bundle-summary = goat-clientd heeft geen geïmporteerde bundle
doctor-step0-goat-client-no-bundle-hint = voer `goat client bundle import <bundle.cbor>` uit met de enrollment-bundle van de client, verbind de client en voer daarna opnieuw `goat doctor` uit
doctor-step0-goat-client-connected-summary = goat-clientd meldt een verbonden tunnel; hierna wordt de bereikbaarheid van de router gecontroleerd
doctor-step0-goat-client-outer-disabled-summary = goat-clientd buitentunnel is uitgeschakeld in netbird-only-modus
doctor-step0-goat-client-down-summary = goat-clientd meldt tunnelstatus `{ $state }`
doctor-step0-goat-client-down-hint = voer `goat client diagnostics` uit, daarna `goat client up` als de bundle is geladen; voer opnieuw `goat doctor` uit
doctor-step0-goat-client-inner-ok-summary = goat-clientd meldt dat de binnenste mesh verbonden is
doctor-step0-goat-client-inner-disabled-summary = goat-clientd binnenste mesh is uitgeschakeld in wg-cp0-only-modus
doctor-step0-goat-client-inner-down-summary = goat-clientd meldt binnenste-meshstatus `{ $state }`
doctor-step0-goat-client-inner-down-hint = voer `goat client diagnostics` uit om de binnenste mesh te inspecteren en voer na herstel opnieuw `goat doctor` uit
doctor-step0-management-reachable-summary = management-endpoint antwoordde met HTTP { $status }
doctor-step0-management-auth-summary = management-HTTPS-endpoint is bereikbaar (HTTP { $status } vereist autorisatie voor deze probe)
doctor-step0-management-unexpected-summary = management-endpoint gaf onverwachte HTTP { $status }
doctor-step0-management-unexpected-hint = controleer de geconfigureerde management-URL en vraag je goatnet-operator dit antwoord te controleren
doctor-step0-management-unreachable-summary = geconfigureerd management-endpoint niet bereikbaar: { $error }
doctor-step0-management-unreachable-hint = controleer DNS en netwerktoegang naar de URL die de client meldt; een bestaande tunnel kan nog werken
doctor-step0-clock-ok-summary = lokale klok wijkt minder dan { $seconds } seconden af van de HTTP-tijd van management
doctor-step0-clock-skew-summary = lokale klok wijkt { $seconds } seconden af van de HTTP-tijd van management
doctor-step0-clock-skew-hint = synchroniseer de klok van het apparaat en voer opnieuw `goat doctor` uit; certificaatvalidatie kan mislukken zolang de klok niet klopt
doctor-step0-clock-unknown-summary = klokafwijking kon niet worden gemeten via een HTTP Date-header van management
doctor-step0-clock-unknown-hint = controleer de tijdsynchronisatie van het apparaat als TLS- of certificaatcontroles falen
error-clock-skew-summary = de klok van het apparaat wijkt te veel af van de beheertijd
error-doctor-inconclusive-summary = doctor zag een storing zonder bewezen oorzaak
error-portal-forbidden-summary = de portal weigerde dit account toegang tot de getestte API
doctor-step4-forbidden-summary = portal gaf HTTP 403 voor dit profiel
doctor-step4-forbidden-hint = deze credential bereikte de portal maar heeft geen toegang tot de getestte API; controleer `goat profile show` en vraag je goatnet-operator de toekenning te verifiëren
doctor-step4-decode-summary = portal antwoordde met een respons die deze goat niet kan lezen
doctor-step4-decode-hint = { $err } — update goat naar de versie die deze goatnet serveert en draai `goat doctor` opnieuw
doctor-step1-control-only-summary = alleen verbonden met de controleserver — geen tunnel naar een van de { $total } peers
doctor-step1-control-only-hint = dit netwerk laat TCP 443 naar buiten door maar blokkeert de mesh-dataporten, dus niets op de mesh (inclusief inlogpagina's) is bereikbaar vanaf dit apparaat. Vraag het netwerkteam om uitgaand TCP en UDP naar { $relays } toe te staan, en UDP 3478 naar { $stun }; direct UDP tussen apparaten wordt ook aanbevolen. Ter controle: verbind met een telefoon-hotspot en draai `netbird status`: peers zouden dan moeten verbinden.
doctor-offmesh-dashboard-hint = relay-vloot (extern vantagepunt): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokaal certificaat, bijbehorende sleutel en CA-bundel zijn leesbaar; certificaat valt binnen de geldigheidsperiode
doctor-step2-material-failed-summary = lokaal TLS-materiaal { $file } kon niet worden gebruikt
doctor-step2-material-failed-hint = { $err } — controleer het genoemde bestand; herstel het uit de uitgegeven bundel of roteer het profiel
doctor-step2-material-invalid-hint = { $file } is geen geldig X.509 PEM-certificaat; herstel het uitgegeven materiaal of draai `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem is ongeldig of past niet bij mtls.cert.pem; herstel het uitgegeven materiaal of draai `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokaal certificaat valt buiten de geldigheidsperiode
doctor-step2-cert-time-hint = draai `goat profile show`; controleer het klokresultaat hierboven, vraag je operator om een nieuwe bundel en draai `goat profile rotate <new-bundle.cbor>` als het certificaat is verlopen
doctor-skipped-no-endpoint-summary = overgeslagen — profile.toml mist { $field }
doctor-skipped-no-profile-summary = overgeslagen — geen leesbare profiel-endpoints voor deze context
doctor-skipped-no-portal-token-summary = overgeslagen — geen leesbaar portaltoken voor deze context
status-ztol-tcp-ok = routerpoort { $host }:{ $port } accepteert TCP (alleen poort — zie sessie)
status-session-ok = zenoh mTLS-sessie geopend in { $ms }ms (router accepteerde dit certificaat)
status-session-skipped = sessie niet geprobeerd (buiten mesh; zie ztol)
status-session-material-missing = zenoh-sessie niet geprobeerd: { $detail }
status-session-config-failed = zenoh-sessieconfiguratie mislukt: { $err }
status-session-failed = zenoh-sessie kon niet worden geopend: { $err } — voer `goat doctor` uit
status-session-timeout = zenoh-sessie niet geopend binnen { $secs }s — de routerpoort accepteert TCP maar er komt geen sessie tot stand (router vastgelopen, certificaat afgewezen of klokafwijking); voer `goat doctor` uit
status-names-live = namen live (mesh-DNS heeft geantwoord)
status-names-ip-only = namen niet getest (profiel-endpoints zijn IP-literals)
status-names-snapshot = namen volgens ondertekende snapshot serial { $serial }, leeftijd { $age } (mesh-DNS onbereikbaar)
status-names-snapshot-aging = namen volgens ondertekende snapshot serial { $serial }, leeftijd { $age } — VERLOPEN (mesh-DNS onbereikbaar; herstel goat-coredns, daarna verversen namen automatisch)
status-names-snapshot-daemon = namen volgens ondertekende snapshot serial { $serial }, leeftijd { $age } — daemon-store (mesh-DNS onbereikbaar)
status-names-snapshot-daemon-aging = namen volgens ondertekende snapshot serial { $serial }, leeftijd { $age } — daemon-store, VERLOPEN (mesh-DNS onbereikbaar; herstel goat-coredns, daarna verversen namen automatisch)
status-names-failed = mesh-DNS onbereikbaar en snapshot-fallback niet beschikbaar — { $reason }
status-pqc-on = wireguard PQC (rosenpass) AAN — { $qr }/{ $connected } verbonden peers quantumbestendig
status-pqc-off = wireguard PQC (rosenpass) UIT — klassieke wireguard-handshakes
status-pqc-unknown = wireguard PQC onbekend — { $reason }
status-device-none = apparaatbrede naambescherming UIT — geen goat-clientd-naamstore (goat-commando's hebben nog wel fallback; inschakelen volgens name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd-naamstore aanwezig (serial { $serial }) maar de forwarder antwoordt NIET op { $addr } — daemon down of naamsubsysteem vastgelopen
status-device-active = apparaatbrede naambescherming ACTIEF — goat-clientd staat voor de mesh-DNS van het OS op { $addr } (store serial { $serial }, { $grade })
status-device-not-fronted = goat-clientd-forwarder actief op { $addr } (store serial { $serial }, { $grade }) — OS niet gefrontet; browsers gebruiken directe mesh-DNS
status-device-fronting-unknown = goat-clientd-forwarder actief op { $addr } (store serial { $serial }, { $grade }) — fronting-status van het OS niet detecteerbaar op dit platform
status-names-observed = namen NIET-CANONIEK (ad hoc): waargenomen record, leeftijd { $age } — niet in ondertekende snapshot (mesh-DNS onbereikbaar; leg dit record vast)
status-names-selfclaim = namen via geverifieerde SELF-CLAIM, leeftijd { $age } — ondertekend door het eigen anker van de naam (mesh-DNS onbereikbaar)
doctor-names-note = via snapshot serial { $serial }, leeftijd { $age }
doctor-names-selfclaim-note = via geverifieerde SELF-CLAIM, leeftijd { $age } — ondertekend door het eigen anker van de naam
doctor-names-selfclaim-hint = mesh-DNS is onbereikbaar — deze stap is opgelost via een geverifieerde SELF-CLAIM (leeftijd { $age }): het eigen anker van de naam heeft deze binding ondertekend en de peer_bindings van de gecachte snapshot autoriseren die. Ondertekend maar niet live; herstel mesh-DNS om dit te wissen.
doctor-names-observed-note = via WAARGENOMEN (ad hoc) record, leeftijd { $age } — niet-canoniek
doctor-names-observed-hint = mesh-DNS is onbereikbaar — deze stap is opgelost via een NIET-CANONIEK waargenomen record (leeftijd { $age }): de naam bestaat alleen live en is nooit vastgelegd in het siteregister. Leg hem vast (group_vars-register → generator → opnieuw ondertekenen) anders verloopt hij; herstel mesh-DNS om dit te wissen.
doctor-names-hint = mesh-DNS is onbereikbaar — deze stap is opgelost via de ondertekende naamsnapshot (serial { $serial }, leeftijd { $age }). De fabric blijft bereikbaar via fallback-namen; herstel mesh-DNS (goat-coredns op de mgmt-host) om dit te wissen.
storm-brake-plan-header = Stormrem-plan (ADR 1081 Laag 4 — handmatig):
storm-brake-plan-target = mgmt-host { $user }@{ $host }  (context { $context })
storm-brake-step-flush = herstart signaal-container `{ $container }` (verwijdert zombie-streams)
storm-brake-step-tighten = L2-limiter op `{ $container }` aanscherpen (rate-limit halveren, neg-cache-TTL verhogen) — { $window } min, handmatig terugdraaien; overgeslagen als L2 niet is uitgerold
storm-brake-step-shed = pauzeer niet-essentiële publisher-unit `{ $unit }`
storm-brake-shed-none = (geen shed-units opgegeven — stel storm_brake_shed_units in in profile.toml of geef --shed-unit mee)
storm-brake-announce-note = schrijf één OperatorActionV1 naar het audittrail met de uitkomst per stap
storm-brake-dryrun-note = proefrun — niets uitgevoerd.
storm-brake-triggers-header = Activeer wanneer de host-net-band van relay-health toont:
storm-brake-trigger-futility = signaling-futility-ratio >= 0.5 bij >= 100 msgs/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = uplinkverlies >= 5% gedurende 10 min (wan.loss_pct)
storm-brake-triggers-band-url = host-band: relay-health.{ $site }.<zone> (zie docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Let op: alleen een signal-flush herstelt de mesh NIET. Laag 1-2 (client-backoff + signal-shedding) zijn de oplossing; de stormrem koopt hersteltijd.
storm-brake-confirm = Stormrem activeren op { $user }@{ $host }? Herstart de signaal-container en pauzeert opgegeven publishers. [y/N]
storm-brake-aborted = stormrem afgebroken (geen bevestiging)
storm-brake-executing = stormrem activeren op { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = mislukt
storm-brake-outcome-skipped = overgeslagen
storm-brake-summary = stormrem klaar: { $ok } ok, { $failed } mislukt, { $skipped } overgeslagen
storm-brake-flush-failed = FLUSH mislukt — de signaal-container is niet herstart, dus mesh-herstel wordt niet verwacht: { $reason }
storm-brake-mgmt-unresolved = mgmt-host niet bepaald — geef --mgmt-host mee, stel mgmt_ssh_host in in de profile.toml van de context, of exporteer GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operatoractie vastgelegd in het audittrail (actor { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = het portaal antwoordde HTTP 404 voor `{ $path }`: daar niet geregistreerd
error-portal-rate-limited-summary = het portaal antwoordde HTTP 429 voor `{ $path }`: te veel verzoeken, probeer het later opnieuw
error-portal-server-error-summary = het portaal antwoordde HTTP { $status } voor `{ $path }`: een fout aan de kant van het portaal
error-portal-refused-summary = het portaal antwoordde HTTP { $status } voor `{ $path }`: verzoek geweigerd

### goat-cli — Latvian (lv). NATO-member language, Tier 2. oss-allow: language name
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = nav profila kontekstam `{ $context }`
error-off-mesh-summary = ārpus ZTOL — nav savienojuma ar mesh tīklu
error-router-unreachable-summary = Zenoh maršrutētājs adresē `{ $endpoint }` nav sasniedzams
error-cert-expired-summary = mTLS sertifikāts ir beidzies vai maršrutētājs to noraidīja
error-portal-unreachable-summary = portāla API adresē `{ $endpoint }` nav sasniedzams
error-token-invalid-summary = portāls noraidīja Bearer marķieri
error-topic-conflict-summary = topic `{ $topic }` jau pastāv
error-bundle-invalid-summary = bundle paraksta vai derīguma pārbaude neizdevās

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = maršrutētāja galapunkts ir nepareizi formatēts
doctor-step1-endpoint-malformed-hint = gaidīts `<proto>/<host>:<port>`; saņemts `{ $endpoint }`
doctor-step1-ok-summary = TCP savienojums ar maršrutētāja resursdatoru { $host }:{ $port } veiksmīgs
doctor-step1-conn-failed-summary = TCP savienojums ar maršrutētāja resursdatoru neizdevās
doctor-step1-conn-failed-hint = { $err } — palaidiet netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); pārbaudiet `netbird status`
doctor-step1-timeout-summary = iestājās TCP savienojuma ar maršrutētāja resursdatoru noildze
doctor-step1-timeout-hint = maršrutētāja resursdators { $host }:{ $port } nav sasniedzams { $ms }ms laikā — iespējams, ārpus mesh
doctor-step2-endpoint-malformed-hint = labojiet bundle izdevēju (--goat-cli-router-endpoint jābūt `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = CA sakņu PEM trūkst vai nav nolasāms
doctor-step2-ca-missing-hint = { $err } — palaidiet vēlreiz `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = maršrutētājs { $host }:{ $port } pieņem TCP (TLS validēts 3. solī)
doctor-step2-refused-summary = maršrutētāja TCP noraidīts
doctor-step2-refused-hint = { $err } — maršrutētājs var būt izslēgts; pārbaudiet Zenoh izvietojumu mgmt-host
doctor-step2-timeout-summary = iestājās maršrutētāja TCP noildze
doctor-step2-timeout-hint = maršrutētājs { $host }:{ $port } nav sasniedzams { $ms }ms laikā
doctor-step3-config-failed-summary = zenoh konfigurācijas izveide neizdevās
doctor-step3-config-failed-hint = nederīgs galapunkts `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = zenoh TLS konfigurācijas izveide neizdevās
doctor-step3-tls-config-failed-hint = { $err } — iespējams, zenoh versiju nesakritība; pārbaudiet atkarību atbilstību
doctor-step3-ok-summary = mTLS sesija atvērta — sertifikātu pieņēma maršrutētāja ACL
doctor-step3-open-failed-summary = zenoh sesijas atvēršana neizdevās
doctor-step3-open-failed-hint = { $err } — sertifikāts var būt beidzies (palaidiet `goat profile show`) vai to noraidīja maršrutētāja ACL (URI-SAN neatbilstība)
doctor-step3-timeout-summary = iestājās zenoh sesijas atvēršanas noildze
doctor-step3-timeout-hint = maršrutētājs nepabeidza rokasspiedienu { $secs }s laikā — pulksteņa nobīde, nepareizs sertifikāts vai pārslodze
doctor-step4-ok-summary = portāls atgrieza { $n } organizāciju rindas
doctor-step4-token-rejected-summary = portāls noraidīja Bearer marķieri
doctor-step4-token-rejected-hint = HTTP { $status } — marķieris beidzies vai atsaukts; reģistrējieties vēlreiz, izmantojot operator-onboard.yml
doctor-step4-request-failed-summary = pieprasījums portālam neizdevās
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = iestājās pieprasījuma portālam noildze
doctor-step4-timeout-hint = nav atbildes { $secs }s laikā — pārbaudiet, vai { $endpoint } ir sasniedzams no mesh
doctor-skipped-summary = izlaists — iepriekšējais solis neizdevās (palaidiet vēlreiz ar --all, lai piespiestu)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Vienota operatora komandrinda goat datu audumam.
help-long-about = goat — operatoram paredzēts CLI, kas apvieno panoscope-core vienā rīkā: profila init/atkārtota izmantošana, ZTOL apzinošs sasniedzamības doctor, organizāciju portāla pārlūkošana, topic CRUD, pub/sub, stethoscope deleģēšana, interaktīvs komandu veidotājs un substrāta kaskādes stāstījums.
help-flag-profile-dir = Ignorēt profila direktoriju (noklusējums: $GOAT_PROFILE_DIR vai ~/.goat).
help-flag-context = Ignorēt aktīvo kontekstu (noklusējums: $GOAT_CONTEXT vides mainīgais, tad current_context failā config.toml, tad "default").
help-flag-narrate = Rādīt substrāta kaskādi, ko izraisa modificējoša komanda.
help-flag-verbose = Iespējot detalizētu atkļūdošanas izvadi (vai iestatīt RUST_LOG=goat=debug).
help-flag-color = Krāsu režīms termināļa izvadei (auto|always|never).
help-flag-lang = Cilvēkam paredzētā teksta valoda (piem. en, uk). Atkāpšanās uz GOAT_LANG, tad OS lokāli, tad angļu valodu.
help-cmd-profile = Pārvaldīt vietējo goat profilu (mTLS sertifikāts + portāla marķieris + galapunktu kopa).
help-cmd-context = Pārvaldīt kontekstus (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Palaist pakāpenisku sasniedzamības prober pret audumu.
help-cmd-doctor-long = Pakāpenisks sasniedzamības prober pret audumu: ZTOL dalība → Zenoh maršrutētājs → sertifikāta pieņemšana → portāls. Katrs neizdevies solis izvada konkrētu labošanas padomu.
help-cmd-vendor = Pārlūkot portālā reģistrētās organizācijas.
help-cmd-topic = Uzskaitīt, parādīt, izveidot vai dzēst topic.
help-cmd-pub = Publicēt lietderīgo slodzi topic atslēgā.
help-cmd-sub = Abonēt topic atslēgas izteiksmi.
help-cmd-inspect = Deleģēt diagnostisko verdikta pieprasījumu stethoscope.
help-cmd-ui = Interaktīvs apvienots atlasītājs: pārlūkot deklarēto ⊕ live sarakstu un pārlēkt uz Diagnose / Tail lēcu izvēlētajā mērķī.
help-cmd-compose = Interaktīvs komandu veidotāja TUI.
help-cmd-narrate = Substrāta kaskādes atspoguļojums par neseno darbību. Vēl nav pieejams (ikdienas ceļš ir --narrate uz modificējošas komandas).
help-cmd-completion = Drukāt čaulas pabeigšanas skriptu uz stdout.
help-cmd-explain = Uzmeklēt kļūdas koda nozīmi + labojumu iegultajā `goat-cli-error-codes.md` dokumentā (cargo --explain modelis).
help-cmd-mock = Publicēt sintētiskus paraugus topic. Noklusējums: triviāls hello-world JSON ar 1Hz. Padodiet `--format <.fds>` + `--message <Name>`, lai tā vietā kodētu operatora sniegto protobuf.
help-cmd-status = Veselības sintēze: ZTOL sasniedzamība + portāla /api/health + vietējā sertifikāta derīguma termiņš, trīs glyph rindas vai viens --json objekts.
help-cmd-services = Pārbaudīt katru izvietoto pakalpojumu failā `ops/serve/services.yaml` un attēlot vienu glyph rindu katram pakalpojumam (vai vienu --json objektu).
help-cmd-client = Aptīt vietējo goat-clientd daemon caur tā JSON-RPC IPC ligzdu (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH balstīta paplašinājumu atklāšana (kubectl stilā). Jebkurš PATH esošs `goat-<name>` izpildāmais ir izsaucams kā `goat <name>`; `goat plugin list` uzskaita instalētos spraudņus, `goat plugin which <name>` atrod ceļu.

# ---------------------------------------------------------------------
# goat status / inspect / --narrate izpildlaika teksts (machine-translated; ADR 1024).
# Contract tokens `{ $var }`, atpakaļvērstās pēdiņas, → —, un fiksētie
# literāļi (/api/health, 2xx, PEM, CERTIFICATE, unix=, TCP, x509, Zenoh,
# stethoscope, (portal-confirmed)) atkārtoti burtiski.
# ---------------------------------------------------------------------
status-ztol-malformed = maršrutētāja galapunkts ir nepareizi formatēts: `{ $endpoint }`
status-ztol-refused = TCP noraidīts maršrutētājam { $host }:{ $port }: { $err }
status-ztol-unreachable = ārpus mesh (maršrutētājs { $host }:{ $port } nav sasniedzams { $ms }ms laikā)
status-portal-ok = portāla /api/health 2xx
status-portal-failed = portāla /api/health neizdevās: { $err }
status-portal-timeout = iestājās portāla /api/health noildze { $secs }s laikā
status-cert-read-failed = sertifikāta nolasīšana neizdevās: { $err }
status-cert-pem-parse-failed = sertifikāta PEM parsēšana neizdevās: { $err }
status-cert-wrong-tag = pirmais PEM bloks ir { $tag }, nepieciešams CERTIFICATE
status-cert-x509-parse-failed = x509 parsēšana neizdevās: { $err }
status-cert-expired = sertifikāts IR BEIDZIES ar unix={ $not_after }, tagad={ $now }
status-cert-valid = sertifikāts ir derīgs; beidzas pēc { $days_left }d (unix={ $not_after })
inspect-finding-label = atklājums ({ $severity }): { $label } — { $detail }
inspect-remedy-label = risinājums: { $title } — { $detail }
inspect-no-outcomes = (nav rezultātu — mērķis neatbilda nevienai pārbaudei)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put pieņemts atslēgā `{ $key }` ({ $bytes } baiti)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskāde nostabilizējās — { $summary } ({ $n } { $n ->
        [zero] seku
        [one] sekas
       *[other] sekas
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Pirmās palaišanas vednis: izvēlieties sandbox vai īsto, pierādiet, ka tas darbojas, norādiet uz pasniedzēju.
help-cmd-tutor = Pašregulēts, praktisks auduma apskats (vimtutor stilā; atsākams).
help-cmd-docs = Pārlūkojiet goat dokumentus vai izvadiet pilnu komandu atsauci (--markdown).
help-cmd-hello = Pārsūtiet hello caur audumu no gala līdz galam (sandbox vai īsta vide).
flourish-all-green = 🐐 viss zaļš — audums darbojas normāli

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = tikai operatora darbība — jums nav atļaujas (`goat admin` ir neaktīvs ne-administratoriem)
help-flag-narrate-json = Izvadīt substrāta kaskādi kā NDJSON (viens objekts uz seku) incidentu žurnāliem + skriptošanai. Ietver --narrate.
help-cmd-admin = Tikai operatora administrēšana: organizācijas dzīves cikls, pievienošana/atsaukšana/rotācija, tīkla vienādranga pārtraukšana, ierīces + operatora līmeņa nodrošināšana, LAN diagnostika. Autorizē ar operatora akreditācijas datiem (servera pusē, izmantojot portāla/NetBird tokena tvērumu); neaktīvs ne-administratoriem — neautorizēts izsaukums neko nemaina un neizdodas ar `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profils kontekstam `{ $context }` pastāv, bet šim lietotājam nav lasāms (piekļuve liegta)
error-portal-bad-response-summary = portāls atbildēja, bet šis goat būvējums nespēja apstrādāt atbildi
doctor-step0-ok-summary = lokālā iestatīšana ir gatava tīkla pārbaudēm
doctor-step0-warn-summary = lokālajā iestatīšanā ir pārbaudes, ko nevarēja apstiprināt
doctor-step0-fail-summary = lokālajai iestatīšanai nepieciešama uzmanība
doctor-step0-profile-absent-summary = nav goat profila kontekstam `{ $context }`
doctor-step0-profile-new-hint = pārbaudi `goat context ls`; ja šī ir pirmā iestatīšana, palaid `goat ca add <bundle-ca.pem>` un salīdzini tā pirkstu nospiedumu ar operatora norādīto, pēc tam `goat profile init <bundle.cbor>`; palaid `goat doctor` vēlreiz
doctor-step0-profile-absent-hint = pārbaudi `goat context ls`; izvēlies esošu kontekstu ar `goat context use <name>` vai palaid `goat profile init <bundle.cbor>` šim kontekstam; palaid `goat doctor` vēlreiz
doctor-step0-profile-denied-summary = goat profils { $path } nav lasāms
doctor-step0-profile-denied-hint = labo profila īpašnieku vai palaid kā īpašnieks; neinicializē esošu profilu no jauna
doctor-step0-profile-incomplete-summary = goat profila fails { $file } trūkst vai nav lasāms
doctor-step0-profile-incomplete-hint = atjauno norādīto failu no izsniegtā komplekta vai izmanto `goat profile rotate <bundle.cbor>`, lai aizstātu šo kontekstu; palaid `goat doctor` vēlreiz
doctor-step0-profile-invalid-summary = goat profilā galapunkti ir nelasāmi vai trūkst
doctor-step0-profile-empty-token-summary = goat profilā portāla tokens ir tukšs
doctor-step0-profile-ok-summary = goat kontekstam `{ $context }` ir lasāms profila saturs
doctor-step0-client-absent-summary = neviens atbalstītais mesh klients neatbildēja
doctor-step0-client-absent-hint = uzstādi mesh klientu, ko nodrošina tavs goatnet, palaid tā dēmonu, pēc tam palaid `goat doctor` vēlreiz
doctor-step0-client-stopped-summary = uzstādītais mesh klients neatgrieza izmantojamu statusu
doctor-step0-client-stopped-hint = pārbaudi `netbird status -d` vai `goat client status --detail`; palaid uzstādīto dēmonu (`sudo netbird service start` NetBird gadījumā), pēc tam palaid `goat doctor` vēlreiz
doctor-step0-netbird-ok-summary = NetBird dēmons atbildēja (ziņotais profils `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd atbildēja (stāvoklis `{ $state }`)
doctor-step0-management-connected-summary = NetBird ziņo: management savienots
doctor-step0-management-disconnected-summary = NetBird ziņo: management atvienots
doctor-step0-management-disconnected-hint = pārbaudi norādīto management URL un `netbird status -d`; izveidots tunelis management atteices laikā var turpināt darboties
doctor-step0-management-unknown-summary = konfigurēto management galapunktu lokāli nevarēja noteikt
doctor-step0-management-unknown-hint = pārbaudi `netbird status -d` vai `goat client diagnostics`; doctor nemēģinās uzminēt pārvaldības URL
doctor-step0-tunnel-down-summary = NetBird ziņo, ka nav mesh adreses
doctor-step0-tunnel-down-hint = palaid `netbird status -d`; ja ierīce ir reģistrēta un dēmons atbild, palaid `goat heal`, tad atkārto `goat doctor`
doctor-step0-tunnel-address-summary = NetBird ziņo par mesh adresi { $address }; tālāk tiek pārbaudīta maršrutētāja sasniedzamība
doctor-step0-goat-client-no-bundle-summary = goat-clientd nav importēta komplekta
doctor-step0-goat-client-no-bundle-hint = palaid `goat client bundle import <bundle.cbor>` ar klienta reģistrācijas komplektu, pievieno klientu, tad atkārto `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd ziņo par izveidotu tuneli; tālāk tiek pārbaudīta maršrutētāja sasniedzamība
doctor-step0-goat-client-outer-disabled-summary = goat-clientd ārējais tunelis ir atslēgts režīmā netbird-only
doctor-step0-goat-client-down-summary = goat-clientd ziņo par tuneļa stāvokli `{ $state }`
doctor-step0-goat-client-down-hint = palaid `goat client diagnostics`, tad `goat client up`, ja komplekts ir ielādēts; atkārto `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd ziņo, ka iekšējais mesh ir savienots
doctor-step0-goat-client-inner-disabled-summary = goat-clientd iekšējais mesh ir atslēgts režīmā wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd ziņo par iekšējā mesh stāvokli `{ $state }`
doctor-step0-goat-client-inner-down-hint = palaid `goat client diagnostics`, lai pārbaudītu iekšējo mesh; pēc tā atjaunošanas atkārto `goat doctor`
doctor-step0-management-reachable-summary = pārvaldības galapunkts atbildēja ar HTTP { $status }
doctor-step0-management-auth-summary = pārvaldības HTTPS galapunkts ir sasniedzams (HTTP { $status }: šim zondējumam nepieciešama autorizācija)
doctor-step0-management-unexpected-summary = pārvaldības galapunkts atgrieza negaidītu HTTP { $status }
doctor-step0-management-unexpected-hint = pārbaudi konfigurēto pārvaldības URL un lūdz goatnet operatoram pārbaudīt šo atbildi
doctor-step0-management-unreachable-summary = konfigurēto pārvaldības galapunktu neizdevās sasniegt: { $error }
doctor-step0-management-unreachable-hint = pārbaudi DNS un tīkla piekļuvi klienta norādītajam URL; esošs tunelis var joprojām darboties
doctor-step0-clock-ok-summary = vietējā pulksteņa novirze no pārvaldības HTTP laika ir līdz { $seconds } s
doctor-step0-clock-skew-summary = vietējais pulkstenis atšķiras no pārvaldības HTTP laika par { $seconds } s
doctor-step0-clock-skew-hint = sinhronizē ierīces pulksteni, tad atkārto `goat doctor`; kamēr pulkstenis ir nepareizs, sertifikātu validācija var neizdoties
doctor-step0-clock-unknown-summary = pulksteņa novirzi nevarēja izmērīt no pārvaldības HTTP Date galvenes
doctor-step0-clock-unknown-hint = pārbaudi ierīces laika sinhronizāciju, ja neizdodas TLS vai sertifikātu pārbaudes
error-clock-skew-summary = ierīces pulkstenis pārāk atšķiras no pārvaldības laika
error-doctor-inconclusive-summary = doctor konstatēja kļūmi bez pierādīta cēloņa
error-portal-forbidden-summary = portāls liedza šim kontam piekļuvi pārbaudītajam API
doctor-step4-forbidden-summary = portāls šim profilam atgrieza HTTP 403
doctor-step4-forbidden-hint = šie akreditācijas dati sasniedza portālu, bet tiem nav piekļuves pārbaudītajam API; pārbaudiet `goat profile show` un lūdziet goatnet operatoram pārbaudīt piešķirtās tiesības
doctor-step4-decode-summary = portāls atbildēja ar atbildi, ko šis goat nespēj nolasīt
doctor-step4-decode-hint = { $err } — atjauniniet goat uz versiju, ko nodrošina šis goatnet, tad vēlreiz palaidiet `goat doctor`
doctor-step1-control-only-summary = savienojums tikai ar vadības serveri — nav tuneļa ne ar vienu no { $total } mezgliem
doctor-step1-control-only-hint = šis tīkls laiž ārā TCP 443, bet bloķē mesh datu portus, tāpēc no šīs ierīces nekas mesh tīklā (arī pierakstīšanās lapas) nav sasniedzams. Lūdziet tīkla komandai atļaut izejošo TCP un UDP uz { $relays } un UDP 3478 uz { $stun }; ieteicams arī tiešais UDP starp ierīcēm. Lai pārbaudītu, pieslēdzieties tālruņa tīklājam un palaidiet `netbird status`: mezgliem tad vajadzētu savienoties.
doctor-offmesh-dashboard-hint = releju flote (ārējais skatpunkts): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokālais sertifikāts, atbilstošā atslēga un CA komplekts tiek nolasīti; sertifikāts ir derīguma termiņā
doctor-step2-material-failed-summary = lokālo TLS materiālu { $file } nevarēja izmantot
doctor-step2-material-failed-hint = { $err } — pārbaudiet norādīto failu; atjaunojiet to no izsniegtā komplekta vai nomainiet profila atslēgas
doctor-step2-material-invalid-hint = { $file } nav derīgs X.509 PEM sertifikāts; atjaunojiet izsniegto materiālu vai palaidiet `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem nav derīga vai neatbilst mtls.cert.pem; atjaunojiet izsniegto materiālu vai palaidiet `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokālais sertifikāts ir ārpus sava derīguma termiņa
doctor-step2-cert-time-hint = palaidiet `goat profile show`; pārbaudiet iepriekšējo pulksteņa rezultātu, tad lūdziet operatoram jaunu komplektu un, ja sertifikāta termiņš ir beidzies, palaidiet `goat profile rotate <new-bundle.cbor>`
doctor-skipped-no-endpoint-summary = izlaists — profile.toml trūkst { $field }
doctor-skipped-no-profile-summary = izlaists — šim kontekstam nav nolasāmu profila galapunktu
doctor-skipped-no-portal-token-summary = izlaists — šim kontekstam nav nolasāma portāla tokena
status-ztol-tcp-ok = rūtera ports { $host }:{ $port } pieņem TCP (tikai ports — skat. sesiju)
status-session-ok = zenoh mTLS sesija atvērta { $ms }ms laikā (rūteris pieņēma šo sertifikātu)
status-session-skipped = sesija netika mēģināta (ārpus mesh; skat. ztol)
status-session-material-missing = zenoh sesija netika mēģināta: { $detail }
status-session-config-failed = zenoh sesijas konfigurācija neizdevās: { $err }
status-session-failed = zenoh sesija neatvērās: { $err } — palaid `goat doctor`
status-session-timeout = zenoh sesija neatvērās { $secs }s laikā — rutera ports pieņem TCP, bet neviena sesija netiek pabeigta (rutera iesīkšana, sertifikāts noraidīts vai pulksteņa nobīde); palaid `goat doctor`
status-names-live = vārdi aktīvi (mesh DNS atbildēja)
status-names-ip-only = vārdi netika pārbaudīti (profila galapunkti ir IP literāļi)
status-names-snapshot = vārdi pēc parakstītā momentuzņēmuma, sērija { $serial }, vecums { $age } (mesh DNS nav sasniedzams)
status-names-snapshot-aging = vārdi pēc parakstītā momentuzņēmuma, sērija { $serial }, vecums { $age } — NOVECOJIS (mesh DNS nav sasniedzams; salabo goat-coredns, tad vārdi atjaunosies automātiski)
status-names-snapshot-daemon = vārdi pēc parakstītā momentuzņēmuma, sērija { $serial }, vecums { $age } — dēmona krātuve (mesh DNS nav sasniedzams)
status-names-snapshot-daemon-aging = vārdi pēc parakstītā momentuzņēmuma, sērija { $serial }, vecums { $age } — dēmona krātuve, NOVECOJIS (mesh DNS nav sasniedzams; salabo goat-coredns, tad vārdi atjaunosies automātiski)
status-names-failed = mesh DNS nav sasniedzams un momentuzņēmuma rezerves variants nav pieejams — { $reason }
status-pqc-on = wireguard PQC (rosenpass) IESLĒGTS — { $qr }/{ $connected } savienoto mezglu ir izturīgi pret kvantu uzbrukumiem
status-pqc-off = wireguard PQC (rosenpass) IZSLĒGTS — klasiskie wireguard rokspiedieni
status-pqc-unknown = wireguard PQC nav zināms — { $reason }
status-device-none = visas ierīces vārdu aizsardzība IZSLĒGTA — nav goat-clientd vārdu krātuves (goat komandām rezerves variants tomēr ir; iespējo pēc name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd vārdu krātuve ir (sērija { $serial }), bet tās pārsūtītājs NEATBILD uz { $addr } — dēmons nedarbojas vai vārdu apakšsistēma iesīkusi
status-device-active = visas ierīces vārdu aizsardzība AKTĪVA — goat-clientd priekšā OS mesh DNS uz { $addr } (krātuves sērija { $serial }, { $grade })
status-device-not-fronted = goat-clientd pārsūtītājs darbojas uz { $addr } (krātuves sērija { $serial }, { $grade }) — OS nav priekšā; pārlūki izmanto tiešo mesh DNS
status-device-fronting-unknown = goat-clientd pārsūtītājs darbojas uz { $addr } (krātuves sērija { $serial }, { $grade }) — OS priekšā novietošanas stāvokli šajā platformā nevar noteikt
status-names-observed = vārdi NEKANONISKI (ad hoc): novērots ieraksts, vecums { $age } — nav parakstītajā momentuzņēmumā (mesh DNS nav sasniedzams; kodificē šo ierakstu)
status-names-selfclaim = vārdi pēc pārbaudīta SELF-CLAIM, vecums { $age } — parakstīts ar paša vārda enkuru (mesh DNS nav sasniedzams)
doctor-names-note = pēc momentuzņēmuma, sērija { $serial }, vecums { $age }
doctor-names-selfclaim-note = pēc pārbaudīta SELF-CLAIM, vecums { $age } — parakstīts ar paša vārda enkuru
doctor-names-selfclaim-hint = mesh DNS nav sasniedzams — šis solis atrisināts ar pārbaudītu SELF-CLAIM (vecums { $age }): paša vārda enkurs parakstīja šo sasaisti, un kešotā momentuzņēmuma peer_bindings to atļauj. Parakstīts, bet nav aktīvs; lai to novērstu, salabo mesh DNS.
doctor-names-observed-note = pēc NOVĒROTA (ad hoc) ieraksta, vecums { $age } — nekanonisks
doctor-names-observed-hint = mesh DNS nav sasniedzams — šis solis atrisināts ar NEKANONISKU novērotu ierakstu (vecums { $age }): vārds ir tikai aktīvs, nekad nav kodificēts vietnes reģistrā. Kodificē to (group_vars reģistrs → ģenerators → pārparaksts) vai tas novecos; lai to novērstu, salabo mesh DNS.
doctor-names-hint = mesh DNS nav sasniedzams — šis solis atrisināts ar parakstīto vārdu momentuzņēmumu (sērija { $serial }, vecums { $age }). Fabric paliek sasniedzams ar rezerves vārdiem; lai to novērstu, salabo mesh DNS (goat-coredns pārvaldības hostā).
storm-brake-plan-header = Vētras bremzes plāns (ADR 1081 4. slānis — manuāli):
storm-brake-plan-target = pārvaldības hosts { $user }@{ $host }  (konteksts { $context })
storm-brake-step-flush = restartēt signalizācijas konteineru `{ $container }` (atmet zombija straumes)
storm-brake-step-tighten = ierobežot L2 limiteri `{ $container }` (uz pusi samazināt ātruma limitu, palielināt neg-cache TTL) — { $window } min, operators atceļ manuāli; izlaists, ja L2 nav izvietots
storm-brake-step-shed = apturēt nebūtisko publicētāja vienību `{ $unit }`
storm-brake-shed-none = (nav deklarētu atslogojamo vienību — iestatiet storm_brake_shed_units failā profile.toml vai padodiet --shed-unit)
storm-brake-announce-note = ierakstīt auditā vienu OperatorActionV1 ar katra soļa rezultātu
storm-brake-dryrun-note = izmēģinājuma palaišana — nekas netika izpildīts.
storm-brake-triggers-header = Iedarbināt, ja relay-health host-net joslā redzams:
storm-brake-trigger-futility = signalizācijas veltīguma attiecība >= 0.5 pie >= 100 ziņ./min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = uplink zudumi >= 5% ilgāk par 10 min (wan.loss_pct)
storm-brake-triggers-band-url = hosta josla: relay-health.{ $site }.<zone> (skat. docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Piezīme: vien signalizācijas flush NEATJAUNO tīklu. Risinājums ir 1.–2. slānis (klienta backoff + signalizācijas atslogošana); bremze tikai iegūst laiku atkopšanai.
storm-brake-confirm = Iedarbināt vētras bremzi pret { $user }@{ $host }? Restartēs signalizācijas konteineru un apturēs deklarētos publicētājus. [y/N]
storm-brake-aborted = vētras bremze pārtraukta (nav apstiprinājuma)
storm-brake-executing = iedarbinu vētras bremzi pret { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = neizdevās
storm-brake-outcome-skipped = izlaists
storm-brake-summary = vētras bremze pabeigta: { $ok } ok, { $failed } neizdevās, { $skipped } izlaisti
storm-brake-flush-failed = FLUSH neizdevās — signalizācijas konteiners netika restartēts, tīkla atkopšanās nav gaidāma: { $reason }
storm-brake-mgmt-unresolved = pārvaldības hosts nav noteikts — padodiet --mgmt-host, iestatiet mgmt_ssh_host konteksta profile.toml vai eksportējiet GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operatora darbība ierakstīta audita žurnālā (aktieris { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portāls atbildēja HTTP 404 pieprasījumam `{ $path }`: tur nav reģistrēts
error-portal-rate-limited-summary = portāls atbildēja HTTP 429 pieprasījumam `{ $path }`: pārāk daudz pieprasījumu, mēģiniet vēlāk
error-portal-server-error-summary = portāls atbildēja HTTP { $status } pieprasījumam `{ $path }`: kļūda portāla pusē
error-portal-refused-summary = portāls atbildēja HTTP { $status } pieprasījumam `{ $path }`: pieprasījums noraidīts

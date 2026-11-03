### goat-cli — Albanian (sq). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = asnjë profil për kontekstin `{ $context }`
error-off-mesh-summary = jashtë ZTOL — i palidhur me rrjetin mesh
error-router-unreachable-summary = ruteri Zenoh në `{ $endpoint }` është i paarritshëm
error-cert-expired-summary = certifikata mTLS ka skaduar ose u refuzua nga ruteri
error-portal-unreachable-summary = API i portalit në `{ $endpoint }` është i paarritshëm
error-token-invalid-summary = portali refuzoi tokenin Bearer
error-topic-conflict-summary = topic `{ $topic }` ekziston tashmë
error-bundle-invalid-summary = verifikimi i nënshkrimit ose i skadimit të bundle dështoi

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = pika fundore e ruterit e formatuar gabim
doctor-step1-endpoint-malformed-hint = pritej `<proto>/<host>:<port>`; u mor `{ $endpoint }`
doctor-step1-ok-summary = lidhja TCP me hostin e ruterit { $host }:{ $port } me sukses
doctor-step1-conn-failed-summary = lidhja TCP me hostin e ruterit dështoi
doctor-step1-conn-failed-hint = { $err } — nisni netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); kontrolloni `netbird status`
doctor-step1-timeout-summary = skadoi koha për lidhjen TCP me hostin e ruterit
doctor-step1-timeout-hint = hosti i ruterit { $host }:{ $port } i paarritshëm në { $ms }ms — ndoshta jashtë mesh
doctor-step2-endpoint-malformed-hint = korrigjoni lëshuesin e bundle (--goat-cli-router-endpoint duhet të jetë `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM-i i rrënjëve CA mungon ose është i palexueshëm
doctor-step2-ca-missing-hint = { $err } — riekzekutoni `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = ruteri { $host }:{ $port } pranon TCP (TLS u vërtetua në hapin 3)
doctor-step2-refused-summary = TCP i ruterit u refuzua
doctor-step2-refused-hint = { $err } — ruteri mund të jetë i fikur; kontrolloni vendosjen e Zenoh në mgmt-host
doctor-step2-timeout-summary = skadoi koha për TCP-në e ruterit
doctor-step2-timeout-hint = ruteri { $host }:{ $port } i paarritshëm në { $ms }ms
doctor-step3-config-failed-summary = ndërtimi i konfigurimit zenoh dështoi
doctor-step3-config-failed-hint = pikë fundore e pavlefshme `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = ndërtimi i konfigurimit TLS zenoh dështoi
doctor-step3-tls-config-failed-hint = { $err } — ndoshta mospërputhje versioni zenoh; verifikoni përputhjen e varësive
doctor-step3-ok-summary = sesioni mTLS u hap — certifikata u pranua nga ACL e ruterit
doctor-step3-open-failed-summary = hapja e sesionit zenoh dështoi
doctor-step3-open-failed-hint = { $err } — certifikata mund të ketë skaduar (ekzekutoni `goat profile show`) ose u refuzua nga ACL e ruterit (mospërputhje URI-SAN)
doctor-step3-timeout-summary = skadoi koha për hapjen e sesionit zenoh
doctor-step3-timeout-hint = ruteri nuk e përfundoi shtrëngimin e duarve në { $secs }s — zhvendosje ore, certifikatë e gabuar ose mbingarkesë
doctor-step4-ok-summary = portali ktheu { $n } rresht(a) organizate
doctor-step4-token-rejected-summary = portali refuzoi tokenin Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — tokeni ka skaduar ose është revokuar; ribashkohuni përmes operator-onboard.yml
doctor-step4-request-failed-summary = kërkesa te portali dështoi
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = skadoi koha për kërkesën te portali
doctor-step4-timeout-hint = asnjë përgjigje në { $secs }s — kontrolloni që { $endpoint } të jetë i arritshëm nga mesh
doctor-skipped-summary = u anashkalua — hapi i mëparshëm dështoi (riekzekutoni me --all për të detyruar)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Linjë komande e unifikuar e operatorit për data fabric goat.
help-long-about = goat — CLI me pamje nga operatori që kompozon panoscope-core në një mjet të vetëm: nisje/ripërdorim profili, doctor i arritshmërisë me ndërgjegjësim ZTOL, shfletim i portalit të organizatave, CRUD topic, pub/sub, delegim te stethoscope, një kompozues interaktiv komandash dhe rrëfim i kaskadës së substrate.
help-flag-profile-dir = Mbishkruan direktoriumin e profilit (parazgjedhje: $GOAT_PROFILE_DIR ose ~/.goat).
help-flag-context = Mbishkruan kontekstin aktiv (parazgjedhje: variabli $GOAT_CONTEXT, pastaj current_context në config.toml, pastaj "default").
help-flag-narrate = Shfaq kaskadën e substrate që nxit një komandë ndryshuese.
help-flag-verbose = Aktivizon daljen e hollësishme të korrigjimit (ose vendos RUST_LOG=goat=debug).
help-flag-color = Modaliteti i ngjyrës për daljen e terminalit (auto|always|never).
help-flag-lang = Gjuha për tekstin që u drejtohet njerëzve (p.sh. en, uk). Kthehet te GOAT_LANG, pastaj te lokalja e OS, pastaj te anglishtja.
help-cmd-profile = Menaxho profilin lokal goat (certifikatë mTLS + token portali + grup pikash fundore).
help-cmd-context = Menaxho kontekstet (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Ekzekuto një sondues të arritshmërisë me hapa ndaj fabric.
help-cmd-doctor-long = Sondues i arritshmërisë me hapa ndaj fabric: anëtarësim ZTOL → ruteri Zenoh → pranim certifikate → portal. Çdo hap i dështuar lëshon një këshillë specifike riparimi.
help-cmd-vendor = Shfleto organizatat e regjistruara në portal.
help-cmd-topic = Listo, shfaq, krijo ose fshi topic.
help-cmd-pub = Publiko një ngarkesë te një çelës topic.
help-cmd-sub = Abonohu te një shprehje çelësi topic.
help-cmd-inspect = Delego te stethoscope një kërkesë për verdikt diagnostikues.
help-cmd-ui = Përzgjedhës interaktiv i bashkuar: shfleto listën e deklaruar ⊕ të drejtpërdrejtë dhe kalo te lentja Diagnose / Tail mbi objektivin e zgjedhur.
help-cmd-compose = TUI interaktive për ndërtimin e komandave.
help-cmd-narrate = Reflektim i kaskadës së substrate mbi aktivitetin e fundit. Ende i padisponueshëm (rruga e përditshme është --narrate mbi një komandë ndryshuese).
help-cmd-completion = Printo skriptin e plotësimit të guaskës te stdout.
help-cmd-explain = Kërko kuptimin e një kodi gabimi + riparimin te dokumenti i ngulitur `goat-cli-error-codes.md` (modeli cargo --explain).
help-cmd-mock = Publiko mostra sintetike te një topic. Parazgjedhje: hello-world JSON i thjeshtë me 1Hz. Kalo `--format <.fds>` + `--message <Name>` për të koduar protobuf të dhënë nga operatori.
help-cmd-status = Sintezë e shëndetit: arritshmëria ZTOL + /api/health i portalit + skadimi lokal i certifikatës, tri rreshta glyph ose një objekt --json.
help-cmd-services = Sondo çdo shërbim të strehuar në `ops/serve/services.yaml` dhe jep një rresht glyph për shërbim (ose një objekt --json).
help-cmd-client = Mbështill daemon-in lokal goat-clientd përmes prizës së tij JSON-RPC IPC (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = Zbulim zgjerimesh i bazuar te PATH (stili kubectl). Çdo i ekzekutueshëm `goat-<name>` te PATH mund të thirret si `goat <name>`; `goat plugin list` numëron plugin-et e instaluar, `goat plugin which <name>` zgjidh shtegun.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = pika fundore e ruterit e formatuar gabim: `{ $endpoint }`
status-ztol-refused = TCP u refuzua te ruteri { $host }:{ $port }: { $err }
status-ztol-unreachable = jashtë mesh (ruteri { $host }:{ $port } i paarritshëm në { $ms }ms)
status-portal-ok = portali /api/health 2xx
status-portal-failed = portali /api/health dështoi: { $err }
status-portal-timeout = portali /api/health skadoi në { $secs }s
status-cert-read-failed = leximi i certifikatës dështoi: { $err }
status-cert-pem-parse-failed = analiza PEM e certifikatës dështoi: { $err }
status-cert-wrong-tag = blloku i parë PEM është { $tag }, pritet CERTIFICATE
status-cert-x509-parse-failed = analiza x509 dështoi: { $err }
status-cert-expired = certifikata KA SKADUAR në unix={ $not_after }, tani={ $now }
status-cert-valid = certifikata e vlefshme; skadon për { $days_left }d (unix={ $not_after })
inspect-finding-label = gjetje ({ $severity }): { $label } — { $detail }
inspect-remedy-label = riparim: { $title } — { $detail }
inspect-no-outcomes = (asnjë rezultat — objektivi nuk përputhi asnjë kontroll)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put u pranua te `{ $key }` ({ $bytes } bytes)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskada u qetësua — { $summary } ({ $n } { $n ->
        [one] pasojë
       *[other] pasoja
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Magjistari i ekzekutimit të parë: zgjidh sandbox ose real, provo se funksionon, drejtohu te tutor.
help-cmd-tutor = Një turne praktik me ritëm të vetëgjykuar i fabric (stili vimtutor; i rifillueshëm).
help-cmd-docs = Shfleto dokumentet e goat, ose lësho referencën e plotë të komandave (--markdown).
help-cmd-hello = Dërgo një hello vajtje-ardhje përmes fabric nga skaji në skaj (mjedis sandbox ose real).
flourish-all-green = 🐐 të gjitha jeshile — fabric në gjendje normale

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = veprim vetëm për operatorë — nuk jeni të autorizuar (`goat admin` është inerte për jo-administratorët)
help-flag-narrate-json = Emetoni kaskadën e substratit si NDJSON (një objekt për pasojë) për regjistrat e incidenteve + skriptim. Nënkupton --narrate.
help-cmd-admin = Administrim vetëm për operatorë: cikli jetësor i organizatës, onboard/revoke/rotate, prerja e mesh-peer, provizionimi i pajisjes + nivelit të operatorit, triazhi LAN. Autorizuar nga kredencialet tuaja të operatorit (nga ana e serverit përmes shtrirjes së token-it portal/NetBird); inerte për jo-administratorët — një thirrje e paautorizuar nuk ndryshon asgjë dhe dështon me `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profili për kontekstin `{ $context }` ekziston, por nuk lexohet nga ky përdorues (leja u refuzua)
error-portal-bad-response-summary = portali u përgjigj, por ky build i goat nuk e analizoi dot përgjigjen
doctor-step0-ok-summary = konfigurimi lokal është gati për sonda rrjeti
doctor-step0-warn-summary = konfigurimi lokal ka kontrolle që nuk u konfirmuan dot
doctor-step0-fail-summary = konfigurimi lokal kërkon vëmendje
doctor-step0-profile-absent-summary = nuk ka profil goat për kontekstin `{ $context }`
doctor-step0-profile-new-hint = kontrollo `goat context ls`; nëse ky është konfigurimi i parë, ekzekuto `goat ca add <bundle-ca.pem>` dhe krahaso shenjën e gishtit me atë të operatorit tënd, pastaj `goat profile init <bundle.cbor>`; ekzekuto sërish `goat doctor`
doctor-step0-profile-absent-hint = kontrollo `goat context ls`; zgjidh një kontekst ekzistues me `goat context use <name>`, ose ekzekuto `goat profile init <bundle.cbor>` për këtë kontekst; ekzekuto sërish `goat doctor`
doctor-step0-profile-denied-summary = profili goat te { $path } nuk lexohet
doctor-step0-profile-denied-hint = rregullo pronësinë e profilit ose ekzekuto si përdoruesi pronar; mos e rinisni një profil ekzistues
doctor-step0-profile-incomplete-summary = skedari i profilit goat { $file } mungon ose nuk lexohet
doctor-step0-profile-incomplete-hint = rikthe skedarin e emërtuar nga bundle i lëshuar, ose përdor `goat profile rotate <bundle.cbor>` për të zëvendësuar këtë kontekst; ekzekuto sërish `goat doctor`
doctor-step0-profile-invalid-summary = profili goat ka endpoint-e të palexueshme ose që mungojnë
doctor-step0-profile-empty-token-summary = profili goat ka token portali bosh
doctor-step0-profile-ok-summary = konteksti goat `{ $context }` ka material profili të lexueshëm
doctor-step0-client-absent-summary = asnjë klient mesh i mbështetur nuk u përgjigj
doctor-step0-client-absent-hint = instalo klientin mesh që ofron goatnet-i yt, nise daemonin e tij, pastaj ekzekuto sërish `goat doctor`
doctor-step0-client-stopped-summary = klienti mesh i instaluar nuk ktheu status të përdorshëm
doctor-step0-client-stopped-hint = kontrollo `netbird status -d` ose `goat client status --detail`; nise daemonin e instaluar (`sudo netbird service start` për NetBird), pastaj ekzekuto sërish `goat doctor`
doctor-step0-netbird-ok-summary = daemoni NetBird u përgjigj (profili i raportuar `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd u përgjigj (gjendja `{ $state }`)
doctor-step0-management-connected-summary = NetBird raporton se management është i lidhur
doctor-step0-management-disconnected-summary = NetBird raporton se management është i shkëputur
doctor-step0-management-disconnected-hint = kontrollo URL-në e raportuar të management dhe `netbird status -d`; një tunel i vendosur mund të funksionojë ende gjatë një ndërprerjeje të management
doctor-step0-management-unknown-summary = endpoint-i i konfiguruar i management nuk u vendos dot në nivel lokal
doctor-step0-management-unknown-hint = inspekto `netbird status -d` ose `goat client diagnostics`; doctor nuk do ta hamendësojë URL-në e menaxhimit
doctor-step0-tunnel-down-summary = NetBird nuk raporton asnjë adresë mesh
doctor-step0-tunnel-down-hint = ekzekuto `netbird status -d`; nëse pajisja është e regjistruar dhe daemon përgjigjet, ekzekuto `goat heal`, pastaj ekzekuto sërish `goat doctor`
doctor-step0-tunnel-address-summary = NetBird raporton adresën mesh { $address }; arritshmëria e router-it kontrollohet më pas
doctor-step0-goat-client-no-bundle-summary = goat-clientd nuk ka bundle të importuar
doctor-step0-goat-client-no-bundle-hint = ekzekuto `goat client bundle import <bundle.cbor>` me bundle-in e regjistrimit të klientit, lidhe klientin, pastaj ekzekuto sërish `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd raporton tunel të lidhur; arritshmëria e router-it kontrollohet më pas
doctor-step0-goat-client-outer-disabled-summary = tuneli i jashtëm i goat-clientd është çaktivizuar në modalitetin netbird-only
doctor-step0-goat-client-down-summary = goat-clientd raporton gjendjen e tunelit `{ $state }`
doctor-step0-goat-client-down-hint = ekzekuto `goat client diagnostics`, pastaj `goat client up` nëse bundle është ngarkuar; ekzekuto sërish `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd raporton se mesh-i i brendshëm është i lidhur
doctor-step0-goat-client-inner-disabled-summary = mesh-i i brendshëm i goat-clientd është çaktivizuar në modalitetin wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd raporton gjendjen e mesh-it të brendshëm `{ $state }`
doctor-step0-goat-client-inner-down-hint = ekzekuto `goat client diagnostics` për të inspektuar mesh-in e brendshëm, pastaj ekzekuto sërish `goat doctor` pasi ta rikthesh
doctor-step0-management-reachable-summary = pika fundore e menaxhimit u përgjigj me HTTP { $status }
doctor-step0-management-auth-summary = pika fundore HTTPS e menaxhimit arrihet (HTTP { $status } kërkon autorizim për këtë provë)
doctor-step0-management-unexpected-summary = pika fundore e menaxhimit ktheu HTTP të papritur { $status }
doctor-step0-management-unexpected-hint = inspekto URL-në e konfiguruar të menaxhimit dhe kërkoji operatorit të goatnet të kontrollojë këtë përgjigje
doctor-step0-management-unreachable-summary = pika fundore e konfiguruar e menaxhimit nuk u arrit: { $error }
doctor-step0-management-unreachable-hint = kontrollo DNS-në dhe qasjen në rrjet drejt URL-së të raportuar nga klienti; një tunel ekzistues mund të funksionojë ende
doctor-step0-clock-ok-summary = ora lokale është brenda { $seconds } sekondave nga ora HTTP e menaxhimit
doctor-step0-clock-skew-summary = ora lokale ndryshon nga ora HTTP e menaxhimit me { $seconds } sekonda
doctor-step0-clock-skew-hint = sinkronizo orën e pajisjes, pastaj ekzekuto sërish `goat doctor`; validimi i certifikatës mund të dështojë ndërsa ora është e gabuar
doctor-step0-clock-unknown-summary = shmangia e orës nuk u mat dot nga një header HTTP Date i menaxhimit
doctor-step0-clock-unknown-hint = kontrollo sinkronizimin e orës së pajisjes nëse dështojnë kontrollet e TLS ose të certifikatës
error-clock-skew-summary = ora e pajisjes është shumë larg nga ora e menaxhimit
error-doctor-inconclusive-summary = doctor vëzhgoi një dështim pa shkak të provuar
error-portal-forbidden-summary = portali ia ndaloi këtij llogarie qasjen te API-ja e provuar
doctor-step4-forbidden-summary = portali ktheu HTTP 403 për këtë profil
doctor-step4-forbidden-hint = ky kredencial arriti te portali, por nuk ka qasje te API-ja e provuar; kontrollo `goat profile show` dhe kërkoji operatorit të goatnet të verifikojë lejen
doctor-step4-decode-summary = portali u përgjigj me një përgjigje që ky goat nuk mund ta lexojë
doctor-step4-decode-hint = { $err } — përditëso goat në versionin që shërbehet nga ky goatnet, pastaj ri-ekzekuto `goat doctor`
doctor-step1-control-only-summary = lidhur vetëm me serverin e kontrollit — pa tunel drejt asnjë prej { $total } peer-ave
doctor-step1-control-only-hint = ky rrjet lejon daljen e TCP 443, por bllokon portat e të dhënave të mesh-it, ndaj asgjë në mesh (përfshirë faqet e hyrjes) nuk arrihet nga kjo pajisje. Kërkoji ekipit të rrjetit të lejojë TCP dhe UDP dalës drejt { $relays }, dhe UDP 3478 drejt { $stun }; rekomandohet edhe UDP i drejtpërdrejtë mes pajisjeve. Për ta konfirmuar, lidhu me hotspotin e një telefoni dhe ekzekuto `netbird status`: peer-at duhet të lidhen.
doctor-offmesh-dashboard-hint = flota e rele-ve (pikëvështrim i jashtëm): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = certifikata lokale, çelësi përkatës dhe paketa CA lexohen; certifikata është brenda intervalit të vlefshmërisë
doctor-step2-material-failed-summary = materiali lokal TLS { $file } nuk u përdor dot
doctor-step2-material-failed-hint = { $err } — kontrollo skedarin e emërtuar; rikthe nga paketa e lëshuar ose rrotullo profilin
doctor-step2-material-invalid-hint = { $file } nuk është certifikatë X.509 PEM e vlefshme; rikthe materialin e lëshuar ose ekzekuto `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem është i pavlefshëm ose nuk përputhet me mtls.cert.pem; rikthe materialin e lëshuar ose ekzekuto `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = certifikata lokale është jashtë intervalit të vlefshmërisë
doctor-step2-cert-time-hint = ekzekuto `goat profile show`; kontrollo rezultatin e orës më sipër, pastaj kërkoji operatorit një paketë të re dhe ekzekuto `goat profile rotate <new-bundle.cbor>` nëse certifikata ka skaduar
doctor-skipped-no-endpoint-summary = anashkaluar — profile.toml nuk ka { $field }
doctor-skipped-no-profile-summary = anashkaluar — nuk ka endpoint-e profili të lexueshme për këtë kontekst
doctor-skipped-no-portal-token-summary = anashkaluar — nuk ka token portali të lexueshëm për këtë kontekst
status-ztol-tcp-ok = porta e router-it { $host }:{ $port } pranon TCP (vetëm porta — shih sesionin)
status-session-ok = sesioni zenoh mTLS u hap për { $ms }ms (router-i pranoi këtë certifikatë)
status-session-skipped = sesioni nuk u provua (jashtë mesh-it; shih ztol)
status-session-material-missing = sesioni zenoh nuk u provua: { $detail }
status-session-config-failed = konfigurimi i sesionit zenoh dështoi: { $err }
status-session-failed = sesioni zenoh nuk u hap: { $err } — ekzekuto `goat doctor`
status-session-timeout = sesioni zenoh nuk u hap për { $secs }s — porta e router-it pranon TCP por asnjë sesion nuk plotësohet (router i bllokuar, certifikatë e refuzuar ose shmangie ore); ekzekuto `goat doctor`
status-names-live = emrat live (DNS i mesh-it u përgjigj)
status-names-ip-only = emrat nuk u provuan (endpoint-et e profilit janë IP literale)
status-names-snapshot = emrat sipas snapshot-it të nënshkruar me serial { $serial }, mosha { $age } (DNS i mesh-it i paarritshëm)
status-names-snapshot-aging = emrat sipas snapshot-it të nënshkruar me serial { $serial }, mosha { $age } — I VJETËR (DNS i mesh-it i paarritshëm; rregullo goat-coredns, pastaj emrat rifreskohen automatikisht)
status-names-snapshot-daemon = emrat sipas snapshot-it të nënshkruar me serial { $serial }, mosha { $age } — depo e daemon-it (DNS i mesh-it i paarritshëm)
status-names-snapshot-daemon-aging = emrat sipas snapshot-it të nënshkruar me serial { $serial }, mosha { $age } — depo e daemon-it, I VJETËR (DNS i mesh-it i paarritshëm; rregullo goat-coredns, pastaj emrat rifreskohen automatikisht)
status-names-failed = DNS i mesh-it i paarritshëm dhe rezerva me snapshot nuk është e disponueshme — { $reason }
status-pqc-on = wireguard PQC (rosenpass) AKTIV — { $qr }/{ $connected } peer-e të lidhur rezistentë ndaj kuantikes
status-pqc-off = wireguard PQC (rosenpass) JOAKTIV — handshake-e klasike wireguard
status-pqc-unknown = wireguard PQC i panjohur — { $reason }
status-device-none = mbrojtja e emrave në nivel pajisjeje JOAKTIVE — nuk ka depo emrash goat-clientd (komandat goat kanë prapë rezervë; aktivizo sipas name-fallback-resolver.md §9.2)
status-device-broken = depoja e emrave e goat-clientd ekziston (serial { $serial }) por forwarder-i i saj NUK përgjigjet në { $addr } — daemon-i ka rënë ose nëndërtesa e emrave është bllokuar
status-device-active = mbrojtja e emrave në nivel pajisjeje AKTIVE — goat-clientd para DNS-it të mesh-it të OS në { $addr } (depo me serial { $serial }, { $grade })
status-device-not-fronted = forwarder-i goat-clientd aktiv në { $addr } (depo me serial { $serial }, { $grade }) — OS nuk kalon përmes tij; shfletuesit përdorin drejtpërdrejt DNS-in e mesh-it
status-device-fronting-unknown = forwarder-i goat-clientd aktiv në { $addr } (depo me serial { $serial }, { $grade }) — gjendja e kalimit përmes tij në OS nuk dallohet në këtë platformë
status-names-observed = emra JOKANONIKË (ad hoc): regjistrim i vëzhguar, mosha { $age } — nuk është në snapshot-in e nënshkruar (DNS i mesh-it i paarritshëm; kodifiko këtë regjistrim)
status-names-selfclaim = emra nëpërmjet SELF-CLAIM të verifikuar, mosha { $age } — nënshkruar nga ankora e vetë emrit (DNS i mesh-it i paarritshëm)
doctor-names-note = nëpërmjet snapshot-it me serial { $serial }, mosha { $age }
doctor-names-selfclaim-note = nëpërmjet SELF-CLAIM të verifikuar, mosha { $age } — nënshkruar nga ankora e vetë emrit
doctor-names-selfclaim-hint = DNS i mesh-it është i paarritshëm — ky hap u zgjidh nëpërmjet një SELF-CLAIM të verifikuar (mosha { $age }): ankora e vetë emrit e nënshkroi këtë lidhje dhe peer_bindings i snapshot-it në cache e autorizojnë. I nënshkruar por jo live; rregullo DNS-in e mesh-it për ta pastruar.
doctor-names-observed-note = nëpërmjet regjistrimit të VËZHGUAR (ad hoc), mosha { $age } — jokanonik
doctor-names-observed-hint = DNS i mesh-it është i paarritshëm — ky hap u zgjidh nëpërmjet një regjistrimi të vëzhguar JOKANONIK (mosha { $age }): emri është vetëm live, kurrë i kodifikuar në regjistrin e site-it. Kodifiko-je (regjistri group_vars → gjeneratori → rinënshkrim) ose skadon; rregullo DNS-in e mesh-it për ta pastruar.
doctor-names-hint = DNS i mesh-it është i paarritshëm — ky hap u zgjidh nëpërmjet snapshot-it të nënshkruar të emrave (serial { $serial }, mosha { $age }). Fabrika mbetet e arritshme me emra rezervë; rregullo DNS-in e mesh-it (goat-coredns në hostin mgmt) për ta pastruar.
storm-brake-plan-header = Plani i frenës së stuhisë (ADR 1081 Shtresa 4 — manual):
storm-brake-plan-target = host mgmt { $user }@{ $host }  (konteksti { $context })
storm-brake-step-flush = rinis sinjalin e kontejnerit `{ $container }` (heq rrjedhat zombi)
storm-brake-step-tighten = kufizo limituesin L2 në `{ $container }` (përgjysmo rate-limit, rrit TTL të neg-cache) — { $window } min, kthehet nga operatori; kapërcehet nëse L2 nuk është i vendosur
storm-brake-step-shed = pezullo njësinë jo-thelbësore të publikuesit `{ $unit }`
storm-brake-shed-none = (asnjë njësi për shkarkim e deklaruar — vendos storm_brake_shed_units te profile.toml ose jep --shed-unit)
storm-brake-announce-note = dërgo një OperatorActionV1 te gjurma e auditimit me rezultatet për çdo hap
storm-brake-dryrun-note = provë pa ekzekutim — nuk u ekzekutua asgjë.
storm-brake-triggers-header = Aktivizo kur brezi host-net i relay-health tregon:
storm-brake-trigger-futility = raporti i së kotës së sinjalizimit >= 0.5 në >= 100 msg/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = humbje uplink >= 5% e vazhdueshme 10 min (wan.loss_pct)
storm-brake-triggers-band-url = brezi i host-it: relay-health.{ $site }.<zone> (shih docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Shënim: vetëm një flush i sinjalit NUK e rikthen mesh-in. Shtresat 1-2 (backoff i klientit + shkarkimi i sinjalit) janë kura; frena të jep kohë për t'u rikuperuar.
storm-brake-confirm = Të aktivizohet frena e stuhisë kundër { $user }@{ $host }? Rinis kontejnerin e sinjalit dhe pezullon publikuesit e deklaruar. [y/N]
storm-brake-aborted = frena e stuhisë u ndërpre (pa konfirmim)
storm-brake-executing = po aktivizohet frena e stuhisë kundër { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = dështoi
storm-brake-outcome-skipped = kapërcyer
storm-brake-summary = frena e stuhisë përfundoi: { $ok } ok, { $failed } dështuan, { $skipped } kapërcyer
storm-brake-flush-failed = FLUSH dështoi — kontejneri i sinjalit nuk u rinis, ndaj rikuperimi i mesh-it nuk pritet: { $reason }
storm-brake-mgmt-unresolved = host-i mgmt nuk u zgjidh — jep --mgmt-host, vendos mgmt_ssh_host te profile.toml i kontekstit, ose eksporto GOAT_MGMT_SSH_HOST
storm-brake-audit-note = veprimi i operatorit u regjistrua në gjurmën e auditimit (aktori { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portali u përgjigj HTTP 404 për `{ $path }`: nuk është i regjistruar atje
error-portal-rate-limited-summary = portali u përgjigj HTTP 429 për `{ $path }`: shumë kërkesa, provoni më vonë
error-portal-server-error-summary = portali u përgjigj HTTP { $status } për `{ $path }`: një defekt nga ana e portalit
error-portal-refused-summary = portali u përgjigj HTTP { $status } për `{ $path }`: kërkesa u refuzua

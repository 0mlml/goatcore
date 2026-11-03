### goat-cli — Croatian (hr). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = nema profila za kontekst `{ $context }`
error-off-mesh-summary = izvan ZTOL-a — bez veze s mesh mrežom
error-router-unreachable-summary = Zenoh usmjerivač na `{ $endpoint }` nije dostupan
error-cert-expired-summary = mTLS certifikat je istekao ili ga je usmjerivač odbio
error-portal-unreachable-summary = API portala na `{ $endpoint }` nije dostupan
error-token-invalid-summary = portal je odbio Bearer token
error-topic-conflict-summary = topic `{ $topic }` već postoji
error-bundle-invalid-summary = provjera potpisa ili isteka bundle-a nije uspjela

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = krajnja točka usmjerivača neispravnog formata
doctor-step1-endpoint-malformed-hint = očekivano `<proto>/<host>:<port>`; primljeno `{ $endpoint }`
doctor-step1-ok-summary = TCP veza s domaćinom usmjerivača { $host }:{ $port } uspjela
doctor-step1-conn-failed-summary = TCP veza s domaćinom usmjerivača nije uspjela
doctor-step1-conn-failed-hint = { $err } — pokrenite netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); provjerite `netbird status`
doctor-step1-timeout-summary = isteklo vrijeme TCP veze s domaćinom usmjerivača
doctor-step1-timeout-hint = domaćin usmjerivača { $host }:{ $port } nedostupan za { $ms }ms — vjerojatno izvan mesh
doctor-step2-endpoint-malformed-hint = ispravite izdavatelja bundle (--goat-cli-router-endpoint mora biti `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM CA korijena nedostaje ili je nečitljiv
doctor-step2-ca-missing-hint = { $err } — ponovno pokrenite `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = usmjerivač { $host }:{ $port } prihvaća TCP (TLS provjeren u koraku 3)
doctor-step2-refused-summary = TCP usmjerivača odbijen
doctor-step2-refused-hint = { $err } — usmjerivač je možda u kvaru; provjerite implementaciju Zenoh na mgmt-host
doctor-step2-timeout-summary = isteklo vrijeme TCP usmjerivača
doctor-step2-timeout-hint = usmjerivač { $host }:{ $port } nedostupan za { $ms }ms
doctor-step3-config-failed-summary = izgradnja konfiguracije zenoh nije uspjela
doctor-step3-config-failed-hint = nevažeća krajnja točka `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = izgradnja TLS konfiguracije zenoh nije uspjela
doctor-step3-tls-config-failed-hint = { $err } — vjerojatno odstupanje verzije zenoh; provjerite podudaranje ovisnosti
doctor-step3-ok-summary = mTLS sesija otvorena — certifikat prihvatio ACL usmjerivača
doctor-step3-open-failed-summary = otvaranje zenoh sesije nije uspjelo
doctor-step3-open-failed-hint = { $err } — certifikat je možda istekao (pokrenite `goat profile show`) ili ga je odbio ACL usmjerivača (nepodudaranje URI-SAN)
doctor-step3-timeout-summary = isteklo vrijeme otvaranja zenoh sesije
doctor-step3-timeout-hint = usmjerivač nije dovršio rukovanje za { $secs }s — pomak sata, pogrešan certifikat ili zagušenje
doctor-step4-ok-summary = portal je vratio { $n } redaka organizacija
doctor-step4-token-rejected-summary = portal je odbio Bearer token
doctor-step4-token-rejected-hint = HTTP { $status } — token istekao ili opozvan; ponovno se uključite putem operator-onboard.yml
doctor-step4-request-failed-summary = zahtjev portalu nije uspio
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = isteklo vrijeme zahtjeva portalu
doctor-step4-timeout-hint = nema odgovora za { $secs }s — provjerite je li { $endpoint } dostupan iz mesh
doctor-skipped-summary = preskočeno — prethodni korak nije uspio (ponovno pokrenite s --all za prisilu)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Objedinjeni operaterski naredbeni redak za data fabric goat.
help-long-about = goat — CLI usmjeren operateru koji sklapa panoscope-core u jedan alat: init/ponovna uporaba profila, doctor dostupnosti svjestan ZTOL-a, pregledavanje portala organizacija, CRUD topica, pub/sub, delegiranje na stethoscope, interaktivni sastavljač naredbi i pripovijedanje o kaskadi substrata.
help-flag-profile-dir = Nadjačava direktorij profila (zadano: $GOAT_PROFILE_DIR ili ~/.goat).
help-flag-context = Nadjačava aktivni kontekst (zadano: varijabla $GOAT_CONTEXT, zatim current_context u config.toml, zatim "default").
help-flag-narrate = Prikazuje kaskadu substrata koju pokreće naredba koja mijenja stanje.
help-flag-verbose = Omogućuje detaljan ispis za otklanjanje pogrešaka (ili postavite RUST_LOG=goat=debug).
help-flag-color = Način boje za ispis terminala (auto|always|never).
help-flag-lang = Jezik za tekst usmjeren korisniku (npr. en, uk). Vraća se na GOAT_LANG, zatim na lokalizaciju operacijskog sustava, zatim na engleski.
help-cmd-profile = Upravlja lokalnim goat profilom (mTLS certifikat + token portala + skup endpointa).
help-cmd-context = Upravlja kontekstima (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Pokreće postupni prober dostupnosti prema fabricu.
help-cmd-doctor-long = Postupni prober dostupnosti prema fabricu: članstvo u ZTOL-u → Zenoh router → prihvaćanje certifikata → portal. Svaki neuspjeli korak ispisuje specifičan savjet za rješavanje.
help-cmd-vendor = Pregledava organizacije registrirane u portalu.
help-cmd-topic = Izlistava, prikazuje, stvara ili briše topice.
help-cmd-pub = Objavljuje payload na ključ topica.
help-cmd-sub = Pretplaćuje se na izraz ključa topica.
help-cmd-inspect = Delegira zahtjev za dijagnostičkom presudom na stethoscope.
help-cmd-ui = Interaktivni objedinjeni birač: pregledavajte deklarirani ⊕ popis uživo i skočite na leću Diagnose / Tail na odabranom cilju.
help-cmd-compose = Interaktivni TUI za izgradnju naredbi.
help-cmd-narrate = Razmatranje kaskade substrata nad nedavnom aktivnošću. Još nije dostupno (svakodnevni put je --narrate na naredbi koja mijenja stanje).
help-cmd-completion = Ispisuje skriptu za dovršavanje ljuske na stdout.
help-cmd-explain = Pretražuje značenje + rješavanje koda pogreške u ugrađenom dokumentu `goat-cli-error-codes.md` (obrazac cargo --explain).
help-cmd-mock = Objavljuje sintetičke uzorke na topic. Zadano: trivijalni JSON hello-world na 1Hz. Proslijedite `--format <.fds>` + `--message <Name>` za kodiranje protobufa koji isporučuje operater.
help-cmd-status = Sinteza stanja: dostupnost ZTOL-a + /api/health portala + istek lokalnog certifikata, tri retka glypha ili jedan --json objekt.
help-cmd-services = Sondira svaku hostanu uslugu u `ops/serve/services.yaml` i renderira jedan redak glypha po usluzi (ili jedan --json objekt).
help-cmd-client = Omata lokalni daemon goat-clientd putem njegovog JSON-RPC IPC socketa (`~/.goat-client/goat-clientd.sock`): status / up / down / uvoz bundlea.
help-cmd-plugin = Otkrivanje proširenja temeljeno na PATH-u (u stilu kubectl). Bilo koja izvršna datoteka `goat-<name>` u PATH-u može se pozvati kao `goat <name>`; `goat plugin list` nabraja instalirane pluginove, `goat plugin which <name>` razrješava putanju.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = krajnja točka usmjerivača neispravnog formata: `{ $endpoint }`
status-ztol-refused = TCP odbijen od usmjerivača { $host }:{ $port }: { $err }
status-ztol-unreachable = izvan mesh (usmjerivač { $host }:{ $port } nedostupan za { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health nije uspio: { $err }
status-portal-timeout = isteklo vrijeme portala /api/health za { $secs }s
status-cert-read-failed = čitanje certifikata nije uspjelo: { $err }
status-cert-pem-parse-failed = parsiranje PEM certifikata nije uspjelo: { $err }
status-cert-wrong-tag = prvi PEM blok je { $tag }, očekivan CERTIFICATE
status-cert-x509-parse-failed = parsiranje x509 nije uspjelo: { $err }
status-cert-expired = certifikat ISTEKAO u unix={ $not_after }, sada={ $now }
status-cert-valid = certifikat valjan; istječe za { $days_left }d (unix={ $not_after })
inspect-finding-label = nalaz ({ $severity }): { $label } — { $detail }
inspect-remedy-label = rješenje: { $title } — { $detail }
inspect-no-outcomes = (nema ishoda — cilj se nije podudarao ni s jednom provjerom)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put prihvaćen na `{ $key }` ({ $bytes } bajtova)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskada se smirila — { $summary } ({ $n } { $n ->
        [one] posljedica
        [few] posljedice
       *[other] posljedica
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Čarobnjak za prvo pokretanje: odaberite sandbox ili stvarno okruženje, potvrdite da radi, uputite na tutora.
help-cmd-tutor = Samostalan praktičan obilazak fabrica vlastitim tempom (u stilu vimtutor; nastavljiv).
help-cmd-docs = Pregledavajte goat dokumentaciju ili ispišite potpunu referencu naredbi (--markdown).
help-cmd-hello = Provodi dvosmjerni hello kroz fabric od početka do kraja (sandbox ili stvarno okruženje).
flourish-all-green = 🐐 sve zeleno — fabric nominalan

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = akcija samo za operatere — niste ovlašteni (`goat admin` je neaktivan za ne-administratore)
help-flag-narrate-json = Emitira kaskadu supstrata kao NDJSON (jedan objekt po posljedici) za evidencije incidenata + skriptiranje. Podrazumijeva --narrate.
help-cmd-admin = Administracija samo za operatere: životni ciklus organizacije, onboard/revoke/rotate, isključenje mesh-peera, provisioniranje uređaja + razine operatera, LAN trijaza. Autorizira se vašim operaterskim vjerodajnicama (poslužiteljski putem opsega portala/NetBird tokena); neaktivan za ne-administratore — neovlašteni poziv ništa ne mijenja i završava s `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profil za kontekst `{ $context }` postoji, ali ga ovaj korisnik ne može čitati (pristup odbijen)
error-portal-bad-response-summary = portal je odgovorio, ali ovaj goat build nije uspio parsirati odgovor
doctor-step0-ok-summary = lokalna postavka je spremna za mrežne probe
doctor-step0-warn-summary = lokalna postavka ima provjere koje se nisu mogle potvrditi
doctor-step0-fail-summary = lokalna postavka traži pažnju
doctor-step0-profile-absent-summary = nema goat profila za kontekst `{ $context }`
doctor-step0-profile-new-hint = provjeri `goat context ls`; ako je ovo prvo postavljanje, pokreni `goat ca add <bundle-ca.pem>` i usporedi njegov otisak s otiskom svog operatera, zatim `goat profile init <bundle.cbor>`; ponovno pokreni `goat doctor`
doctor-step0-profile-absent-hint = provjeri `goat context ls`; odaberi postojeći kontekst s `goat context use <name>` ili pokreni `goat profile init <bundle.cbor>` za ovaj kontekst; ponovno pokreni `goat doctor`
doctor-step0-profile-denied-summary = goat profil na { $path } nije čitljiv
doctor-step0-profile-denied-hint = popravi vlasništvo nad profilom ili pokreni kao vlasnik; ne inicijaliziraj ponovno postojeći profil
doctor-step0-profile-incomplete-summary = datoteka goat profila { $file } nedostaje ili nije čitljiva
doctor-step0-profile-incomplete-hint = vrati navedenu datoteku iz izdanog bundlea ili zamijeni ovaj kontekst s `goat profile rotate <bundle.cbor>`; ponovno pokreni `goat doctor`
doctor-step0-profile-invalid-summary = goat profil ima nečitljive ili nedostajuće endpointe
doctor-step0-profile-empty-token-summary = goat profil ima prazan portal token
doctor-step0-profile-ok-summary = goat kontekst `{ $context }` ima čitljiv materijal profila
doctor-step0-client-absent-summary = nijedan podržani mesh klijent nije odgovorio
doctor-step0-client-absent-hint = instaliraj mesh klijent koji ti daje tvoj goatnet, pokreni njegov daemon, zatim ponovno pokreni `goat doctor`
doctor-step0-client-stopped-summary = instalirani mesh klijent nije vratio upotrebljiv status
doctor-step0-client-stopped-hint = provjeri `netbird status -d` ili `goat client status --detail`; pokreni instalirani daemon (`sudo netbird service start` za NetBird), zatim ponovno pokreni `goat doctor`
doctor-step0-netbird-ok-summary = NetBird daemon je odgovorio (prijavljeni profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd je odgovorio (stanje `{ $state }`)
doctor-step0-management-connected-summary = NetBird javlja da je management povezan
doctor-step0-management-disconnected-summary = NetBird javlja da management nije povezan
doctor-step0-management-disconnected-hint = provjeri prijavljeni management URL i `netbird status -d`; uspostavljen tunel može i dalje raditi tijekom ispada managementa
doctor-step0-management-unknown-summary = konfigurirani management endpoint nije se mogao utvrditi lokalno
doctor-step0-management-unknown-hint = pregledajte `netbird status -d` ili `goat client diagnostics`; doctor neće pogađati URL za management
doctor-step0-tunnel-down-summary = NetBird ne prijavljuje mesh adresu
doctor-step0-tunnel-down-hint = pokrenite `netbird status -d`; ako je uređaj upisan i daemon odgovara, pokrenite `goat heal`, zatim ponovno `goat doctor`
doctor-step0-tunnel-address-summary = NetBird prijavljuje mesh adresu { $address }; dostupnost routera provjerava se sljedeća
doctor-step0-goat-client-no-bundle-summary = goat-clientd nema uvezen bundle
doctor-step0-goat-client-no-bundle-hint = pokrenite `goat client bundle import <bundle.cbor>` s enrollment bundleom klijenta, spojite klijent, zatim ponovno pokrenite `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd prijavljuje spojen tunel; dostupnost routera provjerava se sljedeća
doctor-step0-goat-client-outer-disabled-summary = vanjski tunel goat-clientd isključen je u netbird-only načinu rada
doctor-step0-goat-client-down-summary = goat-clientd prijavljuje stanje tunela `{ $state }`
doctor-step0-goat-client-down-hint = pokrenite `goat client diagnostics`, zatim `goat client up` ako je bundle učitan; ponovno pokrenite `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd prijavljuje spojen unutarnji mesh
doctor-step0-goat-client-inner-disabled-summary = unutarnji mesh goat-clientd isključen je u wg-cp0-only načinu rada
doctor-step0-goat-client-inner-down-summary = goat-clientd prijavljuje stanje unutarnjeg mesha `{ $state }`
doctor-step0-goat-client-inner-down-hint = pokrenite `goat client diagnostics` za pregled unutarnjeg mesha, zatim ponovno pokrenite `goat doctor` nakon njegova oporavka
doctor-step0-management-reachable-summary = management endpoint odgovorio je s HTTP { $status }
doctor-step0-management-auth-summary = management HTTPS endpoint je dostupan (HTTP { $status } za ovu provjeru zahtijeva autorizaciju)
doctor-step0-management-unexpected-summary = management endpoint vratio je neočekivani HTTP { $status }
doctor-step0-management-unexpected-hint = provjerite konfigurirani management URL i zamolite svog goatnet operatora da pregleda ovaj odgovor
doctor-step0-management-unreachable-summary = konfigurirani management endpoint nije dostupan: { $error }
doctor-step0-management-unreachable-hint = provjerite DNS i mrežni pristup URL-u koji prijavljuje klijent; postojeći tunel možda i dalje radi
doctor-step0-clock-ok-summary = lokalni sat je unutar { $seconds } s od HTTP vremena managementa
doctor-step0-clock-skew-summary = lokalni sat odstupa od HTTP vremena managementa za { $seconds } s
doctor-step0-clock-skew-hint = sinkronizirajte sat uređaja, zatim ponovno pokrenite `goat doctor`; provjera certifikata može padati dok je sat pogrešan
doctor-step0-clock-unknown-summary = odstupanje sata nije moguće izmjeriti iz HTTP zaglavlja Date managementa
doctor-step0-clock-unknown-hint = provjerite sinkronizaciju vremena uređaja ako TLS ili provjere certifikata ne uspijevaju
error-clock-skew-summary = sat uređaja previše odstupa od vremena upravljanja
error-doctor-inconclusive-summary = doctor je uočio kvar bez dokazanog uzroka
error-portal-forbidden-summary = portal je ovom računu uskratio pristup ispitanom API-ju
doctor-step4-forbidden-summary = portal je za ovaj profil vratio HTTP 403
doctor-step4-forbidden-hint = ova vjerodajnica je došla do portala, ali nema pristup ispitanom API-ju; provjerite `goat profile show` i zatražite od goatnet operatera da provjeri dozvolu
doctor-step4-decode-summary = portal je odgovorio odgovorom koji ovaj goat ne može pročitati
doctor-step4-decode-hint = { $err } — ažurirajte goat na verziju koju poslužuje ovaj goatnet, pa ponovno pokrenite `goat doctor`
doctor-step1-control-only-summary = povezan samo na kontrolni poslužitelj — nema tunela ni do jednog od { $total } peerova
doctor-step1-control-only-hint = ova mreža propušta TCP 443 prema van, ali blokira podatkovne portove mesha, pa ništa na meshu (uključujući stranice za prijavu) nije dostupno s ovog uređaja. Zamolite mrežni tim da dopusti odlazni TCP i UDP prema { $relays } te UDP 3478 prema { $stun }; preporučuje se i izravni UDP između uređaja. Za potvrdu se spojite na hotspot telefona i pokrenite `netbird status`: peerovi bi se tada trebali povezati.
doctor-offmesh-dashboard-hint = relay flota (vanjska točka promatranja): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokalni certifikat, odgovarajući ključ i CA paket se parsiraju; certifikat je unutar roka valjanosti
doctor-step2-material-failed-summary = lokalni TLS materijal { $file } nije moguće koristiti
doctor-step2-material-failed-hint = { $err } — pregledajte navedenu datoteku; vratite je iz izdanog paketa ili rotirajte profil
doctor-step2-material-invalid-hint = { $file } nije valjani X.509 PEM certifikat; vratite izdani materijal ili pokrenite `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem je neispravan ili ne odgovara datoteci mtls.cert.pem; vratite izdani materijal ili pokrenite `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokalni certifikat je izvan roka valjanosti
doctor-step2-cert-time-hint = pokrenite `goat profile show`; provjerite rezultat sata gore, zatim od operatera zatražite novi paket i, ako je certifikat istekao, pokrenite `goat profile rotate <new-bundle.cbor>`
doctor-skipped-no-endpoint-summary = preskočeno — u profile.toml nedostaje { $field }
doctor-skipped-no-profile-summary = preskočeno — nema čitljivih endpointa profila za ovaj kontekst
doctor-skipped-no-portal-token-summary = preskočeno — nema čitljivog tokena portala za ovaj kontekst
status-ztol-tcp-ok = port routera { $host }:{ $port } prihvaća TCP (samo port — vidi session)
status-session-ok = zenoh mTLS sesija otvorena za { $ms }ms (router je prihvatio ovaj certifikat)
status-session-skipped = sesija nije pokušana (izvan mesha; vidi ztol)
status-session-material-missing = zenoh sesija nije pokušana: { $detail }
status-session-config-failed = konfiguracija zenoh sesije nije uspjela: { $err }
status-session-failed = zenoh sesija se nije otvorila: { $err } — pokreni `goat doctor`
status-session-timeout = zenoh sesija se nije otvorila u { $secs }s — port routera prima TCP, ali nijedna sesija se ne dovršava (router zaglavio, certifikat odbijen ili pomak sata); pokreni `goat doctor`
status-names-live = imena aktivna (mesh DNS odgovorio)
status-names-ip-only = imena nisu provjerena (endpointi profila su IP literali)
status-names-snapshot = imena prema potpisanom snapshotu serijski broj { $serial }, starost { $age } (mesh DNS nedostupan)
status-names-snapshot-aging = imena prema potpisanom snapshotu serijski broj { $serial }, starost { $age } — ZASTARJELO (mesh DNS nedostupan; popravi goat-coredns, imena će se zatim osvježiti automatski)
status-names-snapshot-daemon = imena prema potpisanom snapshotu serijski broj { $serial }, starost { $age } — pohrana daemona (mesh DNS nedostupan)
status-names-snapshot-daemon-aging = imena prema potpisanom snapshotu serijski broj { $serial }, starost { $age } — pohrana daemona, ZASTARJELO (mesh DNS nedostupan; popravi goat-coredns, imena će se zatim osvježiti automatski)
status-names-failed = mesh DNS nedostupan, a snapshot fallback nije dostupan — { $reason }
status-pqc-on = wireguard PQC (rosenpass) UKLJUČEN — { $qr }/{ $connected } povezanih peerova otporno na kvantne napade
status-pqc-off = wireguard PQC (rosenpass) ISKLJUČEN — klasični wireguard handshakeovi
status-pqc-unknown = wireguard PQC nepoznat — { $reason }
status-device-none = zaštita imena na razini uređaja ISKLJUČENA — nema goat-clientd pohrane imena (goat naredbe i dalje imaju fallback; uključi prema name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd pohrana imena postoji (serijski broj { $serial }), ali njezin forwarder NE odgovara na { $addr } — daemon ne radi ili je podsustav imena zaglavio
status-device-active = zaštita imena na razini uređaja AKTIVNA — goat-clientd je ispred OS mesh DNS-a na { $addr } (serijski broj pohrane { $serial }, { $grade })
status-device-not-fronted = goat-clientd forwarder radi na { $addr } (serijski broj pohrane { $serial }, { $grade }) — OS nije preusmjeren; preglednici koriste izravni mesh DNS
status-device-fronting-unknown = goat-clientd forwarder radi na { $addr } (serijski broj pohrane { $serial }, { $grade }) — stanje preusmjeravanja OS-a nije moguće otkriti na ovoj platformi
status-names-observed = imena NEKANONSKA (ad hoc): primijećen zapis, starost { $age } — nije u potpisanom snapshotu (mesh DNS nedostupan; kodificiraj ovaj zapis)
status-names-selfclaim = imena putem provjerenog SELF-CLAIMA, starost { $age } — potpisao ga je vlastiti anchor imena (mesh DNS nedostupan)
doctor-names-note = putem snapshota serijski broj { $serial }, starost { $age }
doctor-names-selfclaim-note = putem provjerenog SELF-CLAIMA, starost { $age } — potpisao ga je vlastiti anchor imena
doctor-names-selfclaim-hint = mesh DNS je nedostupan — ovaj korak razriješen je putem provjerenog SELF-CLAIMA (starost { $age }): vlastiti anchor imena potpisao je ovu vezu, a peer_bindings iz keširanog snapshota je odobravaju. Potpisano, ali ne i aktivno; popravi mesh DNS da ovo nestane.
doctor-names-observed-note = putem PRIMIJEĆENOG (ad hoc) zapisa, starost { $age } — nekanonski
doctor-names-observed-hint = mesh DNS je nedostupan — ovaj korak razriješen je putem NEKANONSKOG primijećenog zapisa (starost { $age }): ime postoji samo uživo, nikad nije kodificirano u registru lokacije. Kodificiraj ga (group_vars registry → generator → ponovno potpisivanje) ili će isteći; popravi mesh DNS da ovo nestane.
doctor-names-hint = mesh DNS je nedostupan — ovaj korak razriješen je putem potpisanog snapshota imena (serijski broj { $serial }, starost { $age }). Fabric ostaje dostupan preko fallback imena; popravi mesh DNS (goat-coredns na mgmt hostu) da ovo nestane.
storm-brake-plan-header = Plan kočnice oluje (ADR 1081, sloj 4 — ručno):
storm-brake-plan-target = mgmt host { $user }@{ $host }  (kontekst { $context })
storm-brake-step-flush = restart signalnog kontejnera `{ $container }` (odbacuje zombi tokove)
storm-brake-step-tighten = stezanje L2 limitera na `{ $container }` (prepolovi rate-limit, podigni neg-cache TTL) — { $window } min, operator vraća ručno; preskače se ako L2 nije razmješten
storm-brake-step-shed = pauziraj nebitnu publisher jedinicu `{ $unit }`
storm-brake-shed-none = (nema deklariranih shed jedinica — postavi storm_brake_shed_units u profile.toml ili proslijedi --shed-unit)
storm-brake-announce-note = zapiši jedan OperatorActionV1 u audit trag s ishodima po koracima
storm-brake-dryrun-note = probni rad — ništa nije izvršeno.
storm-brake-triggers-header = Aktiviraj kad relay-health host-net pojas pokaže:
storm-brake-trigger-futility = omjer uzaludnosti signalizacije >= 0.5 pri >= 100 poruka/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = gubitak uplinka >= 5% kroz 10 min (wan.loss_pct)
storm-brake-triggers-band-url = host pojas: relay-health.{ $site }.<zone> (vidi docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Napomena: samo flush signala NE vraća mesh. Lijek su slojevi 1-2 (client backoff + signal shedding); kočnica samo kupuje vrijeme za oporavak.
storm-brake-confirm = Aktivirati kočnicu oluje na { $user }@{ $host }? Restartira signalni kontejner i pauzira deklarirane publishere. [y/N]
storm-brake-aborted = kočnica oluje prekinuta (nema potvrde)
storm-brake-executing = aktiviram kočnicu oluje na { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = neuspjeh
storm-brake-outcome-skipped = preskočeno
storm-brake-summary = kočnica oluje gotova: { $ok } ok, { $failed } neuspjelo, { $skipped } preskočeno
storm-brake-flush-failed = FLUSH nije uspio — signalni kontejner se nije restartao, oporavak mesha se ne očekuje: { $reason }
storm-brake-mgmt-unresolved = mgmt host nije razriješen — proslijedi --mgmt-host, postavi mgmt_ssh_host u profile.toml konteksta ili izvezi GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operatorska radnja zapisana u audit trag (akter { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portal je odgovorio HTTP 404 za `{ $path }`: nije registrirano tamo
error-portal-rate-limited-summary = portal je odgovorio HTTP 429 za `{ $path }`: previše zahtjeva, pokušajte kasnije
error-portal-server-error-summary = portal je odgovorio HTTP { $status } za `{ $path }`: greška na strani portala
error-portal-refused-summary = portal je odgovorio HTTP { $status } za `{ $path }`: zahtjev je odbijen

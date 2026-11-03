### goat-cli — Montenegrin (cnr). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = nema profila za kontekst `{ $context }`
error-off-mesh-summary = izvan ZTOL-a — bez veze sa mesh mrežom
error-router-unreachable-summary = Zenoh ruter na `{ $endpoint }` nije dostupan
error-cert-expired-summary = mTLS sertifikat je istekao ili ga je ruter odbio
error-portal-unreachable-summary = API portala na `{ $endpoint }` nije dostupan
error-token-invalid-summary = portal je odbio Bearer token
error-topic-conflict-summary = topic `{ $topic }` već postoji
error-bundle-invalid-summary = provjera potpisa ili isteka bundle-a nije uspjela

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = krajnja tačka rutera neispravnog formata
doctor-step1-endpoint-malformed-hint = očekivano `<proto>/<host>:<port>`; primljeno `{ $endpoint }`
doctor-step1-ok-summary = TCP veza sa domaćinom rutera { $host }:{ $port } uspjela
doctor-step1-conn-failed-summary = TCP veza sa domaćinom rutera nije uspjela
doctor-step1-conn-failed-hint = { $err } — pokrenite netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); provjerite `netbird status`
doctor-step1-timeout-summary = isteklo vrijeme TCP veze sa domaćinom rutera
doctor-step1-timeout-hint = domaćin rutera { $host }:{ $port } nedostupan za { $ms }ms — vjerovatno izvan mesh
doctor-step2-endpoint-malformed-hint = ispravite izdavaoca bundle (--goat-cli-router-endpoint mora biti `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM CA korijena nedostaje ili je nečitljiv
doctor-step2-ca-missing-hint = { $err } — ponovo pokrenite `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = ruter { $host }:{ $port } prihvata TCP (TLS provjeren u koraku 3)
doctor-step2-refused-summary = TCP rutera odbijen
doctor-step2-refused-hint = { $err } — ruter je možda u kvaru; provjerite implementaciju Zenoh na mgmt-host
doctor-step2-timeout-summary = isteklo vrijeme TCP rutera
doctor-step2-timeout-hint = ruter { $host }:{ $port } nedostupan za { $ms }ms
doctor-step3-config-failed-summary = izgradnja konfiguracije zenoh nije uspjela
doctor-step3-config-failed-hint = nevažeća krajnja tačka `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = izgradnja TLS konfiguracije zenoh nije uspjela
doctor-step3-tls-config-failed-hint = { $err } — vjerovatno odstupanje verzije zenoh; provjerite podudaranje zavisnosti
doctor-step3-ok-summary = mTLS sesija otvorena — sertifikat prihvatio ACL rutera
doctor-step3-open-failed-summary = otvaranje zenoh sesije nije uspjelo
doctor-step3-open-failed-hint = { $err } — sertifikat je možda istekao (pokrenite `goat profile show`) ili ga je odbio ACL rutera (nepodudaranje URI-SAN)
doctor-step3-timeout-summary = isteklo vrijeme otvaranja zenoh sesije
doctor-step3-timeout-hint = ruter nije završio rukovanje za { $secs }s — pomak sata, pogrešan sertifikat ili zagušenje
doctor-step4-ok-summary = portal je vratio { $n } redova organizacija
doctor-step4-token-rejected-summary = portal je odbio Bearer token
doctor-step4-token-rejected-hint = HTTP { $status } — token istekao ili opozvan; ponovo se uključite putem operator-onboard.yml
doctor-step4-request-failed-summary = zahtjev portalu nije uspio
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = isteklo vrijeme zahtjeva portalu
doctor-step4-timeout-hint = nema odgovora za { $secs }s — provjerite je li { $endpoint } dostupan iz mesh
doctor-skipped-summary = preskočeno — prethodni korak nije uspio (ponovo pokrenite sa --all za prisilu)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Jedinstvena operaterska komandna linija za data fabric goat.
help-long-about = goat — CLI okrenut operateru koji sklapa panoscope-core u jedan alat: inicijalizacija/ponovna upotreba profila, doctor dostupnosti svjestan ZTOL-a, pregled portala organizacija, CRUD topic-a, pub/sub, delegiranje na stethoscope, interaktivni sastavljač komandi i pripovijedanje kaskade substrate.
help-flag-profile-dir = Premosti direktorijum profila (podrazumijevano: $GOAT_PROFILE_DIR ili ~/.goat).
help-flag-context = Premosti aktivni kontekst (podrazumijevano: promjenljiva $GOAT_CONTEXT, zatim current_context u config.toml, zatim "default").
help-flag-narrate = Prikaži kaskadu substrate koju pokreće komanda koja mijenja stanje.
help-flag-verbose = Omogući detaljan izlaz za otklanjanje grešaka (ili postavi RUST_LOG=goat=debug).
help-flag-color = Režim boje za izlaz terminala (auto|always|never).
help-flag-lang = Jezik za tekst namijenjen ljudima (npr. en, uk). Vraća se na GOAT_LANG, zatim na lokalizaciju OS-a, zatim na engleski.
help-cmd-profile = Upravljaj lokalnim goat profilom (mTLS sertifikat + token portala + skup krajnjih tačaka).
help-cmd-context = Upravljaj kontekstima (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Pokreni postupnu sondu dostupnosti prema fabric-u.
help-cmd-doctor-long = Postupna sonda dostupnosti prema fabric-u: članstvo u ZTOL-u → ruter Zenoh → prihvatanje sertifikata → portal. Svaki neuspjeli korak emituje specifičan savjet za otklanjanje.
help-cmd-vendor = Pregledaj organizacije registrovane u portalu.
help-cmd-topic = Izlistaj, prikaži, kreiraj ili obriši topic.
help-cmd-pub = Objavi sadržaj na ključ topic-a.
help-cmd-sub = Pretplati se na izraz ključa topic-a.
help-cmd-inspect = Delegiraj zahtjev za dijagnostički verdikt na stethoscope.
help-cmd-ui = Interaktivni objedinjeni birač: pregledaj deklarisanu ⊕ živu listu i preskoči na sočivo Diagnose / Tail na izabranoj meti.
help-cmd-compose = Interaktivni TUI za izgradnju komandi.
help-cmd-narrate = Refleksija kaskade substrate nad nedavnom aktivnošću. Još nije dostupno (svakodnevni put je --narrate na komandi koja mijenja stanje).
help-cmd-completion = Ispiši skriptu za dovršavanje ljuske na stdout.
help-cmd-explain = Potraži značenje koda greške + otklanjanje u ugrađenom dokumentu `goat-cli-error-codes.md` (obrazac cargo --explain).
help-cmd-mock = Objavi sintetičke uzorke na topic. Podrazumijevano: trivijalni hello-world JSON na 1Hz. Proslijedi `--format <.fds>` + `--message <Name>` da umjesto toga kodiraš protobuf koji daje operater.
help-cmd-status = Sinteza zdravlja: dostupnost ZTOL-a + /api/health portala + lokalni istek sertifikata, tri glyph linije ili jedan --json objekat.
help-cmd-services = Sondiraj svaku hostovanu uslugu u `ops/serve/services.yaml` i prikaži jednu glyph liniju po usluzi (ili jedan --json objekat).
help-cmd-client = Obuhvati lokalni daemon goat-clientd preko njegovog JSON-RPC IPC soketa (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = Otkrivanje proširenja zasnovano na PATH-u (stil kubectl). Bilo koji izvršni fajl `goat-<name>` na PATH-u može se pozvati kao `goat <name>`; `goat plugin list` nabraja instalirane plugin-e, `goat plugin which <name>` razrješava putanju.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = krajnja tačka rutera neispravnog formata: `{ $endpoint }`
status-ztol-refused = TCP odbijen ka ruteru { $host }:{ $port }: { $err }
status-ztol-unreachable = izvan mesh mreže (ruter { $host }:{ $port } nedostupan za { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health nije uspio: { $err }
status-portal-timeout = isteklo vrijeme portal /api/health za { $secs }s
status-cert-read-failed = čitanje sertifikata nije uspjelo: { $err }
status-cert-pem-parse-failed = raščlanjivanje PEM sertifikata nije uspjelo: { $err }
status-cert-wrong-tag = prvi PEM blok je { $tag }, očekuje se CERTIFICATE
status-cert-x509-parse-failed = raščlanjivanje x509 nije uspjelo: { $err }
status-cert-expired = sertifikat je ISTEKAO na unix={ $not_after }, sada={ $now }
status-cert-valid = sertifikat je važeći; ističe za { $days_left }d (unix={ $not_after })
inspect-finding-label = nalaz ({ $severity }): { $label } — { $detail }
inspect-remedy-label = otklanjanje: { $title } — { $detail }
inspect-no-outcomes = (nema rezultata — meta ne odgovara nijednoj provjeri)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put prihvaćen na `{ $key }` ({ $bytes } bajtova)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskada se slegla — { $summary } ({ $n } { $n ->
        [one] posljedica
        [few] posljedice
       *[other] posljedica
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Čarobnjak za prvo pokretanje: izaberite sandbox ili stvarno okruženje, dokažite da radi, uputite na tutora.
help-cmd-tutor = Samostalni praktični obilazak fabric-a (u stilu vimtutor; nastavljiv).
help-cmd-docs = Pregledajte goat dokumentaciju ili generišite potpuni pregled komandi (--markdown).
help-cmd-hello = Provedite hello kroz fabric s kraja na kraj (sandbox ili stvarno okruženje).
flourish-all-green = 🐐 sve zeleno — fabric u normali

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = radnja samo za operatora — nijeste ovlašćeni (`goat admin` je inertno za ne-administratore)
help-flag-narrate-json = Emituj supstratni kaskad kao NDJSON (jedan objekat po posljedici) za logove incidenata + skriptovanje. Podrazumijeva --narrate.
help-cmd-admin = Administracija samo za operatora: životni ciklus organizacije, onboarding/opoziv/rotacija, isključenje mesh-pira, provizionisanje uređaja + operatorskog nivoa, LAN trijažа. Ovlašćeno vašim operatorskim kredencijalima (serverski putem portal/NetBird token scopea); inertno za ne-administratore — neovlašćeni poziv ne mijenja ništa i ne uspijeva sa `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profil za kontekst `{ $context }` postoji, ali ga ovaj korisnik ne može čitati (pristup odbijen)
error-portal-bad-response-summary = portal je odgovorio, ali ova goat verzija nije mogla da parsira odgovor
doctor-step0-ok-summary = lokalna postavka je spremna za mrežne probe
doctor-step0-warn-summary = lokalna postavka ima provjere koje nije bilo moguće potvrditi
doctor-step0-fail-summary = lokalna postavka traži pažnju
doctor-step0-profile-absent-summary = nema goat profila za kontekst `{ $context }`
doctor-step0-profile-new-hint = provjeri `goat context ls`; ako ti je ovo prva postavka, pokreni `goat ca add <bundle-ca.pem>` i uporedi njegov otisak s otiskom svog operatera, zatim `goat profile init <bundle.cbor>`; ponovo pokreni `goat doctor`
doctor-step0-profile-absent-hint = provjeri `goat context ls`; odaberi postojeći kontekst sa `goat context use <name>` ili pokreni `goat profile init <bundle.cbor>` za ovaj kontekst; ponovo pokreni `goat doctor`
doctor-step0-profile-denied-summary = goat profil na { $path } nije čitljiv
doctor-step0-profile-denied-hint = popravi vlasništvo nad profilom ili pokreni kao korisnik vlasnik; ne inicijalizuj ponovo postojeći profil
doctor-step0-profile-incomplete-summary = datoteka goat profila { $file } nedostaje ili nije čitljiva
doctor-step0-profile-incomplete-hint = vrati navedenu datoteku iz izdatog bundle-a ili koristi `goat profile rotate <bundle.cbor>` da zamijeniš ovaj kontekst; ponovo pokreni `goat doctor`
doctor-step0-profile-invalid-summary = goat profil ima nečitljive ili nedostajuće endpointe
doctor-step0-profile-empty-token-summary = goat profil ima prazan portal token
doctor-step0-profile-ok-summary = goat kontekst `{ $context }` ima čitljiv materijal profila
doctor-step0-client-absent-summary = nijedan podržani mesh klijent nije odgovorio
doctor-step0-client-absent-hint = instaliraj mesh klijent koji obezbjeđuje tvoj goatnet, pokreni njegov daemon, pa ponovo pokreni `goat doctor`
doctor-step0-client-stopped-summary = instalirani mesh klijent nije vratio upotrebljiv status
doctor-step0-client-stopped-hint = provjeri `netbird status -d` ili `goat client status --detail`; pokreni instalirani daemon (`sudo netbird service start` za NetBird), pa ponovo pokreni `goat doctor`
doctor-step0-netbird-ok-summary = NetBird daemon je odgovorio (prijavljeni profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd je odgovorio (stanje `{ $state }`)
doctor-step0-management-connected-summary = NetBird prijavljuje da je management povezan
doctor-step0-management-disconnected-summary = NetBird prijavljuje da management nije povezan
doctor-step0-management-disconnected-hint = provjeri prijavljeni management URL i `netbird status -d`; uspostavljen tunel može i dalje raditi tokom prekida managementa
doctor-step0-management-unknown-summary = konfigurisani management endpoint nije bilo moguće utvrditi lokalno
doctor-step0-management-unknown-hint = provjeri `netbird status -d` ili `goat client diagnostics`; doctor neće pogađati management URL
doctor-step0-tunnel-down-summary = NetBird ne prijavljuje mesh adresu
doctor-step0-tunnel-down-hint = pokreni `netbird status -d`; ako je uređaj upisan i daemon odgovara, pokreni `goat heal`, pa ponovo `goat doctor`
doctor-step0-tunnel-address-summary = NetBird prijavljuje mesh adresu { $address }; dostupnost rutera se provjerava sljedeća
doctor-step0-goat-client-no-bundle-summary = goat-clientd nema uvezen bundle
doctor-step0-goat-client-no-bundle-hint = pokreni `goat client bundle import <bundle.cbor>` s klijentskim enrollment bundleom, poveži klijent, pa ponovo pokreni `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd prijavljuje povezan tunel; dostupnost rutera se provjerava sljedeća
doctor-step0-goat-client-outer-disabled-summary = goat-clientd vanjski tunel je isključen u netbird-only režimu
doctor-step0-goat-client-down-summary = goat-clientd prijavljuje stanje tunela `{ $state }`
doctor-step0-goat-client-down-hint = pokreni `goat client diagnostics`, pa `goat client up` ako je bundle učitan; ponovo pokreni `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd prijavljuje da je njegov unutrašnji mesh povezan
doctor-step0-goat-client-inner-disabled-summary = goat-clientd unutrašnji mesh je isključen u wg-cp0-only režimu
doctor-step0-goat-client-inner-down-summary = goat-clientd prijavljuje stanje unutrašnjeg mesha `{ $state }`
doctor-step0-goat-client-inner-down-hint = pokreni `goat client diagnostics` da pregledaš unutrašnji mesh, pa ponovo pokreni `goat doctor` nakon što ga vratiš
doctor-step0-management-reachable-summary = management endpoint je odgovorio HTTP { $status }
doctor-step0-management-auth-summary = management HTTPS endpoint je dostupan (HTTP { $status } traži autorizaciju za ovu probu)
doctor-step0-management-unexpected-summary = management endpoint je vratio neočekivani HTTP { $status }
doctor-step0-management-unexpected-hint = provjeri podešeni management URL i zamoli goatnet operatera da provjeri ovaj odgovor
doctor-step0-management-unreachable-summary = podešeni management endpoint nije dostupan: { $error }
doctor-step0-management-unreachable-hint = provjeri DNS i mrežni pristup URL-u koji je prijavio klijent; postojeći tunel i dalje može raditi
doctor-step0-clock-ok-summary = lokalni sat je unutar { $seconds } sekundi od management HTTP vremena
doctor-step0-clock-skew-summary = lokalni sat se razlikuje od management HTTP vremena za { $seconds } sekundi
doctor-step0-clock-skew-hint = sinhronizuj sat uređaja, pa ponovo pokreni `goat doctor`; validacija certifikata može da padne dok je sat pogrešan
doctor-step0-clock-unknown-summary = odstupanje sata nije moguće izmjeriti iz management HTTP Date zaglavlja
doctor-step0-clock-unknown-hint = provjeri sinhronizaciju vremena na uređaju ako TLS ili provjere certifikata padaju
error-clock-skew-summary = sat uređaja previše odstupa od vremena upravljanja
error-doctor-inconclusive-summary = doctor je uočio grešku bez dokazanog uzroka
error-portal-forbidden-summary = portal je ovom nalogu uskratio pristup ispitanom API-ju
doctor-step4-forbidden-summary = portal je za ovaj profil vratio HTTP 403
doctor-step4-forbidden-hint = ovaj kredencijal je stigao do portala, ali nema pristup ispitanom API-ju; provjerite `goat profile show` i zamolite goatnet operatera da provjeri dozvolu
doctor-step4-decode-summary = portal je odgovorio odgovorom koji ovaj goat ne može pročitati
doctor-step4-decode-hint = { $err } — ažurirajte goat na verziju koju servira ovaj goatnet, pa ponovo pokrenite `goat doctor`
doctor-step1-control-only-summary = povezan samo na kontrolni server — nema tunela ni do jednog od { $total } peer-ova
doctor-step1-control-only-hint = ova mreža propušta TCP 443 prema van, ali blokira data portove mesha, pa ništa na meshu (uključujući stranice za prijavu) nije dostupno sa ovog uređaja. Zamolite mrežni tim da dozvoli odlazni TCP i UDP prema { $relays }, i UDP 3478 prema { $stun }; preporučuje se i direktan UDP između uređaja. Za potvrdu, povežite se na hotspot telefona i pokrenite `netbird status`: peer-ovi bi se tada trebalo da povežu.
doctor-offmesh-dashboard-hint = relay flota (spoljna tačka posmatranja): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokalni sertifikat, odgovarajući ključ i CA paket se parsiraju; sertifikat je unutar perioda važenja
doctor-step2-material-failed-summary = lokalni TLS materijal { $file } nije moguće upotrijebiti
doctor-step2-material-failed-hint = { $err } — provjerite navedenu datoteku; vratite je iz izdatog paketa ili rotirajte profil
doctor-step2-material-invalid-hint = { $file } nije važeći X.509 PEM sertifikat; vratite izdati materijal ili pokrenite `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem je nevažeći ili ne odgovara datoteci mtls.cert.pem; vratite izdati materijal ili pokrenite `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokalni sertifikat je van perioda važenja
doctor-step2-cert-time-hint = pokrenite `goat profile show`; provjerite rezultat za sat iznad, pa zatražite od operatera novi paket i pokrenite `goat profile rotate <new-bundle.cbor>` ako je sertifikat istekao
doctor-skipped-no-endpoint-summary = preskočeno — u profile.toml nedostaje { $field }
doctor-skipped-no-profile-summary = preskočeno — nema čitljivih endpointa profila za ovaj kontekst
doctor-skipped-no-portal-token-summary = preskočeno — nema čitljivog portal tokena za ovaj kontekst
status-ztol-tcp-ok = port rutera { $host }:{ $port } prihvata TCP (samo port — vidi sesiju)
status-session-ok = zenoh mTLS sesija otvorena za { $ms }ms (ruter je prihvatio ovaj sertifikat)
status-session-skipped = sesija nije pokušana (van mesha; vidi ztol)
status-session-material-missing = zenoh sesija nije pokušana: { $detail }
status-session-config-failed = konfiguracija zenoh sesije nije uspjela: { $err }
status-session-failed = zenoh sesija se nije otvorila: { $err } — pokrenite `goat doctor`
status-session-timeout = zenoh sesija se nije otvorila u roku od { $secs }s — port rutera prima TCP, ali nijedna sesija se ne završava (ruter se zaglavio, sertifikat odbijen ili pomak sata); pokrenite `goat doctor`
status-names-live = imena aktivna (mesh DNS odgovorio)
status-names-ip-only = imena nisu provjerena (endpointi profila su IP adrese)
status-names-snapshot = imena prema potpisanom snimku, serijski broj { $serial }, starost { $age } (mesh DNS nedostupan)
status-names-snapshot-aging = imena prema potpisanom snimku, serijski broj { $serial }, starost { $age } — ZASTARJELO (mesh DNS nedostupan; popravite goat-coredns, imena će se zatim osvježiti automatski)
status-names-snapshot-daemon = imena prema potpisanom snimku, serijski broj { $serial }, starost { $age } — skladište daemona (mesh DNS nedostupan)
status-names-snapshot-daemon-aging = imena prema potpisanom snimku, serijski broj { $serial }, starost { $age } — skladište daemona, ZASTARJELO (mesh DNS nedostupan; popravite goat-coredns, imena će se zatim osvježiti automatski)
status-names-failed = mesh DNS nedostupan, a rezervni snimak nije dostupan — { $reason }
status-pqc-on = wireguard PQC (rosenpass) UKLJUČEN — { $qr }/{ $connected } povezanih peerova otporno na kvantne napade
status-pqc-off = wireguard PQC (rosenpass) ISKLJUČEN — klasični wireguard handshake-ovi
status-pqc-unknown = wireguard PQC nepoznat — { $reason }
status-device-none = zaštita imena na nivou uređaja ISKLJUČENA — nema goat-clientd skladišta imena (goat komande i dalje imaju rezervno rješenje; uključite prema name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd skladište imena postoji (serijski broj { $serial }), ali njegov forwarder NE odgovara na { $addr } — daemon ne radi ili se podsistem imena zaglavio
status-device-active = zaštita imena na nivou uređaja AKTIVNA — goat-clientd je ispred OS mesh DNS-a na { $addr } (serijski broj skladišta { $serial }, { $grade })
status-device-not-fronted = goat-clientd forwarder radi na { $addr } (serijski broj skladišta { $serial }, { $grade }) — OS nije usmjeren na njega; browseri koriste direktan mesh DNS
status-device-fronting-unknown = goat-clientd forwarder radi na { $addr } (serijski broj skladišta { $serial }, { $grade }) — stanje usmjeravanja OS-a nije moguće utvrditi na ovoj platformi
status-names-observed = imena NEKANONSKA (ad hoc): uočen zapis, starost { $age } — nije u potpisanom snimku (mesh DNS nedostupan; kodifikujte ovaj zapis)
status-names-selfclaim = imena putem provjerenog SELF-CLAIM-a, starost { $age } — potpisao ga je sopstveni anchor imena (mesh DNS nedostupan)
doctor-names-note = putem snimka, serijski broj { $serial }, starost { $age }
doctor-names-selfclaim-note = putem provjerenog SELF-CLAIM-a, starost { $age } — potpisao ga je sopstveni anchor imena
doctor-names-selfclaim-hint = mesh DNS je nedostupan — ovaj korak je razriješen putem provjerenog SELF-CLAIM-a (starost { $age }): sopstveni anchor imena potpisao je ovu vezu, a peer_bindings iz keširanog snimka je odobravaju. Potpisano, ali nije aktivno; popravite mesh DNS da ovo nestane.
doctor-names-observed-note = putem UOČENOG (ad hoc) zapisa, starost { $age } — nekanonski
doctor-names-observed-hint = mesh DNS je nedostupan — ovaj korak je razriješen putem NEKANONSKOG uočenog zapisa (starost { $age }): ime postoji samo uživo i nikad nije kodifikovano u registru lokacije. Kodifikujte ga (group_vars registar → generator → ponovni potpis) ili će isteći; popravite mesh DNS da ovo nestane.
doctor-names-hint = mesh DNS je nedostupan — ovaj korak je razriješen putem potpisanog snimka imena (serijski broj { $serial }, starost { $age }). Fabric ostaje dostupan preko rezervnih imena; popravite mesh DNS (goat-coredns na mgmt hostu) da ovo nestane.
storm-brake-plan-header = Plan kočnice oluje (ADR 1081 sloj 4 — ručno):
storm-brake-plan-target = mgmt host { $user }@{ $host }  (kontekst { $context })
storm-brake-step-flush = restartuj signal kontejner `{ $container }` (odbacuje zombi tokove)
storm-brake-step-tighten = stegni L2 limiter na `{ $container }` (prepolovi rate-limit, povećaj TTL neg-keša) — { $window } min, operator vraća ručno; preskače se ako L2 nije raspoređen
storm-brake-step-shed = pauziraj nebitnu publisher jedinicu `{ $unit }`
storm-brake-shed-none = (nijedna shed jedinica nije deklarisana — postavi storm_brake_shed_units u profile.toml ili proslijedi --shed-unit)
storm-brake-announce-note = pošalji jedan OperatorActionV1 u audit trag s ishodima po koracima
storm-brake-dryrun-note = probni rad — ništa nije izvršeno.
storm-brake-triggers-header = Aktiviraj kada relay-health host-net pojas pokaže:
storm-brake-trigger-futility = omjer uzaludnosti signalizacije >= 0.5 pri >= 100 poruka/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = gubitak uplinka >= 5% u trajanju 10 min (wan.loss_pct)
storm-brake-triggers-band-url = pojas hosta: relay-health.{ $site }.<zone> (vidi docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Napomena: samo pražnjenje signala NE vraća mesh. Slojevi 1-2 (client backoff + signal shedding) su lijek; kočnica kupuje vrijeme za oporavak.
storm-brake-confirm = Aktivirati kočnicu oluje na { $user }@{ $host }? Restartuje signal kontejner i pauzira deklarisane publishere. [y/N]
storm-brake-aborted = kočnica oluje prekinuta (nema potvrde)
storm-brake-executing = aktiviram kočnicu oluje na { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = neuspjelo
storm-brake-outcome-skipped = preskočeno
storm-brake-summary = kočnica oluje završena: { $ok } ok, { $failed } neuspjelo, { $skipped } preskočeno
storm-brake-flush-failed = FLUSH nije uspio — signal kontejner se nije restartovao, pa se oporavak mesha ne očekuje: { $reason }
storm-brake-mgmt-unresolved = mgmt host nije razriješen — proslijedi --mgmt-host, postavi mgmt_ssh_host u profile.toml konteksta ili izvezi GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operatorska radnja zapisana u audit trag (akter { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portal je odgovorio HTTP 404 za `{ $path }`: nije registrovano tamo
error-portal-rate-limited-summary = portal je odgovorio HTTP 429 za `{ $path }`: previše zahtjeva, pokušajte kasnije
error-portal-server-error-summary = portal je odgovorio HTTP { $status } za `{ $path }`: greška na strani portala
error-portal-refused-summary = portal je odgovorio HTTP { $status } za `{ $path }`: zahtjev je odbijen

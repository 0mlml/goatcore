### goat-cli — Slovenian (sl). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = ni profila za kontekst `{ $context }`
error-off-mesh-summary = zunaj ZTOL — ni povezave z omrežjem mesh
error-router-unreachable-summary = usmerjevalnik Zenoh na `{ $endpoint }` ni dosegljiv
error-cert-expired-summary = potrdilo mTLS je poteklo ali ga je usmerjevalnik zavrnil
error-portal-unreachable-summary = API portala na `{ $endpoint }` ni dosegljiv
error-token-invalid-summary = portal je zavrnil žeton Bearer
error-topic-conflict-summary = topic `{ $topic }` že obstaja
error-bundle-invalid-summary = preverjanje podpisa ali poteka paketa bundle ni uspelo

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = končna točka usmerjevalnika je napačno oblikovana
doctor-step1-endpoint-malformed-hint = pričakovano `<proto>/<host>:<port>`; prejeto `{ $endpoint }`
doctor-step1-ok-summary = povezava TCP z gostiteljem usmerjevalnika { $host }:{ $port } uspešna
doctor-step1-conn-failed-summary = povezava TCP z gostiteljem usmerjevalnika ni uspela
doctor-step1-conn-failed-hint = { $err } — zaženite netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); preverite `netbird status`
doctor-step1-timeout-summary = časovna omejitev povezave TCP z gostiteljem usmerjevalnika
doctor-step1-timeout-hint = gostitelj usmerjevalnika { $host }:{ $port } nedosegljiv v { $ms }ms — verjetno zunaj mesh
doctor-step2-endpoint-malformed-hint = popravite izdajatelja bundle (--goat-cli-router-endpoint mora biti `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM korenov CA manjka ali ni berljiv
doctor-step2-ca-missing-hint = { $err } — znova zaženite `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = usmerjevalnik { $host }:{ $port } sprejema TCP (TLS preverjen v koraku 3)
doctor-step2-refused-summary = TCP usmerjevalnika zavrnjen
doctor-step2-refused-hint = { $err } — usmerjevalnik je morda izklopljen; preverite namestitev Zenoh na mgmt-host
doctor-step2-timeout-summary = časovna omejitev TCP usmerjevalnika
doctor-step2-timeout-hint = usmerjevalnik { $host }:{ $port } nedosegljiv v { $ms }ms
doctor-step3-config-failed-summary = gradnja konfiguracije zenoh ni uspela
doctor-step3-config-failed-hint = neveljavna končna točka `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = gradnja konfiguracije TLS zenoh ni uspela
doctor-step3-tls-config-failed-hint = { $err } — verjetno odstopanje različice zenoh; preverite ujemanje odvisnosti
doctor-step3-ok-summary = seja mTLS odprta — potrdilo sprejela ACL usmerjevalnika
doctor-step3-open-failed-summary = odpiranje seje zenoh ni uspelo
doctor-step3-open-failed-hint = { $err } — potrdilo je morda poteklo (zaženite `goat profile show`) ali ga je zavrnila ACL usmerjevalnika (neujemanje URI-SAN)
doctor-step3-timeout-summary = časovna omejitev odpiranja seje zenoh
doctor-step3-timeout-hint = usmerjevalnik ni dokončal rokovanja v { $secs }s — zamik ure, napačno potrdilo ali preobremenitev
doctor-step4-ok-summary = portal je vrnil { $n } vrstic organizacije
doctor-step4-token-rejected-summary = portal je zavrnil žeton Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — žeton je potekel ali bil preklican; znova se vključite prek operator-onboard.yml
doctor-step4-request-failed-summary = zahteva do portala ni uspela
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = časovna omejitev zahteve do portala
doctor-step4-timeout-hint = ni odgovora v { $secs }s — preverite, ali je { $endpoint } dosegljiv iz mesh
doctor-skipped-summary = preskočeno — prejšnji korak ni uspel (znova zaženite z --all za prisilo)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Poenotena operaterska ukazna vrstica za podatkovno strukturo goat.
help-long-about = goat — operaterski CLI, ki sestavlja panoscope-core v eno orodje: inicializacija/ponovna uporaba profila, ZTOL-zavedni doctor dosegljivosti, brskanje po portalu organizacij, CRUD topic, pub/sub, delegiranje stethoscope, interaktivni sestavljalnik ukazov in pripoved kaskade strukture.
help-flag-profile-dir = Preglasi imenik profila (privzeto: $GOAT_PROFILE_DIR ali ~/.goat).
help-flag-context = Preglasi aktivni kontekst (privzeto: okolje $GOAT_CONTEXT, nato current_context v config.toml, nato „default").
help-flag-narrate = Pokaži kaskado strukture, ki jo sproži ukaz, ki spreminja stanje.
help-flag-verbose = Omogoči podroben razhroščevalni izpis (ali nastavi RUST_LOG=goat=debug).
help-flag-color = Barvni način za izpis terminala (auto|always|never).
help-flag-lang = Jezik za besedilo, namenjeno ljudem (npr. en, uk). Vrne se na GOAT_LANG, nato na področne nastavitve OS, nato na angleščino.
help-cmd-profile = Upravljaj lokalni profil goat (potrdilo mTLS + žeton portala + nabor končnih točk).
help-cmd-context = Upravljaj kontekste (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Zaženi stopenjski preizkuševalnik dosegljivosti glede na strukturo.
help-cmd-doctor-long = Stopenjski preizkuševalnik dosegljivosti glede na strukturo: članstvo v ZTOL → usmerjevalnik Zenoh → sprejem potrdila → portal. Vsak neuspeli korak odda specifičen namig za odpravo.
help-cmd-vendor = Brskaj po organizacijah, registriranih v portalu.
help-cmd-topic = Naštej, prikaži, ustvari ali izbriši topic.
help-cmd-pub = Objavi tovor na ključ topic.
help-cmd-sub = Naroči se na izraz ključa topic.
help-cmd-inspect = Deleguj zahtevo za diagnostično sodbo na stethoscope.
help-cmd-ui = Interaktivni združeni izbirnik: brskaj po seznamu deklariranih ⊕ živih in skoči na lečo Diagnose / Tail na izbranem cilju.
help-cmd-compose = Interaktivni TUI za gradnjo ukazov.
help-cmd-narrate = Refleksija kaskade strukture nad nedavno dejavnostjo. Še ni na voljo (vsakodnevna pot je --narrate na ukazu, ki spreminja stanje).
help-cmd-completion = Izpiši skript za dopolnjevanje lupine na stdout.
help-cmd-explain = Poišči pomen kode napake + odpravo v vgrajenem dokumentu `goat-cli-error-codes.md` (vzorec cargo --explain).
help-cmd-mock = Objavi sintetične vzorce na topic. Privzeto: trivialni hello-world JSON pri 1Hz. Podaj `--format <.fds>` + `--message <Name>`, da namesto tega kodiraš protobuf, ki ga je priskrbel operater.
help-cmd-status = Sinteza zdravja: dosegljivost ZTOL + portal /api/health + potek lokalnega potrdila, tri vrstice glyph ali en objekt --json.
help-cmd-services = Preizkusi vsako gostovano storitev v `ops/serve/services.yaml` in izriši eno vrstico glyph na storitev (ali en objekt --json).
help-cmd-client = Ovij lokalni daemon goat-clientd prek njegovega vtičnika JSON-RPC IPC (`~/.goat-client/goat-clientd.sock`): status / up / down / uvoz bundle.
help-cmd-plugin = Odkrivanje razširitev na podlagi PATH (slog kubectl). Vsako izvršljivo datoteko `goat-<name>` na PATH je mogoče priklicati kot `goat <name>`; `goat plugin list` našteje nameščene vtičnike, `goat plugin which <name>` razreši pot.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = končna točka usmerjevalnika je napačno oblikovana: `{ $endpoint }`
status-ztol-refused = TCP zavrnjen s strani usmerjevalnika { $host }:{ $port }: { $err }
status-ztol-unreachable = zunaj mesh (usmerjevalnik { $host }:{ $port } nedosegljiv v { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health ni uspel: { $err }
status-portal-timeout = časovna omejitev portala /api/health v { $secs }s
status-cert-read-failed = branje potrdila ni uspelo: { $err }
status-cert-pem-parse-failed = razčlenjevanje PEM potrdila ni uspelo: { $err }
status-cert-wrong-tag = prvi blok PEM je { $tag }, pričakovan CERTIFICATE
status-cert-x509-parse-failed = razčlenjevanje x509 ni uspelo: { $err }
status-cert-expired = potrdilo je POTEKLO ob unix={ $not_after }, zdaj={ $now }
status-cert-valid = potrdilo veljavno; poteče čez { $days_left }d (unix={ $not_after })
inspect-finding-label = ugotovitev ({ $severity }): { $label } — { $detail }
inspect-remedy-label = odprava: { $title } — { $detail }
inspect-no-outcomes = (ni izidov — cilj se ni ujemal z nobeno kontrolo)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put sprejet na `{ $key }` ({ $bytes } bajtov)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskada se je umirila — { $summary } ({ $n } { $n ->
        [one] posledica
        [two] posledici
        [few] posledice
       *[other] posledic
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Čarovnik za prvi zagon: izberi sandbox ali pravo okolje, preveri delovanje, usmeri na vodnik.
help-cmd-tutor = Samostojni praktični ogled strukture v lastnem tempu (v slogu vimtutor; nadaljevanje mogoče).
help-cmd-docs = Brskaj po dokumentaciji goat ali izpiši celotno referenco ukazov (--markdown).
help-cmd-hello = Izvede dvosmerni hello skozi strukturo od začetka do konca (sandbox ali pravo okolje).
flourish-all-green = 🐐 vse zeleno — struktura nominalna

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = dejanje samo za operaterje — niste pooblaščeni (`goat admin` je neaktiven za ne-administratorje)
help-flag-narrate-json = Oddaj kaskado substrata kot NDJSON (en objekt na posledico) za dnevnike incidentov + skriptiranje. Implicira --narrate.
help-cmd-admin = Administracija samo za operaterje: življenjski cikel organizacije, vključevanje/preklic/rotacija, prekinitev mesh-peer, zagotavljanje naprave + operaterske ravni, triažiranje LAN. Pooblaščeno z vašimi operaterskimi poverilnicami (strežniška stran prek obsega žetona portal/NetBird); neaktivno za ne-administratorje — nepooblaščen klic ne spremeni ničesar in ne uspe z `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profil za kontekst `{ $context }` obstaja, vendar ga ta uporabnik ne more prebrati (dostop zavrnjen)
error-portal-bad-response-summary = portal je odgovoril, vendar te izdaje goat ni mogla razčleniti odgovora
doctor-step0-ok-summary = lokalna nastavitev je pripravljena za omrežne preizkuse
doctor-step0-warn-summary = lokalna nastavitev vsebuje preverjanja, ki jih ni bilo mogoče potrditi
doctor-step0-fail-summary = lokalna nastavitev zahteva pozornost
doctor-step0-profile-absent-summary = ni profila goat za kontekst `{ $context }`
doctor-step0-profile-new-hint = preverite `goat context ls`; če nastavljate prvič, zaženite `goat ca add <bundle-ca.pem>` in primerjajte prstni odtis z operaterjevim, nato `goat profile init <bundle.cbor>`; ponovno zaženite `goat doctor`
doctor-step0-profile-absent-hint = preverite `goat context ls`; izberite obstoječi kontekst z `goat context use <name>` ali zaženite `goat profile init <bundle.cbor>` za ta kontekst; ponovno zaženite `goat doctor`
doctor-step0-profile-denied-summary = profila goat na { $path } ni mogoče prebrati
doctor-step0-profile-denied-hint = popravite lastništvo profila ali zaženite kot lastnik; obstoječega profila ne inicializirajte znova
doctor-step0-profile-incomplete-summary = datoteka profila goat { $file } manjka ali je ni mogoče prebrati
doctor-step0-profile-incomplete-hint = obnovite navedeno datoteko iz izdanega paketa ali uporabite `goat profile rotate <bundle.cbor>` za zamenjavo tega konteksta; ponovno zaženite `goat doctor`
doctor-step0-profile-invalid-summary = profil goat ima neberljive ali manjkajoče končne točke
doctor-step0-profile-empty-token-summary = profil goat ima prazen žeton portala
doctor-step0-profile-ok-summary = kontekst goat `{ $context }` ima berljivo gradivo profila
doctor-step0-client-absent-summary = noben podprt odjemalec mesh ni odgovoril
doctor-step0-client-absent-hint = namestite odjemalca mesh, ki ga zagotavlja vaš goatnet, zaženite njegov daemon in ponovno zaženite `goat doctor`
doctor-step0-client-stopped-summary = nameščeni odjemalec mesh ni vrnil uporabnega stanja
doctor-step0-client-stopped-hint = preverite `netbird status -d` ali `goat client status --detail`; zaženite nameščeni daemon (`sudo netbird service start` za NetBird), nato ponovno zaženite `goat doctor`
doctor-step0-netbird-ok-summary = daemon NetBird je odgovoril (poročani profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd je odgovoril (stanje `{ $state }`)
doctor-step0-management-connected-summary = NetBird poroča, da je management povezan
doctor-step0-management-disconnected-summary = NetBird poroča, da management ni povezan
doctor-step0-management-disconnected-hint = preverite poročani URL managementa in `netbird status -d`; vzpostavljen tunel lahko med izpadom managementa še deluje
doctor-step0-management-unknown-summary = konfigurirane končne točke managementa lokalno ni bilo mogoče vzpostaviti
doctor-step0-management-unknown-hint = preglejte `netbird status -d` ali `goat client diagnostics`; doctor ne bo ugibal URL-ja upravljanja
doctor-step0-tunnel-down-summary = NetBird ne poroča nobenega naslova mesh
doctor-step0-tunnel-down-hint = zaženite `netbird status -d`; če je naprava vpisana in demon odgovarja, zaženite `goat heal`, nato znova `goat doctor`
doctor-step0-tunnel-address-summary = NetBird poroča naslov mesh { $address }; sledi preverjanje dosegljivosti usmerjevalnika
doctor-step0-goat-client-no-bundle-summary = goat-clientd nima uvoženega paketa
doctor-step0-goat-client-no-bundle-hint = zaženite `goat client bundle import <bundle.cbor>` s paketom za vpis odjemalca, povežite odjemalca, nato znova zaženite `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd poroča o vzpostavljenem tunelu; sledi preverjanje dosegljivosti usmerjevalnika
doctor-step0-goat-client-outer-disabled-summary = zunanji tunel goat-clientd je onemogočen v načinu samo-netbird
doctor-step0-goat-client-down-summary = goat-clientd poroča stanje tunela `{ $state }`
doctor-step0-goat-client-down-hint = zaženite `goat client diagnostics`, nato `goat client up`, če je paket naložen; znova zaženite `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd poroča, da je notranji mesh povezan
doctor-step0-goat-client-inner-disabled-summary = notranji mesh goat-clientd je onemogočen v načinu samo-wg-cp0
doctor-step0-goat-client-inner-down-summary = goat-clientd poroča stanje notranjega mesha `{ $state }`
doctor-step0-goat-client-inner-down-hint = zaženite `goat client diagnostics` za pregled notranjega mesha, po obnovitvi znova zaženite `goat doctor`
doctor-step0-management-reachable-summary = končna točka upravljanja je odgovorila s HTTP { $status }
doctor-step0-management-auth-summary = končna točka HTTPS upravljanja je dosegljiva (HTTP { $status } pri tem preverjanju zahteva avtorizacijo)
doctor-step0-management-unexpected-summary = končna točka upravljanja je vrnila nepričakovan HTTP { $status }
doctor-step0-management-unexpected-hint = preverite nastavljeni URL upravljanja in prosite operaterja goatnet, naj preveri ta odgovor
doctor-step0-management-unreachable-summary = nastavljene končne točke upravljanja ni bilo mogoče doseči: { $error }
doctor-step0-management-unreachable-hint = preverite DNS in omrežni dostop do URL-ja, ki ga javlja odjemalec; obstoječi tunel morda še vedno deluje
doctor-step0-clock-ok-summary = lokalna ura se od HTTP časa upravljanja razlikuje za manj kot { $seconds } s
doctor-step0-clock-skew-summary = lokalna ura se od HTTP časa upravljanja razlikuje za { $seconds } s
doctor-step0-clock-skew-hint = uskladite uro naprave, nato znova zaženite `goat doctor`; preverjanje certifikatov lahko ne uspe, dokler ura ni pravilna
doctor-step0-clock-unknown-summary = zamika ure ni bilo mogoče izmeriti iz glave HTTP Date upravljanja
doctor-step0-clock-unknown-hint = če TLS ali preverjanje certifikatov ne uspe, preverite časovno sinhronizacijo naprave
error-clock-skew-summary = ura naprave je preveč oddaljena od upravljalskega časa
error-doctor-inconclusive-summary = doctor je zaznal napako brez dokazanega vzroka
error-portal-forbidden-summary = portal je temu računu zavrnil dostop do preizkušenega API-ja
doctor-step4-forbidden-summary = portal je za ta profil vrnil HTTP 403
doctor-step4-forbidden-hint = ta poverilnica je dosegla portal, vendar nima dostopa do preizkušenega API-ja; preverite `goat profile show` in prosite operaterja goatneta, naj preveri dodelitev
doctor-step4-decode-summary = portal je odgovoril z odgovorom, ki ga ta goat ne more prebrati
doctor-step4-decode-hint = { $err } — posodobite goat na različico, ki jo streže ta goatnet, nato znova zaženite `goat doctor`
doctor-step1-control-only-summary = povezan samo s kontrolnim strežnikom — brez tunela do nobenega od { $total } soležnikov
doctor-step1-control-only-hint = to omrežje spušča TCP 443 ven, blokira pa podatkovna vrata mesh, zato z te naprave ni dosegljivo nič na meshu (vključno s prijavnimi stranmi). Prosite omrežno ekipo, naj dovoli izhodni TCP in UDP do { $relays } ter UDP 3478 do { $stun }; priporočen je tudi neposreden UDP med napravami. Za potrditev se povežite na hotspot telefona in zaženite `netbird status`: soležniki bi se morali povezati.
doctor-offmesh-dashboard-hint = flota relayjev (zunanja perspektiva): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokalno potrdilo, ustrezni ključ in paket CA se razčlenijo; potrdilo je znotraj obdobja veljavnosti
doctor-step2-material-failed-summary = lokalnega materiala TLS { $file } ni bilo mogoče uporabiti
doctor-step2-material-failed-hint = { $err } — preglejte navedeno datoteko; obnovite jo iz izdanega paketa ali rotirajte profil
doctor-step2-material-invalid-hint = { $file } ni veljavno potrdilo X.509 PEM; obnovite izdani material ali zaženite `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem je neveljaven ali se ne ujema z mtls.cert.pem; obnovite izdani material ali zaženite `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokalno potrdilo je izven obdobja veljavnosti
doctor-step2-cert-time-hint = zaženite `goat profile show`; preverite rezultat ure zgoraj, nato prosite operaterja za nov paket in zaženite `goat profile rotate <new-bundle.cbor>`, če je potrdilo poteklo
doctor-skipped-no-endpoint-summary = preskočeno — v profile.toml manjka { $field }
doctor-skipped-no-profile-summary = preskočeno — za ta kontekst ni berljivih končnih točk profila
doctor-skipped-no-portal-token-summary = preskočeno — za ta kontekst ni berljivega žetona portala
status-ztol-tcp-ok = vrata usmerjevalnika { $host }:{ $port } sprejemajo TCP (samo vrata — glej session)
status-session-ok = zenoh mTLS seja odprta v { $ms }ms (usmerjevalnik je sprejel to potrdilo)
status-session-skipped = seja ni bila poskušana (zunaj mesha; glej ztol)
status-session-material-missing = zenoh seja ni bila poskušana: { $detail }
status-session-config-failed = konfiguracija zenoh seje ni uspela: { $err }
status-session-failed = seja zenoh se ni odprla: { $err } — zaženi `goat doctor`
status-session-timeout = seja zenoh se ni odprla v { $secs } s — vrata routerja sprejemajo TCP, a nobena seja se ne vzpostavi (router zatekel, certifikat zavrnjen ali zamik ure); zaženi `goat doctor`
status-names-live = imena živa (mesh DNS je odgovoril)
status-names-ip-only = imena niso preizkušena (končne točke profila so IP naslovi)
status-names-snapshot = imena po podpisanem posnetku, serija { $serial }, starost { $age } (mesh DNS ni dosegljiv)
status-names-snapshot-aging = imena po podpisanem posnetku, serija { $serial }, starost { $age } — ZASTARELO (mesh DNS ni dosegljiv; popravi goat-coredns, nato se imena osvežijo samodejno)
status-names-snapshot-daemon = imena po podpisanem posnetku, serija { $serial }, starost { $age } — shramba demona (mesh DNS ni dosegljiv)
status-names-snapshot-daemon-aging = imena po podpisanem posnetku, serija { $serial }, starost { $age } — shramba demona, ZASTARELO (mesh DNS ni dosegljiv; popravi goat-coredns, nato se imena osvežijo samodejno)
status-names-failed = mesh DNS ni dosegljiv in rezerva s posnetkom ni na voljo — { $reason }
status-pqc-on = wireguard PQC (rosenpass) VKLOPLJEN — { $qr }/{ $connected } povezanih vrstnikov odpornih na kvantne napade
status-pqc-off = wireguard PQC (rosenpass) IZKLOPLJEN — klasična wireguard rokovanja
status-pqc-unknown = wireguard PQC neznan — { $reason }
status-device-none = zaščita imen na celotni napravi IZKLOPLJENA — ni shrambe imen goat-clientd (ukazi goat še imajo rezervo; vklopi po name-fallback-resolver.md §9.2)
status-device-broken = shramba imen goat-clientd obstaja (serija { $serial }), a njen posredovalnik NE odgovarja na { $addr } — demon ne teče ali je podsistem imen zatekel
status-device-active = zaščita imen na celotni napravi AKTIVNA — goat-clientd je pred OS mesh DNS na { $addr } (serija shrambe { $serial }, { $grade })
status-device-not-fronted = posredovalnik goat-clientd teče na { $addr } (serija shrambe { $serial }, { $grade }) — OS ni preusmerjen; brskalniki uporabljajo neposredni mesh DNS
status-device-fronting-unknown = posredovalnik goat-clientd teče na { $addr } (serija shrambe { $serial }, { $grade }) — stanja preusmeritve OS na tej platformi ni mogoče zaznati
status-names-observed = imena NEKANONIČNA (ad hoc): opazovan zapis, starost { $age } — ni v podpisanem posnetku (mesh DNS ni dosegljiv; kodificiraj ta zapis)
status-names-selfclaim = imena prek preverjene SAMOTRDITVE, starost { $age } — podpisano z lastnim sidrom imena (mesh DNS ni dosegljiv)
doctor-names-note = prek posnetka serije { $serial }, starost { $age }
doctor-names-selfclaim-note = prek preverjene SAMOTRDITVE, starost { $age } — podpisano z lastnim sidrom imena
doctor-names-selfclaim-hint = mesh DNS ni dosegljiv — ta korak je bil razrešen prek preverjene SAMOTRDITVE (starost { $age }): lastno sidro imena je podpisalo to vezavo, peer_bindings predpomnjenega posnetka pa jo odobrijo. Podpisano, a ne živo; popravi mesh DNS, da to izgine.
doctor-names-observed-note = prek OPAZOVANEGA (ad hoc) zapisa, starost { $age } — nekanonično
doctor-names-observed-hint = mesh DNS ni dosegljiv — ta korak je bil razrešen prek NEKANONIČNEGA opazovanega zapisa (starost { $age }): ime je samo živo, nikoli kodificirano v registru lokacije. Kodificiraj ga (register group_vars → generator → ponovni podpis) ali bo potekel; popravi mesh DNS, da to izgine.
doctor-names-hint = mesh DNS ni dosegljiv — ta korak je bil razrešen prek podpisanega posnetka imen (serija { $serial }, starost { $age }). Fabric ostaja dosegljiv prek rezervnih imen; popravi mesh DNS (goat-coredns na upravljalskem gostitelju), da to izgine.
storm-brake-plan-header = Načrt zavore viharja (ADR 1081, plast 4 — ročno):
storm-brake-plan-target = mgmt gostitelj { $user }@{ $host }  (kontekst { $context })
storm-brake-step-flush = ponovni zagon signalnega vsebnika `{ $container }` (odvrže zombi tokove)
storm-brake-step-tighten = zoži omejevalnik L2 na `{ $container }` (prepolovi omejitev hitrosti, zvišaj TTL neg-cache) — { $window } min, operater ročno razveljavi; preskočeno, če L2 ni nameščen
storm-brake-step-shed = ustavi nebistveno enoto objavitelja `{ $unit }`
storm-brake-shed-none = (ni deklariranih enot za odvrnitev — nastavi storm_brake_shed_units v profile.toml ali podaj --shed-unit)
storm-brake-announce-note = zapiši en OperatorActionV1 v revizijsko sled z izidi po korakih
storm-brake-dryrun-note = poskusni zagon — nič ni bilo izvedeno.
storm-brake-triggers-header = Sproži, ko pas host-net v relay-health kaže:
storm-brake-trigger-futility = razmerje zaman signalizacije >= 0.5 pri >= 100 sporočil/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = izguba uplinka >= 5% neprekinjeno 10 min (wan.loss_pct)
storm-brake-triggers-band-url = pas gostitelja: relay-health.{ $site }.<zone> (glej docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Opomba: samo izpraznitev signalizacije NE obnovi meshа. Plasti 1–2 (odjemalski backoff + odvrnitev signalizacije) so zdravilo; zavora kupi čas za obnovo.
storm-brake-confirm = Sprožim zavoro viharja na { $user }@{ $host }? Znova zažene signalni vsebnik in ustavi deklarirane objavitelje. [y/N]
storm-brake-aborted = zavora viharja prekinjena (brez potrditve)
storm-brake-executing = sprožam zavoro viharja na { $user }@{ $host } ...
storm-brake-outcome-ok = v redu
storm-brake-outcome-failed = neuspelo
storm-brake-outcome-skipped = preskočeno
storm-brake-summary = zavora viharja končana: { $ok } v redu, { $failed } neuspelih, { $skipped } preskočenih
storm-brake-flush-failed = FLUSH ni uspel — signalni vsebnik se ni znova zagnal, zato obnova meshа ni pričakovana: { $reason }
storm-brake-mgmt-unresolved = mgmt gostitelj ni razrešen — podaj --mgmt-host, nastavi mgmt_ssh_host v profile.toml konteksta ali izvozi GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operaterjevo dejanje zapisano v revizijsko sled (akter { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portal je odgovoril HTTP 404 za `{ $path }`: tam ni registrirano
error-portal-rate-limited-summary = portal je odgovoril HTTP 429 za `{ $path }`: preveč zahtev, poskusite pozneje
error-portal-server-error-summary = portal je odgovoril HTTP { $status } za `{ $path }`: napaka na strani portala
error-portal-refused-summary = portal je odgovoril HTTP { $status } za `{ $path }`: zahteva zavrnjena

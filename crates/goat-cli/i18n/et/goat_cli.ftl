### goat-cli — Estonian (et). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = kontekstil `{ $context }` puudub profiil
error-off-mesh-summary = väljaspool ZTOL-i — pole mesh-võrku ühendatud
error-router-unreachable-summary = Zenoh ruuter aadressil `{ $endpoint }` pole kättesaadav
error-cert-expired-summary = mTLS-sertifikaat on aegunud või ruuter lükkas selle tagasi
error-portal-unreachable-summary = portaali API aadressil `{ $endpoint }` pole kättesaadav
error-token-invalid-summary = portaal lükkas Bearer-tokeni tagasi
error-topic-conflict-summary = topic `{ $topic }` on juba olemas
error-bundle-invalid-summary = bundle'i allkirja või aegumise kontroll ebaõnnestus

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = ruteri lõp-punkt on vigaselt vormindatud
doctor-step1-endpoint-malformed-hint = oodati `<proto>/<host>:<port>`; saadi `{ $endpoint }`
doctor-step1-ok-summary = TCP-ühendus ruteri hostiga { $host }:{ $port } õnnestus
doctor-step1-conn-failed-summary = TCP-ühendus ruteri hostiga ebaõnnestus
doctor-step1-conn-failed-hint = { $err } — käivitage netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); kontrollige `netbird status`
doctor-step1-timeout-summary = TCP-ühenduse ajalõpp ruteri hostiga
doctor-step1-timeout-hint = ruteri host { $host }:{ $port } pole kättesaadav { $ms }ms jooksul — tõenäoliselt väljaspool mesh-i
doctor-step2-endpoint-malformed-hint = parandage bundle väljaandja (--goat-cli-router-endpoint peab olema `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = CA juurte PEM puudub või pole loetav
doctor-step2-ca-missing-hint = { $err } — käivitage uuesti `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = ruter { $host }:{ $port } aktsepteerib TCP-d (TLS valideeritud sammus 3)
doctor-step2-refused-summary = ruteri TCP keelati
doctor-step2-refused-hint = { $err } — ruter võib olla maas; kontrollige Zenoh juurutust mgmt-hostis
doctor-step2-timeout-summary = ruteri TCP ajalõpp
doctor-step2-timeout-hint = ruter { $host }:{ $port } pole kättesaadav { $ms }ms jooksul
doctor-step3-config-failed-summary = zenoh konfiguratsiooni koostamine ebaõnnestus
doctor-step3-config-failed-hint = vigane lõpp-punkt `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = zenoh TLS konfiguratsiooni koostamine ebaõnnestus
doctor-step3-tls-config-failed-hint = { $err } — tõenäoliselt zenoh versiooni lahknevus; kontrollige sõltuvuste vastavust
doctor-step3-ok-summary = mTLS seanss avatud — sertifikaadi aktsepteeris ruteri ACL
doctor-step3-open-failed-summary = zenoh seansi avamine ebaõnnestus
doctor-step3-open-failed-hint = { $err } — sertifikaat võib olla aegunud (käivitage `goat profile show`) või ruteri ACL lükkas selle tagasi (URI-SAN mittevastavus)
doctor-step3-timeout-summary = zenoh seansi avamise ajalõpp
doctor-step3-timeout-hint = ruter ei lõpetanud käepigistust { $secs }s jooksul — kella nihe, vale sertifikaat või ülekoormus
doctor-step4-ok-summary = portaal tagastas { $n } organisatsiooni rida
doctor-step4-token-rejected-summary = portaal lükkas Bearer-tokeni tagasi
doctor-step4-token-rejected-hint = HTTP { $status } — token on aegunud või tühistatud; registreeruge uuesti operator-onboard.yml kaudu
doctor-step4-request-failed-summary = portaali päring ebaõnnestus
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = portaali päringu ajalõpp
doctor-step4-timeout-hint = vastust pole { $secs }s jooksul — kontrollige, kas { $endpoint } on mesh-ist kättesaadav
doctor-skipped-summary = vahele jäetud — eelmine samm ebaõnnestus (käivitage uuesti --all sundimiseks)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Ühtne operaatori käsurida goat andmekoe jaoks.
help-long-about = goat — operaatorile suunatud CLI, mis koondab panoscope-core ühte tööriista: profiili init/taaskasutus, ZTOL-teadlik kättesaadavuse doctor, organisatsiooniportaali sirvimine, topic CRUD, pub/sub, stethoscope delegeerimine, interaktiivne käskude koostaja ja aluskihi-kaskaadi jutustus.
help-flag-profile-dir = Alista profiilikataloog (vaikimisi: $GOAT_PROFILE_DIR või ~/.goat).
help-flag-context = Alista aktiivne kontekst (vaikimisi: $GOAT_CONTEXT keskkonnamuutuja, seejärel current_context failis config.toml, seejärel "default").
help-flag-narrate = Näita aluskihi-kaskaadi, mille muutev käsk käivitab.
help-flag-verbose = Luba paljusõnaline silumisväljund (või sea RUST_LOG=goat=debug).
help-flag-color = Värvirežiim terminaliväljundile (auto|always|never).
help-flag-lang = Inimesele suunatud teksti keel (nt en, uk). Tagavaravariandina kasutatakse GOAT_LANG, seejärel OS-i lokaati, seejärel inglise keelt.
help-cmd-profile = Halda kohalikku goat profiili (mTLS sert + portaali token + lõpp-punktide komplekt).
help-cmd-context = Halda kontekste (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Käivita astmeline kättesaadavuse prober andmekoe vastu.
help-cmd-doctor-long = Astmeline kättesaadavuse prober andmekoe vastu: ZTOL liikmesus → Zenoh ruter → serdi aktsepteerimine → portaal. Iga ebaõnnestunud samm väljastab konkreetse paranduse vihje.
help-cmd-vendor = Sirvi portaalis registreeritud organisatsioone.
help-cmd-topic = Loetle, näita, loo või kustuta topic'uid.
help-cmd-pub = Avalda kasulik koormus topic'u võtmesse.
help-cmd-sub = Telli topic'u võtmeavaldis.
help-cmd-inspect = Delegeeri diagnostiline verdikti päring stethoscope'ile.
help-cmd-ui = Interaktiivne liidetud valija: sirvi deklareeritud ⊕ live roosterit ja hüppa valitud sihtmärgil Diagnose / Tail läätsele.
help-cmd-compose = Interaktiivne käskude koostaja TUI.
help-cmd-narrate = Aluskihi-kaskaadi peegeldus hiljutise tegevuse üle. Pole veel saadaval (igapäevane viis on --narrate muutval käsul).
help-cmd-completion = Trüki shelli täiendusskript stdout'i.
help-cmd-explain = Otsi veakoodi tähendus + parandus manustatud `goat-cli-error-codes.md` dokumendist (cargo --explain muster).
help-cmd-mock = Avalda topic'usse sünteetilisi näidiseid. Vaikimisi: triviaalne hello-world JSON sagedusega 1Hz. Operaatori esitatud protobufi kodeerimiseks anna `--format <.fds>` + `--message <Name>`.
help-cmd-status = Tervise süntees: ZTOL kättesaadavus + portaali /api/health + kohaliku serdi aegumine, kolm glyph rida või üks --json objekt.
help-cmd-services = Kontrolli iga majutatud teenust failis `ops/serve/services.yaml` ja renderda üks glyph rida teenuse kohta (või üks --json objekt).
help-cmd-client = Mähi kohalik goat-clientd daemon selle JSON-RPC IPC sokli kaudu (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH-põhine laienduste avastamine (kubectl-stiilis). Iga PATH-il olev `goat-<name>` käivitatav on käivitatav kui `goat <name>`; `goat plugin list` loetleb installitud pluginad, `goat plugin which <name>` lahendab tee.

# ---------------------------------------------------------------------
# goat status / inspect / --narrate käitusaja tekst (machine-translated; ADR 1024).
# Contract tokens `{ $var }`, tagurpidi-ülakomad, → —, ja fikseeritud
# literaalid (/api/health, 2xx, PEM, CERTIFICATE, unix=, TCP, x509, Zenoh,
# stethoscope, (portal-confirmed)) korratud sõna-sõnalt.
# ---------------------------------------------------------------------
status-ztol-malformed = ruteri lõpp-punkt on vigaselt vormindatud: `{ $endpoint }`
status-ztol-refused = TCP keelati ruterile { $host }:{ $port }: { $err }
status-ztol-unreachable = väljaspool mesh-i (ruter { $host }:{ $port } pole kättesaadav { $ms }ms jooksul)
status-portal-ok = portaali /api/health 2xx
status-portal-failed = portaali /api/health ebaõnnestus: { $err }
status-portal-timeout = portaali /api/health ajalõpp { $secs }s jooksul
status-cert-read-failed = serdi lugemine ebaõnnestus: { $err }
status-cert-pem-parse-failed = serdi PEM parsimine ebaõnnestus: { $err }
status-cert-wrong-tag = esimene PEM plokk on { $tag }, vaja CERTIFICATE
status-cert-x509-parse-failed = x509 parsimine ebaõnnestus: { $err }
status-cert-expired = sert on AEGUNUD ajal unix={ $not_after }, nüüd={ $now }
status-cert-valid = sert on kehtiv; aegub { $days_left }d pärast (unix={ $not_after })
inspect-finding-label = leid ({ $severity }): { $label } — { $detail }
inspect-remedy-label = parandus: { $title } — { $detail }
inspect-no-outcomes = (tulemusi pole — sihtmärk ei vastanud ühelegi kontrollile)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put aktsepteeritud võtmes `{ $key }` ({ $bytes } baiti)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskaad rahunes — { $summary } ({ $n } { $n ->
        [one] tagajärg
       *[other] tagajärge
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Esmakäivituse viisard: vali sandbox või päris, tõesta et töötab, suuna juhendaja juurde.
help-cmd-tutor = Isetempoline, praktiline andmekoe ringkäik (vimtutor-stiilis; jätkatav).
help-cmd-docs = Sirvi goat dokumente või väljasta täielik käskude viide (--markdown).
help-cmd-hello = Saada hello läbi andmekoe otsast otsani (sandbox või päris keskkond).
flourish-all-green = 🐐 kõik roheline — andmekude on töökorras

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = ainult operaatori toiming — teil pole volitust (`goat admin` on mitteadminidele mittefunktsionaalne)
help-flag-narrate-json = Väljastab substraadi kaskaadi NDJSON-ina (üks objekt tagajärje kohta) intsidendilogi ja skriptimise jaoks. Tähendab --narrate.
help-cmd-admin = Ainult operaatorile mõeldud administreerimine: organisatsiooni elutsükkel, lisamine/tühistamine/rotatsioon, mesh-peer katkestus, seadme + operaatoritaseme ettevalmistamine, LAN-i triaaž. Autoriseeritud teie operaatoriandmete alusel (serveripoolne portaali/NetBird-i tokeni ulatuse kaudu); mitteadminidele mittefunktsionaalne — volitamata kõne ei muuda midagi ja ebaõnnestub veateatega `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = konteksti `{ $context }` profiil on olemas, kuid see pole selle kasutaja jaoks loetav (juurdepääs keelatud)
error-portal-bad-response-summary = portaal vastas, kuid see goat'i järk ei suutnud vastust parsida
doctor-step0-ok-summary = kohalik seadistus on võrgutestideks valmis
doctor-step0-warn-summary = kohalikul seadistusel on kontrolle, mida ei õnnestunud kinnitada
doctor-step0-fail-summary = kohalik seadistus vajab tähelepanu
doctor-step0-profile-absent-summary = konteksti `{ $context }` goat'i profiili pole
doctor-step0-profile-new-hint = kontrolli `goat context ls`; kui see on sinu esmane seadistus, käivita `goat ca add <bundle-ca.pem>` ja võrdle selle sõrmejälge oma operaatori omaga, seejärel `goat profile init <bundle.cbor>`; käivita `goat doctor` uuesti
doctor-step0-profile-absent-hint = kontrolli `goat context ls`; vali olemasolev kontekst käsuga `goat context use <name>` või käivita selle konteksti jaoks `goat profile init <bundle.cbor>`; käivita `goat doctor` uuesti
doctor-step0-profile-denied-summary = goat'i profiil asukohas { $path } pole loetav
doctor-step0-profile-denied-hint = paranda profiili omanik või käivita omanikust kasutajana; ära lähtesta olemasolevat profiili uuesti
doctor-step0-profile-incomplete-summary = goat'i profiili fail { $file } puudub või pole loetav
doctor-step0-profile-incomplete-hint = taasta nimetatud fail väljastatud komplektist või asenda see kontekst käsuga `goat profile rotate <bundle.cbor>`; käivita `goat doctor` uuesti
doctor-step0-profile-invalid-summary = goat'i profiili lõpp-punktid on loetamatud või puuduvad
doctor-step0-profile-empty-token-summary = goat'i profiili portaali tunnus on tühi
doctor-step0-profile-ok-summary = goat'i kontekstil `{ $context }` on loetav profiilimaterjal
doctor-step0-client-absent-summary = ükski toetatud mesh-klient ei vastanud
doctor-step0-client-absent-hint = paigalda oma goatnet'i pakutav mesh-klient, käivita selle deemon ja seejärel käivita `goat doctor` uuesti
doctor-step0-client-stopped-summary = paigaldatud mesh-klient ei andnud kasutatavat olekut
doctor-step0-client-stopped-hint = kontrolli `netbird status -d` või `goat client status --detail`; käivita paigaldatud deemon (NetBirdi puhul `sudo netbird service start`) ja seejärel käivita `goat doctor` uuesti
doctor-step0-netbird-ok-summary = NetBirdi deemon vastas (raporteeritud profiil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd vastas (olek `{ $state }`)
doctor-step0-management-connected-summary = NetBird teatab, et haldus on ühendatud
doctor-step0-management-disconnected-summary = NetBird teatab, et haldus on lahti ühendatud
doctor-step0-management-disconnected-hint = kontrolli raporteeritud halduse URL-i ja `netbird status -d`; olemasolev tunnel võib halduskatkestuse ajal siiski töötada
doctor-step0-management-unknown-summary = seadistatud halduse lõpp-punkti ei õnnestunud kohapeal tuvastada
doctor-step0-management-unknown-hint = vaata `netbird status -d` või `goat client diagnostics`; doctor ei arva halduse URL-i ära
doctor-step0-tunnel-down-summary = NetBird teatab, et mesh-aadress puudub
doctor-step0-tunnel-down-hint = käivita `netbird status -d`; kui seade on registreeritud ja deemon vastab, käivita `goat heal`, seejärel käivita `goat doctor` uuesti
doctor-step0-tunnel-address-summary = NetBird teatab mesh-aadressi { $address }; järgmisena kontrollitakse ruuteri kättesaadavust
doctor-step0-goat-client-no-bundle-summary = goat-clientd-l pole imporditud bundle'it
doctor-step0-goat-client-no-bundle-hint = käivita `goat client bundle import <bundle.cbor>` kliendi registreerimisbundle'iga, ühenda klient, seejärel käivita `goat doctor` uuesti
doctor-step0-goat-client-connected-summary = goat-clientd teatab ühendatud tunnelist; järgmisena kontrollitakse ruuteri kättesaadavust
doctor-step0-goat-client-outer-disabled-summary = goat-clientd välistunnel on netbird-only režiimis keelatud
doctor-step0-goat-client-down-summary = goat-clientd teatab tunneli olekuks `{ $state }`
doctor-step0-goat-client-down-hint = käivita `goat client diagnostics`, seejärel `goat client up`, kui bundle on laaditud; käivita `goat doctor` uuesti
doctor-step0-goat-client-inner-ok-summary = goat-clientd teatab, et sisemine mesh on ühendatud
doctor-step0-goat-client-inner-disabled-summary = goat-clientd sisemine mesh on wg-cp0-only režiimis keelatud
doctor-step0-goat-client-inner-down-summary = goat-clientd teatab sisemise mesh'i olekuks `{ $state }`
doctor-step0-goat-client-inner-down-hint = käivita `goat client diagnostics`, et sisemist mesh'i kontrollida; pärast taastamist käivita `goat doctor` uuesti
doctor-step0-management-reachable-summary = halduse lõpp-punkt vastas HTTP { $status }
doctor-step0-management-auth-summary = halduse HTTPS-lõpp-punkt on kättesaadav (HTTP { $status } nõuab selle päringu jaoks autoriseerimist)
doctor-step0-management-unexpected-summary = halduse lõpp-punkt tagastas ootamatu HTTP { $status }
doctor-step0-management-unexpected-hint = kontrolli seadistatud halduse URL-i ja palu goatneti operaatoril see vastus üle vaadata
doctor-step0-management-unreachable-summary = seadistatud halduse lõpp-punkti ei õnnestunud kätte saada: { $error }
doctor-step0-management-unreachable-hint = kontrolli DNS-i ja võrgujuurdepääsu kliendi teatatud URL-ile; olemasolev tunnel võib siiski töötada
doctor-step0-clock-ok-summary = kohalik kell on halduse HTTP-ajast { $seconds } sekundi piires
doctor-step0-clock-skew-summary = kohalik kell erineb halduse HTTP-ajast { $seconds } sekundi võrra
doctor-step0-clock-skew-hint = sünkrooni seadme kell, seejärel käivita `goat doctor` uuesti; vale kella korral võib sertifikaadi valideerimine ebaõnnestuda
doctor-step0-clock-unknown-summary = kellanihet ei õnnestunud halduse HTTP Date-päisest mõõta
doctor-step0-clock-unknown-hint = kontrolli seadme ajasünkroonimist, kui TLS- või sertifikaadikontrollid ebaõnnestuvad
error-clock-skew-summary = seadme kell erineb haldusajast liiga palju
error-doctor-inconclusive-summary = doctor tuvastas tõrke, mille põhjust ei õnnestunud tõendada
error-portal-forbidden-summary = portaal keelas sellel kontol juurdepääsu kontrollitud API-le
doctor-step4-forbidden-summary = portaal tagastas selle profiili jaoks HTTP 403
doctor-step4-forbidden-hint = see volitus jõudis portaalini, kuid tal puudub juurdepääs kontrollitud API-le; vaata `goat profile show` ja palu goatneti operaatoril õigus üle kontrollida
doctor-step4-decode-summary = portaal vastas, aga see goat ei oska vastust lugeda
doctor-step4-decode-hint = { $err } — uuenda goat selle goatneti pakutavale versioonile ja käivita `goat doctor` uuesti
doctor-step1-control-only-summary = ühendatud ainult juhtserveriga — tunnelit ühegi { $total } partneriga pole
doctor-step1-control-only-hint = see võrk laseb TCP 443 välja, kuid blokeerib mesh-andmepordid, mistõttu pole sellelt seadmelt midagi mesh'is (sh sisselogimislehti) kättesaadav. Palu võrgumeeskonnal lubada väljuv TCP ja UDP aadressidele { $relays } ning UDP 3478 aadressidele { $stun }; soovitatav on lubada ka otsene UDP seadmete vahel. Kinnitamiseks ühenda telefoni kuumkohaga ja käivita `netbird status`: partnerid peaksid siis ühenduma.
doctor-offmesh-dashboard-hint = releede park (väline vaatepunkt): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = kohalik sertifikaat, sobiv võti ja CA komplekt on loetavad; sertifikaat on kehtivusajas
doctor-step2-material-failed-summary = kohalikku TLS-materjali { $file } ei saanud kasutada
doctor-step2-material-failed-hint = { $err } — kontrolli nimetatud faili; taasta see väljastatud komplektist või roteeri profiil
doctor-step2-material-invalid-hint = { $file } ei ole kehtiv X.509 PEM-sertifikaat; taasta väljastatud materjal või käivita `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem on vigane või ei sobi kokku failiga mtls.cert.pem; taasta väljastatud materjal või käivita `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = kohalik sertifikaat on väljaspool kehtivusaega
doctor-step2-cert-time-hint = käivita `goat profile show`; vaata ülaltoodud kella tulemust, seejärel küsi operaatorilt uus komplekt ja käivita `goat profile rotate <new-bundle.cbor>`, kui sertifikaat on aegunud
doctor-skipped-no-endpoint-summary = vahele jäetud — profile.toml-ist puudub { $field }
doctor-skipped-no-profile-summary = vahele jäetud — selle konteksti jaoks pole loetavaid profiili lõpp-punkte
doctor-skipped-no-portal-token-summary = vahele jäetud — selle konteksti jaoks pole loetavat portaali tokenit
status-ztol-tcp-ok = ruuteri port { $host }:{ $port } võtab TCP vastu (ainult port — vaata sessiooni)
status-session-ok = zenoh mTLS-sessioon avati { $ms }ms-ga (ruuter võttis selle sertifikaadi vastu)
status-session-skipped = sessiooni ei proovitud (mesh'ist väljas; vaata ztol)
status-session-material-missing = zenoh-sessiooni ei proovitud: { $detail }
status-session-config-failed = zenoh-sessiooni seadistus ebaõnnestus: { $err }
status-session-failed = zenoh'i sessioon ei avanenud: { $err } — käivita `goat doctor`
status-session-timeout = zenoh'i sessioon ei avanenud { $secs } s jooksul — ruuteri port võtab TCP vastu, kuid ükski sessioon ei lõpe (ruuter hangunud, sert lükati tagasi või kellanihe); käivita `goat doctor`
status-names-live = nimed live (mesh DNS vastas)
status-names-ip-only = nimesid ei katsetatud (profiili lõpp-punktid on IP-literaalid)
status-names-snapshot = nimed allkirjastatud hetktõmmise seisuga, seeria { $serial }, vanus { $age } (mesh DNS ei ole kättesaadav)
status-names-snapshot-aging = nimed allkirjastatud hetktõmmise seisuga, seeria { $serial }, vanus { $age } — AEGUNUD (mesh DNS ei ole kättesaadav; paranda goat-coredns, siis nimed värskenduvad automaatselt)
status-names-snapshot-daemon = nimed allkirjastatud hetktõmmise seisuga, seeria { $serial }, vanus { $age } — deemoni hoidla (mesh DNS ei ole kättesaadav)
status-names-snapshot-daemon-aging = nimed allkirjastatud hetktõmmise seisuga, seeria { $serial }, vanus { $age } — deemoni hoidla, AEGUNUD (mesh DNS ei ole kättesaadav; paranda goat-coredns, siis nimed värskenduvad automaatselt)
status-names-failed = mesh DNS ei ole kättesaadav ja hetktõmmise varuvariant puudub — { $reason }
status-pqc-on = wireguard PQC (rosenpass) SEES — { $qr }/{ $connected } ühendatud partnerit on kvantkindlad
status-pqc-off = wireguard PQC (rosenpass) VÄLJAS — klassikalised wireguard'i käepigistused
status-pqc-unknown = wireguard PQC teadmata — { $reason }
status-device-none = seadmepõhine nimekaitse VÄLJAS — goat-clientd nimehoidlat pole (goat'i käskudel on varuvariant siiski olemas; luba vastavalt name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd nimehoidla olemas (seeria { $serial }), kuid selle edastaja EI vasta aadressil { $addr } — deemon maas või nimede alamsüsteem hangunud
status-device-active = seadmepõhine nimekaitse AKTIIVNE — goat-clientd on OS-i mesh DNS-i ees aadressil { $addr } (hoidla seeria { $serial }, { $grade })
status-device-not-fronted = goat-clientd edastaja töötab aadressil { $addr } (hoidla seeria { $serial }, { $grade }) — OS ei ole selle taga; brauserid kasutavad otse mesh DNS-i
status-device-fronting-unknown = goat-clientd edastaja töötab aadressil { $addr } (hoidla seeria { $serial }, { $grade }) — OS-i eelühenduse olekut ei saa sellel platvormil tuvastada
status-names-observed = nimed MITTEKANOONILISED (ad hoc): vaadeldud kirje, vanus { $age } — pole allkirjastatud hetktõmmises (mesh DNS ei ole kättesaadav; kodeeri see kirje)
status-names-selfclaim = nimed kontrollitud ISEENDA-NÕUDE kaudu, vanus { $age } — allkirjastanud nime enda ankur (mesh DNS ei ole kättesaadav)
doctor-names-note = hetktõmmise kaudu, seeria { $serial }, vanus { $age }
doctor-names-selfclaim-note = kontrollitud ISEENDA-NÕUDE kaudu, vanus { $age } — allkirjastanud nime enda ankur
doctor-names-selfclaim-hint = mesh DNS ei ole kättesaadav — see samm lahendati kontrollitud ISEENDA-NÕUDE kaudu (vanus { $age }): nime enda ankur allkirjastas selle sidumise ja vahemällu salvestatud hetktõmmise peer_bindings lubab seda. Allkirjastatud, kuid mitte live; paranda mesh DNS, et see eemaldada.
doctor-names-observed-note = VAADELDUD (ad hoc) kirje kaudu, vanus { $age } — mittekanooniline
doctor-names-observed-hint = mesh DNS ei ole kättesaadav — see samm lahendati MITTEKANOONILISE vaadeldud kirje kaudu (vanus { $age }): nimi on ainult live, seda pole kunagi saidi registris kodeeritud. Kodeeri see (group_vars registry → generator → uuesti allkirjasta), muidu see aegub; paranda mesh DNS, et see eemaldada.
doctor-names-hint = mesh DNS ei ole kättesaadav — see samm lahendati allkirjastatud nimehetktõmmise kaudu (seeria { $serial }, vanus { $age }). Fabric jääb varunimedega kättesaadavaks; paranda mesh DNS (goat-coredns mgmt-hostis), et see eemaldada.
storm-brake-plan-header = Tormipiduri plaan (ADR 1081 kiht 4 — käsitsi):
storm-brake-plan-target = haldushost { $user }@{ $host }  (kontekst { $context })
storm-brake-step-flush = taaskäivita signaalikonteiner `{ $container }` (katkestab zombivood)
storm-brake-step-tighten = kitsenda L2 piirajat konteineril `{ $container }` (poolita kiiruspiirang, tõsta neg-cache TTL) — { $window } min, operaator tühistab; vahele jäetud, kui L2 pole paigaldatud
storm-brake-step-shed = peata mittekriitiline publitseerija üksus `{ $unit }`
storm-brake-shed-none = (peatatavaid üksusi pole deklareeritud — määra storm_brake_shed_units failis profile.toml või anna --shed-unit)
storm-brake-announce-note = saada auditijäljele üks OperatorActionV1 koos sammude tulemustega
storm-brake-dryrun-note = kuivkäivitus — midagi ei täidetud.
storm-brake-triggers-header = Käivita, kui relay-health host-net riba näitab:
storm-brake-trigger-futility = signaalimise asjatuse suhe >= 0.5 sagedusel >= 100 sõnumit/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = uplink'i kadu >= 5% püsivalt 10 min (wan.loss_pct)
storm-brake-triggers-band-url = hosti riba: relay-health.{ $site }.<zone> (vt docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Märkus: ainult signaali tühjendus EI taasta võrku. Ravi on kihid 1–2 (kliendi backoff + signaali piiramine); pidur ostab taastumisaega.
storm-brake-confirm = Käivitada tormipidur hostil { $user }@{ $host }? Taaskäivitab signaalikonteineri ja peatab deklareeritud publitseerijad. [y/N]
storm-brake-aborted = tormipidur katkestatud (kinnitust pole)
storm-brake-executing = käivitan tormipiduri hostil { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = ebaõnnestus
storm-brake-outcome-skipped = vahele jäetud
storm-brake-summary = tormipidur lõpetas: { $ok } ok, { $failed } ebaõnnestus, { $skipped } vahele jäetud
storm-brake-flush-failed = FLUSH ebaõnnestus — signaalikonteiner ei taaskäivitunud, seega võrgu taastumist ei oodata: { $reason }
storm-brake-mgmt-unresolved = haldushostit ei leitud — anna --mgmt-host, määra mgmt_ssh_host konteksti failis profile.toml või ekspordi GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operaatori tegevus salvestatud auditijäljele (tegija { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portaal vastas HTTP 404 päringule `{ $path }`: seal pole registreeritud
error-portal-rate-limited-summary = portaal vastas HTTP 429 päringule `{ $path }`: liiga palju päringuid, proovige hiljem uuesti
error-portal-server-error-summary = portaal vastas HTTP { $status } päringule `{ $path }`: viga portaali poolel
error-portal-refused-summary = portaal vastas HTTP { $status } päringule `{ $path }`: päring lükati tagasi

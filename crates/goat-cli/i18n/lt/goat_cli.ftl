### goat-cli — Lithuanian (lt). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = nėra profilio kontekstui `{ $context }`
error-off-mesh-summary = už ZTOL ribų — neprisijungta prie mesh tinklo
error-router-unreachable-summary = Zenoh maršrutizatorius adresu `{ $endpoint }` nepasiekiamas
error-cert-expired-summary = mTLS sertifikatas baigė galioti arba jį atmetė maršrutizatorius
error-portal-unreachable-summary = portalo API adresu `{ $endpoint }` nepasiekiamas
error-token-invalid-summary = portalas atmetė Bearer prieigos raktą
error-topic-conflict-summary = topic `{ $topic }` jau egzistuoja
error-bundle-invalid-summary = nepavyko patikrinti bundle parašo ar galiojimo

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = netinkamai suformuotas maršrutizatoriaus galinis taškas
doctor-step1-endpoint-malformed-hint = tikėtasi `<proto>/<host>:<port>`; gauta `{ $endpoint }`
doctor-step1-ok-summary = TCP ryšys su maršrutizatoriaus prieglobą { $host }:{ $port } sėkmingas
doctor-step1-conn-failed-summary = TCP ryšys su maršrutizatoriaus prieglobą nepavyko
doctor-step1-conn-failed-hint = { $err } — paleiskite netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); patikrinkite `netbird status`
doctor-step1-timeout-summary = baigėsi TCP ryšio su maršrutizatoriaus prieglobą skirtasis laikas
doctor-step1-timeout-hint = maršrutizatoriaus prieglobs { $host }:{ $port } nepasiekiamas per { $ms }ms — tikriausiai už mesh ribų
doctor-step2-endpoint-malformed-hint = pataisykite bundle išdavėją (--goat-cli-router-endpoint turi būti `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = CA šaknų PEM trūksta arba neįskaitomas
doctor-step2-ca-missing-hint = { $err } — paleiskite iš naujo `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = maršrutizatorius { $host }:{ $port } priima TCP (TLS patvirtintas 3 žingsnyje)
doctor-step2-refused-summary = maršrutizatoriaus TCP atmestas
doctor-step2-refused-hint = { $err } — maršrutizatorius gali būti išjungtas; patikrinkite Zenoh diegimą mgmt-host
doctor-step2-timeout-summary = baigėsi maršrutizatoriaus TCP skirtasis laikas
doctor-step2-timeout-hint = maršrutizatorius { $host }:{ $port } nepasiekiamas per { $ms }ms
doctor-step3-config-failed-summary = nepavyko sukurti zenoh konfigūracijos
doctor-step3-config-failed-hint = netinkamas galinis taškas `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = nepavyko sukurti zenoh TLS konfigūracijos
doctor-step3-tls-config-failed-hint = { $err } — tikriausiai zenoh versijų neatitikimas; patikrinkite priklausomybių atitiktį
doctor-step3-ok-summary = mTLS sesija atidaryta — sertifikatą priėmė maršrutizatoriaus ACL
doctor-step3-open-failed-summary = nepavyko atidaryti zenoh sesijos
doctor-step3-open-failed-hint = { $err } — sertifikatas gali būti pasibaigęs (paleiskite `goat profile show`) arba atmestas maršrutizatoriaus ACL (URI-SAN neatitikimas)
doctor-step3-timeout-summary = baigėsi zenoh sesijos atidarymo skirtasis laikas
doctor-step3-timeout-hint = maršrutizatorius nebaigė rankų paspaudimo per { $secs }s — laikrodžio poslinkis, neteisingas sertifikatas arba perkrova
doctor-step4-ok-summary = portalas grąžino { $n } organizacijų eilučių
doctor-step4-token-rejected-summary = portalas atmetė Bearer prieigos raktą
doctor-step4-token-rejected-hint = HTTP { $status } — prieigos raktas pasibaigė arba atšauktas; iš naujo prisijunkite per operator-onboard.yml
doctor-step4-request-failed-summary = užklausa į portalą nepavyko
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = baigėsi užklausos į portalą skirtasis laikas
doctor-step4-timeout-hint = nėra atsakymo per { $secs }s — patikrinkite, ar { $endpoint } pasiekiamas iš mesh
doctor-skipped-summary = praleista — ankstesnis žingsnis nepavyko (paleiskite iš naujo su --all, kad priverstumėte)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Vieninga operatoriaus komandinė eilutė goat duomenų audiniui.
help-long-about = goat — operatoriui skirtas CLI, sujungiantis panoscope-core į vieną įrankį: profilio init/pakartotinis naudojimas, ZTOL atpažįstantis pasiekiamumo doctor, organizacijų portalo naršymas, topic CRUD, pub/sub, stethoscope delegavimas, interaktyvus komandų kūrėjas ir pagrindo kaskados pasakojimas.
help-flag-profile-dir = Nepaisyti profilio katalogo (numatytasis: $GOAT_PROFILE_DIR arba ~/.goat).
help-flag-context = Nepaisyti aktyvaus konteksto (numatytasis: $GOAT_CONTEXT aplinkos kintamasis, tada current_context faile config.toml, tada "default").
help-flag-narrate = Rodyti pagrindo kaskadą, kurią paleidžia keičianti komanda.
help-flag-verbose = Įjungti išsamią derinimo išvestį (arba nustatyti RUST_LOG=goat=debug).
help-flag-color = Spalvų režimas terminalo išvesčiai (auto|always|never).
help-flag-lang = Žmogui skirtos prozos kalba (pvz. en, uk). Atsarginis variantas GOAT_LANG, tada OS lokalė, tada anglų kalba.
help-cmd-profile = Tvarkyti vietinį goat profilį (mTLS sertifikatas + portalo prieigos raktas + galinių taškų rinkinys).
help-cmd-context = Tvarkyti kontekstus (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Paleisti pakopinį pasiekiamumo prober prieš audinį.
help-cmd-doctor-long = Pakopinis pasiekiamumo prober prieš audinį: ZTOL narystė → Zenoh maršrutizatorius → sertifikato priėmimas → portalas. Kiekvienas nepavykęs žingsnis pateikia konkrečią taisymo užuominą.
help-cmd-vendor = Naršyti portale registruotas organizacijas.
help-cmd-topic = Išvardyti, parodyti, sukurti arba ištrinti topic.
help-cmd-pub = Paskelbti naudingąją apkrovą į topic raktą.
help-cmd-sub = Užsiprenumeruoti topic rakto išraišką.
help-cmd-inspect = Deleguoti diagnostinį verdikto prašymą stethoscope.
help-cmd-ui = Interaktyvus sujungtas parinkiklis: naršyti deklaruotą ⊕ live sąrašą ir peršokti į Diagnose / Tail lęšį ant pasirinkto taikinio.
help-cmd-compose = Interaktyvus komandų kūrimo TUI.
help-cmd-narrate = Pagrindo kaskados atspindys per neseną veiklą. Dar neprieinama (kasdienis būdas yra --narrate ant keičiančios komandos).
help-cmd-completion = Spausdinti apvalkalo užbaigimo skriptą į stdout.
help-cmd-explain = Ieškoti klaidos kodo reikšmės + taisymo įterptame `goat-cli-error-codes.md` dokumente (cargo --explain šablonas).
help-cmd-mock = Paskelbti sintetinius pavyzdžius į topic. Numatytasis: trivialus hello-world JSON 1Hz dažniu. Vietoje to perduokite `--format <.fds>` + `--message <Name>`, kad užkoduotumėte operatoriaus pateiktą protobuf.
help-cmd-status = Būklės sintezė: ZTOL pasiekiamumas + portalo /api/health + vietinio sertifikato galiojimo pabaiga, trys glyph eilutės arba vienas --json objektas.
help-cmd-services = Patikrinti kiekvieną talpinamą paslaugą faile `ops/serve/services.yaml` ir atvaizduoti vieną glyph eilutę kiekvienai paslaugai (arba vieną --json objektą).
help-cmd-client = Apgaubti vietinį goat-clientd daemon per jo JSON-RPC IPC lizdą (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH pagrįstas plėtinių aptikimas (kubectl stiliaus). Bet koks PATH esantis `goat-<name>` vykdomasis failas iškviečiamas kaip `goat <name>`; `goat plugin list` išvardija įdiegtus papildinius, `goat plugin which <name>` nustato kelią.

# ---------------------------------------------------------------------
# goat status / inspect / --narrate runtime proza (machine-translated; ADR 1024).
# Contract tokens `{ $var }`, backtick'ai, → —, ir fiksuoti literalai
# (/api/health, 2xx, PEM, CERTIFICATE, unix=, TCP, x509, Zenoh,
# stethoscope, (portal-confirmed)) atkartoti pažodžiui.
# ---------------------------------------------------------------------
status-ztol-malformed = netinkamai suformuotas maršrutizatoriaus galinis taškas: `{ $endpoint }`
status-ztol-refused = TCP atmestas maršrutizatoriui { $host }:{ $port }: { $err }
status-ztol-unreachable = už mesh ribų (maršrutizatorius { $host }:{ $port } nepasiekiamas per { $ms }ms)
status-portal-ok = portalo /api/health 2xx
status-portal-failed = portalo /api/health nepavyko: { $err }
status-portal-timeout = baigėsi portalo /api/health skirtasis laikas per { $secs }s
status-cert-read-failed = sertifikato skaitymas nepavyko: { $err }
status-cert-pem-parse-failed = sertifikato PEM analizė nepavyko: { $err }
status-cert-wrong-tag = pirmas PEM blokas yra { $tag }, reikia CERTIFICATE
status-cert-x509-parse-failed = x509 analizė nepavyko: { $err }
status-cert-expired = sertifikatas BAIGĖ GALIOTI ties unix={ $not_after }, dabar={ $now }
status-cert-valid = sertifikatas galioja; baigia galioti po { $days_left }d (unix={ $not_after })
inspect-finding-label = radinys ({ $severity }): { $label } — { $detail }
inspect-remedy-label = taisymas: { $title } — { $detail }
inspect-no-outcomes = (nėra rezultatų — taikinys neatitiko jokių patikrų)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put priimtas rakte `{ $key }` ({ $bytes } baitų)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskada nusistovėjo — { $summary } ({ $n } { $n ->
        [one] pasekmė
        [few] pasekmės
       *[other] pasekmių
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Pirmojo paleidimo vediklis: pasirinkite sandbox arba tikrą, įrodykite, kad veikia, nukreipkite į mokytoją.
help-cmd-tutor = Savarankiškas, praktinis audinio turas (vimtutor stiliaus; tęsiamas).
help-cmd-docs = Naršykite goat dokumentus arba pateikite visą komandų žinyną (--markdown).
help-cmd-hello = Persiųskite hello per audinį nuo galo iki galo (sandbox arba tikra aplinka).
flourish-all-green = 🐐 viskas žalia — audinys veikia normaliai

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = tik operatoriaus veiksmas — jūs nesate įgaliotas (`goat admin` neveikia ne administratoriams)
help-flag-narrate-json = Išvesti substrato kaskadinę seką kaip NDJSON (vienas objektas vienam pasekmei) incidentų žurnalams ir scenarijų rašymui. Įjungia --narrate.
help-cmd-admin = Tik operatoriaus administravimas: organizacijų gyvavimo ciklas, prijungimas/atšaukimas/rotacija, tinklelio mazgo atjungimas, įrenginių ir operatoriaus lygio parengimas, LAN diagnostika. Įgaliojamas operatoriaus kredencialais (serverio pusėje per portal/NetBird prieigos sritį); neveikia ne administratoriams — neleistinas iškvietimas nieko nekeičia ir baigiasi klaida `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = konteksto `{ $context }` profilis egzistuoja, bet šis naudotojas negali jo skaityti (prieiga uždrausta)
error-portal-bad-response-summary = portalas atsakė, bet ši goat versija nesugebėjo apdoroti atsakymo
doctor-step0-ok-summary = vietinė sąranka paruošta tinklo patikroms
doctor-step0-warn-summary = vietinėje sąrankoje yra patikrų, kurių nepavyko patvirtinti
doctor-step0-fail-summary = vietinei sąrankai reikia dėmesio
doctor-step0-profile-absent-summary = nėra goat profilio kontekstui `{ $context }`
doctor-step0-profile-new-hint = patikrinkite `goat context ls`; jei tai pirmoji sąranka, paleiskite `goat ca add <bundle-ca.pem>` ir palyginkite jo kontrolinį kodą su operatoriaus pateiktu, tada `goat profile init <bundle.cbor>`; paleiskite `goat doctor` iš naujo
doctor-step0-profile-absent-hint = patikrinkite `goat context ls`; pasirinkite esamą kontekstą naudodami `goat context use <name>` arba paleiskite `goat profile init <bundle.cbor>` šiam kontekstui; paleiskite `goat doctor` iš naujo
doctor-step0-profile-denied-summary = goat profilis { $path } neskaitomas
doctor-step0-profile-denied-hint = pataisykite profilio savininką arba paleiskite kaip savininkas; neinicializuokite esamo profilio iš naujo
doctor-step0-profile-incomplete-summary = goat profilio failo { $file } trūksta arba jo nepavyksta perskaityti
doctor-step0-profile-incomplete-hint = atkurkite nurodytą failą iš išduoto rinkinio arba naudokite `goat profile rotate <bundle.cbor>` šiam kontekstui pakeisti; paleiskite `goat doctor` iš naujo
doctor-step0-profile-invalid-summary = goat profilio galiniai taškai neskaitomi arba jų trūksta
doctor-step0-profile-empty-token-summary = goat profilio portalo žetonas tuščias
doctor-step0-profile-ok-summary = goat kontekstas `{ $context }` turi skaitomus profilio duomenis
doctor-step0-client-absent-summary = neatsakė nė vienas palaikomas mesh klientas
doctor-step0-client-absent-hint = įdiekite jūsų goatnet pateiktą mesh klientą, paleiskite jo tarnybą, tada paleiskite `goat doctor` iš naujo
doctor-step0-client-stopped-summary = įdiegtas mesh klientas negrąžino tinkamos būsenos
doctor-step0-client-stopped-hint = patikrinkite `netbird status -d` arba `goat client status --detail`; paleiskite įdiegtą tarnybą (`sudo netbird service start` NetBird atveju), tada paleiskite `goat doctor` iš naujo
doctor-step0-netbird-ok-summary = NetBird tarnyba atsakė (nurodytas profilis `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd atsakė (būsena `{ $state }`)
doctor-step0-management-connected-summary = NetBird praneša, kad valdymas prijungtas
doctor-step0-management-disconnected-summary = NetBird praneša, kad valdymas atjungtas
doctor-step0-management-disconnected-hint = patikrinkite nurodytą valdymo URL ir `netbird status -d`; jau sukurtas tunelis valdymo sutrikimo metu gali veikti
doctor-step0-management-unknown-summary = sukonfigūruoto valdymo galinio taško nepavyko nustatyti vietoje
doctor-step0-management-unknown-hint = patikrinkite `netbird status -d` arba `goat client diagnostics`; doctor nespėlios valdymo URL
doctor-step0-tunnel-down-summary = NetBird nenurodo mesh adreso
doctor-step0-tunnel-down-hint = paleiskite `netbird status -d`; jei įrenginys užregistruotas ir demonas atsako, paleiskite `goat heal`, tada dar kartą `goat doctor`
doctor-step0-tunnel-address-summary = NetBird nurodo mesh adresą { $address }; toliau tikrinamas maršrutizatoriaus pasiekiamumas
doctor-step0-goat-client-no-bundle-summary = goat-clientd neturi importuoto paketo
doctor-step0-goat-client-no-bundle-hint = paleiskite `goat client bundle import <bundle.cbor>` su kliento registracijos paketu, prijunkite klientą, tada dar kartą `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd praneša, kad tunelis prijungtas; toliau tikrinamas maršrutizatoriaus pasiekiamumas
doctor-step0-goat-client-outer-disabled-summary = goat-clientd išorinis tunelis išjungtas netbird-only režimu
doctor-step0-goat-client-down-summary = goat-clientd praneša tunelio būseną `{ $state }`
doctor-step0-goat-client-down-hint = paleiskite `goat client diagnostics`, o jei paketas įkeltas – `goat client up`; tada dar kartą `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd praneša, kad vidinis mesh prijungtas
doctor-step0-goat-client-inner-disabled-summary = goat-clientd vidinis mesh išjungtas wg-cp0-only režimu
doctor-step0-goat-client-inner-down-summary = goat-clientd praneša vidinio mesh būseną `{ $state }`
doctor-step0-goat-client-inner-down-hint = paleiskite `goat client diagnostics` vidiniam mesh patikrinti, o jį atkūrę dar kartą paleiskite `goat doctor`
doctor-step0-management-reachable-summary = valdymo galinis taškas atsakė HTTP { $status }
doctor-step0-management-auth-summary = valdymo HTTPS galinis taškas pasiekiamas (HTTP { $status }: šiam patikrinimui reikalinga autorizacija)
doctor-step0-management-unexpected-summary = valdymo galinis taškas grąžino netikėtą HTTP { $status }
doctor-step0-management-unexpected-hint = patikrinkite sukonfigūruotą valdymo URL ir paprašykite goatnet operatoriaus peržiūrėti šį atsakymą
doctor-step0-management-unreachable-summary = nepavyko pasiekti sukonfigūruoto valdymo galinio taško: { $error }
doctor-step0-management-unreachable-hint = patikrinkite DNS ir tinklo prieigą prie kliento nurodyto URL; esamas tunelis vis tiek gali veikti
doctor-step0-clock-ok-summary = vietinis laikrodis nuo valdymo HTTP laiko skiriasi ne daugiau nei { $seconds } sek.
doctor-step0-clock-skew-summary = vietinis laikrodis nuo valdymo HTTP laiko skiriasi { $seconds } sek.
doctor-step0-clock-skew-hint = sinchronizuokite įrenginio laikrodį, tada dar kartą paleiskite `goat doctor`; kol laikrodis neteisingas, sertifikatų tikrinimas gali nepavykti
doctor-step0-clock-unknown-summary = nepavyko išmatuoti laikrodžio nuokrypio pagal valdymo HTTP antraštę Date
doctor-step0-clock-unknown-hint = jei nepavyksta TLS ar sertifikatų patikros, patikrinkite įrenginio laiko sinchronizavimą
error-clock-skew-summary = įrenginio laikrodis per daug nutolęs nuo valdymo laiko
error-doctor-inconclusive-summary = doctor užfiksavo gedimą be įrodytos priežasties
error-portal-forbidden-summary = portalas uždraudė šiai paskyrai prieigą prie tikrinamos API
doctor-step4-forbidden-summary = portalas šiam profiliui grąžino HTTP 403
doctor-step4-forbidden-hint = šis kredencialas pasiekė portalą, bet neturi prieigos prie tikrinamos API; patikrinkite `goat profile show` ir paprašykite goatnet operatoriaus patikrinti prieigos teises
doctor-step4-decode-summary = portalas atsakė taip, kad šis goat atsakymo perskaityti negali
doctor-step4-decode-hint = { $err } — atnaujinkite goat iki šio goatnet teikiamos versijos, tada vėl paleiskite `goat doctor`
doctor-step1-control-only-summary = prisijungta tik prie valdymo serverio — tunelio nė su vienu iš { $total } mazgų nėra
doctor-step1-control-only-hint = šis tinklas išleidžia TCP 443, bet blokuoja mesh duomenų prievadus, todėl iš šio įrenginio nepasiekiama nieko mesh tinkle (įskaitant prisijungimo puslapius). Paprašykite tinklo komandos leisti išeinantį TCP ir UDP į { $relays } bei UDP 3478 į { $stun }; tiesioginis UDP tarp įrenginių taip pat rekomenduojamas. Norėdami patvirtinti, prisijunkite prie telefono prieigos taško ir paleiskite `netbird status`: mazgai turėtų prisijungti.
doctor-offmesh-dashboard-hint = relay parkas (išorinė perspektyva): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = vietinis sertifikatas, atitinkantis raktas ir CA rinkinys nuskaitomi; sertifikatas galioja
doctor-step2-material-failed-summary = nepavyko panaudoti vietinės TLS medžiagos { $file }
doctor-step2-material-failed-hint = { $err } — patikrinkite nurodytą failą; atkurkite jį iš išduoto rinkinio arba rotuokite profilį
doctor-step2-material-invalid-hint = { $file } nėra tinkamas X.509 PEM sertifikatas; atkurkite išduotą medžiagą arba paleiskite `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem negalioja arba neatitinka mtls.cert.pem; atkurkite išduotą medžiagą arba paleiskite `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = vietinis sertifikatas už galiojimo laikotarpio ribų
doctor-step2-cert-time-hint = paleiskite `goat profile show`; patikrinkite aukščiau pateiktą laikrodžio rezultatą, tada paprašykite operatoriaus naujo rinkinio ir, jei sertifikato galiojimas baigėsi, paleiskite `goat profile rotate <new-bundle.cbor>`
doctor-skipped-no-endpoint-summary = praleista — profile.toml trūksta { $field }
doctor-skipped-no-profile-summary = praleista — šiame kontekste nėra skaitomų profilio galinių taškų
doctor-skipped-no-portal-token-summary = praleista — šiame kontekste nėra skaitomo portalo žetono
status-ztol-tcp-ok = maršrutizatoriaus prievadas { $host }:{ $port } priima TCP (tik prievadas — žr. sesiją)
status-session-ok = zenoh mTLS sesija atidaryta per { $ms }ms (maršrutizatorius priėmė šį sertifikatą)
status-session-skipped = sesija nebandyta (ne mesh tinkle; žr. ztol)
status-session-material-missing = zenoh sesija nebandyta: { $detail }
status-session-config-failed = zenoh sesijos konfigūracija nepavyko: { $err }
status-session-failed = zenoh sesija neatsidarė: { $err } — paleiskite `goat doctor`
status-session-timeout = zenoh sesija neatsidarė per { $secs }s — maršrutizatoriaus prievadas priima TCP, bet sesija neužsimezga (maršrutizatorius užstrigęs, sertifikatas atmestas arba laikrodžio nuokrypis); paleiskite `goat doctor`
status-names-live = vardai gyvi (mesh DNS atsakė)
status-names-ip-only = vardai nepatikrinti (profilio galiniai taškai yra IP adresai)
status-names-snapshot = vardai pagal pasirašytą momentinę kopiją, serija { $serial }, amžius { $age } (mesh DNS nepasiekiamas)
status-names-snapshot-aging = vardai pagal pasirašytą momentinę kopiją, serija { $serial }, amžius { $age } — PASENĘ (mesh DNS nepasiekiamas; sutaisykite goat-coredns, tada vardai atsinaujins automatiškai)
status-names-snapshot-daemon = vardai pagal pasirašytą momentinę kopiją, serija { $serial }, amžius { $age } — demono saugykla (mesh DNS nepasiekiamas)
status-names-snapshot-daemon-aging = vardai pagal pasirašytą momentinę kopiją, serija { $serial }, amžius { $age } — demono saugykla, PASENĘ (mesh DNS nepasiekiamas; sutaisykite goat-coredns, tada vardai atsinaujins automatiškai)
status-names-failed = mesh DNS nepasiekiamas, momentinės kopijos atsarginis variantas nepasiekiamas — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ĮJUNGTA — { $qr }/{ $connected } prijungtų mazgų atsparūs kvantiniams kompiuteriams
status-pqc-off = wireguard PQC (rosenpass) IŠJUNGTA — klasikiniai wireguard rankos paspaudimai
status-pqc-unknown = wireguard PQC nežinoma — { $reason }
status-device-none = visos įrenginio vardų apsauga IŠJUNGTA — nėra goat-clientd vardų saugyklos (goat komandos vis tiek turi atsarginį variantą; įjunkite pagal name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd vardų saugykla yra (serija { $serial }), bet jos persiuntėjas NEATSAKO adresu { $addr } — demonas neveikia arba vardų posistemis užstrigo
status-device-active = visos įrenginio vardų apsauga AKTYVI — goat-clientd dengia OS mesh DNS adresu { $addr } (saugyklos serija { $serial }, { $grade })
status-device-not-fronted = goat-clientd persiuntėjas veikia adresu { $addr } (saugyklos serija { $serial }, { $grade }) — OS nedengiama; naršyklės naudoja tiesioginį mesh DNS
status-device-fronting-unknown = goat-clientd persiuntėjas veikia adresu { $addr } (saugyklos serija { $serial }, { $grade }) — OS dengimo būsenos šioje platformoje nustatyti negalima
status-names-observed = vardai NEKANONINIAI (ad hoc): pastebėtas įrašas, amžius { $age } — nėra pasirašytoje momentinėje kopijoje (mesh DNS nepasiekiamas; įtvirtinkite šį įrašą)
status-names-selfclaim = vardai pagal patikrintą SAVIPAREIŠKIMĄ, amžius { $age } — pasirašė paties vardo inkaras (mesh DNS nepasiekiamas)
doctor-names-note = pagal momentinę kopiją, serija { $serial }, amžius { $age }
doctor-names-selfclaim-note = pagal patikrintą SAVIPAREIŠKIMĄ, amžius { $age } — pasirašė paties vardo inkaras
doctor-names-selfclaim-hint = mesh DNS nepasiekiamas — šis žingsnis išspręstas pagal patikrintą SAVIPAREIŠKIMĄ (amžius { $age }): šį susiejimą pasirašė paties vardo inkaras, o jį įgalioja talpykloje esančios momentinės kopijos peer_bindings. Pasirašyta, bet ne gyva; sutaisykite mesh DNS, kad tai išnyktų.
doctor-names-observed-note = pagal PASTEBĖTĄ (ad hoc) įrašą, amžius { $age } — nekanoninis
doctor-names-observed-hint = mesh DNS nepasiekiamas — šis žingsnis išspręstas pagal NEKANONINĮ pastebėtą įrašą (amžius { $age }): vardas yra tik gyvas, niekada neįtvirtintas svetainės registre. Įtvirtinkite jį (group_vars registras → generatorius → pasirašyti iš naujo) arba jis pasens; sutaisykite mesh DNS, kad tai išnyktų.
doctor-names-hint = mesh DNS nepasiekiamas — šis žingsnis išspręstas pagal pasirašytą vardų momentinę kopiją (serija { $serial }, amžius { $age }). Tinklas lieka pasiekiamas per atsarginius vardus; sutaisykite mesh DNS (goat-coredns valdymo mazge), kad tai išnyktų.
storm-brake-plan-header = Audros stabdžio planas (ADR 1081 4 sluoksnis — rankinis):
storm-brake-plan-target = valdymo mazgas { $user }@{ $host }  (kontekstas { $context })
storm-brake-step-flush = iš naujo paleisti signalų konteinerį `{ $container }` (nutraukia zombių srautus)
storm-brake-step-tighten = apriboti L2 ribotuvą `{ $container }` (perpus sumažinti spartos limitą, padidinti neg-cache TTL) — { $window } min., operatorius atstato pats; praleidžiama, jei L2 neįdiegtas
storm-brake-step-shed = sustabdyti neesminį publikuotojo vienetą `{ $unit }`
storm-brake-shed-none = (nenurodyta numetamų vienetų — nustatykite storm_brake_shed_units faile profile.toml arba perduokite --shed-unit)
storm-brake-announce-note = įrašyti vieną OperatorActionV1 į audito pėdsaką su kiekvieno žingsnio rezultatais
storm-brake-dryrun-note = bandomasis paleidimas — nieko nebuvo įvykdyta.
storm-brake-triggers-header = Naudoti, kai relay-health mazgo tinklo juosta rodo:
storm-brake-trigger-futility = signalizacijos bergždumo santykis >= 0.5 esant >= 100 pr./min. (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = ryšio nuostoliai >= 5 % išlieka 10 min. (wan.loss_pct)
storm-brake-triggers-band-url = mazgo juosta: relay-health.{ $site }.<zone> (žr. docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Pastaba: vien signalų išvalymas NEatkuria tinklo. Gydo 1–2 sluoksniai (kliento atsitraukimas + signalų ribojimas); stabdis tik laimi laiko atsigavimui.
storm-brake-confirm = Paleisti audros stabdį { $user }@{ $host }? Perkraunamas signalų konteineris ir sustabdomi nurodyti publikuotojai. [y/N]
storm-brake-aborted = audros stabdis nutrauktas (nepatvirtinta)
storm-brake-executing = paleidžiamas audros stabdis { $user }@{ $host } ...
storm-brake-outcome-ok = gerai
storm-brake-outcome-failed = nepavyko
storm-brake-outcome-skipped = praleista
storm-brake-summary = audros stabdis baigtas: { $ok } sėkmingai, { $failed } nepavyko, { $skipped } praleista
storm-brake-flush-failed = FLUSH nepavyko — signalų konteineris neperkrautas, tad tinklo atsigavimo tikėtis nereikia: { $reason }
storm-brake-mgmt-unresolved = valdymo mazgas nenustatytas — perduokite --mgmt-host, nustatykite mgmt_ssh_host konteksto profile.toml arba eksportuokite GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operatoriaus veiksmas įrašytas į audito pėdsaką (vykdytojas { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portalas atsakė HTTP 404 užklausai `{ $path }`: ten neužregistruota
error-portal-rate-limited-summary = portalas atsakė HTTP 429 užklausai `{ $path }`: per daug užklausų, bandykite vėliau
error-portal-server-error-summary = portalas atsakė HTTP { $status } užklausai `{ $path }`: gedimas portalo pusėje
error-portal-refused-summary = portalas atsakė HTTP { $status } užklausai `{ $path }`: užklausa atmesta

### goat-cli — Icelandic (is). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = ekkert snið fyrir samhengið `{ $context }`
error-off-mesh-summary = utan ZTOL — ekki tengt við mesh-netið
error-router-unreachable-summary = Zenoh-beinir á `{ $endpoint }` er ekki tiltækur
error-cert-expired-summary = mTLS-skilríki útrunnið eða hafnað af beini
error-portal-unreachable-summary = portal-API á `{ $endpoint }` er ekki tiltækt
error-token-invalid-summary = portal hafnaði Bearer-tókanum
error-topic-conflict-summary = topic `{ $topic }` er þegar til
error-bundle-invalid-summary = staðfesting á bundle-undirskrift eða gildistíma mistókst

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = endapunktur beinis er rangt myndaður
doctor-step1-endpoint-malformed-hint = bjóst við `<proto>/<host>:<port>`; fékk `{ $endpoint }`
doctor-step1-ok-summary = TCP-tenging við beinihýsil { $host }:{ $port } tókst
doctor-step1-conn-failed-summary = TCP-tenging við beinihýsil mistókst
doctor-step1-conn-failed-hint = { $err } — ræstu netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); athugaðu `netbird status`
doctor-step1-timeout-summary = tímamörk runnu út fyrir TCP-tengingu við beinihýsil
doctor-step1-timeout-hint = beinihýsill { $host }:{ $port } náðist ekki á { $ms }ms — líklega utan mesh
doctor-step2-endpoint-malformed-hint = lagaðu útgefanda bundle (--goat-cli-router-endpoint verður að vera `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = CA-rætur-PEM vantar eða er ólæsilegt
doctor-step2-ca-missing-hint = { $err } — keyrðu `goat profile init <bundle.cbor>` aftur
doctor-step2-ok-summary = beinir { $host }:{ $port } samþykkir TCP (TLS staðfest í skrefi 3)
doctor-step2-refused-summary = TCP beinis hafnað
doctor-step2-refused-hint = { $err } — beinirinn gæti verið niðri; athugaðu Zenoh-uppsetningu á mgmt-host
doctor-step2-timeout-summary = tímamörk runnu út fyrir TCP beinis
doctor-step2-timeout-hint = beinir { $host }:{ $port } náðist ekki á { $ms }ms
doctor-step3-config-failed-summary = bygging zenoh-stillinga mistókst
doctor-step3-config-failed-hint = ógildur endapunktur `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = bygging zenoh-TLS-stillinga mistókst
doctor-step3-tls-config-failed-hint = { $err } — líklega zenoh-útgáfufrávik; staðfestu að útgáfur passi
doctor-step3-ok-summary = mTLS-lota opnuð — skírteini samþykkt af ACL beinis
doctor-step3-open-failed-summary = opnun zenoh-lotu mistókst
doctor-step3-open-failed-hint = { $err } — skírteini gæti verið útrunnið (keyrðu `goat profile show`) eða hafnað af ACL beinis (URI-SAN-ósamræmi)
doctor-step3-timeout-summary = tímamörk runnu út fyrir opnun zenoh-lotu
doctor-step3-timeout-hint = beinirinn lauk ekki handabandi á { $secs }s — klukkuskekkja, rangt skírteini eða umferðarteppa
doctor-step4-ok-summary = gáttin skilaði { $n } fyrirtækjalínu(m)
doctor-step4-token-rejected-summary = gáttin hafnaði Bearer-teikni
doctor-step4-token-rejected-hint = HTTP { $status } — teikn útrunnið eða afturkallað; skráðu þig aftur í gegnum operator-onboard.yml
doctor-step4-request-failed-summary = beiðni til gáttar mistókst
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = tímamörk runnu út fyrir beiðni til gáttar
doctor-step4-timeout-hint = ekkert svar á { $secs }s — athugaðu hvort { $endpoint } náist frá mesh
doctor-skipped-summary = sleppt — fyrra skref mistókst (keyrðu aftur með --all til að þvinga)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Sameinuð rekstrarskipanalína fyrir goat-gagnaveituna.
help-long-about = goat — rekstraraðila-CLI sem fléttar panoscope-core í eitt tól: frumstilling/endurnýting sniða, ZTOL-meðvitaður doctor fyrir aðgengi, vafur um fyrirtækja-portal, topic CRUD, pub/sub, stethoscope-úthlutun, gagnvirkur skipanasamsetjari og frásögn af substrate-fossi.
help-flag-profile-dir = Hnekkja sniðmöppunni (sjálfgefið: $GOAT_PROFILE_DIR eða ~/.goat).
help-flag-context = Hnekkja virku samhengi (sjálfgefið: $GOAT_CONTEXT umhverfisbreyta, svo current_context í config.toml, svo "default").
help-flag-narrate = Sýna substrate-fossinn sem breytandi skipun setur af stað.
help-flag-verbose = Virkja ítarlegan villuleitarúttak (eða setja RUST_LOG=goat=debug).
help-flag-color = Litahamur fyrir útstöðvarúttak (auto|always|never).
help-flag-lang = Tungumál fyrir manneskjusýnilegan texta (t.d. en, uk). Fellur til baka á GOAT_LANG, svo staðfærslu stýrikerfis, svo ensku.
help-cmd-profile = Hafa umsjón með staðværa goat-sniðinu (mTLS-skírteini + portal-tóki + endapunktasafn).
help-cmd-context = Hafa umsjón með samhengjum (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Keyra þrepaskipta aðgengiskönnun gegn veitunni.
help-cmd-doctor-long = Þrepaskipt aðgengiskönnun gegn veitunni: ZTOL-aðild → Zenoh-beinir → samþykki skírteinis → portal. Hvert misheppnað þrep gefur frá sér sértæka úrbótavísbendingu.
help-cmd-vendor = Vafra um fyrirtæki skráð í portalinu.
help-cmd-topic = Lista, sýna, búa til eða eyða topic-um.
help-cmd-pub = Birta gögn á topic-lykil.
help-cmd-sub = Gerast áskrifandi að topic-lykilsegð.
help-cmd-inspect = Úthluta beiðni um greiningarúrskurð til stethoscope.
help-cmd-ui = Gagnvirkur sameinaður veljari: vafra um declared ⊕ live skrána og hoppa á Diagnose / Tail linsuna á völdu marki.
help-cmd-compose = Gagnvirkt TUI til skipanasamsetningar.
help-cmd-narrate = Speglun substrate-foss yfir nýlega virkni. Ekki enn í boði (daglega leiðin er --narrate á breytandi skipun).
help-cmd-completion = Prenta klárunarforskrift skeljar á stdout.
help-cmd-explain = Fletta upp merkingu villukóða + úrbót í innbyggða `goat-cli-error-codes.md` skjalinu (cargo --explain mynstrið).
help-cmd-mock = Birta tilbúin sýni á topic. Sjálfgefið: einfalt hello-world JSON á 1Hz. Sendu `--format <.fds>` + `--message <Name>` til að kóða protobuf frá rekstraraðila í staðinn.
help-cmd-status = Heilsusamantekt: ZTOL-aðgengi + portal /api/health + útrunnatími staðværs skírteinis, þrjár glyph-línur eða einn --json hlutur.
help-cmd-services = Kanna hverja hýsta þjónustu í `ops/serve/services.yaml` og birta eina glyph-línu á þjónustu (eða einn --json hlut).
help-cmd-client = Vefja staðværa goat-clientd daemon-inn gegnum JSON-RPC IPC-sökkulinn hans (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH-byggð uppgötvun viðbóta (kubectl-stíll). Hvaða `goat-<name>` keyrsla á PATH sem er má kalla sem `goat <name>`; `goat plugin list` telur upp uppsettar viðbætur, `goat plugin which <name>` finnur slóðina.

# goat status / inspect / --narrate runtime prose (ADR 1024).
# Contract tokens + backticked keys + glyphs → — reproduced verbatim.
status-ztol-malformed = endapunktur beinis er rangt myndaður: `{ $endpoint }`
status-ztol-refused = TCP hafnað á beini { $host }:{ $port }: { $err }
status-ztol-unreachable = utan mesh (beinir { $host }:{ $port } ekki tiltækur á { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health mistókst: { $err }
status-portal-timeout = portal /api/health rann út á tíma á { $secs }s
status-cert-read-failed = lestur skírteinis mistókst: { $err }
status-cert-pem-parse-failed = PEM-þáttun skírteinis mistókst: { $err }
status-cert-wrong-tag = fyrsta PEM-blokkin er { $tag }, vænti CERTIFICATE
status-cert-x509-parse-failed = x509-þáttun mistókst: { $err }
status-cert-expired = skírteini ÚTRUNNIÐ við unix={ $not_after }, núna={ $now }
status-cert-valid = skírteini gilt; rennur út eftir { $days_left }d (unix={ $not_after })
inspect-finding-label = niðurstaða ({ $severity }): { $label } — { $detail }
inspect-remedy-label = úrbót: { $title } — { $detail }
inspect-no-outcomes = (engar niðurstöður — markið passaði við engar prófanir)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh-put samþykkt á `{ $key }` ({ $bytes } bæti)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = fossinn settist — { $summary } ({ $n } { $n ->
        [one] afleiðing
       *[other] afleiðingar
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Leiðsögn við fyrstu keyrslu: veldu sandbox eða raunverulegt, sannaðu að það virki, vísaðu á tutor.
help-cmd-tutor = Sjálfstýrð, verkleg yfirferð um veituna (í stíl vimtutor; má halda áfram síðar).
help-cmd-docs = Vafra um goat-skjölunina, eða gefa út fulla skipanaviðmiðun (--markdown).
help-cmd-hello = Senda hello fram og til baka gegnum veituna enda á milli (sandbox eða raunverulegt umhverfi).
flourish-all-green = 🐐 allt grænt — veitan í lagi

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = aðgerð fyrir rekstraraðila eingöngu — þú hefur ekki heimild (`goat admin` gerir ekkert fyrir þá sem eru ekki stjórnendur)
help-flag-narrate-json = Sendir frá sér substrate-keðjuna sem NDJSON (eitt hlutfall á afleiðingu) fyrir atvikaannála + skriftun. Felur í sér --narrate.
help-cmd-admin = Stjórnun fyrir rekstraraðila eingöngu: líftími fyrirtækis, innganga/afturköllun/snúningur, mesh-jafningi skorið af, tæki + rekstraraðilaþrepa úthlutun, LAN greining. Heimilað af rekstraraðilaskilríkjum þínum (þjónshlið í gegnum portal/NetBird tóknasvið); gerir ekkert fyrir þá sem eru ekki stjórnendur — óheimilt kall breytir engu og mistekst með `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = prófíllinn fyrir samhengið `{ $context }` er til en þessi notandi hefur ekki leyfi til að lesa hann (aðgangi hafnað)
error-portal-bad-response-summary = gáttin svaraði en þessi goat-útgáfa gat ekki túlkað svarið
doctor-step0-ok-summary = staðbundin uppsetning er tilbúin fyrir netprófanir
doctor-step0-warn-summary = ekki tókst að staðfesta sumar athuganir á staðbundinni uppsetningu
doctor-step0-fail-summary = staðbundin uppsetning þarfnast athugunar
doctor-step0-profile-absent-summary = enginn goat-prófíll fyrir samhengið `{ $context }`
doctor-step0-profile-new-hint = athugaðu `goat context ls`; ef þetta er fyrsta uppsetning, keyrðu `goat ca add <bundle-ca.pem>` og berðu fingrafarið saman við fingrafar rekstraraðilans, síðan `goat profile init <bundle.cbor>`; keyrðu `goat doctor` aftur
doctor-step0-profile-absent-hint = athugaðu `goat context ls`; veldu fyrirliggjandi samhengi með `goat context use <name>`, eða keyrðu `goat profile init <bundle.cbor>` fyrir þetta samhengi; keyrðu `goat doctor` aftur
doctor-step0-profile-denied-summary = goat-prófíllinn á { $path } er ólæsilegur
doctor-step0-profile-denied-hint = lagaðu eignarhald prófílsins eða keyrðu sem eigandi hans; ekki frumstilla fyrirliggjandi prófíl aftur
doctor-step0-profile-incomplete-summary = goat-prófílskrána { $file } vantar eða hún er ólæsileg
doctor-step0-profile-incomplete-hint = endurheimtu nefndu skrána úr útgefna búntinu, eða notaðu `goat profile rotate <bundle.cbor>` til að skipta þessu samhengi út; keyrðu `goat doctor` aftur
doctor-step0-profile-invalid-summary = goat-prófíll er með ólæsilega eða vantandi endapunkta
doctor-step0-profile-empty-token-summary = goat-prófíll er með tómt gáttartákn
doctor-step0-profile-ok-summary = goat-samhengið `{ $context }` er með læsileg prófílgögn
doctor-step0-client-absent-summary = enginn studdur mesh-biðlari svaraði
doctor-step0-client-absent-hint = settu upp mesh-biðlarann sem goatnet þitt útvegar, ræstu púkann hans og keyrðu síðan `goat doctor` aftur
doctor-step0-client-stopped-summary = uppsettur mesh-biðlari skilaði ekki nothæfri stöðu
doctor-step0-client-stopped-hint = athugaðu `netbird status -d` eða `goat client status --detail`; ræstu uppsetta púkann (`sudo netbird service start` fyrir NetBird), keyrðu síðan `goat doctor` aftur
doctor-step0-netbird-ok-summary = NetBird-púkinn svaraði (skráður prófíll `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd svaraði (staða `{ $state }`)
doctor-step0-management-connected-summary = NetBird tilkynnir að stjórnunartenging sé virk
doctor-step0-management-disconnected-summary = NetBird tilkynnir að stjórnunartenging sé rofin
doctor-step0-management-disconnected-hint = athugaðu uppgefna stjórnunarslóð og `netbird status -d`; uppsett göng geta enn virkað í stjórnunarbilun
doctor-step0-management-unknown-summary = ekki tókst að koma á stilltum stjórnunarendapunkti staðbundið
doctor-step0-management-unknown-hint = skoðaðu `netbird status -d` eða `goat client diagnostics`; doctor giskar ekki á stjórnunarvefslóð
doctor-step0-tunnel-down-summary = NetBird tilkynnir ekkert möskvavistfang
doctor-step0-tunnel-down-hint = keyrðu `netbird status -d`; ef tækið er skráð og púkinn svarar, keyrðu `goat heal` og svo `goat doctor` aftur
doctor-step0-tunnel-address-summary = NetBird tilkynnir möskvavistfang { $address }; næst er athugað hvort beinirinn næst
doctor-step0-goat-client-no-bundle-summary = goat-clientd er ekki með neinn innfluttan pakka
doctor-step0-goat-client-no-bundle-hint = keyrðu `goat client bundle import <bundle.cbor>` með skráningarpakka biðlarans, tengdu biðlarann og keyrðu svo `goat doctor` aftur
doctor-step0-goat-client-connected-summary = goat-clientd tilkynnir tengd göng; næst er athugað hvort beinirinn næst
doctor-step0-goat-client-outer-disabled-summary = ytri göng goat-clientd eru óvirk í netbird-only ham
doctor-step0-goat-client-down-summary = goat-clientd tilkynnir ástand ganga `{ $state }`
doctor-step0-goat-client-down-hint = keyrðu `goat client diagnostics` og svo `goat client up` ef pakkinn er hlaðinn; keyrðu `goat doctor` aftur
doctor-step0-goat-client-inner-ok-summary = goat-clientd tilkynnir að innra möskvanet sé tengt
doctor-step0-goat-client-inner-disabled-summary = innra möskvanet goat-clientd er óvirkt í wg-cp0-only ham
doctor-step0-goat-client-inner-down-summary = goat-clientd tilkynnir ástand innra möskvanets `{ $state }`
doctor-step0-goat-client-inner-down-hint = keyrðu `goat client diagnostics` til að skoða innra möskvanetið og keyrðu svo `goat doctor` aftur eftir að það er komið í lag
doctor-step0-management-reachable-summary = stjórnunarendapunktur svaraði með HTTP { $status }
doctor-step0-management-auth-summary = HTTPS-stjórnunarendapunktur næst (HTTP { $status } krefst auðkenningar fyrir þessa prófun)
doctor-step0-management-unexpected-summary = stjórnunarendapunktur skilaði óvæntu HTTP { $status }
doctor-step0-management-unexpected-hint = athugaðu stilltu stjórnunarvefslóðina og biddu goatnet-stjórnanda þinn að skoða þetta svar
doctor-step0-management-unreachable-summary = ekki náðist í stilltan stjórnunarendapunkt: { $error }
doctor-step0-management-unreachable-hint = athugaðu DNS og netaðgang að vefslóðinni sem biðlarinn gefur upp; núverandi göng gætu samt virkað
doctor-step0-clock-ok-summary = staðartími er innan { $seconds } sekúndna frá HTTP-tíma stjórnunar
doctor-step0-clock-skew-summary = staðartími víkur um { $seconds } sekúndur frá HTTP-tíma stjórnunar
doctor-step0-clock-skew-hint = samstilltu klukku tækisins og keyrðu `goat doctor` aftur; sannprófun skírteina getur mistekist meðan klukkan er röng
doctor-step0-clock-unknown-summary = ekki tókst að mæla klukkuskekkju út frá Date-haus í HTTP-svari stjórnunar
doctor-step0-clock-unknown-hint = athugaðu tímasamstillingu tækisins ef TLS- eða skírteinisathuganir mistakast
error-clock-skew-summary = klukka tækisins er of langt frá tíma stjórnkerfisins
error-doctor-inconclusive-summary = doctor sá bilun án sannaðrar orsakar
error-portal-forbidden-summary = gáttin neitaði þessum aðgangi um aðgang að prófaða API-viðmótinu
doctor-step4-forbidden-summary = gáttin skilaði HTTP 403 fyrir þennan prófíl
doctor-step4-forbidden-hint = þetta auðkenni náði til gáttarinnar en hefur ekki aðgang að prófaða API-viðmótinu; athugaðu `goat profile show` og biddu goatnet-stjórnanda um að staðfesta veitinguna
doctor-step4-decode-summary = gáttin svaraði með svari sem þessi goat getur ekki lesið
doctor-step4-decode-hint = { $err } — uppfærðu goat í útgáfuna sem þetta goatnet býður, keyrðu svo `goat doctor` aftur
doctor-step1-control-only-summary = tengt við stjórnþjón eingöngu — engin göng til neins af { $total } jafningjum
doctor-step1-control-only-hint = þetta net hleypir TCP 443 út en lokar á gagnaportin í mesh-netinu, svo ekkert á mesh-netinu (þar með talið innskráningarsíður) næst úr þessu tæki. Biddu netteymið um að leyfa útgangandi TCP og UDP til { $relays } og UDP 3478 til { $stun }; bein UDP-samskipti milli tækja eru einnig æskileg. Til að staðfesta: tengstu símahotspot og keyrðu `netbird status`: jafningjar ættu þá að tengjast.
doctor-offmesh-dashboard-hint = relay-flotinn (utanaðkomandi sjónarhorn): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = staðbundið skilríki, samsvarandi lykill og CA-búnt þáttast; skilríkið er innan gildistíma
doctor-step2-material-failed-summary = ekki var hægt að nota staðbundið TLS-gagn { $file }
doctor-step2-material-failed-hint = { $err } — athugaðu tilgreinda skrá; endurheimtu hana úr útgefna búntinu eða skiptu um lykla prófílsins
doctor-step2-material-invalid-hint = { $file } er ekki gilt X.509 PEM-skilríki; endurheimtu útgefna gagnið eða keyrðu `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem er ógildur eða samsvarar ekki mtls.cert.pem; endurheimtu útgefna gagnið eða keyrðu `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = staðbundna skilríkið er utan gildistíma síns
doctor-step2-cert-time-hint = keyrðu `goat profile show`; athugaðu niðurstöðu klukkuprófsins hér að ofan, biddu svo stjórnandann um nýtt búnt og keyrðu `goat profile rotate <new-bundle.cbor>` ef skilríkið er útrunnið
doctor-skipped-no-endpoint-summary = sleppt — profile.toml vantar { $field }
doctor-skipped-no-profile-summary = sleppt — engin læsileg endapunkta í prófíl fyrir þetta samhengi
doctor-skipped-no-portal-token-summary = sleppt — enginn læsilegur gáttarlykill fyrir þetta samhengi
status-ztol-tcp-ok = router-port { $host }:{ $port } tekur við TCP (aðeins port — sjá session)
status-session-ok = zenoh mTLS-lota opnuð á { $ms }ms (router samþykkti þetta skilríki)
status-session-skipped = lota ekki reynd (utan mesh-nets; sjá ztol)
status-session-material-missing = zenoh-lota ekki reynd: { $detail }
status-session-config-failed = uppsetning zenoh-lotu mistókst: { $err }
status-session-failed = zenoh-lota opnaðist ekki: { $err } — keyrðu `goat doctor`
status-session-timeout = zenoh-lota opnaðist ekki innan { $secs }s — router-gáttin tekur við TCP en engin lota klárast (router fastur, skírteini hafnað eða tímaskekkja); keyrðu `goat doctor`
status-names-live = nöfn virk (mesh DNS svaraði)
status-names-ip-only = nöfn ekki reynd (endapunktar sniðs eru IP-tölur)
status-names-snapshot = nöfn skv. undirrituðu skyndisafni, raðnúmer { $serial }, aldur { $age } (mesh DNS næst ekki)
status-names-snapshot-aging = nöfn skv. undirrituðu skyndisafni, raðnúmer { $serial }, aldur { $age } — ÚRELT (mesh DNS næst ekki; lagaðu goat-coredns, þá endurnýjast nöfnin sjálfkrafa)
status-names-snapshot-daemon = nöfn skv. undirrituðu skyndisafni, raðnúmer { $serial }, aldur { $age } — geymsla þjónsins (mesh DNS næst ekki)
status-names-snapshot-daemon-aging = nöfn skv. undirrituðu skyndisafni, raðnúmer { $serial }, aldur { $age } — geymsla þjónsins, ÚRELT (mesh DNS næst ekki; lagaðu goat-coredns, þá endurnýjast nöfnin sjálfkrafa)
status-names-failed = mesh DNS næst ekki og skyndisafn til vara er ekki tiltækt — { $reason }
status-pqc-on = wireguard PQC (rosenpass) VIRKT — { $qr }/{ $connected } tengdra jafningja þola skammtatölvur
status-pqc-off = wireguard PQC (rosenpass) SLÖKKT — hefðbundin wireguard-handtök
status-pqc-unknown = wireguard PQC óþekkt — { $reason }
status-device-none = nafnavörn fyrir allt tækið SLÖKKT — engin nafnageymsla goat-clientd (goat-skipanir hafa samt varaleið; virkjaðu skv. name-fallback-resolver.md §9.2)
status-device-broken = nafnageymsla goat-clientd er til (raðnúmer { $serial }) en framsendirinn svarar EKKI á { $addr } — þjónn niðri eða nafnakerfið fast
status-device-active = nafnavörn fyrir allt tækið VIRK — goat-clientd stendur fyrir framan mesh DNS stýrikerfisins á { $addr } (geymsla raðnúmer { $serial }, { $grade })
status-device-not-fronted = framsendir goat-clientd uppi á { $addr } (geymsla raðnúmer { $serial }, { $grade }) — stýrikerfið ekki fyrir aftan hann; vafrar nota mesh DNS beint
status-device-fronting-unknown = framsendir goat-clientd uppi á { $addr } (geymsla raðnúmer { $serial }, { $grade }) — ekki hægt að greina hvort stýrikerfið er fyrir aftan hann á þessum vettvangi
status-names-observed = nöfn ÓSTAÐLAÐ (ad hoc): skráð færsla, aldur { $age } — ekki í undirrituðu skyndisafni (mesh DNS næst ekki; festu færsluna í skrá)
status-names-selfclaim = nöfn með staðfestri SJÁLFSKRÖFU, aldur { $age } — undirrituð af akkeri nafnsins sjálfs (mesh DNS næst ekki)
doctor-names-note = skv. skyndisafni raðnúmer { $serial }, aldur { $age }
doctor-names-selfclaim-note = með staðfestri SJÁLFSKRÖFU, aldur { $age } — undirrituð af akkeri nafnsins sjálfs
doctor-names-selfclaim-hint = mesh DNS næst ekki — þetta skref leystist með staðfestri SJÁLFSKRÖFU (aldur { $age }): akkeri nafnsins sjálfs undirritaði þessa tengingu og peer_bindings í skyndisafninu heimila hana. Undirritað en ekki virkt; lagaðu mesh DNS til að losna við þetta.
doctor-names-observed-note = með SKRÁÐRI (ad hoc) færslu, aldur { $age } — óstaðlað
doctor-names-observed-hint = mesh DNS næst ekki — þetta skref leystist með ÓSTAÐLAÐRI skráðri færslu (aldur { $age }): nafnið er aðeins virkt í rauntíma og aldrei fest í skrá svæðisins. Festu það (group_vars registry → generator → endurundirritun) annars rennur það út; lagaðu mesh DNS til að losna við þetta.
doctor-names-hint = mesh DNS næst ekki — þetta skref leystist með undirrituðu nafnaskyndisafni (raðnúmer { $serial }, aldur { $age }). Netið helst aðgengilegt á varanöfnum; lagaðu mesh DNS (goat-coredns á mgmt-vélinni) til að losna við þetta.
storm-brake-plan-header = Neyðarhemlaáætlun (ADR 1081 lag 4 — handvirkt):
storm-brake-plan-target = stjórnhýsill { $user }@{ $host }  (samhengi { $context })
storm-brake-step-flush = endurræsa merkjagám `{ $container }` (fjarlægir uppvakningastrauma)
storm-brake-step-tighten = herða L2-takmarkara á `{ $container }` (helminga hraðatakmörk, hækka TTL neikvæðrar skyndiminnis) — { $window } mín., rekstraraðili afturkallar; sleppt ef L2 er ekki uppsett
storm-brake-step-shed = gera hlé á ónauðsynlegri útgáfueiningu `{ $unit }`
storm-brake-shed-none = (engar afléttingareiningar skilgreindar — stilltu storm_brake_shed_units í profile.toml eða notaðu --shed-unit)
storm-brake-announce-note = senda eina OperatorActionV1 í endurskoðunarslóðina með niðurstöðu hvers skrefs
storm-brake-dryrun-note = þurrkeyrsla — ekkert var keyrt.
storm-brake-triggers-header = Ræstu þegar host-net-svið relay-health sýnir:
storm-brake-trigger-futility = tilgangsleysishlutfall merkja >= 0,5 við >= 100 skeyti/mín. (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = upptengilstap >= 5% í 10 mín. samfleytt (wan.loss_pct)
storm-brake-triggers-band-url = hýsilsvið: relay-health.{ $site }.<zone> (sjá docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Athugið: merkjaflush eitt og sér endurheimtir EKKI netið. Lög 1-2 (undanhald biðlara + afléttun merkja) eru lækningin; hemillinn kaupir tíma til endurheimtar.
storm-brake-confirm = Ræsa neyðarhemil gegn { $user }@{ $host }? Endurræsir merkjagáminn og gerir hlé á tilgreindum útgefendum. [y/N]
storm-brake-aborted = neyðarhemill afturkallaður (engin staðfesting)
storm-brake-executing = ræsi neyðarhemil gegn { $user }@{ $host } ...
storm-brake-outcome-ok = í lagi
storm-brake-outcome-failed = mistókst
storm-brake-outcome-skipped = sleppt
storm-brake-summary = neyðarhemill lokið: { $ok } í lagi, { $failed } mistókust, { $skipped } sleppt
storm-brake-flush-failed = FLUSH mistókst — merkjagámurinn endurræstist ekki, því er ekki búist við endurheimt netsins: { $reason }
storm-brake-mgmt-unresolved = stjórnhýsill ekki leystur — notaðu --mgmt-host, stilltu mgmt_ssh_host í profile.toml samhengisins eða fluttu út GOAT_MGMT_SSH_HOST
storm-brake-audit-note = aðgerð rekstraraðila skráð í endurskoðunarslóð (gerandi { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = gáttin svaraði HTTP 404 fyrir `{ $path }`: ekki skráð þar
error-portal-rate-limited-summary = gáttin svaraði HTTP 429 fyrir `{ $path }`: of margar beiðnir, reyndu aftur síðar
error-portal-server-error-summary = gáttin svaraði HTTP { $status } fyrir `{ $path }`: villa hjá gáttinni
error-portal-refused-summary = gáttin svaraði HTTP { $status } fyrir `{ $path }`: beiðni hafnað

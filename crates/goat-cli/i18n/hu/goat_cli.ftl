### goat-cli — Hungarian (hu). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = nincs profil a(z) `{ $context }` környezethez
error-off-mesh-summary = ZTOL-on kívül — nincs kapcsolat a mesh hálózattal
error-router-unreachable-summary = a(z) `{ $endpoint }` címen lévő Zenoh-útválasztó nem érhető el
error-cert-expired-summary = az mTLS-tanúsítvány lejárt vagy az útválasztó elutasította
error-portal-unreachable-summary = a(z) `{ $endpoint }` címen lévő portál API nem érhető el
error-token-invalid-summary = a portál elutasította a Bearer tokent
error-topic-conflict-summary = a(z) `{ $topic }` topic már létezik
error-bundle-invalid-summary = a bundle aláírásának vagy lejáratának ellenőrzése sikertelen

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = a router végpontja hibás formátumú
doctor-step1-endpoint-malformed-hint = várt `<proto>/<host>:<port>`; kapott `{ $endpoint }`
doctor-step1-ok-summary = TCP-kapcsolat a router gazdagéphez { $host }:{ $port } sikeres
doctor-step1-conn-failed-summary = a TCP-kapcsolat a router gazdagéphez sikertelen
doctor-step1-conn-failed-hint = { $err } — indítsa el a netbirdöt (`sudo netbird up --management-url <mgmt> --setup-key <key>`); ellenőrizze: `netbird status`
doctor-step1-timeout-summary = időtúllépés a router gazdagéphez való TCP-kapcsolatnál
doctor-step1-timeout-hint = a router gazdagép { $host }:{ $port } nem érhető el { $ms }ms alatt — valószínűleg a meshen kívül
doctor-step2-endpoint-malformed-hint = javítsa a bundle kibocsátóját (a --goat-cli-router-endpoint értékének `<proto>/<host>:<port>` formátumúnak kell lennie)
doctor-step2-ca-missing-summary = a CA-gyökerek PEM hiányzik vagy olvashatatlan
doctor-step2-ca-missing-hint = { $err } — futtassa újra: `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = a router { $host }:{ $port } elfogadja a TCP-t (a TLS a 3. lépésben ellenőrizve)
doctor-step2-refused-summary = a router TCP elutasítva
doctor-step2-refused-hint = { $err } — a router leállhatott; ellenőrizze a Zenoh telepítését a mgmt-hoston
doctor-step2-timeout-summary = időtúllépés a router TCP-nél
doctor-step2-timeout-hint = a router { $host }:{ $port } nem érhető el { $ms }ms alatt
doctor-step3-config-failed-summary = a zenoh konfiguráció felépítése sikertelen
doctor-step3-config-failed-hint = érvénytelen végpont `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = a zenoh TLS konfiguráció felépítése sikertelen
doctor-step3-tls-config-failed-hint = { $err } — valószínűleg zenoh verzióeltérés; ellenőrizze, hogy a függőségek egyeznek-e
doctor-step3-ok-summary = mTLS munkamenet megnyitva — a tanúsítványt a router ACL elfogadta
doctor-step3-open-failed-summary = a zenoh munkamenet megnyitása sikertelen
doctor-step3-open-failed-hint = { $err } — a tanúsítvány lejárhatott (futtassa: `goat profile show`) vagy a router ACL elutasította (URI-SAN eltérés)
doctor-step3-timeout-summary = időtúllépés a zenoh munkamenet megnyitásánál
doctor-step3-timeout-hint = a router nem fejezte be a kézfogást { $secs }s alatt — óraeltérés, rossz tanúsítvány vagy torlódás
doctor-step4-ok-summary = a portál { $n } szervezeti sort adott vissza
doctor-step4-token-rejected-summary = a portál elutasította a Bearer tokent
doctor-step4-token-rejected-hint = HTTP { $status } — a token lejárt vagy visszavonták; csatlakozzon újra az operator-onboard.yml segítségével
doctor-step4-request-failed-summary = a portálkérés sikertelen
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = időtúllépés a portálkérésnél
doctor-step4-timeout-hint = nincs válasz { $secs }s alatt — ellenőrizze, hogy a { $endpoint } elérhető-e a meshből
doctor-skipped-summary = kihagyva — az előző lépés sikertelen (futtassa újra a --all kapcsolóval a kényszerítéshez)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Egységes operátori parancssor a goat adatszövethez.
help-long-about = goat — operátoroknak szánt CLI, amely a panoscope-core-t egyetlen eszközzé fogja össze: profil init/újrahasználat, ZTOL-tudatos elérhetőségi doctor, szervezeti portál böngészése, topic CRUD, pub/sub, stethoscope delegálás, interaktív parancsösszeállító és szubsztrátum-kaszkád narráció.
help-flag-profile-dir = Profilkönyvtár felülbírálása (alapértelmezett: $GOAT_PROFILE_DIR vagy ~/.goat).
help-flag-context = Az aktív kontextus felülbírálása (alapértelmezett: $GOAT_CONTEXT környezeti változó, majd current_context a config.toml fájlban, majd "default").
help-flag-narrate = Mutasd a szubsztrátum-kaszkádot, amelyet egy módosító parancs elindít.
help-flag-verbose = Részletes hibakeresési kimenet engedélyezése (vagy állítsd be: RUST_LOG=goat=debug).
help-flag-color = Színmód a terminálkimenethez (auto|always|never).
help-flag-lang = Az emberi olvasásra szánt szöveg nyelve (pl. en, uk). Tartalék a GOAT_LANG, majd az OS területi beállítása, majd az angol.
help-cmd-profile = A helyi goat profil kezelése (mTLS tanúsítvány + portál token + végpontkészlet).
help-cmd-context = Kontextusok kezelése (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Lépcsőzetes elérhetőségi prober futtatása a szövettel szemben.
help-cmd-doctor-long = Lépcsőzetes elérhetőségi prober a szövettel szemben: ZTOL tagság → Zenoh router → tanúsítvány elfogadása → portál. Minden sikertelen lépés egy konkrét javítási tippet bocsát ki.
help-cmd-vendor = A portálban regisztrált szervezetek böngészése.
help-cmd-topic = Topic-ok listázása, megjelenítése, létrehozása vagy törlése.
help-cmd-pub = Hasznos teher közzététele egy topic kulcsra.
help-cmd-sub = Feliratkozás egy topic kulcskifejezésre.
help-cmd-inspect = Diagnosztikai verdikt-kérés delegálása a stethoscope felé.
help-cmd-ui = Interaktív egyesített választó: böngészd a deklarált ⊕ live névsort és ugorj a Diagnose / Tail lencsére a kiválasztott célon.
help-cmd-compose = Interaktív parancsépítő TUI.
help-cmd-narrate = Szubsztrátum-kaszkád visszatükrözése a közelmúltbeli tevékenység felett. Még nem elérhető (a mindennapi út a --narrate egy módosító parancson).
help-cmd-completion = Shell-kiegészítő szkript kiírása a stdout-ra.
help-cmd-explain = Egy hibakód jelentésének + javításának kikeresése a beágyazott `goat-cli-error-codes.md` dokumentumban (cargo --explain minta).
help-cmd-mock = Szintetikus minták közzététele egy topic-ra. Alapértelmezett: triviális hello-world JSON 1Hz-en. Add meg a `--format <.fds>` + `--message <Name>` opciókat, hogy helyette az operátor által megadott protobuf-ot kódold.
help-cmd-status = Állapotszintézis: ZTOL elérhetőség + portál /api/health + helyi tanúsítvány lejárata, három glyph sor vagy egy --json objektum.
help-cmd-services = Vizsgáld meg az `ops/serve/services.yaml` fájlban szereplő minden üzemeltetett szolgáltatást, és jeleníts meg szolgáltatásonként egy glyph sort (vagy egy --json objektumot).
help-cmd-client = A helyi goat-clientd daemon becsomagolása a JSON-RPC IPC socketjén keresztül (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH-alapú bővítményfelfedezés (kubectl-stílus). Bármely PATH-on lévő `goat-<name>` futtatható meghívható így: `goat <name>`; a `goat plugin list` felsorolja a telepített bővítményeket, a `goat plugin which <name>` feloldja az elérési utat.

# ---------------------------------------------------------------------
# goat status / inspect / --narrate futásidejű szöveg (machine-translated; ADR 1024).
# A contract tokenek `{ $var }`, a visszafelé idézőjelek, a → —, és a
# rögzített literálok (/api/health, 2xx, PEM, CERTIFICATE, unix=, TCP,
# x509, Zenoh, stethoscope, (portal-confirmed)) szó szerint reprodukálva.
# ---------------------------------------------------------------------
status-ztol-malformed = a router végpontja hibás formátumú: `{ $endpoint }`
status-ztol-refused = TCP elutasítva a routerhez { $host }:{ $port }: { $err }
status-ztol-unreachable = a meshen kívül (a router { $host }:{ $port } nem érhető el { $ms }ms alatt)
status-portal-ok = portál /api/health 2xx
status-portal-failed = portál /api/health sikertelen: { $err }
status-portal-timeout = portál /api/health időtúllépés { $secs }s alatt
status-cert-read-failed = a tanúsítvány olvasása sikertelen: { $err }
status-cert-pem-parse-failed = a tanúsítvány PEM elemzése sikertelen: { $err }
status-cert-wrong-tag = az első PEM blokk { $tag }, CERTIFICATE szükséges
status-cert-x509-parse-failed = x509 elemzés sikertelen: { $err }
status-cert-expired = a tanúsítvány LEJÁRT ekkor: unix={ $not_after }, most={ $now }
status-cert-valid = a tanúsítvány érvényes; lejár { $days_left }d múlva (unix={ $not_after })
inspect-finding-label = megállapítás ({ $severity }): { $label } — { $detail }
inspect-remedy-label = javítás: { $title } — { $detail }
inspect-no-outcomes = (nincsenek eredmények — a cél nem felelt meg egyetlen ellenőrzésnek sem)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put elfogadva a(z) `{ $key }` kulcson ({ $bytes } bájt)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = a kaszkád elült — { $summary } ({ $n } { $n ->
        [one] következmény
       *[other] következmény
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Első indítási varázsló: válassz sandboxot vagy valódit, bizonyítsd hogy működik, mutass az oktatóra.
help-cmd-tutor = Saját tempójú, gyakorlatias körséta a szöveten (vimtutor-stílus; folytatható).
help-cmd-docs = Böngészd a goat dokumentumokat, vagy add ki a teljes parancsreferenciát (--markdown).
help-cmd-hello = Küldj át egy hellót a szöveten elejétől a végéig (sandbox vagy valódi környezet).
flourish-all-green = 🐐 minden zöld — a szövet rendben

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = csak operátori művelet — nem vagy jogosult (`goat admin` nem adminoknak inaktív)
help-flag-narrate-json = A szubsztrát-kaszkádot NDJSON formátumban adja ki (objektumonként egy következmény) incidensnaplókhoz és szkriptekhez. Magában foglalja a --narrate kapcsolót.
help-cmd-admin = Csak operátori adminisztráció: szervezet-életciklus, onboard/visszavonás/rotálás, mesh-peer vágás, eszköz- és operátori szintű kiépítés, LAN-triage. Az operátori hitelesítő adatok jogosítják fel (szerver oldalon portal/NetBird token hatókörrel); nem adminoknak inaktív — jogosulatlan hívás semmit sem változtat, és `E_NOT_ADMIN` hibával meghiúsul.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = a `{ $context }` kontextus profilja létezik, de ez a felhasználó nem olvashatja (hozzáférés megtagadva)
error-portal-bad-response-summary = a portál válaszolt, de ez a goat build nem tudta értelmezni a választ
doctor-step0-ok-summary = a helyi beállítás készen áll a hálózati próbákra
doctor-step0-warn-summary = a helyi beállításnál vannak nem megerősíthető ellenőrzések
doctor-step0-fail-summary = a helyi beállítás figyelmet igényel
doctor-step0-profile-absent-summary = nincs goat profil a `{ $context }` kontextushoz
doctor-step0-profile-new-hint = ellenőrizd: `goat context ls`; ha ez az első beállításod, futtasd: `goat ca add <bundle-ca.pem>`, és hasonlítsd össze az ujjlenyomatát az operátorodéval, majd futtasd: `goat profile init <bundle.cbor>`; futtasd újra: `goat doctor`
doctor-step0-profile-absent-hint = ellenőrizd: `goat context ls`; válassz egy létező kontextust: `goat context use <name>`, vagy futtasd ehhez a kontextushoz: `goat profile init <bundle.cbor>`; futtasd újra: `goat doctor`
doctor-step0-profile-denied-summary = a goat profil itt: { $path } nem olvasható
doctor-step0-profile-denied-hint = javítsd a profil tulajdonosát, vagy futtasd a tulajdonos felhasználóként; ne inicializáld újra a meglévő profilt
doctor-step0-profile-incomplete-summary = a goat profil { $file } fájlja hiányzik vagy nem olvasható
doctor-step0-profile-incomplete-hint = állítsd vissza a megnevezett fájlt a kiadott bundle-ből, vagy cseréld le ezt a kontextust: `goat profile rotate <bundle.cbor>`; futtasd újra: `goat doctor`
doctor-step0-profile-invalid-summary = a goat profil végpontjai nem olvashatók vagy hiányoznak
doctor-step0-profile-empty-token-summary = a goat profilban üres a portál token
doctor-step0-profile-ok-summary = a `{ $context }` goat kontextus profilanyaga olvasható
doctor-step0-client-absent-summary = egyetlen támogatott mesh kliens sem válaszolt
doctor-step0-client-absent-hint = telepítsd a goatnet által biztosított mesh klienst, indítsd el a démonját, majd futtasd újra: `goat doctor`
doctor-step0-client-stopped-summary = a telepített mesh kliens nem adott használható állapotot
doctor-step0-client-stopped-hint = ellenőrizd: `netbird status -d` vagy `goat client status --detail`; indítsd el a telepített démont (NetBirdnél: `sudo netbird service start`), majd futtasd újra: `goat doctor`
doctor-step0-netbird-ok-summary = a NetBird démon válaszolt (jelentett profil: `{ $profile }`)
doctor-step0-goat-client-ok-summary = a goat-clientd válaszolt (állapot: `{ $state }`)
doctor-step0-management-connected-summary = a NetBird szerint a management kapcsolódva van
doctor-step0-management-disconnected-summary = a NetBird szerint a management nincs kapcsolódva
doctor-step0-management-disconnected-hint = ellenőrizd a jelentett management URL-t és ezt: `netbird status -d`; a már felépült alagút management-kiesés alatt is működhet
doctor-step0-management-unknown-summary = a beállított management végpontra nem sikerült helyileg kapcsolatot létesíteni
doctor-step0-management-unknown-hint = ellenőrizd: `netbird status -d` vagy `goat client diagnostics`; a doctor nem találgat management URL-t
doctor-step0-tunnel-down-summary = a NetBird nem jelez mesh-címet
doctor-step0-tunnel-down-hint = futtasd: `netbird status -d`; ha az eszköz regisztrálva van és a démon válaszol, futtasd: `goat heal`, majd futtasd újra: `goat doctor`
doctor-step0-tunnel-address-summary = a NetBird mesh-címe: { $address }; következik a router elérhetőségének ellenőrzése
doctor-step0-goat-client-no-bundle-summary = a goat-clientd-nek nincs importált csomagja (bundle)
doctor-step0-goat-client-no-bundle-hint = futtasd: `goat client bundle import <bundle.cbor>` a kliens regisztrációs csomagjával, csatlakoztasd a klienst, majd futtasd újra: `goat doctor`
doctor-step0-goat-client-connected-summary = a goat-clientd csatlakozott alagutat jelez; következik a router elérhetőségének ellenőrzése
doctor-step0-goat-client-outer-disabled-summary = a goat-clientd külső alagútja netbird-only módban le van tiltva
doctor-step0-goat-client-down-summary = a goat-clientd alagútállapota: `{ $state }`
doctor-step0-goat-client-down-hint = futtasd: `goat client diagnostics`, majd `goat client up`, ha a csomag be van töltve; futtasd újra: `goat doctor`
doctor-step0-goat-client-inner-ok-summary = a goat-clientd belső mesh-e csatlakozott
doctor-step0-goat-client-inner-disabled-summary = a goat-clientd belső mesh-e wg-cp0-only módban le van tiltva
doctor-step0-goat-client-inner-down-summary = a goat-clientd belső mesh-állapota: `{ $state }`
doctor-step0-goat-client-inner-down-hint = futtasd: `goat client diagnostics` a belső mesh vizsgálatához, majd helyreállítás után futtasd újra: `goat doctor`
doctor-step0-management-reachable-summary = a management végpont HTTP { $status } választ adott
doctor-step0-management-auth-summary = a management HTTPS végpont elérhető (HTTP { $status }: a próbához engedély kell)
doctor-step0-management-unexpected-summary = a management végpont váratlan HTTP { $status } választ adott
doctor-step0-management-unexpected-hint = ellenőrizd a beállított management URL-t, és kérd meg a goatnet operátort, hogy vizsgálja meg ezt a választ
doctor-step0-management-unreachable-summary = a beállított management végpont nem érhető el: { $error }
doctor-step0-management-unreachable-hint = ellenőrizd a DNS-t és a hálózati hozzáférést a kliens által jelzett URL-hez; a meglévő alagút még működhet
doctor-step0-clock-ok-summary = a helyi óra { $seconds } másodpercen belül egyezik a management HTTP idejével
doctor-step0-clock-skew-summary = a helyi óra { $seconds } másodperccel tér el a management HTTP idejétől
doctor-step0-clock-skew-hint = szinkronizáld az eszköz óráját, majd futtasd újra: `goat doctor`; hibás óra mellett a tanúsítvány-ellenőrzés elbukhat
doctor-step0-clock-unknown-summary = az óraeltérés nem mérhető a management HTTP Date fejlécéből
doctor-step0-clock-unknown-hint = ha a TLS- vagy tanúsítvány-ellenőrzés hibázik, ellenőrizd az eszköz időszinkronját
error-clock-skew-summary = az eszköz órája túl messze jár a menedzsment idejétől
error-doctor-inconclusive-summary = a doctor hibát észlelt, de az okát nem sikerült igazolni
error-portal-forbidden-summary = a portál megtagadta e fiók hozzáférését a vizsgált API-hoz
doctor-step4-forbidden-summary = a portál HTTP 403-at adott ehhez a profilhoz
doctor-step4-forbidden-hint = ez a hitelesítő adat elérte a portált, de nincs hozzáférése a vizsgált API-hoz; nézd meg: `goat profile show`, és kérd meg a goatnet operátort, hogy ellenőrizze a jogosultságot
doctor-step4-decode-summary = a portál olyan választ adott, amelyet ez a goat nem tud olvasni
doctor-step4-decode-hint = { $err } — frissítsd a goatot a goatnet által kiszolgált verzióra, majd futtasd újra: `goat doctor`
doctor-step1-control-only-summary = csak a vezérlőszerverhez csatlakozott — egyik a(z) { $total } peer közül sem érhető el tunnelen
doctor-step1-control-only-hint = ez a hálózat kiengedi a TCP 443-at, de blokkolja a mesh adatportjait, így a meshen semmi (a bejelentkezési oldalakat is beleértve) nem érhető el erről az eszközről. Kérd meg a hálózati csapatot, hogy engedélyezze a kimenő TCP- és UDP-forgalmat ide: { $relays }, valamint az UDP 3478-at ide: { $stun }; az eszközök közti közvetlen UDP is ajánlott. Ellenőrzéshez csatlakozz telefon hotspothoz, és futtasd: `netbird status` — a peereknek ekkor csatlakozniuk kell.
doctor-offmesh-dashboard-hint = relay flotta (külső nézőpont): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = a helyi tanúsítvány, a hozzá tartozó kulcs és a CA-csomag feldolgozható; a tanúsítvány az érvényességi időn belül van
doctor-step2-material-failed-summary = a helyi TLS-anyag nem használható: { $file }
doctor-step2-material-failed-hint = { $err } — vizsgáld meg a megnevezett fájlt; állítsd vissza a kiadott csomagból, vagy rotáld a profilt
doctor-step2-material-invalid-hint = { $file } nem érvényes X.509 PEM tanúsítvány; állítsd vissza a kiadott anyagot, vagy futtasd: `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = az mtls.key.pem érvénytelen, vagy nem egyezik az mtls.cert.pem fájllal; állítsd vissza a kiadott anyagot, vagy futtasd: `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = a helyi tanúsítvány kívül esik az érvényességi időn
doctor-step2-cert-time-hint = futtasd: `goat profile show`; nézd meg a fenti óraeredményt, majd kérj az operátortól új csomagot, és ha a tanúsítvány lejárt, futtasd: `goat profile rotate <new-bundle.cbor>`
doctor-skipped-no-endpoint-summary = kihagyva — a profile.toml-ból hiányzik: { $field }
doctor-skipped-no-profile-summary = kihagyva — ehhez a kontextushoz nincs olvasható profil-végpont
doctor-skipped-no-portal-token-summary = kihagyva — ehhez a kontextushoz nincs olvasható portáltoken
status-ztol-tcp-ok = a router portja ({ $host }:{ $port }) fogad TCP-t (csak a port — lásd: session)
status-session-ok = a zenoh mTLS munkamenet { $ms } ms alatt megnyílt (a router elfogadta ezt a tanúsítványt)
status-session-skipped = a munkamenet nem lett megkísérelve (mesh-en kívül; lásd: ztol)
status-session-material-missing = a zenoh munkamenet nem lett megkísérelve: { $detail }
status-session-config-failed = a zenoh munkamenet konfigurációja sikertelen: { $err }
status-session-failed = a zenoh-munkamenet nem nyílt meg: { $err } — futtasd a `goat doctor` parancsot
status-session-timeout = a zenoh-munkamenet { $secs } mp alatt sem nyílt meg — a router portja fogadja a TCP-t, de egyetlen munkamenet sem jön létre (a router megakadt, a tanúsítványt elutasította, vagy óraeltérés van); futtasd a `goat doctor` parancsot
status-names-live = nevek élőben feloldva (a mesh DNS válaszolt)
status-names-ip-only = nevek nincsenek használatban (a profil végpontjai IP-literálok)
status-names-snapshot = nevek az aláírt pillanatkép szerint, sorozatszám: { $serial }, kor: { $age } (a mesh DNS nem érhető el)
status-names-snapshot-aging = nevek az aláírt pillanatkép szerint, sorozatszám: { $serial }, kor: { $age } — ELAVULT (a mesh DNS nem érhető el; javítsd a goat-coredns-t, utána a nevek maguktól frissülnek)
status-names-snapshot-daemon = nevek az aláírt pillanatkép szerint, sorozatszám: { $serial }, kor: { $age } — démon tárolója (a mesh DNS nem érhető el)
status-names-snapshot-daemon-aging = nevek az aláírt pillanatkép szerint, sorozatszám: { $serial }, kor: { $age } — démon tárolója, ELAVULT (a mesh DNS nem érhető el; javítsd a goat-coredns-t, utána a nevek maguktól frissülnek)
status-names-failed = a mesh DNS nem érhető el, és a pillanatkép-tartalék sem áll rendelkezésre — { $reason }
status-pqc-on = wireguard PQC (rosenpass) BE — { $qr }/{ $connected } csatlakozott peer kvantumrezisztens
status-pqc-off = wireguard PQC (rosenpass) KI — klasszikus wireguard-kézfogások
status-pqc-unknown = wireguard PQC állapota ismeretlen — { $reason }
status-device-none = eszközszintű névvédelem KI — nincs goat-clientd névtároló (a goat parancsoknak van tartalékuk; engedélyezés: name-fallback-resolver.md §9.2)
status-device-broken = van goat-clientd névtároló (sorozatszám: { $serial }), de a továbbítója NEM válaszol itt: { $addr } — a démon leállt, vagy a névkezelő alrendszer megakadt
status-device-active = eszközszintű névvédelem AKTÍV — a goat-clientd az OS mesh DNS elé ül itt: { $addr } (tároló sorozatszáma: { $serial }, { $grade })
status-device-not-fronted = goat-clientd továbbító fut itt: { $addr } (tároló sorozatszáma: { $serial }, { $grade }) — az OS elé nincs kapcsolva; a böngészők közvetlenül a mesh DNS-t használják
status-device-fronting-unknown = goat-clientd továbbító fut itt: { $addr } (tároló sorozatszáma: { $serial }, { $grade }) — az OS elé kapcsolás állapota ezen a platformon nem állapítható meg
status-names-observed = nevek NEM KANONIKUSAK (ad hoc): megfigyelt rekord, kor: { $age } — nincs az aláírt pillanatképben (a mesh DNS nem érhető el; rögzítsd ezt a rekordot)
status-names-selfclaim = nevek ellenőrzött ÖNBEJELENTÉS alapján, kor: { $age } — a név saját horgonya írta alá (a mesh DNS nem érhető el)
doctor-names-note = pillanatképből, sorozatszám: { $serial }, kor: { $age }
doctor-names-selfclaim-note = ellenőrzött ÖNBEJELENTÉS alapján, kor: { $age } — a név saját horgonya írta alá
doctor-names-selfclaim-hint = a mesh DNS nem érhető el — ez a lépés ellenőrzött ÖNBEJELENTÉS alapján oldódott fel (kor: { $age }): a név saját horgonya írta alá a kötést, és a gyorsítótárazott pillanatkép peer_bindings mezője engedélyezi. Aláírt, de nem élő; a mesh DNS javítása szünteti meg.
doctor-names-observed-note = MEGFIGYELT (ad hoc) rekordból, kor: { $age } — nem kanonikus
doctor-names-observed-hint = a mesh DNS nem érhető el — ez a lépés NEM KANONIKUS megfigyelt rekordból oldódott fel (kor: { $age }): a név csak élőben létezik, a site registryben sosem rögzítették. Rögzítsd (group_vars registry → generátor → újraaláírás), különben lejár; a mesh DNS javítása szünteti meg a figyelmeztetést.
doctor-names-hint = a mesh DNS nem érhető el — ez a lépés az aláírt névpillanatképből oldódott fel (sorozatszám: { $serial }, kor: { $age }). A fabric tartalék nevekkel elérhető marad; javítsd a mesh DNS-t (goat-coredns az mgmt hoszton), hogy megszűnjön.
storm-brake-plan-header = Viharfék-terv (ADR 1081, 4. réteg — kézi):
storm-brake-plan-target = mgmt hoszt { $user }@{ $host }  (kontextus { $context })
storm-brake-step-flush = signal konténer újraindítása `{ $container }` (eldobja a zombi streameket)
storm-brake-step-tighten = L2 limiter szűkítése `{ $container }` (felezi a rate-limitet, emeli a neg-cache TTL-t) — { $window } perc, az operátor állítja vissza; kimarad, ha az L2 nincs telepítve
storm-brake-step-shed = nem létfontosságú publisher unit szüneteltetése `{ $unit }`
storm-brake-shed-none = (nincs megadott leállítandó unit — állítsd be a storm_brake_shed_units értéket a profile.toml-ban, vagy add meg a --shed-unit kapcsolót)
storm-brake-announce-note = egy OperatorActionV1 küldése az audit naplóba lépésenkénti eredményekkel
storm-brake-dryrun-note = próbafuttatás — semmi sem futott le.
storm-brake-triggers-header = Indítsd, ha a relay-health host-net sáv ezt mutatja:
storm-brake-trigger-futility = signaling futility arány >= 0.5 legalább 100 üzenet/perc mellett (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = uplink veszteség >= 5%, 10 percig tartósan (wan.loss_pct)
storm-brake-triggers-band-url = host sáv: relay-health.{ $site }.<zone> (lásd docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Megjegyzés: a signal flush önmagában NEM állítja helyre a meshet. A gyógymód az 1–2. réteg (kliens backoff + signal shedding); a fék csak időt nyer a helyreálláshoz.
storm-brake-confirm = Indítod a viharféket ezen: { $user }@{ $host }? Újraindítja a signal konténert, és szünetelteti a megadott publishereket. [y/N]
storm-brake-aborted = viharfék megszakítva (nincs megerősítés)
storm-brake-executing = viharfék indítása ezen: { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = sikertelen
storm-brake-outcome-skipped = kihagyva
storm-brake-summary = viharfék kész: { $ok } ok, { $failed } sikertelen, { $skipped } kihagyva
storm-brake-flush-failed = FLUSH sikertelen — a signal konténer nem indult újra, ezért mesh-helyreállás nem várható: { $reason }
storm-brake-mgmt-unresolved = mgmt hoszt nem oldható fel — add meg a --mgmt-host kapcsolót, állítsd be a mgmt_ssh_host értéket a kontextus profile.toml-jában, vagy exportáld a GOAT_MGMT_SSH_HOST változót
storm-brake-audit-note = operátori művelet rögzítve az audit naplóban (végrehajtó { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = a portál HTTP 404-gyel válaszolt erre: `{ $path }`: ott nincs regisztrálva
error-portal-rate-limited-summary = a portál HTTP 429-cel válaszolt erre: `{ $path }`: túl sok kérés, próbálja később
error-portal-server-error-summary = a portál HTTP { $status } kóddal válaszolt erre: `{ $path }`: hiba a portál oldalán
error-portal-refused-summary = a portál HTTP { $status } kóddal válaszolt erre: `{ $path }`: a kérés elutasítva

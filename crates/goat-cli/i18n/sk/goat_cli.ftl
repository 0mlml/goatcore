### goat-cli — Slovak (sk). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = žiadny profil pre kontext `{ $context }`
error-off-mesh-summary = mimo ZTOL — bez pripojenia k sieti mesh
error-router-unreachable-summary = smerovač Zenoh na `{ $endpoint }` je nedostupný
error-cert-expired-summary = certifikát mTLS vypršal alebo ho smerovač odmietol
error-portal-unreachable-summary = API portálu na `{ $endpoint }` je nedostupné
error-token-invalid-summary = portál odmietol token Bearer
error-topic-conflict-summary = topic `{ $topic }` už existuje
error-bundle-invalid-summary = overenie podpisu alebo platnosti balíka bundle zlyhalo

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = koncový bod smerovača má nesprávny formát
doctor-step1-endpoint-malformed-hint = očakávalo sa `<proto>/<host>:<port>`; prijaté `{ $endpoint }`
doctor-step1-ok-summary = pripojenie TCP k hostiteľovi smerovača { $host }:{ $port } úspešné
doctor-step1-conn-failed-summary = pripojenie TCP k hostiteľovi smerovača zlyhalo
doctor-step1-conn-failed-hint = { $err } — spustite netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); skontrolujte `netbird status`
doctor-step1-timeout-summary = vypršal časový limit pripojenia TCP k hostiteľovi smerovača
doctor-step1-timeout-hint = hostiteľ smerovača { $host }:{ $port } nedostupný za { $ms }ms — pravdepodobne mimo mesh
doctor-step2-endpoint-malformed-hint = opravte vydavateľa bundle (--goat-cli-router-endpoint musí byť `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM koreňov CA chýba alebo je nečitateľný
doctor-step2-ca-missing-hint = { $err } — znovu spustite `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = smerovač { $host }:{ $port } prijíma TCP (TLS overené v kroku 3)
doctor-step2-refused-summary = TCP smerovača odmietnuté
doctor-step2-refused-hint = { $err } — smerovač môže byť mimo prevádzky; skontrolujte nasadenie Zenoh na mgmt-host
doctor-step2-timeout-summary = vypršal časový limit TCP smerovača
doctor-step2-timeout-hint = smerovač { $host }:{ $port } nedostupný za { $ms }ms
doctor-step3-config-failed-summary = zostavenie konfigurácie zenoh zlyhalo
doctor-step3-config-failed-hint = neplatný koncový bod `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = zostavenie konfigurácie TLS zenoh zlyhalo
doctor-step3-tls-config-failed-hint = { $err } — pravdepodobne odchýlka verzie zenoh; overte zhodu závislostí
doctor-step3-ok-summary = relácia mTLS otvorená — certifikát prijatý ACL smerovača
doctor-step3-open-failed-summary = otvorenie relácie zenoh zlyhalo
doctor-step3-open-failed-hint = { $err } — certifikát môže byť expirovaný (spustite `goat profile show`) alebo odmietnutý ACL smerovača (nezhoda URI-SAN)
doctor-step3-timeout-summary = vypršal časový limit otvorenia relácie zenoh
doctor-step3-timeout-hint = smerovač nedokončil handshake za { $secs }s — posun hodín, nesprávny certifikát alebo zahltenie
doctor-step4-ok-summary = portál vrátil { $n } riadkov organizácie
doctor-step4-token-rejected-summary = portál odmietol token Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — token expiroval alebo bol zrušený; znovu sa zaregistrujte cez operator-onboard.yml
doctor-step4-request-failed-summary = požiadavka na portál zlyhala
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = vypršal časový limit požiadavky na portál
doctor-step4-timeout-hint = žiadna odpoveď za { $secs }s — skontrolujte, či je { $endpoint } dostupný z mesh
doctor-skipped-summary = preskočené — predchádzajúci krok zlyhal (spustite znovu s --all na vynútenie)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Jednotný operátorský príkazový riadok pre dátovú štruktúru goat.
help-long-about = goat — CLI určené pre operátorov, ktoré skladá panoscope-core do jedného nástroja: inicializácia/opätovné použitie profilu, doctor dostupnosti so znalosťou ZTOL, prehliadanie portálu organizácií, CRUD topic, pub/sub, delegovanie stethoscope, interaktívny zostavovač príkazov a rozprávanie kaskády štruktúry.
help-flag-profile-dir = Prepíš adresár profilu (predvolene: $GOAT_PROFILE_DIR alebo ~/.goat).
help-flag-context = Prepíš aktívny kontext (predvolene: premenná $GOAT_CONTEXT, potom current_context v config.toml, potom „default").
help-flag-narrate = Zobraz kaskádu štruktúry, ktorú spustí príkaz meniaci stav.
help-flag-verbose = Povoľ podrobný ladiaci výstup (alebo nastav RUST_LOG=goat=debug).
help-flag-color = Režim farieb pre výstup terminálu (auto|always|never).
help-flag-lang = Jazyk pre text určený ľuďom (napr. en, uk). Vracia sa na GOAT_LANG, potom na miestne nastavenie OS, potom na angličtinu.
help-cmd-profile = Spravuj lokálny profil goat (certifikát mTLS + token portálu + sadu koncových bodov).
help-cmd-context = Spravuj kontexty (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Spusti krokový snímač dostupnosti voči štruktúre.
help-cmd-doctor-long = Krokový snímač dostupnosti voči štruktúre: členstvo v ZTOL → smerovač Zenoh → prijatie certifikátu → portal. Každý neúspešný krok vydá konkrétny tip na nápravu.
help-cmd-vendor = Prehliadaj organizácie registrované v portáli.
help-cmd-topic = Vypíš, zobraz, vytvor alebo odstráň topic.
help-cmd-pub = Publikuj náklad na kľúč topic.
help-cmd-sub = Prihlás sa na výraz kľúča topic.
help-cmd-inspect = Deleguj požiadavku na diagnostický verdikt na stethoscope.
help-cmd-ui = Interaktívny zlúčený výber: prehliadaj zoznam deklarovaných ⊕ živých a preskoč na šošovku Diagnose / Tail na vybranom cieli.
help-cmd-compose = Interaktívny TUI na zostavovanie príkazov.
help-cmd-narrate = Reflexia kaskády štruktúry nad nedávnou aktivitou. Zatiaľ nedostupné (každodenná cesta je --narrate na príkaze meniacom stav).
help-cmd-completion = Vypíš skript dopĺňania shellu na stdout.
help-cmd-explain = Vyhľadaj význam chybového kódu + nápravu vo vstavanom dokumente `goat-cli-error-codes.md` (vzor cargo --explain).
help-cmd-mock = Publikuj syntetické vzorky na topic. Predvolene: triviálny hello-world JSON pri 1Hz. Zadaj `--format <.fds>` + `--message <Name>`, aby si namiesto toho zakódoval protobuf dodaný operátorom.
help-cmd-status = Syntéza stavu: dostupnosť ZTOL + portal /api/health + expirácia lokálneho certifikátu, tri riadky glyph alebo jeden objekt --json.
help-cmd-services = Otestuj každú hostovanú službu v `ops/serve/services.yaml` a vykresli jeden riadok glyph na službu (alebo jeden objekt --json).
help-cmd-client = Obaľ lokálny daemon goat-clientd cez jeho JSON-RPC IPC socket (`~/.goat-client/goat-clientd.sock`): status / up / down / import bundle.
help-cmd-plugin = Objavovanie rozšírení založené na PATH (štýl kubectl). Akýkoľvek spustiteľný súbor `goat-<name>` na PATH možno vyvolať ako `goat <name>`; `goat plugin list` vymenuje nainštalované zásuvné moduly, `goat plugin which <name>` vyrieši cestu.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = koncový bod smerovača má nesprávny formát: `{ $endpoint }`
status-ztol-refused = TCP odmietnuté smerovačom { $host }:{ $port }: { $err }
status-ztol-unreachable = mimo mesh (smerovač { $host }:{ $port } nedostupný za { $ms }ms)
status-portal-ok = portál /api/health 2xx
status-portal-failed = portál /api/health zlyhal: { $err }
status-portal-timeout = vypršal časový limit portálu /api/health za { $secs }s
status-cert-read-failed = čítanie certifikátu zlyhalo: { $err }
status-cert-pem-parse-failed = parsovanie PEM certifikátu zlyhalo: { $err }
status-cert-wrong-tag = prvý blok PEM je { $tag }, očakávaný CERTIFICATE
status-cert-x509-parse-failed = parsovanie x509 zlyhalo: { $err }
status-cert-expired = certifikát VYPRŠAL v unix={ $not_after }, teraz={ $now }
status-cert-valid = certifikát platný; vyprší za { $days_left }d (unix={ $not_after })
inspect-finding-label = zistenie ({ $severity }): { $label } — { $detail }
inspect-remedy-label = náprava: { $title } — { $detail }
inspect-no-outcomes = (žiadne výsledky — cieľ nezodpovedal žiadnej kontrole)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put prijatý na `{ $key }` ({ $bytes } bajtov)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskáda ustálená — { $summary } ({ $n } { $n ->
        [one] dôsledok
        [few] dôsledky
        [many] dôsledku
       *[other] dôsledkov
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Sprievodca prvým spustením: vyber sandbox alebo reálne prostredie, over funkčnosť, nasmeruj na tútora.
help-cmd-tutor = Samostatná praktická prehliadka štruktúry vlastným tempom (v štýle vimtutor; obnoviteľná).
help-cmd-docs = Prehliadaj dokumentáciu goat alebo vypíš úplnú referenciu príkazov (--markdown).
help-cmd-hello = Prejde obojsmerný hello cez štruktúru od začiatku do konca (sandbox alebo reálne prostredie).
flourish-all-green = 🐐 všetko zelené — štruktúra nominálna

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = akcia len pre operátorov — nie ste oprávnený (`goat admin` je pre ne-adminov nefunkčný)
help-flag-narrate-json = Emitovať kaskádu substrátu ako NDJSON (jeden objekt na dôsledok) pre záznamy incidentov + skriptovanie. Implikuje --narrate.
help-cmd-admin = Administrácia len pre operátorov: životný cyklus organizácie, onboard/revoke/rotate, odpojenie mesh-peer, provisioning zariadení + operator-tier, triage LAN. Autorizované vašimi operátorskými prihlasovacími údajmi (server-side cez portal/NetBird token scope); nefunkčné pre ne-adminov — neoprávnené volanie nič nezmení a zlyhá s `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profil pre kontext `{ $context }` existuje, ale tento používateľ ho nemôže čítať (prístup zamietnutý)
error-portal-bad-response-summary = portál odpovedal, ale tento build goat nedokázal spracovať odpoveď
doctor-step0-ok-summary = lokálne nastavenie je pripravené na sieťové testy
doctor-step0-warn-summary = lokálne nastavenie obsahuje kontroly, ktoré sa nepodarilo potvrdiť
doctor-step0-fail-summary = lokálne nastavenie vyžaduje pozornosť
doctor-step0-profile-absent-summary = žiadny profil goat pre kontext `{ $context }`
doctor-step0-profile-new-hint = skontrolujte `goat context ls`; ak ide o prvé nastavenie, spustite `goat ca add <bundle-ca.pem>` a porovnajte jeho odtlačok s odtlačkom od vášho operátora, potom `goat profile init <bundle.cbor>`; znova spustite `goat doctor`
doctor-step0-profile-absent-hint = skontrolujte `goat context ls`; vyberte existujúci kontext cez `goat context use <name>` alebo spustite `goat profile init <bundle.cbor>` pre tento kontext; znova spustite `goat doctor`
doctor-step0-profile-denied-summary = profil goat v { $path } je nečitateľný
doctor-step0-profile-denied-hint = opravte vlastníctvo profilu alebo spustite ako vlastniaci používateľ; existujúci profil neinicializujte znova
doctor-step0-profile-incomplete-summary = súbor profilu goat { $file } chýba alebo je nečitateľný
doctor-step0-profile-incomplete-hint = obnovte uvedený súbor z vydaného balíka alebo nahraďte tento kontext cez `goat profile rotate <bundle.cbor>`; znova spustite `goat doctor`
doctor-step0-profile-invalid-summary = profil goat má nečitateľné alebo chýbajúce endpointy
doctor-step0-profile-empty-token-summary = profil goat má prázdny token portálu
doctor-step0-profile-ok-summary = kontext goat `{ $context }` má čitateľný profilový materiál
doctor-step0-client-absent-summary = žiadny podporovaný mesh klient neodpovedal
doctor-step0-client-absent-hint = nainštalujte mesh klienta dodaného vaším goatnetom, spustite jeho démona a znova spustite `goat doctor`
doctor-step0-client-stopped-summary = nainštalovaný mesh klient nevrátil použiteľný stav
doctor-step0-client-stopped-hint = skontrolujte `netbird status -d` alebo `goat client status --detail`; spustite nainštalovaného démona (`sudo netbird service start` pre NetBird) a znova spustite `goat doctor`
doctor-step0-netbird-ok-summary = démon NetBird odpovedal (nahlásený profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd odpovedal (stav `{ $state }`)
doctor-step0-management-connected-summary = NetBird hlási pripojený management
doctor-step0-management-disconnected-summary = NetBird hlási odpojený management
doctor-step0-management-disconnected-hint = skontrolujte nahlásenú URL managementu a `netbird status -d`; zavedený tunel môže počas výpadku managementu stále fungovať
doctor-step0-management-unknown-summary = nakonfigurovaný management endpoint sa lokálne nepodarilo nadviazať
doctor-step0-management-unknown-hint = skontrolujte `netbird status -d` alebo `goat client diagnostics`; doctor URL managementu neuhádne
doctor-step0-tunnel-down-summary = NetBird nehlási žiadnu adresu mesh
doctor-step0-tunnel-down-hint = spustite `netbird status -d`; ak je zariadenie zaregistrované a démon odpovedá, spustite `goat heal` a potom znova `goat doctor`
doctor-step0-tunnel-address-summary = NetBird hlási adresu mesh { $address }; ďalej sa kontroluje dostupnosť routera
doctor-step0-goat-client-no-bundle-summary = goat-clientd nemá importovaný bundle
doctor-step0-goat-client-no-bundle-hint = spustite `goat client bundle import <bundle.cbor>` s registračným bundlom klienta, pripojte klienta a potom znova spustite `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd hlási pripojený tunel; ďalej sa kontroluje dostupnosť routera
doctor-step0-goat-client-outer-disabled-summary = vonkajší tunel goat-clientd je v režime netbird-only vypnutý
doctor-step0-goat-client-down-summary = goat-clientd hlási stav tunela `{ $state }`
doctor-step0-goat-client-down-hint = spustite `goat client diagnostics`, potom `goat client up`, ak je bundle načítaný; znova spustite `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd hlási pripojenú vnútornú mesh
doctor-step0-goat-client-inner-disabled-summary = vnútorná mesh goat-clientd je v režime wg-cp0-only vypnutá
doctor-step0-goat-client-inner-down-summary = goat-clientd hlási stav vnútornej mesh `{ $state }`
doctor-step0-goat-client-inner-down-hint = spustite `goat client diagnostics` a skontrolujte vnútornú mesh; po jej obnovení znova spustite `goat doctor`
doctor-step0-management-reachable-summary = endpoint managementu odpovedal HTTP { $status }
doctor-step0-management-auth-summary = HTTPS endpoint managementu je dostupný (HTTP { $status } pre túto sondu vyžaduje autorizáciu)
doctor-step0-management-unexpected-summary = endpoint managementu vrátil neočakávaný HTTP { $status }
doctor-step0-management-unexpected-hint = skontrolujte nastavenú URL managementu a požiadajte operátora goatnetu, aby preveril túto odpoveď
doctor-step0-management-unreachable-summary = nastavený endpoint managementu je nedostupný: { $error }
doctor-step0-management-unreachable-hint = skontrolujte DNS a sieťový prístup k URL, ktorú hlási klient; existujúci tunel môže stále fungovať
doctor-step0-clock-ok-summary = lokálne hodiny sa líšia od HTTP času managementu o menej ako { $seconds } s
doctor-step0-clock-skew-summary = lokálne hodiny sa líšia od HTTP času managementu o { $seconds } s
doctor-step0-clock-skew-hint = synchronizujte hodiny zariadenia a potom znova spustite `goat doctor`; pri nesprávnych hodinách môže zlyhať overenie certifikátov
doctor-step0-clock-unknown-summary = odchýlku hodín sa nepodarilo zmerať z hlavičky HTTP Date managementu
doctor-step0-clock-unknown-hint = ak zlyhajú kontroly TLS alebo certifikátov, skontrolujte synchronizáciu času zariadenia
error-clock-skew-summary = hodiny zariadenia sa príliš líšia od času správy
error-doctor-inconclusive-summary = doctor zaznamenal chybu bez preukázanej príčiny
error-portal-forbidden-summary = portál odmietol tomuto účtu prístup k testovanému API
doctor-step4-forbidden-summary = portál vrátil HTTP 403 pre tento profil
doctor-step4-forbidden-hint = tento poverovací údaj sa dostal k portálu, ale nemá prístup k testovanému API; skontroluj `goat profile show` a požiadaj operátora goatnetu o overenie oprávnenia
doctor-step4-decode-summary = portál odpovedal v podobe, ktorú tento goat nevie prečítať
doctor-step4-decode-hint = { $err } — aktualizuj goat na verziu, ktorú poskytuje tento goatnet, a znova spusti `goat doctor`
doctor-step1-control-only-summary = pripojené iba k riadiacemu serveru — žiadny tunel k žiadnemu z { $total } peerov
doctor-step1-control-only-hint = táto sieť prepúšťa TCP 443 von, ale blokuje dátové porty mesh, takže z tohto zariadenia nie je dostupné nič na mesh (vrátane prihlasovacích stránok). Požiadaj sieťový tím o povolenie odchádzajúceho TCP a UDP na { $relays } a UDP 3478 na { $stun }; odporúča sa aj priame UDP medzi zariadeniami. Na overenie sa pripoj cez hotspot telefónu a spusti `netbird status`: peery by sa mali pripojiť.
doctor-offmesh-dashboard-hint = flotila relay (externý pohľad): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokálny certifikát, zodpovedajúci kľúč a balík CA sa dajú spracovať; certifikát je v období platnosti
doctor-step2-material-failed-summary = lokálny TLS materiál { $file } sa nepodarilo použiť
doctor-step2-material-failed-hint = { $err } — skontroluj uvedený súbor; obnov ho z vydaného balíka alebo rotuj profil
doctor-step2-material-invalid-hint = { $file } nie je platný certifikát X.509 PEM; obnov vydaný materiál alebo spusti `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem je neplatný alebo nezodpovedá mtls.cert.pem; obnov vydaný materiál alebo spusti `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokálny certifikát je mimo obdobia platnosti
doctor-step2-cert-time-hint = spusti `goat profile show`; pozri výsledok kontroly hodín vyššie, potom požiadaj operátora o nový balík a ak certifikát vypršal, spusti `goat profile rotate <new-bundle.cbor>`
doctor-skipped-no-endpoint-summary = preskočené — v profile.toml chýba { $field }
doctor-skipped-no-profile-summary = preskočené — pre tento kontext nie sú čitateľné koncové body profilu
doctor-skipped-no-portal-token-summary = preskočené — pre tento kontext nie je čitateľný token portálu
status-ztol-tcp-ok = port routera { $host }:{ $port } prijíma TCP (iba port — pozri session)
status-session-ok = zenoh mTLS session otvorená za { $ms }ms (router prijal tento certifikát)
status-session-skipped = session sa nepokúšala otvoriť (mimo mesh; pozri ztol)
status-session-material-missing = zenoh session sa nepokúšala otvoriť: { $detail }
status-session-config-failed = konfigurácia zenoh session zlyhala: { $err }
status-session-failed = zenoh session sa neotvorila: { $err } — spustite `goat doctor`
status-session-timeout = zenoh session sa neotvorila do { $secs }s — port routera prijíma TCP, ale žiadna session sa nedokončí (router zaseknutý, certifikát odmietnutý alebo posun hodín); spustite `goat doctor`
status-names-live = mená živé (mesh DNS odpovedal)
status-names-ip-only = mená neoverené (endpointy profilu sú IP adresy)
status-names-snapshot = mená podľa podpísaného snapshotu so sériou { $serial }, vek { $age } (mesh DNS nedostupný)
status-names-snapshot-aging = mená podľa podpísaného snapshotu so sériou { $serial }, vek { $age } — ZASTARANÉ (mesh DNS nedostupný; opravte goat-coredns, mená sa potom obnovia automaticky)
status-names-snapshot-daemon = mená podľa podpísaného snapshotu so sériou { $serial }, vek { $age } — úložisko daemona (mesh DNS nedostupný)
status-names-snapshot-daemon-aging = mená podľa podpísaného snapshotu so sériou { $serial }, vek { $age } — úložisko daemona, ZASTARANÉ (mesh DNS nedostupný; opravte goat-coredns, mená sa potom obnovia automaticky)
status-names-failed = mesh DNS nedostupný a záložný snapshot nie je k dispozícii — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ZAPNUTÉ — { $qr }/{ $connected } pripojených peerov odolných voči kvantovým útokom
status-pqc-off = wireguard PQC (rosenpass) VYPNUTÉ — klasické wireguard handshaky
status-pqc-unknown = wireguard PQC neznáme — { $reason }
status-device-none = ochrana mien na celom zariadení VYPNUTÁ — chýba úložisko mien goat-clientd (príkazy goat majú stále fallback; zapnite podľa name-fallback-resolver.md §9.2)
status-device-broken = úložisko mien goat-clientd existuje (séria { $serial }), ale jeho forwarder NEODPOVEDÁ na { $addr } — daemon nebeží alebo je podsystém mien zaseknutý
status-device-active = ochrana mien na celom zariadení AKTÍVNA — goat-clientd predradený OS mesh DNS na { $addr } (séria úložiska { $serial }, { $grade })
status-device-not-fronted = forwarder goat-clientd beží na { $addr } (séria úložiska { $serial }, { $grade }) — OS nie je predradený; prehliadače používajú priamo mesh DNS
status-device-fronting-unknown = forwarder goat-clientd beží na { $addr } (séria úložiska { $serial }, { $grade }) — stav predradenia OS sa na tejto platforme nedá zistiť
status-names-observed = mená NEKANONICKÉ (ad hoc): pozorovaný záznam, vek { $age } — nie je v podpísanom snapshote (mesh DNS nedostupný; záznam zakotvite do registra)
status-names-selfclaim = mená cez overený SELF-CLAIM, vek { $age } — podpísané vlastným anchorom mena (mesh DNS nedostupný)
doctor-names-note = cez snapshot so sériou { $serial }, vek { $age }
doctor-names-selfclaim-note = cez overený SELF-CLAIM, vek { $age } — podpísané vlastným anchorom mena
doctor-names-selfclaim-hint = mesh DNS je nedostupný — tento krok sa vyriešil cez overený SELF-CLAIM (vek { $age }): väzbu podpísal vlastný anchor mena a peer_bindings z cache-ovaného snapshotu ju povoľujú. Podpísané, ale nie živé; opravte mesh DNS, aby toto zmizlo.
doctor-names-observed-note = cez POZOROVANÝ (ad hoc) záznam, vek { $age } — nekanonický
doctor-names-observed-hint = mesh DNS je nedostupný — tento krok sa vyriešil cez NEKANONICKÝ pozorovaný záznam (vek { $age }): meno je len živé, nikdy nebolo zapísané do registra lokality. Zapíšte ho (group_vars registry → generátor → opätovné podpísanie), inak vyexpiruje; opravte mesh DNS, aby toto zmizlo.
doctor-names-hint = mesh DNS je nedostupný — tento krok sa vyriešil cez podpísaný snapshot mien (séria { $serial }, vek { $age }). Fabric zostáva dostupný cez záložné mená; opravte mesh DNS (goat-coredns na mgmt hoste), aby toto zmizlo.
storm-brake-plan-header = Plán brzdy búrky (ADR 1081 vrstva 4 — manuálne):
storm-brake-plan-target = mgmt hostiteľ { $user }@{ $host }  (kontext { $context })
storm-brake-step-flush = reštart signalizačného kontajnera `{ $container }` (zahodí zombie streamy)
storm-brake-step-tighten = pritiahnutie L2 limitera na `{ $container }` (polovičný rate-limit, vyššie neg-cache TTL) — { $window } min, späť vracia operátor; preskočí sa, ak L2 nie je nasadená
storm-brake-step-shed = pozastavenie nepodstatnej publisher jednotky `{ $unit }`
storm-brake-shed-none = (nie sú deklarované žiadne shed jednotky — nastav storm_brake_shed_units v profile.toml alebo zadaj --shed-unit)
storm-brake-announce-note = zapíš jeden OperatorActionV1 do auditnej stopy s výsledkami jednotlivých krokov
storm-brake-dryrun-note = nanečisto — nič sa nevykonalo.
storm-brake-triggers-header = Spusti, keď pásmo host-net v relay-health ukazuje:
storm-brake-trigger-futility = pomer márnych signalizácií >= 0.5 pri >= 100 správ/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = strata uplinku >= 5 % trvajúca 10 min (wan.loss_pct)
storm-brake-triggers-band-url = pásmo hostiteľa: relay-health.{ $site }.<zone> (pozri docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Pozn.: samotný flush signalizácie NEOBNOVÍ mesh. Liekom sú vrstvy 1-2 (client backoff + signal shedding); brzda len získa čas na zotavenie.
storm-brake-confirm = Spustiť brzdu búrky na { $user }@{ $host }? Reštartuje signalizačný kontajner a pozastaví deklarovaných publisherov. [y/N]
storm-brake-aborted = brzda búrky zrušená (bez potvrdenia)
storm-brake-executing = spúšťam brzdu búrky na { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = zlyhalo
storm-brake-outcome-skipped = preskočené
storm-brake-summary = brzda búrky dokončená: { $ok } ok, { $failed } zlyhalo, { $skipped } preskočené
storm-brake-flush-failed = FLUSH zlyhal — signalizačný kontajner sa nereštartoval, obnova mesh sa nečaká: { $reason }
storm-brake-mgmt-unresolved = mgmt hostiteľ sa nepodarilo určiť — zadaj --mgmt-host, nastav mgmt_ssh_host v profile.toml kontextu alebo exportuj GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operátorská akcia zapísaná do auditnej stopy (aktér { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portál odpovedal HTTP 404 pre `{ $path }`: nie je tam registrované
error-portal-rate-limited-summary = portál odpovedal HTTP 429 pre `{ $path }`: príliš veľa požiadaviek, skúste neskôr
error-portal-server-error-summary = portál odpovedal HTTP { $status } pre `{ $path }`: chyba na strane portálu
error-portal-refused-summary = portál odpovedal HTTP { $status } pre `{ $path }`: požiadavka zamietnutá

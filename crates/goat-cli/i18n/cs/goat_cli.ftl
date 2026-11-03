### goat-cli — Czech (cs). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = žádný profil pro kontext `{ $context }`
error-off-mesh-summary = mimo ZTOL — bez připojení k síti mesh
error-router-unreachable-summary = směrovač Zenoh na `{ $endpoint }` je nedostupný
error-cert-expired-summary = certifikát mTLS vypršel nebo byl odmítnut směrovačem
error-portal-unreachable-summary = API portálu na `{ $endpoint }` je nedostupné
error-token-invalid-summary = portál odmítl token Bearer
error-topic-conflict-summary = topic `{ $topic }` již existuje
error-bundle-invalid-summary = ověření podpisu nebo platnosti balíčku bundle selhalo

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = koncový bod směrovače má chybný formát
doctor-step1-endpoint-malformed-hint = očekáváno `<proto>/<host>:<port>`; obdrženo `{ $endpoint }`
doctor-step1-ok-summary = připojení TCP k hostiteli směrovače { $host }:{ $port } úspěšné
doctor-step1-conn-failed-summary = připojení TCP k hostiteli směrovače selhalo
doctor-step1-conn-failed-hint = { $err } — spusťte netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); zkontrolujte `netbird status`
doctor-step1-timeout-summary = vypršel časový limit připojení TCP k hostiteli směrovače
doctor-step1-timeout-hint = hostitel směrovače { $host }:{ $port } nedostupný za { $ms }ms — pravděpodobně mimo mesh
doctor-step2-endpoint-malformed-hint = opravte vydavatele bundle (--goat-cli-router-endpoint musí být `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM kořenů CA chybí nebo je nečitelný
doctor-step2-ca-missing-hint = { $err } — znovu spusťte `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = směrovač { $host }:{ $port } přijímá TCP (TLS ověřeno v kroku 3)
doctor-step2-refused-summary = TCP směrovače odmítnuto
doctor-step2-refused-hint = { $err } — směrovač může být mimo provoz; zkontrolujte nasazení Zenoh na mgmt-host
doctor-step2-timeout-summary = vypršel časový limit TCP směrovače
doctor-step2-timeout-hint = směrovač { $host }:{ $port } nedostupný za { $ms }ms
doctor-step3-config-failed-summary = sestavení konfigurace zenoh selhalo
doctor-step3-config-failed-hint = neplatný koncový bod `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = sestavení konfigurace TLS zenoh selhalo
doctor-step3-tls-config-failed-hint = { $err } — pravděpodobně odchylka verze zenoh; ověřte shodu závislostí
doctor-step3-ok-summary = relace mTLS otevřena — certifikát přijat ACL směrovače
doctor-step3-open-failed-summary = otevření relace zenoh selhalo
doctor-step3-open-failed-hint = { $err } — certifikát může být prošlý (spusťte `goat profile show`) nebo odmítnut ACL směrovače (neshoda URI-SAN)
doctor-step3-timeout-summary = vypršel časový limit otevření relace zenoh
doctor-step3-timeout-hint = směrovač nedokončil handshake za { $secs }s — posun hodin, špatný certifikát nebo zahlcení
doctor-step4-ok-summary = portál vrátil { $n } řádků organizace
doctor-step4-token-rejected-summary = portál odmítl token Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — token vypršel nebo byl odvolán; znovu se zaregistrujte přes operator-onboard.yml
doctor-step4-request-failed-summary = požadavek na portál selhal
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = vypršel časový limit požadavku na portál
doctor-step4-timeout-hint = žádná odpověď za { $secs }s — zkontrolujte, zda je { $endpoint } dostupný z mesh
doctor-skipped-summary = přeskočeno — předchozí krok selhal (spusťte znovu s --all pro vynucení)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Jednotná příkazová řádka operátora pro data fabric goat.
help-long-about = goat — CLI orientované na operátora, které skládá panoscope-core do jednoho nástroje: init/znovupoužití profilu, doctor dostupnosti zohledňující ZTOL, procházení portálu organizací, CRUD topiců, pub/sub, delegování na stethoscope, interaktivní skladač příkazů a vyprávění o kaskádě substrátu.
help-flag-profile-dir = Přepíše adresář profilu (výchozí: $GOAT_PROFILE_DIR nebo ~/.goat).
help-flag-context = Přepíše aktivní kontext (výchozí: proměnná $GOAT_CONTEXT, poté current_context v config.toml, poté "default").
help-flag-narrate = Zobrazí kaskádu substrátu, kterou spustí měnící příkaz.
help-flag-verbose = Zapne podrobný ladicí výstup (nebo nastavte RUST_LOG=goat=debug).
help-flag-color = Barevný režim pro výstup terminálu (auto|always|never).
help-flag-lang = Jazyk pro text určený uživateli (např. en, uk). Vrací se k GOAT_LANG, poté k locale operačního systému, poté k angličtině.
help-cmd-profile = Spravuje místní profil goat (certifikát mTLS + token portálu + sadu endpointů).
help-cmd-context = Spravuje kontexty (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Spustí krokový prober dostupnosti vůči fabric.
help-cmd-doctor-long = Krokový prober dostupnosti vůči fabric: členství v ZTOL → router Zenoh → přijetí certifikátu → portál. Každý neúspěšný krok vydá konkrétní nápovědu k nápravě.
help-cmd-vendor = Procházejte organizace registrované v portálu.
help-cmd-topic = Vypíše, zobrazí, vytvoří nebo smaže topicy.
help-cmd-pub = Publikuje payload na klíč topicu.
help-cmd-sub = Přihlásí se k odběru výrazu klíče topicu.
help-cmd-inspect = Deleguje požadavek na diagnostický verdikt na stethoscope.
help-cmd-ui = Interaktivní sloučený výběr: procházejte deklarovaný ⊕ živý seznam a přeskočte na čočku Diagnose / Tail na vybraném cíli.
help-cmd-compose = Interaktivní TUI pro sestavování příkazů.
help-cmd-narrate = Reflexe kaskády substrátu nad nedávnou aktivitou. Zatím není dostupné (každodenní cesta je --narrate na měnícím příkazu).
help-cmd-completion = Vytiskne skript pro doplňování shellu na stdout.
help-cmd-explain = Vyhledá význam + nápravu chybového kódu ve vestavěném dokumentu `goat-cli-error-codes.md` (vzor cargo --explain).
help-cmd-mock = Publikuje syntetické vzorky na topic. Výchozí: triviální JSON hello-world při 1Hz. Předejte `--format <.fds>` + `--message <Name>` pro kódování protobufu dodaného operátorem.
help-cmd-status = Syntéza stavu: dostupnost ZTOL + /api/health portálu + vypršení místního certifikátu, tři řádky glyph nebo jeden objekt --json.
help-cmd-services = Sonduje každou hostovanou službu v `ops/serve/services.yaml` a vykreslí jeden řádek glyph na službu (nebo jeden objekt --json).
help-cmd-client = Obaluje místní daemon goat-clientd přes jeho IPC socket JSON-RPC (`~/.goat-client/goat-clientd.sock`): status / up / down / import bundle.
help-cmd-plugin = Objevování rozšíření založené na PATH (ve stylu kubectl). Jakýkoli spustitelný soubor `goat-<name>` v PATH lze vyvolat jako `goat <name>`; `goat plugin list` vyjmenuje nainstalované pluginy, `goat plugin which <name>` vyřeší cestu.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = koncový bod směrovače má chybný formát: `{ $endpoint }`
status-ztol-refused = TCP odmítnuto směrovačem { $host }:{ $port }: { $err }
status-ztol-unreachable = mimo mesh (směrovač { $host }:{ $port } nedostupný za { $ms }ms)
status-portal-ok = portál /api/health 2xx
status-portal-failed = portál /api/health selhal: { $err }
status-portal-timeout = vypršel časový limit portálu /api/health za { $secs }s
status-cert-read-failed = čtení certifikátu selhalo: { $err }
status-cert-pem-parse-failed = parsování PEM certifikátu selhalo: { $err }
status-cert-wrong-tag = první blok PEM je { $tag }, očekáván CERTIFICATE
status-cert-x509-parse-failed = parsování x509 selhalo: { $err }
status-cert-expired = certifikát VYPRŠEL v unix={ $not_after }, nyní={ $now }
status-cert-valid = certifikát platný; vyprší za { $days_left }d (unix={ $not_after })
inspect-finding-label = zjištění ({ $severity }): { $label } — { $detail }
inspect-remedy-label = náprava: { $title } — { $detail }
inspect-no-outcomes = (žádné výsledky — cíl neodpovídal žádné kontrole)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put přijat na `{ $key }` ({ $bytes } bajtů)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskáda ustálena — { $summary } ({ $n } { $n ->
        [one] důsledek
        [few] důsledky
        [many] důsledku
       *[other] důsledků
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Průvodce prvním spuštěním: vyberte sandbox nebo reálné prostředí, ověřte funkčnost, nasměrujte na tutora.
help-cmd-tutor = Samostatná praktická prohlídka fabric vlastním tempem (ve stylu vimtutor; lze obnovit).
help-cmd-docs = Procházejte dokumentaci goat nebo vypište úplnou referenci příkazů (--markdown).
help-cmd-hello = Provede obousměrný hello skrz fabric od začátku do konce (sandbox nebo reálné prostředí).
flourish-all-green = 🐐 vše zelené — fabric nominální

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = akce pouze pro operátora — nejste oprávněn (`goat admin` je bez účinku pro ne-adminy)
help-flag-narrate-json = Emituj substátní kaskádu jako NDJSON (jeden objekt na důsledek) pro záznamy incidentů + skriptování. Implikuje --narrate.
help-cmd-admin = Administrace pouze pro operátora: životní cyklus organizace, onboard/revoke/rotate, odpojení mesh-peeru, provisioning zařízení + operátorské úrovně, LAN triage. Autorizováno vašimi přihlašovacími údaji operátora (na straně serveru přes portal/NetBird token scope); bez účinku pro ne-adminy — neoprávněné volání nic nezmění a selže s `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profil pro kontext `{ $context }` existuje, ale tento uživatel ho nemůže číst (přístup odepřen)
error-portal-bad-response-summary = portál odpověděl, ale tento build goat nedokázal odpověď zpracovat
doctor-step0-ok-summary = místní nastavení je připraveno na síťové sondy
doctor-step0-warn-summary = některé kontroly místního nastavení se nepodařilo potvrdit
doctor-step0-fail-summary = místní nastavení vyžaduje pozornost
doctor-step0-profile-absent-summary = žádný profil goat pro kontext `{ $context }`
doctor-step0-profile-new-hint = zkontrolujte `goat context ls`; pokud jde o první nastavení, spusťte `goat ca add <bundle-ca.pem>` a porovnejte jeho otisk s otiskem od svého operátora, poté `goat profile init <bundle.cbor>`; spusťte `goat doctor` znovu
doctor-step0-profile-absent-hint = zkontrolujte `goat context ls`; vyberte existující kontext pomocí `goat context use <name>` nebo pro tento kontext spusťte `goat profile init <bundle.cbor>`; spusťte `goat doctor` znovu
doctor-step0-profile-denied-summary = profil goat v { $path } je nečitelný
doctor-step0-profile-denied-hint = opravte vlastnictví profilu nebo spusťte jako vlastnící uživatel; existující profil neinicializujte znovu
doctor-step0-profile-incomplete-summary = soubor profilu goat { $file } chybí nebo je nečitelný
doctor-step0-profile-incomplete-hint = obnovte uvedený soubor z vydaného balíčku, nebo nahraďte tento kontext pomocí `goat profile rotate <bundle.cbor>`; spusťte `goat doctor` znovu
doctor-step0-profile-invalid-summary = profil goat má nečitelné nebo chybějící endpointy
doctor-step0-profile-empty-token-summary = profil goat má prázdný token portálu
doctor-step0-profile-ok-summary = kontext goat `{ $context }` má čitelný materiál profilu
doctor-step0-client-absent-summary = žádný podporovaný klient mesh neodpověděl
doctor-step0-client-absent-hint = nainstalujte klienta mesh dodaného vaším goatnetem, spusťte jeho démona a poté znovu spusťte `goat doctor`
doctor-step0-client-stopped-summary = nainstalovaný klient mesh nevrátil použitelný stav
doctor-step0-client-stopped-hint = zkontrolujte `netbird status -d` nebo `goat client status --detail`; spusťte nainstalovaného démona (u NetBirdu `sudo netbird service start`) a poté znovu spusťte `goat doctor`
doctor-step0-netbird-ok-summary = démon NetBird odpověděl (hlášený profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd odpověděl (stav `{ $state }`)
doctor-step0-management-connected-summary = NetBird hlásí připojený management
doctor-step0-management-disconnected-summary = NetBird hlásí odpojený management
doctor-step0-management-disconnected-hint = zkontrolujte hlášenou URL managementu a `netbird status -d`; navázaný tunel může při výpadku managementu stále fungovat
doctor-step0-management-unknown-summary = nakonfigurovaný endpoint managementu se nepodařilo lokálně ověřit
doctor-step0-management-unknown-hint = zkontrolujte `netbird status -d` nebo `goat client diagnostics`; doctor management URL neuhaduje
doctor-step0-tunnel-down-summary = NetBird nehlásí žádnou adresu v mesh síti
doctor-step0-tunnel-down-hint = spusťte `netbird status -d`; pokud je zařízení zaregistrováno a démon odpovídá, spusťte `goat heal` a poté znovu `goat doctor`
doctor-step0-tunnel-address-summary = NetBird hlásí adresu v mesh síti { $address }; dále se kontroluje dostupnost routeru
doctor-step0-goat-client-no-bundle-summary = goat-clientd nemá importovaný balíček
doctor-step0-goat-client-no-bundle-hint = spusťte `goat client bundle import <bundle.cbor>` s registračním balíčkem klienta, připojte klienta a poté znovu spusťte `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd hlásí připojený tunel; dále se kontroluje dostupnost routeru
doctor-step0-goat-client-outer-disabled-summary = vnější tunel goat-clientd je v režimu netbird-only vypnutý
doctor-step0-goat-client-down-summary = goat-clientd hlásí stav tunelu `{ $state }`
doctor-step0-goat-client-down-hint = spusťte `goat client diagnostics`, poté `goat client up`, pokud je balíček načtený; znovu spusťte `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd hlásí připojenou vnitřní mesh síť
doctor-step0-goat-client-inner-disabled-summary = vnitřní mesh síť goat-clientd je v režimu wg-cp0-only vypnutá
doctor-step0-goat-client-inner-down-summary = goat-clientd hlásí stav vnitřní mesh sítě `{ $state }`
doctor-step0-goat-client-inner-down-hint = spusťte `goat client diagnostics` a zkontrolujte vnitřní mesh síť; po její obnově znovu spusťte `goat doctor`
doctor-step0-management-reachable-summary = management endpoint odpověděl HTTP { $status }
doctor-step0-management-auth-summary = management HTTPS endpoint je dostupný (HTTP { $status } pro tento test vyžaduje autorizaci)
doctor-step0-management-unexpected-summary = management endpoint vrátil neočekávané HTTP { $status }
doctor-step0-management-unexpected-hint = zkontrolujte nastavenou management URL a požádejte operátora goatnet, aby tuto odpověď prověřil
doctor-step0-management-unreachable-summary = nastavený management endpoint není dostupný: { $error }
doctor-step0-management-unreachable-hint = zkontrolujte DNS a síťový přístup k URL, kterou klient hlásí; existující tunel může stále fungovat
doctor-step0-clock-ok-summary = místní hodiny se liší od času management HTTP o méně než { $seconds } s
doctor-step0-clock-skew-summary = místní hodiny se liší od času management HTTP o { $seconds } s
doctor-step0-clock-skew-hint = synchronizujte hodiny zařízení a znovu spusťte `goat doctor`; při špatném čase může selhat ověření certifikátů
doctor-step0-clock-unknown-summary = odchylku hodin nelze změřit z hlavičky Date management HTTP
doctor-step0-clock-unknown-hint = pokud selže kontrola TLS nebo certifikátů, zkontrolujte synchronizaci času zařízení
error-clock-skew-summary = hodiny zařízení se příliš liší od času správy
error-doctor-inconclusive-summary = doctor zaznamenal selhání bez prokázané příčiny
error-portal-forbidden-summary = portál odepřel tomuto účtu přístup k testovanému API
doctor-step4-forbidden-summary = portál vrátil pro tento profil HTTP 403
doctor-step4-forbidden-hint = tento přihlašovací údaj portál dosáhl, ale nemá přístup k testovanému API; zkontrolujte `goat profile show` a požádejte operátora goatnetu o ověření oprávnění
doctor-step4-decode-summary = portál odpověděl odpovědí, kterou tento goat neumí přečíst
doctor-step4-decode-hint = { $err } — aktualizujte goat na verzi, kterou poskytuje tento goatnet, a znovu spusťte `goat doctor`
doctor-step1-control-only-summary = připojeno jen k řídicímu serveru — žádný tunel k žádnému z { $total } peerů
doctor-step1-control-only-hint = tato síť pouští ven TCP 443, ale blokuje datové porty mesh, takže z tohoto zařízení nic v mesh (včetně přihlašovacích stránek) není dostupné. Požádejte síťový tým o povolení odchozího TCP a UDP na { $relays } a UDP 3478 na { $stun }; doporučen je i přímý UDP provoz mezi zařízeními. Pro ověření se připojte k hotspotu telefonu a spusťte `netbird status`: peery by se pak měly připojit.
doctor-offmesh-dashboard-hint = flotila relay (externí pohled): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokální certifikát, odpovídající klíč a balíček CA se načetly; certifikát je v době platnosti
doctor-step2-material-failed-summary = lokální TLS materiál { $file } nelze použít
doctor-step2-material-failed-hint = { $err } — zkontrolujte uvedený soubor; obnovte jej z vydaného balíčku nebo rotujte profil
doctor-step2-material-invalid-hint = { $file } není platný certifikát X.509 PEM; obnovte vydaný materiál nebo spusťte `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem je neplatný nebo neodpovídá mtls.cert.pem; obnovte vydaný materiál nebo spusťte `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokální certifikát je mimo dobu platnosti
doctor-step2-cert-time-hint = spusťte `goat profile show`; zkontrolujte výsledek hodin výše, poté požádejte operátora o nový balíček a pokud certifikát vypršel, spusťte `goat profile rotate <new-bundle.cbor>`
doctor-skipped-no-endpoint-summary = přeskočeno — v profile.toml chybí { $field }
doctor-skipped-no-profile-summary = přeskočeno — pro tento kontext nejsou čitelné žádné endpointy profilu
doctor-skipped-no-portal-token-summary = přeskočeno — pro tento kontext není čitelný žádný token portálu
status-ztol-tcp-ok = port routeru { $host }:{ $port } přijímá TCP (jen port — viz session)
status-session-ok = zenoh mTLS session otevřena za { $ms } ms (router přijal tento certifikát)
status-session-skipped = session nebyla zahájena (mimo mesh; viz ztol)
status-session-material-missing = zenoh session nebyla zahájena: { $detail }
status-session-config-failed = konfigurace zenoh session selhala: { $err }
status-session-failed = zenoh session se neotevřela: { $err } — spusťte `goat doctor`
status-session-timeout = zenoh session se neotevřela do { $secs }s — port routeru přijímá TCP, ale žádná session se nedokončí (router zaseklý, certifikát odmítnut nebo posun hodin); spusťte `goat doctor`
status-names-live = jména živě (mesh DNS odpovědělo)
status-names-ip-only = jména neověřena (endpointy profilu jsou IP adresy)
status-names-snapshot = jména podle podepsaného snapshotu, sériové číslo { $serial }, stáří { $age } (mesh DNS nedostupné)
status-names-snapshot-aging = jména podle podepsaného snapshotu, sériové číslo { $serial }, stáří { $age } — ZASTARALÉ (mesh DNS nedostupné; opravte goat-coredns, jména se pak obnoví automaticky)
status-names-snapshot-daemon = jména podle podepsaného snapshotu, sériové číslo { $serial }, stáří { $age } — úložiště démona (mesh DNS nedostupné)
status-names-snapshot-daemon-aging = jména podle podepsaného snapshotu, sériové číslo { $serial }, stáří { $age } — úložiště démona, ZASTARALÉ (mesh DNS nedostupné; opravte goat-coredns, jména se pak obnoví automaticky)
status-names-failed = mesh DNS nedostupné a záložní snapshot není k dispozici — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ZAPNUTO — { $qr }/{ $connected } připojených peerů odolných vůči kvantovým útokům
status-pqc-off = wireguard PQC (rosenpass) VYPNUTO — klasické wireguard handshaky
status-pqc-unknown = wireguard PQC neznámé — { $reason }
status-device-none = ochrana jmen pro celé zařízení VYPNUTA — chybí úložiště jmen goat-clientd (příkazy goat mají stále fallback; zapněte podle name-fallback-resolver.md §9.2)
status-device-broken = úložiště jmen goat-clientd existuje (sériové číslo { $serial }), ale jeho forwarder NEODPOVÍDÁ na { $addr } — démon neběží nebo je subsystém jmen zaseknutý
status-device-active = ochrana jmen pro celé zařízení AKTIVNÍ — goat-clientd předsazen před mesh DNS v OS na { $addr } (sériové číslo úložiště { $serial }, { $grade })
status-device-not-fronted = forwarder goat-clientd běží na { $addr } (sériové číslo úložiště { $serial }, { $grade }) — OS není předsazen; prohlížeče používají přímo mesh DNS
status-device-fronting-unknown = forwarder goat-clientd běží na { $addr } (sériové číslo úložiště { $serial }, { $grade }) — stav předsazení v OS nelze na této platformě zjistit
status-names-observed = jména NEKANONICKÁ (ad hoc): pozorovaný záznam, stáří { $age } — není v podepsaném snapshotu (mesh DNS nedostupné; záznam kodifikujte)
status-names-selfclaim = jména přes ověřený SELF-CLAIM, stáří { $age } — podepsáno vlastní kotvou jména (mesh DNS nedostupné)
doctor-names-note = ze snapshotu, sériové číslo { $serial }, stáří { $age }
doctor-names-selfclaim-note = přes ověřený SELF-CLAIM, stáří { $age } — podepsáno vlastní kotvou jména
doctor-names-selfclaim-hint = mesh DNS je nedostupné — tento krok se vyřešil přes ověřený SELF-CLAIM (stáří { $age }): vlastní kotva jména podepsala tuto vazbu a peer_bindings v cache snapshotu ji povolují. Podepsané, ale ne živé; opravte mesh DNS, aby toto zmizelo.
doctor-names-observed-note = přes POZOROVANÝ (ad hoc) záznam, stáří { $age } — nekanonický
doctor-names-observed-hint = mesh DNS je nedostupné — tento krok se vyřešil přes NEKANONICKÝ pozorovaný záznam (stáří { $age }): jméno existuje jen živě a nikdy nebylo kodifikováno v registru lokality. Kodifikujte ho (group_vars registry → generátor → nový podpis), jinak vyprší; opravte mesh DNS, aby toto zmizelo.
doctor-names-hint = mesh DNS je nedostupné — tento krok se vyřešil přes podepsaný snapshot jmen (sériové číslo { $serial }, stáří { $age }). Fabric zůstává dostupný přes záložní jména; opravte mesh DNS (goat-coredns na mgmt hostu), aby toto zmizelo.
storm-brake-plan-header = Plán storm brake (ADR 1081 vrstva 4 — ruční):
storm-brake-plan-target = mgmt hostitel { $user }@{ $host }  (kontext { $context })
storm-brake-step-flush = restartovat signalizační kontejner `{ $container }` (zahodí zombie streamy)
storm-brake-step-tighten = přiškrtit L2 limiter na `{ $container }` (půlka rate-limitu, delší TTL neg-cache) — { $window } min, vrací operátor; přeskočí se, pokud L2 není nasazená
storm-brake-step-shed = pozastavit nepodstatnou publisher jednotku `{ $unit }`
storm-brake-shed-none = (žádné shed jednotky nejsou deklarovány — nastavte storm_brake_shed_units v profile.toml nebo předejte --shed-unit)
storm-brake-announce-note = zapsat jeden OperatorActionV1 do auditní stopy s výsledky jednotlivých kroků
storm-brake-dryrun-note = nanečisto — nic nebylo provedeno.
storm-brake-triggers-header = Spusťte, když pásmo host-net v relay-health ukazuje:
storm-brake-trigger-futility = poměr marnosti signalizace >= 0.5 při >= 100 zpráv/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = ztráta uplinku >= 5 % po dobu 10 min (wan.loss_pct)
storm-brake-triggers-band-url = pásmo hostitele: relay-health.{ $site }.<zone> (viz docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Pozn.: samotný flush signalizace mesh NEOBNOVÍ. Lékem jsou vrstvy 1–2 (backoff klientů + shedding signalizace); brake jen získává čas na zotavení.
storm-brake-confirm = Spustit storm brake proti { $user }@{ $host }? Restartuje signalizační kontejner a pozastaví deklarované publishery. [y/N]
storm-brake-aborted = storm brake přerušen (bez potvrzení)
storm-brake-executing = spouštím storm brake proti { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = selhalo
storm-brake-outcome-skipped = přeskočeno
storm-brake-summary = storm brake dokončen: { $ok } ok, { $failed } selhalo, { $skipped } přeskočeno
storm-brake-flush-failed = FLUSH selhal — signalizační kontejner se nerestartoval, obnova mesh se nečeká: { $reason }
storm-brake-mgmt-unresolved = mgmt hostitel nenalezen — předejte --mgmt-host, nastavte mgmt_ssh_host v profile.toml kontextu, nebo exportujte GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operátorská akce zapsána do auditní stopy (aktér { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portál odpověděl HTTP 404 pro `{ $path }`: není tam registrováno
error-portal-rate-limited-summary = portál odpověděl HTTP 429 pro `{ $path }`: příliš mnoho požadavků, zkuste to později
error-portal-server-error-summary = portál odpověděl HTTP { $status } pro `{ $path }`: chyba na straně portálu
error-portal-refused-summary = portál odpověděl HTTP { $status } pro `{ $path }`: požadavek odmítnut

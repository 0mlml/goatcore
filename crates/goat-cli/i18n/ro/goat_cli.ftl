### goat-cli — Romanian (ro). NATO-member language, Tier 2. oss-allow: language name
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = niciun profil pentru contextul `{ $context }`
error-off-mesh-summary = în afara ZTOL — neconectat la rețeaua mesh
error-router-unreachable-summary = routerul Zenoh de la `{ $endpoint }` este inaccesibil
error-cert-expired-summary = certificatul mTLS a expirat sau a fost respins de router
error-portal-unreachable-summary = API-ul portalului de la `{ $endpoint }` este inaccesibil
error-token-invalid-summary = portalul a respins tokenul Bearer
error-topic-conflict-summary = topicul `{ $topic }` există deja
error-bundle-invalid-summary = verificarea semnăturii sau a expirării bundle-ului a eșuat

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = punctul final al routerului este format greșit
doctor-step1-endpoint-malformed-hint = se aștepta `<proto>/<host>:<port>`; s-a primit `{ $endpoint }`
doctor-step1-ok-summary = conexiunea TCP la gazda routerului { $host }:{ $port } a reușit
doctor-step1-conn-failed-summary = conexiunea TCP la gazda routerului a eșuat
doctor-step1-conn-failed-hint = { $err } — porniți netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); verificați `netbird status`
doctor-step1-timeout-summary = expirare la conexiunea TCP la gazda routerului
doctor-step1-timeout-hint = gazda routerului { $host }:{ $port } inaccesibilă în { $ms }ms — probabil în afara mesh
doctor-step2-endpoint-malformed-hint = corectați emitentul bundle (--goat-cli-router-endpoint trebuie să fie `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM-ul rădăcinilor CA lipsește sau este ilizibil
doctor-step2-ca-missing-hint = { $err } — rulați din nou `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = routerul { $host }:{ $port } acceptă TCP (TLS validat la pasul 3)
doctor-step2-refused-summary = TCP-ul routerului a fost refuzat
doctor-step2-refused-hint = { $err } — routerul ar putea fi căzut; verificați implementarea Zenoh pe mgmt-host
doctor-step2-timeout-summary = expirare la TCP-ul routerului
doctor-step2-timeout-hint = routerul { $host }:{ $port } inaccesibil în { $ms }ms
doctor-step3-config-failed-summary = construirea configurației zenoh a eșuat
doctor-step3-config-failed-hint = punct final nevalid `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = construirea configurației TLS zenoh a eșuat
doctor-step3-tls-config-failed-hint = { $err } — probabil deviere de versiune zenoh; verificați potrivirea dependențelor
doctor-step3-ok-summary = sesiune mTLS deschisă — certificat acceptat de ACL-ul routerului
doctor-step3-open-failed-summary = deschiderea sesiunii zenoh a eșuat
doctor-step3-open-failed-hint = { $err } — certificatul ar putea fi expirat (rulați `goat profile show`) sau respins de ACL-ul routerului (nepotrivire URI-SAN)
doctor-step3-timeout-summary = expirare la deschiderea sesiunii zenoh
doctor-step3-timeout-hint = routerul nu a finalizat strângerea de mână în { $secs }s — decalaj de ceas, certificat greșit sau congestie
doctor-step4-ok-summary = portalul a returnat { $n } rând(uri) de organizație
doctor-step4-token-rejected-summary = portalul a respins tokenul Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — token expirat sau revocat; reînregistrați-vă prin operator-onboard.yml
doctor-step4-request-failed-summary = cererea către portal a eșuat
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = expirare la cererea către portal
doctor-step4-timeout-hint = niciun răspuns în { $secs }s — verificați dacă { $endpoint } este accesibil din mesh
doctor-skipped-summary = omis — pasul anterior a eșuat (rulați din nou cu --all pentru a forța)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Linie de comandă unificată pentru operatorul data fabric goat.
help-long-about = goat — CLI orientat către operator care compune panoscope-core într-un singur instrument: init/reutilizare profil, doctor de accesibilitate conștient de ZTOL, navigare în portalul organizațiilor, CRUD pe topic-uri, pub/sub, delegare către stethoscope, un compozitor interactiv de comenzi și narațiunea cascadei substratului.
help-flag-profile-dir = Suprascrie directorul de profil (implicit: $GOAT_PROFILE_DIR sau ~/.goat).
help-flag-context = Suprascrie contextul activ (implicit: variabila $GOAT_CONTEXT, apoi current_context din config.toml, apoi "default").
help-flag-narrate = Afișează cascada substratului declanșată de o comandă de modificare.
help-flag-verbose = Activează ieșirea de depanare detaliată (sau setați RUST_LOG=goat=debug).
help-flag-color = Mod de culoare pentru ieșirea în terminal (auto|always|never).
help-flag-lang = Limba pentru textul orientat către utilizator (ex. en, uk). Revine la GOAT_LANG, apoi la localizarea sistemului de operare, apoi la engleză.
help-cmd-profile = Gestionează profilul goat local (certificat mTLS + token de portal + set de endpoint-uri).
help-cmd-context = Gestionează contextele (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Rulează un prober de accesibilitate pas cu pas împotriva fabric-ului.
help-cmd-doctor-long = Prober de accesibilitate pas cu pas împotriva fabric-ului: apartenență la ZTOL → router Zenoh → acceptarea certificatului → portal. Fiecare pas eșuat emite un indiciu de remediere specific.
help-cmd-vendor = Navighează organizațiile înregistrate în portal.
help-cmd-topic = Listează, afișează, creează sau șterge topic-uri.
help-cmd-pub = Publică un payload către o cheie de topic.
help-cmd-sub = Abonează-te la o expresie de cheie de topic.
help-cmd-inspect = Deleagă o cerere de verdict diagnostic către stethoscope.
help-cmd-ui = Selector interactiv unificat: navighează lista declarată ⊕ în timp real și sari la lentila Diagnose / Tail pe ținta selectată.
help-cmd-compose = TUI interactiv pentru construirea comenzilor.
help-cmd-narrate = Reflecție asupra cascadei substratului peste activitatea recentă. Nu este încă disponibilă (calea de zi cu zi este --narrate pe o comandă de modificare).
help-cmd-completion = Tipărește scriptul de completare al shell-ului la stdout.
help-cmd-explain = Caută semnificația + remedierea unui cod de eroare în documentul încorporat `goat-cli-error-codes.md` (model cargo --explain).
help-cmd-mock = Publică mostre sintetice către un topic. Implicit: JSON hello-world trivial la 1Hz. Treceți `--format <.fds>` + `--message <Name>` pentru a codifica în schimb protobuf furnizat de operator.
help-cmd-status = Sinteză a stării: accesibilitate ZTOL + /api/health al portalului + expirarea certificatului local, trei linii de glyph sau un singur obiect --json.
help-cmd-services = Sondează fiecare serviciu găzduit din `ops/serve/services.yaml` și randează o linie de glyph per serviciu (sau un singur obiect --json).
help-cmd-client = Învelește daemon-ul goat-clientd local prin socket-ul său IPC JSON-RPC (`~/.goat-client/goat-clientd.sock`): status / up / down / import de bundle.
help-cmd-plugin = Descoperirea extensiilor bazată pe PATH (stil kubectl). Orice executabil `goat-<name>` din PATH poate fi invocat ca `goat <name>`; `goat plugin list` enumeră plugin-urile instalate, `goat plugin which <name>` rezolvă calea.

# goat status / inspect / --narrate text de execuție (ADR 1024).
status-ztol-malformed = punctul final al routerului este format greșit: `{ $endpoint }`
status-ztol-refused = TCP refuzat către routerul { $host }:{ $port }: { $err }
status-ztol-unreachable = în afara mesh (routerul { $host }:{ $port } inaccesibil în { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portalul /api/health a eșuat: { $err }
status-portal-timeout = expirare la portalul /api/health în { $secs }s
status-cert-read-failed = citirea certificatului a eșuat: { $err }
status-cert-pem-parse-failed = analiza PEM a certificatului a eșuat: { $err }
status-cert-wrong-tag = primul bloc PEM este { $tag }, se aștepta CERTIFICATE
status-cert-x509-parse-failed = analiza x509 a eșuat: { $err }
status-cert-expired = certificat EXPIRAT la unix={ $not_after }, acum={ $now }
status-cert-valid = certificat valid; expiră în { $days_left }d (unix={ $not_after })
inspect-finding-label = constatare ({ $severity }): { $label } — { $detail }
inspect-remedy-label = remediere: { $title } — { $detail }
inspect-no-outcomes = (niciun rezultat — ținta nu a corespuns niciunui control)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = put Zenoh acceptat pe `{ $key }` ({ $bytes } octeți)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = cascadă stabilizată — { $summary } ({ $n } { $n ->
        [one] consecință
       *[other] consecințe
    })

# Ajutor pe o linie pentru onboarding/documentația goat + finalul triumfal totul-verde (ADR 1024).
help-cmd-init = Asistent la prima rulare: alege sandbox sau real, confirmă că funcționează, îndreaptă către tutor.
help-cmd-tutor = Un tur practic, în ritm propriu, al fabric-ului (stil vimtutor; reluabil).
help-cmd-docs = Navighează documentația goat sau emite referința completă a comenzilor (--markdown).
help-cmd-hello = Trimite un hello dus-întors prin fabric, cap la cap (mediu sandbox sau real).
flourish-all-green = 🐐 totul verde — fabric nominal

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = acțiune rezervată operatorului — nu ești autorizat (`goat admin` este inert pentru non-admini)
help-flag-narrate-json = Emite cascada substratului ca NDJSON (un obiect per consecință) pentru jurnale de incidente + scripting. Implică --narrate.
help-cmd-admin = Administrare rezervată operatorului: ciclu de viață al organizației, onboard/revocare/rotație, tăiere mesh-peer, provizionare dispozitive + nivel operator, triaj LAN. Autorizat prin credențialele tale de operator (server-side via portal/token scope NetBird); inert pentru non-admini — un apel neautorizat nu modifică nimic și eșuează cu `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profilul pentru contextul `{ $context }` există, dar nu poate fi citit de acest utilizator (permisiune refuzată)
error-portal-bad-response-summary = portalul a răspuns, dar această versiune goat nu a putut interpreta răspunsul
doctor-step0-ok-summary = configurarea locală este pregătită pentru sondările de rețea
doctor-step0-warn-summary = configurarea locală are verificări care nu au putut fi confirmate
doctor-step0-fail-summary = configurarea locală necesită atenție
doctor-step0-profile-absent-summary = niciun profil goat pentru contextul `{ $context }`
doctor-step0-profile-new-hint = verifică `goat context ls`; dacă este prima configurare, rulează `goat ca add <bundle-ca.pem>` și compară amprenta cu cea a operatorului tău, apoi `goat profile init <bundle.cbor>`; rulează din nou `goat doctor`
doctor-step0-profile-absent-hint = verifică `goat context ls`; selectează un context existent cu `goat context use <name>` sau rulează `goat profile init <bundle.cbor>` pentru acest context; rulează din nou `goat doctor`
doctor-step0-profile-denied-summary = profilul goat de la { $path } nu poate fi citit
doctor-step0-profile-denied-hint = corectează proprietarul profilului sau rulează ca utilizatorul proprietar; nu reinițializa un profil existent
doctor-step0-profile-incomplete-summary = fișierul de profil goat { $file } lipsește sau nu poate fi citit
doctor-step0-profile-incomplete-hint = restaurează fișierul indicat din bundle-ul emis sau folosește `goat profile rotate <bundle.cbor>` pentru a înlocui acest context; rulează din nou `goat doctor`
doctor-step0-profile-invalid-summary = profilul goat are endpointuri ilizibile sau lipsă
doctor-step0-profile-empty-token-summary = profilul goat are un token de portal gol
doctor-step0-profile-ok-summary = contextul goat `{ $context }` are material de profil lizibil
doctor-step0-client-absent-summary = niciun client mesh compatibil nu a răspuns
doctor-step0-client-absent-hint = instalează clientul mesh furnizat de goatnet, pornește daemonul acestuia, apoi rulează din nou `goat doctor`
doctor-step0-client-stopped-summary = clientul mesh instalat nu a returnat o stare utilizabilă
doctor-step0-client-stopped-hint = verifică `netbird status -d` sau `goat client status --detail`; pornește daemonul instalat (`sudo netbird service start` pentru NetBird), apoi rulează din nou `goat doctor`
doctor-step0-netbird-ok-summary = daemonul NetBird a răspuns (profil raportat `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd a răspuns (stare `{ $state }`)
doctor-step0-management-connected-summary = NetBird raportează management conectat
doctor-step0-management-disconnected-summary = NetBird raportează management deconectat
doctor-step0-management-disconnected-hint = verifică URL-ul de management raportat și `netbird status -d`; un tunel deja stabilit poate funcționa în continuare în timpul unei întreruperi a managementului
doctor-step0-management-unknown-summary = endpointul de management configurat nu a putut fi stabilit local
doctor-step0-management-unknown-hint = verifică `netbird status -d` sau `goat client diagnostics`; doctor nu ghicește URL-ul de management
doctor-step0-tunnel-down-summary = NetBird nu raportează nicio adresă mesh
doctor-step0-tunnel-down-hint = rulează `netbird status -d`; dacă dispozitivul este înrolat și daemonul răspunde, rulează `goat heal`, apoi rulează din nou `goat doctor`
doctor-step0-tunnel-address-summary = NetBird raportează adresa mesh { $address }; urmează verificarea accesibilității routerului
doctor-step0-goat-client-no-bundle-summary = goat-clientd nu are niciun bundle importat
doctor-step0-goat-client-no-bundle-hint = rulează `goat client bundle import <bundle.cbor>` cu bundle-ul de înrolare al clientului, conectează clientul, apoi rulează din nou `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd raportează un tunel conectat; urmează verificarea accesibilității routerului
doctor-step0-goat-client-outer-disabled-summary = tunelul exterior goat-clientd este dezactivat în modul netbird-only
doctor-step0-goat-client-down-summary = goat-clientd raportează starea tunelului `{ $state }`
doctor-step0-goat-client-down-hint = rulează `goat client diagnostics`, apoi `goat client up` dacă bundle-ul este încărcat; rulează din nou `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd raportează meshul interior conectat
doctor-step0-goat-client-inner-disabled-summary = meshul interior goat-clientd este dezactivat în modul wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd raportează starea meshului interior `{ $state }`
doctor-step0-goat-client-inner-down-hint = rulează `goat client diagnostics` pentru a inspecta meshul interior, apoi rulează din nou `goat doctor` după ce l-ai restabilit
doctor-step0-management-reachable-summary = endpoint-ul de management a răspuns cu HTTP { $status }
doctor-step0-management-auth-summary = endpoint-ul HTTPS de management este accesibil (HTTP { $status } cere autorizare pentru această sondare)
doctor-step0-management-unexpected-summary = endpoint-ul de management a returnat un HTTP { $status } neașteptat
doctor-step0-management-unexpected-hint = verifică URL-ul de management configurat și cere operatorului goatnet să verifice acest răspuns
doctor-step0-management-unreachable-summary = endpoint-ul de management configurat nu a putut fi contactat: { $error }
doctor-step0-management-unreachable-hint = verifică DNS-ul și accesul la rețea către URL-ul raportat de client; un tunel existent poate funcționa în continuare
doctor-step0-clock-ok-summary = ceasul local se încadrează în { $seconds } secunde față de ora HTTP a managementului
doctor-step0-clock-skew-summary = ceasul local diferă de ora HTTP a managementului cu { $seconds } secunde
doctor-step0-clock-skew-hint = sincronizează ceasul dispozitivului, apoi rulează din nou `goat doctor`; validarea certificatelor poate eșua cât timp ceasul este greșit
doctor-step0-clock-unknown-summary = decalajul ceasului nu a putut fi măsurat din antetul HTTP Date al managementului
doctor-step0-clock-unknown-hint = verifică sincronizarea orei pe dispozitiv dacă verificările TLS sau ale certificatelor eșuează
error-clock-skew-summary = ceasul dispozitivului diferă prea mult de ora de management
error-doctor-inconclusive-summary = doctor a observat o eroare fără cauză dovedită
error-portal-forbidden-summary = portalul a refuzat accesul acestui cont la API-ul testat
doctor-step4-forbidden-summary = portalul a returnat HTTP 403 pentru acest profil
doctor-step4-forbidden-hint = această credențială a ajuns la portal, dar nu are acces la API-ul testat; verifică `goat profile show` și cere operatorului goatnet să verifice permisiunea acordată
doctor-step4-decode-summary = portalul a răspuns cu un răspuns pe care acest goat nu îl poate citi
doctor-step4-decode-hint = { $err } — actualizează goat la versiunea servită de acest goatnet, apoi rulează din nou `goat doctor`
doctor-step1-control-only-summary = conectat doar la serverul de control — niciun tunel către vreunul dintre cei { $total } peers
doctor-step1-control-only-hint = această rețea permite ieșirea pe TCP 443, dar blochează porturile de date ale mesh-ului, deci nimic din mesh (inclusiv paginile de autentificare) nu e accesibil de pe acest dispozitiv. Cere echipei de rețea să permită TCP și UDP de ieșire către { $relays } și UDP 3478 către { $stun }; se recomandă și UDP direct între dispozitive. Pentru confirmare, conectează-te la hotspotul unui telefon și rulează `netbird status`: peers ar trebui să se conecteze.
doctor-offmesh-dashboard-hint = flotă de relay (punct de observație extern): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = certificatul local, cheia corespunzătoare și bundle-ul CA se parsează; certificatul se află în intervalul de valabilitate
doctor-step2-material-failed-summary = materialul TLS local { $file } nu a putut fi folosit
doctor-step2-material-failed-hint = { $err } — inspectează fișierul indicat; restaurează-l din bundle-ul emis sau rotește profilul
doctor-step2-material-invalid-hint = { $file } nu este un certificat X.509 PEM valid; restaurează materialul emis sau rulează `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem este invalid sau nu corespunde cu mtls.cert.pem; restaurează materialul emis sau rulează `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = certificatul local este în afara intervalului de valabilitate
doctor-step2-cert-time-hint = rulează `goat profile show`; verifică rezultatul ceasului de mai sus, apoi cere operatorului un bundle nou și rulează `goat profile rotate <new-bundle.cbor>` dacă certificatul a expirat
doctor-skipped-no-endpoint-summary = omis — în profile.toml lipsește { $field }
doctor-skipped-no-profile-summary = omis — niciun endpoint de profil lizibil pentru acest context
doctor-skipped-no-portal-token-summary = omis — niciun token de portal lizibil pentru acest context
status-ztol-tcp-ok = portul routerului { $host }:{ $port } acceptă TCP (doar portul — vezi sesiunea)
status-session-ok = sesiune zenoh mTLS deschisă în { $ms }ms (routerul a acceptat acest certificat)
status-session-skipped = sesiune neîncercată (în afara mesh-ului; vezi ztol)
status-session-material-missing = sesiune zenoh neîncercată: { $detail }
status-session-config-failed = configurarea sesiunii zenoh a eșuat: { $err }
status-session-failed = sesiunea zenoh nu s-a deschis: { $err } — rulează `goat doctor`
status-session-timeout = sesiunea zenoh nu s-a deschis în { $secs }s — portul routerului acceptă TCP, dar nicio sesiune nu se finalizează (router blocat, certificat respins sau decalaj de ceas); rulează `goat doctor`
status-names-live = nume live (DNS-ul mesh a răspuns)
status-names-ip-only = nume netestate (endpoint-urile din profil sunt adrese IP)
status-names-snapshot = nume conform snapshotului semnat, serial { $serial }, vechime { $age } (DNS mesh inaccesibil)
status-names-snapshot-aging = nume conform snapshotului semnat, serial { $serial }, vechime { $age } — EXPIRAT (DNS mesh inaccesibil; repară goat-coredns, apoi numele se reîmprospătează automat)
status-names-snapshot-daemon = nume conform snapshotului semnat, serial { $serial }, vechime { $age } — store daemon (DNS mesh inaccesibil)
status-names-snapshot-daemon-aging = nume conform snapshotului semnat, serial { $serial }, vechime { $age } — store daemon, EXPIRAT (DNS mesh inaccesibil; repară goat-coredns, apoi numele se reîmprospătează automat)
status-names-failed = DNS mesh inaccesibil și fallback-ul pe snapshot indisponibil — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ACTIV — { $qr }/{ $connected } peers conectați rezistenți cuantic
status-pqc-off = wireguard PQC (rosenpass) INACTIV — handshake-uri wireguard clasice
status-pqc-unknown = wireguard PQC necunoscut — { $reason }
status-device-none = protecția numelor la nivel de dispozitiv INACTIVĂ — nu există store de nume goat-clientd (comenzile goat au totuși fallback; activează conform name-fallback-resolver.md §9.2)
status-device-broken = store-ul de nume goat-clientd există (serial { $serial }), dar forwarder-ul său NU răspunde pe { $addr } — daemon oprit sau subsistemul de nume blocat
status-device-active = protecția numelor la nivel de dispozitiv ACTIVĂ — goat-clientd în fața DNS-ului mesh al OS la { $addr } (store serial { $serial }, { $grade })
status-device-not-fronted = forwarder goat-clientd activ la { $addr } (store serial { $serial }, { $grade }) — OS-ul nu e protejat; browserele folosesc direct DNS mesh
status-device-fronting-unknown = forwarder goat-clientd activ la { $addr } (store serial { $serial }, { $grade }) — starea de fronting a OS nu poate fi detectată pe această platformă
status-names-observed = nume NECANONICE (ad hoc): înregistrare observată, vechime { $age } — absentă din snapshotul semnat (DNS mesh inaccesibil; codifică această înregistrare)
status-names-selfclaim = nume prin SELF-CLAIM verificat, vechime { $age } — semnat de ancora proprie a numelui (DNS mesh inaccesibil)
doctor-names-note = prin snapshot serial { $serial }, vechime { $age }
doctor-names-selfclaim-note = prin SELF-CLAIM verificat, vechime { $age } — semnat de ancora proprie a numelui
doctor-names-selfclaim-hint = DNS mesh este inaccesibil — acest pas s-a rezolvat printr-un SELF-CLAIM verificat (vechime { $age }): ancora proprie a numelui a semnat această asociere, iar peer_bindings din snapshotul din cache o autorizează. Semnat, dar nu live; repară DNS mesh pentru a elimina acest mesaj.
doctor-names-observed-note = prin înregistrare OBSERVATĂ (ad hoc), vechime { $age } — necanonică
doctor-names-observed-hint = DNS mesh este inaccesibil — acest pas s-a rezolvat printr-o înregistrare observată NECANONICĂ (vechime { $age }): numele există doar live, nu a fost niciodată codificat în registrul site-ului. Codifică-l (registry group_vars → generator → re-semnare) sau expiră; repară DNS mesh pentru a elimina acest mesaj.
doctor-names-hint = DNS mesh este inaccesibil — acest pas s-a rezolvat prin snapshotul semnat de nume (serial { $serial }, vechime { $age }). Fabrica rămâne accesibilă pe nume de fallback; repară DNS mesh (goat-coredns pe hostul mgmt) pentru a elimina acest mesaj.
storm-brake-plan-header = Plan frână de furtună (ADR 1081 Layer 4 — manual):
storm-brake-plan-target = host mgmt { $user }@{ $host }  (context { $context })
storm-brake-step-flush = repornește containerul de semnalizare `{ $container }` (elimină fluxurile zombie)
storm-brake-step-tighten = restrânge limitatorul L2 de pe `{ $container }` (înjumătățește rate-limit, crește TTL neg-cache) — { $window } min, revenire manuală de către operator; omis dacă L2 nu e desfășurat
storm-brake-step-shed = pune în pauză unitatea de publisher neesențială `{ $unit }`
storm-brake-shed-none = (nicio unitate de shed declarată — setează storm_brake_shed_units în profile.toml sau transmite --shed-unit)
storm-brake-announce-note = emite un OperatorActionV1 în jurnalul de audit, cu rezultatul fiecărui pas
storm-brake-dryrun-note = rulare de probă — nu s-a executat nimic.
storm-brake-triggers-header = Declanșează când banda host-net relay-health arată:
storm-brake-trigger-futility = raport de futilitate al semnalizării >= 0.5 la >= 100 msgs/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = pierdere uplink >= 5% susținută 10 min (wan.loss_pct)
storm-brake-triggers-band-url = banda host: relay-health.{ $site }.<zone> (vezi docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Notă: un flush de semnalizare singur NU restabilește mesh-ul. Straturile 1-2 (backoff client + shedding de semnalizare) sunt remediul; frâna doar câștigă timp de recuperare.
storm-brake-confirm = Declanșezi frâna de furtună pe { $user }@{ $host }? Repornește containerul de semnalizare și pune în pauză publisherii declarați. [y/N]
storm-brake-aborted = frână de furtună anulată (fără confirmare)
storm-brake-executing = se declanșează frâna de furtună pe { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = eșuat
storm-brake-outcome-skipped = omis
storm-brake-summary = frână de furtună finalizată: { $ok } ok, { $failed } eșuate, { $skipped } omise
storm-brake-flush-failed = FLUSH eșuat — containerul de semnalizare nu a repornit, deci nu te aștepta la recuperarea mesh-ului: { $reason }
storm-brake-mgmt-unresolved = host mgmt nerezolvat — transmite --mgmt-host, setează mgmt_ssh_host în profile.toml al contextului sau exportă GOAT_MGMT_SSH_HOST
storm-brake-audit-note = acțiune de operator înregistrată în jurnalul de audit (actor { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portalul a răspuns HTTP 404 pentru `{ $path }`: nu este înregistrat acolo
error-portal-rate-limited-summary = portalul a răspuns HTTP 429 pentru `{ $path }`: prea multe cereri, reîncercați mai târziu
error-portal-server-error-summary = portalul a răspuns HTTP { $status } pentru `{ $path }`: o defecțiune de partea portalului
error-portal-refused-summary = portalul a răspuns HTTP { $status } pentru `{ $path }`: cerere refuzată

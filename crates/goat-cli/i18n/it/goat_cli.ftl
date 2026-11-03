### goat-cli — Italian (it). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = nessun profilo per il contesto `{ $context }`
error-off-mesh-summary = fuori dalla ZTOL — non connesso alla mesh
error-router-unreachable-summary = il router Zenoh su `{ $endpoint }` è irraggiungibile
error-cert-expired-summary = certificato mTLS scaduto o rifiutato dal router
error-portal-unreachable-summary = l'API del portale su `{ $endpoint }` è irraggiungibile
error-token-invalid-summary = il portale ha rifiutato il token Bearer
error-topic-conflict-summary = il topic `{ $topic }` esiste già
error-bundle-invalid-summary = verifica della firma o della scadenza del bundle non riuscita

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = endpoint del router non valido
doctor-step1-endpoint-malformed-hint = previsto `<proto>/<host>:<port>`; ricevuto `{ $endpoint }`
doctor-step1-ok-summary = connessione TCP all'host del router { $host }:{ $port } riuscita
doctor-step1-conn-failed-summary = connessione TCP all'host del router non riuscita
doctor-step1-conn-failed-hint = { $err } — avvia netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); controlla `netbird status`
doctor-step1-timeout-summary = timeout della connessione TCP all'host del router
doctor-step1-timeout-hint = host del router { $host }:{ $port } irraggiungibile in { $ms }ms — probabilmente fuori dalla mesh
doctor-step2-endpoint-malformed-hint = correggi l'emittente del bundle (--goat-cli-router-endpoint deve essere `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM delle radici CA mancante o illeggibile
doctor-step2-ca-missing-hint = { $err } — riesegui `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = router { $host }:{ $port } accetta TCP (TLS validato al passo 3)
doctor-step2-refused-summary = TCP del router rifiutato
doctor-step2-refused-hint = { $err } — il router potrebbe essere inattivo; controlla il deploy di Zenoh sul mgmt-host
doctor-step2-timeout-summary = timeout del TCP del router
doctor-step2-timeout-hint = router { $host }:{ $port } irraggiungibile in { $ms }ms
doctor-step3-config-failed-summary = creazione della configurazione zenoh non riuscita
doctor-step3-config-failed-hint = endpoint non valido `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = creazione della configurazione TLS zenoh non riuscita
doctor-step3-tls-config-failed-hint = { $err } — probabile disallineamento di versione zenoh; verifica che le dipendenze corrispondano
doctor-step3-ok-summary = sessione mTLS aperta — certificato accettato dall'ACL del router
doctor-step3-open-failed-summary = apertura della sessione zenoh non riuscita
doctor-step3-open-failed-hint = { $err } — il certificato potrebbe essere scaduto (esegui `goat profile show`) o rifiutato dall'ACL del router (mancata corrispondenza URI-SAN)
doctor-step3-timeout-summary = timeout dell'apertura della sessione zenoh
doctor-step3-timeout-hint = il router non ha completato l'handshake in { $secs }s — sfasamento dell'orologio, certificato errato o congestione
doctor-step4-ok-summary = il portale ha restituito { $n } riga/righe di organizzazione
doctor-step4-token-rejected-summary = il portale ha rifiutato il token Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — token scaduto o revocato; ripeti l'onboarding tramite operator-onboard.yml
doctor-step4-request-failed-summary = richiesta al portale non riuscita
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = timeout della richiesta al portale
doctor-step4-timeout-hint = nessuna risposta in { $secs }s — verifica che { $endpoint } sia raggiungibile dalla mesh
doctor-skipped-summary = saltato — il passo precedente è fallito (riesegui con --all per forzare)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Riga di comando unificata per l'operatore della data fabric goat.
help-long-about = goat — CLI rivolta all'operatore che compone panoscope-core in un unico strumento: init/riuso del profilo, doctor di raggiungibilità consapevole della ZTOL, navigazione del portale organizzazioni, CRUD dei topic, pub/sub, delega a stethoscope, un compositore di comandi interattivo e narrazione della cascata del substrato.
help-flag-profile-dir = Sovrascrive la directory del profilo (predefinita: $GOAT_PROFILE_DIR o ~/.goat).
help-flag-context = Sovrascrive il contesto attivo (predefinito: variabile $GOAT_CONTEXT, poi current_context in config.toml, poi "default").
help-flag-narrate = Mostra la cascata del substrato innescata da un comando di modifica.
help-flag-verbose = Abilita l'output di debug dettagliato (oppure imposta RUST_LOG=goat=debug).
help-flag-color = Modalità colore per l'output del terminale (auto|always|never).
help-flag-lang = Lingua per il testo rivolto all'utente (es. en, uk). Ripiega su GOAT_LANG, poi sul locale del sistema operativo, poi sull'inglese.
help-cmd-profile = Gestisce il profilo goat locale (certificato mTLS + token del portale + set di endpoint).
help-cmd-context = Gestisce i contesti (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Esegue un prober di raggiungibilità a passi contro la fabric.
help-cmd-doctor-long = Prober di raggiungibilità a passi contro la fabric: appartenenza alla ZTOL → router Zenoh → accettazione del certificato → portale. Ogni passo fallito emette un suggerimento di rimedio specifico.
help-cmd-vendor = Naviga le organizzazioni registrate nel portale.
help-cmd-topic = Elenca, mostra, crea o elimina topic.
help-cmd-pub = Pubblica un payload su una chiave di topic.
help-cmd-sub = Sottoscrivi un'espressione di chiave di topic.
help-cmd-inspect = Delega una richiesta di verdetto diagnostico a stethoscope.
help-cmd-ui = Selettore interattivo unificato: naviga l'elenco dichiarato ⊕ in tempo reale e passa alla lente Diagnose / Tail sul target selezionato.
help-cmd-compose = TUI interattiva per la costruzione dei comandi.
help-cmd-narrate = Riflessione sulla cascata del substrato sull'attività recente. Non ancora disponibile (il percorso quotidiano è --narrate su un comando di modifica).
help-cmd-completion = Stampa lo script di completamento della shell su stdout.
help-cmd-explain = Cerca il significato + il rimedio di un codice di errore nel documento incorporato `goat-cli-error-codes.md` (schema cargo --explain).
help-cmd-mock = Pubblica campioni sintetici su un topic. Predefinito: banale JSON hello-world a 1Hz. Passa `--format <.fds>` + `--message <Name>` per codificare invece protobuf fornito dall'operatore.
help-cmd-status = Sintesi dello stato: raggiungibilità ZTOL + /api/health del portale + scadenza del certificato locale, tre righe di glyph o un singolo oggetto --json.
help-cmd-services = Sonda ogni servizio ospitato in `ops/serve/services.yaml` e renderizza una riga di glyph per servizio (o un singolo oggetto --json).
help-cmd-client = Avvolge il daemon goat-clientd locale tramite il suo socket IPC JSON-RPC (`~/.goat-client/goat-clientd.sock`): status / up / down / import del bundle.
help-cmd-plugin = Individuazione delle estensioni basata su PATH (stile kubectl). Qualsiasi eseguibile `goat-<name>` su PATH è invocabile come `goat <name>`; `goat plugin list` enumera i plugin installati, `goat plugin which <name>` risolve il percorso.

# goat status / inspect / --narrate prosa di runtime (ADR 1024).
status-ztol-malformed = endpoint del router non valido: `{ $endpoint }`
status-ztol-refused = TCP rifiutato verso il router { $host }:{ $port }: { $err }
status-ztol-unreachable = fuori dalla mesh (router { $host }:{ $port } irraggiungibile in { $ms }ms)
status-portal-ok = portale /api/health 2xx
status-portal-failed = portale /api/health non riuscito: { $err }
status-portal-timeout = timeout del portale /api/health in { $secs }s
status-cert-read-failed = lettura del certificato non riuscita: { $err }
status-cert-pem-parse-failed = analisi PEM del certificato non riuscita: { $err }
status-cert-wrong-tag = il primo blocco PEM è { $tag }, atteso CERTIFICATE
status-cert-x509-parse-failed = analisi x509 non riuscita: { $err }
status-cert-expired = certificato SCADUTO a unix={ $not_after }, ora={ $now }
status-cert-valid = certificato valido; scade tra { $days_left }d (unix={ $not_after })
inspect-finding-label = riscontro ({ $severity }): { $label } — { $detail }
inspect-remedy-label = rimedio: { $title } — { $detail }
inspect-no-outcomes = (nessun esito — il target non ha corrisposto ad alcun controllo)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = put di Zenoh accettato su `{ $key }` ({ $bytes } byte)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = cascata stabilizzata — { $summary } ({ $n } { $n ->
        [one] conseguenza
       *[other] conseguenze
    })

# Aiuto in una riga per l'onboarding/la documentazione di goat + il colpo di scena tutto-verde (ADR 1024).
help-cmd-init = Procedura guidata al primo avvio: scegli sandbox o reale, verifica che funzioni, indirizza al tutor.
help-cmd-tutor = Un tour pratico e con ritmo autonomo della fabric (stile vimtutor; riprendibile).
help-cmd-docs = Sfoglia la documentazione di goat, oppure emetti il riferimento completo dei comandi (--markdown).
help-cmd-hello = Fai compiere a un hello un giro di andata e ritorno attraverso la fabric end-to-end (ambiente sandbox o reale).
flourish-all-green = 🐐 tutto verde — fabric nominale

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = azione solo per operatori — non sei autorizzato (`goat admin` è inerte per i non-admin)
help-flag-narrate-json = Emette la cascata del substrato come NDJSON (un oggetto per conseguenza) per log degli incidenti + scripting. Implica --narrate.
help-cmd-admin = Amministrazione solo per operatori: ciclo di vita delle organizzazioni, onboard/revoca/rotazione, taglio mesh-peer, provisioning dispositivi + tier operatori, triage LAN. Autorizzato dalle credenziali operatore (lato server tramite portal/NetBird token scope); inerte per i non-admin — una chiamata non autorizzata non modifica nulla e fallisce con `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = il profilo per il contesto `{ $context }` esiste ma non è leggibile da questo utente (permesso negato)
error-portal-bad-response-summary = il portale ha risposto ma questa build di goat non è riuscita a interpretare la risposta
doctor-step0-ok-summary = la configurazione locale è pronta per i test di rete
doctor-step0-warn-summary = la configurazione locale ha controlli che non è stato possibile confermare
doctor-step0-fail-summary = la configurazione locale richiede attenzione
doctor-step0-profile-absent-summary = nessun profilo goat per il contesto `{ $context }`
doctor-step0-profile-new-hint = controlla `goat context ls`; se è la prima configurazione, esegui `goat ca add <bundle-ca.pem>` e confronta la sua impronta con quella del tuo operatore, poi `goat profile init <bundle.cbor>`; riesegui `goat doctor`
doctor-step0-profile-absent-hint = controlla `goat context ls`; seleziona un contesto esistente con `goat context use <name>`, oppure esegui `goat profile init <bundle.cbor>` per questo contesto; riesegui `goat doctor`
doctor-step0-profile-denied-summary = il profilo goat in { $path } non è leggibile
doctor-step0-profile-denied-hint = correggi il proprietario del profilo o esegui come utente proprietario; non reinizializzare un profilo esistente
doctor-step0-profile-incomplete-summary = il file del profilo goat { $file } è mancante o non leggibile
doctor-step0-profile-incomplete-hint = ripristina il file indicato dal bundle emesso, oppure usa `goat profile rotate <bundle.cbor>` per sostituire questo contesto; riesegui `goat doctor`
doctor-step0-profile-invalid-summary = il profilo goat ha endpoint non leggibili o mancanti
doctor-step0-profile-empty-token-summary = il profilo goat ha un token del portale vuoto
doctor-step0-profile-ok-summary = il contesto goat `{ $context }` ha materiale di profilo leggibile
doctor-step0-client-absent-summary = nessun client mesh supportato ha risposto
doctor-step0-client-absent-hint = installa il client mesh fornito dalla tua goatnet, avvia il suo daemon, poi riesegui `goat doctor`
doctor-step0-client-stopped-summary = il client mesh installato non ha restituito uno stato utilizzabile
doctor-step0-client-stopped-hint = controlla `netbird status -d` o `goat client status --detail`; avvia il daemon installato (`sudo netbird service start` per NetBird), poi riesegui `goat doctor`
doctor-step0-netbird-ok-summary = il daemon NetBird ha risposto (profilo riportato `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd ha risposto (stato `{ $state }`)
doctor-step0-management-connected-summary = NetBird segnala il management connesso
doctor-step0-management-disconnected-summary = NetBird segnala il management disconnesso
doctor-step0-management-disconnected-hint = controlla l'URL del management riportato e `netbird status -d`; un tunnel già stabilito può continuare a funzionare durante un'interruzione del management
doctor-step0-management-unknown-summary = l'endpoint di management configurato non è stato stabilito in locale
doctor-step0-management-unknown-hint = controlla `netbird status -d` o `goat client diagnostics`; doctor non ipotizza un URL di management
doctor-step0-tunnel-down-summary = NetBird non riporta alcun indirizzo mesh
doctor-step0-tunnel-down-hint = esegui `netbird status -d`; se il dispositivo è registrato e il daemon risponde, esegui `goat heal`, poi riesegui `goat doctor`
doctor-step0-tunnel-address-summary = NetBird riporta l'indirizzo mesh { $address }; poi si verifica la raggiungibilità del router
doctor-step0-goat-client-no-bundle-summary = goat-clientd non ha nessun bundle importato
doctor-step0-goat-client-no-bundle-hint = esegui `goat client bundle import <bundle.cbor>` con il bundle di registrazione del client, connetti il client, poi riesegui `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd riporta un tunnel connesso; poi si verifica la raggiungibilità del router
doctor-step0-goat-client-outer-disabled-summary = il tunnel esterno di goat-clientd è disabilitato in modalità netbird-only
doctor-step0-goat-client-down-summary = goat-clientd riporta lo stato del tunnel `{ $state }`
doctor-step0-goat-client-down-hint = esegui `goat client diagnostics`, poi `goat client up` se il bundle è caricato; riesegui `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd riporta la mesh interna connessa
doctor-step0-goat-client-inner-disabled-summary = la mesh interna di goat-clientd è disabilitata in modalità wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd riporta lo stato della mesh interna `{ $state }`
doctor-step0-goat-client-inner-down-hint = esegui `goat client diagnostics` per ispezionare la mesh interna, poi riesegui `goat doctor` dopo averla ripristinata
doctor-step0-management-reachable-summary = l'endpoint di management ha risposto HTTP { $status }
doctor-step0-management-auth-summary = l'endpoint HTTPS di management è raggiungibile (HTTP { $status } richiede autorizzazione per questa verifica)
doctor-step0-management-unexpected-summary = l'endpoint di management ha restituito un HTTP { $status } inatteso
doctor-step0-management-unexpected-hint = controlla l'URL di management configurato e chiedi al tuo operatore goatnet di verificare questa risposta
doctor-step0-management-unreachable-summary = impossibile raggiungere l'endpoint di management configurato: { $error }
doctor-step0-management-unreachable-hint = controlla DNS e accesso di rete verso l'URL riportato dal client; un tunnel esistente potrebbe comunque funzionare
doctor-step0-clock-ok-summary = l'orologio locale è entro { $seconds } secondi dall'ora HTTP del management
doctor-step0-clock-skew-summary = l'orologio locale differisce di { $seconds } secondi dall'ora HTTP del management
doctor-step0-clock-skew-hint = sincronizza l'orologio del dispositivo, poi riesegui `goat doctor`; la validazione dei certificati può fallire finché l'orologio è errato
doctor-step0-clock-unknown-summary = impossibile misurare lo scarto dell'orologio dall'header Date HTTP del management
doctor-step0-clock-unknown-hint = controlla la sincronizzazione dell'ora del dispositivo se i controlli TLS o dei certificati falliscono
error-clock-skew-summary = l'orologio del dispositivo è troppo lontano dall'ora di gestione
error-doctor-inconclusive-summary = doctor ha rilevato un errore senza una causa accertata
error-portal-forbidden-summary = il portale ha negato a questo account l'accesso all'API sondata
doctor-step4-forbidden-summary = il portale ha restituito HTTP 403 per questo profilo
doctor-step4-forbidden-hint = questa credenziale ha raggiunto il portale ma non ha accesso all'API sondata; controlla `goat profile show` e chiedi al tuo operatore goatnet di verificare l'autorizzazione
doctor-step4-decode-summary = il portale ha risposto con una risposta che questo goat non riesce a leggere
doctor-step4-decode-hint = { $err } — aggiorna goat alla versione servita da questa goatnet, poi riesegui `goat doctor`
doctor-step1-control-only-summary = connesso solo al server di controllo — nessun tunnel verso nessuno dei { $total } peer
doctor-step1-control-only-hint = questa rete lascia uscire TCP 443 ma blocca le porte dati della mesh, quindi nulla sulla mesh (comprese le pagine di accesso) è raggiungibile da questo dispositivo. Chiedi al team di rete di consentire TCP e UDP in uscita verso { $relays } e UDP 3478 verso { $stun }; è consigliato anche UDP diretto tra i dispositivi. Per confermare, collegati all'hotspot di un telefono ed esegui `netbird status`: i peer dovrebbero connettersi.
doctor-offmesh-dashboard-hint = flotta di relay (punto di osservazione esterno): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = certificato locale, chiave corrispondente e bundle CA validi al parsing; il certificato è nel suo intervallo di validità
doctor-step2-material-failed-summary = impossibile usare il materiale TLS locale { $file }
doctor-step2-material-failed-hint = { $err } — controlla il file indicato; ripristinalo dal bundle emesso o ruota il profilo
doctor-step2-material-invalid-hint = { $file } non è un certificato X.509 PEM valido; ripristina il materiale emesso o esegui `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem non è valida o non corrisponde a mtls.cert.pem; ripristina il materiale emesso o esegui `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = il certificato locale è fuori dal suo intervallo di validità
doctor-step2-cert-time-hint = esegui `goat profile show`; controlla il risultato dell'orologio sopra, poi chiedi all'operatore un nuovo bundle ed esegui `goat profile rotate <new-bundle.cbor>` se il certificato è scaduto
doctor-skipped-no-endpoint-summary = saltato — profile.toml senza { $field }
doctor-skipped-no-profile-summary = saltato — nessun endpoint del profilo leggibile per questo contesto
doctor-skipped-no-portal-token-summary = saltato — nessun token del portale leggibile per questo contesto
status-ztol-tcp-ok = la porta del router { $host }:{ $port } accetta TCP (solo porta — vedi sessione)
status-session-ok = sessione zenoh mTLS aperta in { $ms }ms (il router ha accettato questo certificato)
status-session-skipped = sessione non tentata (fuori mesh; vedi ztol)
status-session-material-missing = sessione zenoh non tentata: { $detail }
status-session-config-failed = configurazione della sessione zenoh non riuscita: { $err }
status-session-failed = sessione zenoh non aperta: { $err } — esegui `goat doctor`
status-session-timeout = sessione zenoh non aperta in { $secs }s — la porta del router accetta TCP ma nessuna sessione si completa (router bloccato, certificato rifiutato o clock skew); esegui `goat doctor`
status-names-live = nomi live (DNS mesh ha risposto)
status-names-ip-only = nomi non verificati (gli endpoint del profilo sono IP letterali)
status-names-snapshot = nomi da snapshot firmato serial { $serial }, età { $age } (DNS mesh non raggiungibile)
status-names-snapshot-aging = nomi da snapshot firmato serial { $serial }, età { $age } — OBSOLETO (DNS mesh non raggiungibile; ripara goat-coredns, poi i nomi si aggiornano da soli)
status-names-snapshot-daemon = nomi da snapshot firmato serial { $serial }, età { $age } — store del daemon (DNS mesh non raggiungibile)
status-names-snapshot-daemon-aging = nomi da snapshot firmato serial { $serial }, età { $age } — store del daemon, OBSOLETO (DNS mesh non raggiungibile; ripara goat-coredns, poi i nomi si aggiornano da soli)
status-names-failed = DNS mesh non raggiungibile e fallback su snapshot non disponibile — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ATTIVO — { $qr }/{ $connected } peer connessi resistenti ai quanti
status-pqc-off = wireguard PQC (rosenpass) DISATTIVO — handshake wireguard classici
status-pqc-unknown = wireguard PQC sconosciuto — { $reason }
status-device-none = protezione dei nomi a livello dispositivo DISATTIVATA — nessuno store nomi goat-clientd (i comandi goat hanno comunque il fallback; abilita come da name-fallback-resolver.md §9.2)
status-device-broken = store nomi goat-clientd presente (serial { $serial }) ma il suo forwarder NON risponde su { $addr } — daemon fermo o sottosistema nomi bloccato
status-device-active = protezione dei nomi a livello dispositivo ATTIVA — goat-clientd davanti al DNS mesh del sistema su { $addr } (serial store { $serial }, { $grade })
status-device-not-fronted = forwarder goat-clientd attivo su { $addr } (serial store { $serial }, { $grade }) — sistema non coperto; i browser usano il DNS mesh diretto
status-device-fronting-unknown = forwarder goat-clientd attivo su { $addr } (serial store { $serial }, { $grade }) — stato di copertura del sistema non rilevabile su questa piattaforma
status-names-observed = nomi NON CANONICI (ad hoc): record osservato, età { $age } — non presente nello snapshot firmato (DNS mesh non raggiungibile; codifica questo record)
status-names-selfclaim = nomi tramite SELF-CLAIM verificato, età { $age } — firmato dall'anchor del nome stesso (DNS mesh non raggiungibile)
doctor-names-note = da snapshot serial { $serial }, età { $age }
doctor-names-selfclaim-note = tramite SELF-CLAIM verificato, età { $age } — firmato dall'anchor del nome stesso
doctor-names-selfclaim-hint = DNS mesh non raggiungibile — questo passo è stato risolto tramite un SELF-CLAIM verificato (età { $age }): l'anchor del nome ha firmato questo binding e i peer_bindings dello snapshot in cache lo autorizzano. Firmato ma non live; ripristina il DNS mesh per eliminare questo avviso.
doctor-names-observed-note = tramite record OSSERVATO (ad hoc), età { $age } — non canonico
doctor-names-observed-hint = DNS mesh non raggiungibile — questo passo è stato risolto tramite un record osservato NON CANONICO (età { $age }): il nome è solo live, mai codificato nel registro del sito. Codificalo (registro group_vars → generatore → nuova firma) o scadrà; ripristina il DNS mesh per eliminare questo avviso.
doctor-names-hint = DNS mesh non raggiungibile — questo passo è stato risolto tramite lo snapshot firmato dei nomi (serial { $serial }, età { $age }). La fabric resta raggiungibile con i nomi di fallback; ripristina il DNS mesh (goat-coredns sull'host mgmt) per eliminare questo avviso.
storm-brake-plan-header = Piano storm brake (ADR 1081 Layer 4 — manuale):
storm-brake-plan-target = host mgmt { $user }@{ $host }  (contesto { $context })
storm-brake-step-flush = riavvia il container signal `{ $container }` (elimina gli stream zombie)
storm-brake-step-tighten = limita il limiter L2 su `{ $container }` (dimezza il rate-limit, alza il TTL della neg-cache) — { $window } min, ripristino manuale a cura dell'operatore; saltato se L2 non è distribuito
storm-brake-step-shed = metti in pausa l'unità publisher non essenziale `{ $unit }`
storm-brake-shed-none = (nessuna unità da sospendere dichiarata — imposta storm_brake_shed_units in profile.toml o passa --shed-unit)
storm-brake-announce-note = emetti un OperatorActionV1 nell'audit trail con l'esito di ogni step
storm-brake-dryrun-note = dry run — non è stato eseguito nulla.
storm-brake-triggers-header = Attiva quando la banda host-net di relay-health mostra:
storm-brake-trigger-futility = rapporto di futilità del signaling >= 0.5 con >= 100 msg/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = perdita uplink >= 5% per 10 min consecutivi (wan.loss_pct)
storm-brake-triggers-band-url = banda host: relay-health.{ $site }.<zone> (vedi docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Nota: un flush del signal da solo NON ripristina la mesh. La cura sono i Layer 1-2 (backoff client + signal shedding); il brake serve a guadagnare tempo per il recupero.
storm-brake-confirm = Attivare lo storm brake su { $user }@{ $host }? Riavvia il container signal e mette in pausa i publisher dichiarati. [y/N]
storm-brake-aborted = storm brake annullato (nessuna conferma)
storm-brake-executing = attivazione dello storm brake su { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = fallito
storm-brake-outcome-skipped = saltato
storm-brake-summary = storm brake completato: { $ok } ok, { $failed } falliti, { $skipped } saltati
storm-brake-flush-failed = FLUSH fallito — il container signal non è stato riavviato, quindi non ci si aspetta il recupero della mesh: { $reason }
storm-brake-mgmt-unresolved = host mgmt non risolto — passa --mgmt-host, imposta mgmt_ssh_host nel profile.toml del contesto, oppure esporta GOAT_MGMT_SSH_HOST
storm-brake-audit-note = azione operatore registrata nell'audit trail (attore { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = il portale ha risposto HTTP 404 per `{ $path }`: non registrato lì
error-portal-rate-limited-summary = il portale ha risposto HTTP 429 per `{ $path }`: troppe richieste, riprovare più tardi
error-portal-server-error-summary = il portale ha risposto HTTP { $status } per `{ $path }`: un guasto lato portale
error-portal-refused-summary = il portale ha risposto HTTP { $status } per `{ $path }`: richiesta rifiutata

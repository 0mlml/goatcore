### goat-cli — French (fr). Tier-1 seed, COMPLETE for the externalized
### surface (all 8 error summaries). French is one of NATO's two official
### languages (with English), the defensible member-nation seed.
### source: machine-assisted (Claude), NOT native-reviewed
### review: machine
### reconciled-against-en: introduced with track/goat-cli-i18n (this change-set)
### coverage: complete for the errors.rs surface; grows as more surfaces
###           externalize. `ZTOL`, `mTLS`, `Bearer`, `bundle`, and `topic`
###           are kept as contract/domain terms, not localized.

error-profile-missing-summary = aucun profil pour le contexte `{ $context }`
error-off-mesh-summary = hors ZTOL — non connecté au maillage
error-router-unreachable-summary = routeur Zenoh à `{ $endpoint }` injoignable
error-cert-expired-summary = certificat mTLS expiré ou rejeté par le routeur
error-portal-unreachable-summary = API du portail à `{ $endpoint }` injoignable
error-token-invalid-summary = le portail a rejeté le jeton Bearer
error-topic-conflict-summary = le topic `{ $topic }` existe déjà
error-bundle-invalid-summary = échec de vérification de la signature ou de l'expiration du bundle

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = point de terminaison du routeur mal formé
doctor-step1-endpoint-malformed-hint = format attendu `<proto>/<host>:<port>` ; reçu `{ $endpoint }`
doctor-step1-ok-summary = connexion TCP à l'hôte du routeur { $host }:{ $port } réussie
doctor-step1-conn-failed-summary = échec de la connexion TCP à l'hôte du routeur
doctor-step1-conn-failed-hint = { $err } — démarrez netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`) ; vérifiez `netbird status`
doctor-step1-timeout-summary = délai dépassé pour la connexion TCP à l'hôte du routeur
doctor-step1-timeout-hint = hôte du routeur { $host }:{ $port } injoignable en { $ms }ms — probablement hors du maillage
doctor-step2-endpoint-malformed-hint = corrigez l'émetteur du bundle (--goat-cli-router-endpoint doit être `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM des racines CA manquant ou illisible
doctor-step2-ca-missing-hint = { $err } — relancez `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = routeur { $host }:{ $port } accepte le TCP (TLS validé à l'étape 3)
doctor-step2-refused-summary = TCP du routeur refusé
doctor-step2-refused-hint = { $err } — le routeur est peut-être hors service ; vérifiez le déploiement Zenoh sur le mgmt-host
doctor-step2-timeout-summary = délai TCP du routeur dépassé
doctor-step2-timeout-hint = routeur { $host }:{ $port } injoignable en { $ms }ms
doctor-step3-config-failed-summary = échec de construction de la configuration zenoh
doctor-step3-config-failed-hint = point de terminaison invalide `{ $endpoint }` : { $err }
doctor-step3-tls-config-failed-summary = échec de construction de la configuration TLS zenoh
doctor-step3-tls-config-failed-hint = { $err } — probablement une dérive de version zenoh ; vérifiez la correspondance des dépendances
doctor-step3-ok-summary = session mTLS ouverte — certificat accepté par l'ACL du routeur
doctor-step3-open-failed-summary = échec de l'ouverture de la session zenoh
doctor-step3-open-failed-hint = { $err } — le certificat est peut-être expiré (exécutez `goat profile show`) ou rejeté par l'ACL du routeur (incohérence URI-SAN)
doctor-step3-timeout-summary = délai d'ouverture de la session zenoh dépassé
doctor-step3-timeout-hint = le routeur n'a pas terminé la poignée de main en { $secs }s — décalage d'horloge, mauvais certificat ou congestion
doctor-step4-ok-summary = le portail a renvoyé { $n } ligne(s) d'organisation
doctor-step4-token-rejected-summary = le portail a rejeté le jeton Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — jeton expiré ou révoqué ; ré-enrôlez via operator-onboard.yml
doctor-step4-request-failed-summary = échec de la requête au portail
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = délai de la requête au portail dépassé
doctor-step4-timeout-hint = aucune réponse en { $secs }s — vérifiez que { $endpoint } est joignable depuis le maillage
doctor-skipped-summary = ignoré — l'étape précédente a échoué (relancez avec --all pour forcer)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Ligne de commande opérateur unifiée pour la fabrique de données goat.
help-long-about = goat — CLI orientée opérateur qui compose panoscope-core en un seul outil : initialisation/réutilisation de profil, doctor de joignabilité sensible à ZTOL, navigation dans le portail des organisations, CRUD de topic, pub/sub, délégation à stethoscope, compositeur de commandes interactif et narration de la cascade du substrat.
help-flag-profile-dir = Remplacer le répertoire de profil (par défaut : $GOAT_PROFILE_DIR ou ~/.goat).
help-flag-context = Remplacer le contexte actif (par défaut : variable $GOAT_CONTEXT, puis current_context dans config.toml, puis "default").
help-flag-narrate = Afficher la cascade du substrat qu'une commande de mutation déclenche.
help-flag-verbose = Activer la sortie de débogage détaillée (ou définir RUST_LOG=goat=debug).
help-flag-color = Mode couleur pour la sortie du terminal (auto|always|never).
help-flag-lang = Langue du texte destiné aux humains (p. ex. en, uk). Se rabat sur GOAT_LANG, puis la locale du système, puis l'anglais.
help-cmd-profile = Gérer le profil goat local (certificat mTLS + jeton de portail + ensemble de points de terminaison).
help-cmd-context = Gérer les contextes (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Exécuter un sondeur de joignabilité par étapes contre la fabrique.
help-cmd-doctor-long = Sondeur de joignabilité par étapes contre la fabrique : appartenance ZTOL → routeur Zenoh → acceptation du certificat → portail. Chaque étape échouée émet un indice de remédiation spécifique.
help-cmd-vendor = Parcourir les organisations enregistrées dans le portail.
help-cmd-topic = Lister, afficher, créer ou supprimer des topics.
help-cmd-pub = Publier une charge utile sur une clé de topic.
help-cmd-sub = S'abonner à une expression de clé de topic.
help-cmd-inspect = Déléguer une demande de verdict de diagnostic à stethoscope.
help-cmd-ui = Sélecteur fusionné interactif : parcourir le registre declared ⊕ live et basculer vers la lentille Diagnose / Tail sur la cible sélectionnée.
help-cmd-compose = TUI interactif de construction de commandes.
help-cmd-narrate = Réflexion sur la cascade du substrat à partir de l'activité récente. Pas encore disponible (la voie courante est --narrate sur une commande de mutation).
help-cmd-completion = Imprimer le script de complétion du shell sur stdout.
help-cmd-explain = Rechercher la signification d'un code d'erreur + sa remédiation dans le document `goat-cli-error-codes.md` intégré (modèle cargo --explain).
help-cmd-mock = Publier des échantillons synthétiques sur un topic. Par défaut : un JSON hello-world trivial à 1Hz. Passez `--format <.fds>` + `--message <Name>` pour encoder à la place du protobuf fourni par l'opérateur.
help-cmd-status = Synthèse de l'état de santé : joignabilité ZTOL + portail /api/health + expiration du certificat local, trois lignes de glyph ou un objet --json.
help-cmd-services = Sonder chaque service hébergé dans `ops/serve/services.yaml` et afficher une ligne de glyph par service (ou un objet --json).
help-cmd-client = Encapsuler le daemon goat-clientd local via son socket IPC JSON-RPC (`~/.goat-client/goat-clientd.sock`) : status / up / down / bundle import.
help-cmd-plugin = Découverte d'extensions basée sur le PATH (à la kubectl). Tout exécutable `goat-<name>` sur le PATH est appelable en tant que `goat <name>` ; `goat plugin list` énumère les plugins installés, `goat plugin which <name>` résout le chemin.

# goat status / inspect / --narrate prose d'exécution (ADR 1024).
status-ztol-malformed = point de terminaison du routeur mal formé : `{ $endpoint }`
status-ztol-refused = TCP refusé vers le routeur { $host }:{ $port } : { $err }
status-ztol-unreachable = hors maillage (routeur { $host }:{ $port } injoignable en { $ms }ms)
status-portal-ok = portail /api/health 2xx
status-portal-failed = échec du portail /api/health : { $err }
status-portal-timeout = délai dépassé pour le portail /api/health en { $secs }s
status-cert-read-failed = échec de lecture du certificat : { $err }
status-cert-pem-parse-failed = échec d'analyse PEM du certificat : { $err }
status-cert-wrong-tag = le premier bloc PEM est { $tag }, attendu CERTIFICATE
status-cert-x509-parse-failed = échec d'analyse x509 : { $err }
status-cert-expired = certificat EXPIRÉ à unix={ $not_after }, maintenant={ $now }
status-cert-valid = certificat valide ; expire dans { $days_left }d (unix={ $not_after })
inspect-finding-label = constat ({ $severity }) : { $label } — { $detail }
inspect-remedy-label = remède : { $title } — { $detail }
inspect-no-outcomes = (aucun résultat — la cible n'a correspondu à aucun contrôle)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put accepté sur `{ $key }` ({ $bytes } octets)
narrator-stethoscope = stethoscope : { $check_id } → { $verdict } ({ $summary })
narrator-settled = cascade stabilisée — { $summary } ({ $n } { $n ->
        [one] conséquence
       *[other] conséquences
    })

# Aide en une ligne pour l'onboarding/la doc goat + le bouquet final tout-vert (ADR 1024).
help-cmd-init = Assistant de premier lancement : choisissez sandbox ou réel, vérifiez que ça marche, orientez vers le tuteur.
help-cmd-tutor = Une visite guidée pratique et à votre rythme de la fabrique (style vimtutor ; reprenable).
help-cmd-docs = Parcourir la doc goat, ou émettre la référence complète des commandes (--markdown).
help-cmd-hello = Faire un aller-retour d'un hello à travers la fabrique de bout en bout (env sandbox ou réel).
flourish-all-green = 🐐 tout est vert — fabrique nominale

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = action réservée aux opérateurs — vous n'êtes pas autorisé (`goat admin` est inerte pour les non-administrateurs)
help-flag-narrate-json = Émet la cascade de substrat en NDJSON (un objet par conséquence) pour les journaux d'incident et le scripting. Implique --narrate.
help-cmd-admin = Administration réservée aux opérateurs : cycle de vie des organisations, intégration/révocation/rotation, coupure mesh-peer, provisionnement appareil + niveau opérateur, triage LAN. Autorisé par vos identifiants opérateur (côté serveur via le scope token portal/NetBird) ; inerte pour les non-administrateurs — un appel non autorisé ne modifie rien et échoue avec `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = le profil du contexte `{ $context }` existe mais n'est pas lisible par cet utilisateur (permission refusée)
error-portal-bad-response-summary = le portail a répondu, mais cette version de goat n'a pas pu analyser la réponse
doctor-step0-ok-summary = la configuration locale est prête pour les sondes réseau
doctor-step0-warn-summary = la configuration locale comporte des vérifications non confirmées
doctor-step0-fail-summary = la configuration locale demande votre attention
doctor-step0-profile-absent-summary = aucun profil goat pour le contexte `{ $context }`
doctor-step0-profile-new-hint = vérifiez avec `goat context ls` ; si c'est votre première configuration, lancez `goat ca add <bundle-ca.pem>` et comparez son empreinte avec celle de votre opérateur, puis `goat profile init <bundle.cbor>` ; relancez `goat doctor`
doctor-step0-profile-absent-hint = vérifiez avec `goat context ls` ; sélectionnez un contexte existant avec `goat context use <name>`, ou lancez `goat profile init <bundle.cbor>` pour ce contexte ; relancez `goat doctor`
doctor-step0-profile-denied-summary = le profil goat à { $path } est illisible
doctor-step0-profile-denied-hint = corrigez le propriétaire du profil ou exécutez en tant qu'utilisateur propriétaire ; ne réinitialisez pas un profil existant
doctor-step0-profile-incomplete-summary = le fichier de profil goat { $file } est manquant ou illisible
doctor-step0-profile-incomplete-hint = restaurez le fichier indiqué depuis le bundle fourni, ou utilisez `goat profile rotate <bundle.cbor>` pour remplacer ce contexte ; relancez `goat doctor`
doctor-step0-profile-invalid-summary = le profil goat a des endpoints illisibles ou manquants
doctor-step0-profile-empty-token-summary = le profil goat contient un jeton de portail vide
doctor-step0-profile-ok-summary = le contexte goat `{ $context }` a un profil lisible
doctor-step0-client-absent-summary = aucun client mesh pris en charge n'a répondu
doctor-step0-client-absent-hint = installez le client mesh fourni par votre goatnet, démarrez son daemon, puis relancez `goat doctor`
doctor-step0-client-stopped-summary = le client mesh installé n'a pas renvoyé d'état exploitable
doctor-step0-client-stopped-hint = vérifiez avec `netbird status -d` ou `goat client status --detail` ; démarrez le daemon installé (`sudo netbird service start` pour NetBird), puis relancez `goat doctor`
doctor-step0-netbird-ok-summary = le daemon NetBird a répondu (profil signalé `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd a répondu (état `{ $state }`)
doctor-step0-management-connected-summary = NetBird signale le management connecté
doctor-step0-management-disconnected-summary = NetBird signale le management déconnecté
doctor-step0-management-disconnected-hint = vérifiez l'URL de management signalée et `netbird status -d` ; un tunnel déjà établi peut continuer à fonctionner pendant une panne du management
doctor-step0-management-unknown-summary = l'endpoint de management configuré n'a pas pu être établi localement
doctor-step0-management-unknown-hint = inspectez `netbird status -d` ou `goat client diagnostics` ; doctor ne devine pas l'URL de management
doctor-step0-tunnel-down-summary = NetBird ne signale aucune adresse mesh
doctor-step0-tunnel-down-hint = lancez `netbird status -d` ; si le client est enrôlé et que le démon répond, lancez `goat heal`, puis relancez `goat doctor`
doctor-step0-tunnel-address-summary = NetBird signale l'adresse mesh { $address } ; l'accessibilité du routeur est vérifiée ensuite
doctor-step0-goat-client-no-bundle-summary = goat-clientd n'a aucun bundle importé
doctor-step0-goat-client-no-bundle-hint = lancez `goat client bundle import <bundle.cbor>` avec le bundle d'enrôlement du client, connectez le client, puis relancez `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd signale un tunnel connecté ; l'accessibilité du routeur est vérifiée ensuite
doctor-step0-goat-client-outer-disabled-summary = le tunnel externe de goat-clientd est désactivé en mode netbird-only
doctor-step0-goat-client-down-summary = goat-clientd signale l'état de tunnel `{ $state }`
doctor-step0-goat-client-down-hint = lancez `goat client diagnostics`, puis `goat client up` si le bundle est chargé ; relancez `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd signale son mesh interne connecté
doctor-step0-goat-client-inner-disabled-summary = le mesh interne de goat-clientd est désactivé en mode wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd signale l'état du mesh interne `{ $state }`
doctor-step0-goat-client-inner-down-hint = lancez `goat client diagnostics` pour inspecter le mesh interne, puis relancez `goat doctor` après l'avoir rétabli
doctor-step0-management-reachable-summary = le endpoint de management a répondu HTTP { $status }
doctor-step0-management-auth-summary = le endpoint HTTPS de management est joignable (HTTP { $status } : autorisation requise pour cette sonde)
doctor-step0-management-unexpected-summary = le endpoint de management a renvoyé un HTTP { $status } inattendu
doctor-step0-management-unexpected-hint = vérifiez l'URL de management configurée et demandez à votre opérateur goatnet de contrôler cette réponse
doctor-step0-management-unreachable-summary = le endpoint de management configuré est injoignable : { $error }
doctor-step0-management-unreachable-hint = vérifiez le DNS et l'accès réseau vers l'URL indiquée par le client ; un tunnel existant peut rester fonctionnel
doctor-step0-clock-ok-summary = l'horloge locale est à moins de { $seconds } secondes de l'heure HTTP du management
doctor-step0-clock-skew-summary = l'horloge locale diffère de { $seconds } secondes de l'heure HTTP du management
doctor-step0-clock-skew-hint = synchronisez l'horloge de l'appareil, puis relancez `goat doctor` ; la validation des certificats peut échouer tant que l'horloge est fausse
doctor-step0-clock-unknown-summary = impossible de mesurer le décalage d'horloge à partir de l'en-tête HTTP Date du management
doctor-step0-clock-unknown-hint = vérifiez la synchronisation de l'heure de l'appareil si les contrôles TLS ou de certificat échouent
error-clock-skew-summary = l'horloge de l'appareil est trop éloignée de l'heure de gestion
error-doctor-inconclusive-summary = doctor a observé un échec sans cause établie
error-portal-forbidden-summary = le portail a refusé à ce compte l'accès à l'API sondée
doctor-step4-forbidden-summary = le portail a renvoyé HTTP 403 pour ce profil
doctor-step4-forbidden-hint = ce credential a atteint le portail mais n'a pas accès à l'API sondée ; vérifiez `goat profile show` et demandez à votre opérateur goatnet de vérifier l'autorisation
doctor-step4-decode-summary = le portail a répondu avec une réponse que ce goat ne sait pas lire
doctor-step4-decode-hint = { $err } — mettez goat à jour vers la version servie par ce goatnet, puis relancez `goat doctor`
doctor-step1-control-only-summary = connecté au serveur de contrôle uniquement — aucun tunnel vers les { $total } pairs
doctor-step1-control-only-hint = ce réseau laisse sortir le TCP 443 mais bloque les ports de données du maillage ; rien sur le maillage (pages de connexion comprises) n'est donc joignable depuis cet appareil. Demandez à l'équipe réseau d'autoriser le TCP et l'UDP sortants vers { $relays }, et l'UDP 3478 vers { $stun } ; l'UDP direct entre appareils est également recommandé. Pour confirmer, connectez-vous à un partage de connexion de téléphone et lancez `netbird status` : les pairs devraient alors se connecter.
doctor-offmesh-dashboard-hint = flotte de relais (point de vue externe) : https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = le certificat local, la clé correspondante et le bundle CA sont lisibles ; le certificat est dans sa période de validité
doctor-step2-material-failed-summary = le matériel TLS local { $file } est inutilisable
doctor-step2-material-failed-hint = { $err } — inspectez le fichier indiqué ; restaurez-le depuis le bundle émis ou faites une rotation du profil
doctor-step2-material-invalid-hint = { $file } n'est pas un certificat X.509 PEM valide ; restaurez le matériel émis ou lancez `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem est invalide ou ne correspond pas à mtls.cert.pem ; restaurez le matériel émis ou lancez `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = le certificat local est hors de sa période de validité
doctor-step2-cert-time-hint = lancez `goat profile show` ; vérifiez le résultat d'horloge ci-dessus, puis demandez un nouveau bundle à votre opérateur et lancez `goat profile rotate <new-bundle.cbor>` si le certificat a expiré
doctor-skipped-no-endpoint-summary = ignoré — { $field } absent de profile.toml
doctor-skipped-no-profile-summary = ignoré — aucun endpoint de profil lisible pour ce contexte
doctor-skipped-no-portal-token-summary = ignoré — aucun jeton de portail lisible pour ce contexte
status-ztol-tcp-ok = le port du routeur { $host }:{ $port } accepte le TCP (port seulement — voir session)
status-session-ok = session zenoh mTLS ouverte en { $ms } ms (le routeur a accepté ce certificat)
status-session-skipped = session non tentée (hors maillage ; voir ztol)
status-session-material-missing = session zenoh non tentée : { $detail }
status-session-config-failed = échec de la configuration de la session zenoh : { $err }
status-session-failed = la session zenoh ne s'est pas ouverte : { $err } — lancer `goat doctor`
status-session-timeout = la session zenoh ne s'est pas ouverte en { $secs }s — le port du routeur accepte le TCP mais aucune session n'aboutit (routeur bloqué, certificat rejeté ou décalage d'horloge) ; lancer `goat doctor`
status-names-live = noms live (DNS mesh a répondu)
status-names-ip-only = noms non testés (les endpoints du profil sont des IP littérales)
status-names-snapshot = noms d'après le snapshot signé série { $serial }, âge { $age } (DNS mesh injoignable)
status-names-snapshot-aging = noms d'après le snapshot signé série { $serial }, âge { $age } — PÉRIMÉ (DNS mesh injoignable ; réparer goat-coredns, les noms se rafraîchiront ensuite automatiquement)
status-names-snapshot-daemon = noms d'après le snapshot signé série { $serial }, âge { $age } — magasin du daemon (DNS mesh injoignable)
status-names-snapshot-daemon-aging = noms d'après le snapshot signé série { $serial }, âge { $age } — magasin du daemon, PÉRIMÉ (DNS mesh injoignable ; réparer goat-coredns, les noms se rafraîchiront ensuite automatiquement)
status-names-failed = DNS mesh injoignable et repli sur snapshot indisponible — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ACTIF — { $qr }/{ $connected } pairs connectés résistants au quantique
status-pqc-off = wireguard PQC (rosenpass) INACTIF — handshakes wireguard classiques
status-pqc-unknown = wireguard PQC inconnu — { $reason }
status-device-none = protection des noms à l'échelle de l'appareil INACTIVE — pas de magasin de noms goat-clientd (les commandes goat ont toujours le repli ; activer selon name-fallback-resolver.md §9.2)
status-device-broken = magasin de noms goat-clientd présent (série { $serial }) mais son forwarder NE répond PAS sur { $addr } — daemon arrêté ou sous-système de noms bloqué
status-device-active = protection des noms à l'échelle de l'appareil ACTIVE — goat-clientd devant le DNS mesh de l'OS sur { $addr } (magasin série { $serial }, { $grade })
status-device-not-fronted = forwarder goat-clientd actif sur { $addr } (magasin série { $serial }, { $grade }) — OS non fronté ; les navigateurs utilisent le DNS mesh direct
status-device-fronting-unknown = forwarder goat-clientd actif sur { $addr } (magasin série { $serial }, { $grade }) — état du fronting de l'OS indétectable sur cette plateforme
status-names-observed = noms NON CANONIQUES (ad hoc) : enregistrement observé, âge { $age } — absent du snapshot signé (DNS mesh injoignable ; codifier cet enregistrement)
status-names-selfclaim = noms via AUTO-REVENDICATION vérifiée, âge { $age } — signée par l'ancre propre du nom (DNS mesh injoignable)
doctor-names-note = via snapshot série { $serial }, âge { $age }
doctor-names-selfclaim-note = via AUTO-REVENDICATION vérifiée, âge { $age } — signée par l'ancre propre du nom
doctor-names-selfclaim-hint = DNS mesh injoignable — cette étape a été résolue via une AUTO-REVENDICATION vérifiée (âge { $age }) : l'ancre propre du nom a signé ce binding et les peer_bindings du snapshot en cache l'autorisent. Signé mais pas live ; réparer le DNS mesh pour l'effacer.
doctor-names-observed-note = via enregistrement OBSERVÉ (ad hoc), âge { $age } — non canonique
doctor-names-observed-hint = DNS mesh injoignable — cette étape a été résolue via un enregistrement observé NON CANONIQUE (âge { $age }) : le nom n'existe qu'en live, jamais codifié dans le registre du site. Le codifier (registre group_vars → générateur → re-signature) sinon il expire ; réparer le DNS mesh pour l'effacer.
doctor-names-hint = DNS mesh injoignable — cette étape a été résolue via le snapshot de noms signé (série { $serial }, âge { $age }). Le fabric reste joignable via les noms de repli ; réparer le DNS mesh (goat-coredns sur l'hôte mgmt) pour l'effacer.
storm-brake-plan-header = Plan de frein anti-tempête (ADR 1081 couche 4 — manuel) :
storm-brake-plan-target = hôte mgmt { $user }@{ $host }  (contexte { $context })
storm-brake-step-flush = redémarrer le conteneur de signalisation `{ $container }` (supprime les flux zombies)
storm-brake-step-tighten = brider le limiteur L2 sur `{ $container }` (débit limite divisé par deux, TTL du neg-cache augmenté) — { $window } min, rétablissement manuel ; ignoré si L2 non déployé
storm-brake-step-shed = mettre en pause l'unité d'éditeur non essentielle `{ $unit }`
storm-brake-shed-none = (aucune unité à délester déclarée — définir storm_brake_shed_units dans profile.toml ou passer --shed-unit)
storm-brake-announce-note = émettre un OperatorActionV1 dans la piste d'audit avec le résultat de chaque étape
storm-brake-dryrun-note = simulation — rien n'a été exécuté.
storm-brake-triggers-header = Déclencher quand la bande host-net de relay-health indique :
storm-brake-trigger-futility = ratio d'inutilité de la signalisation >= 0,5 à >= 100 msgs/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = perte de liaison montante >= 5 % pendant 10 min (wan.loss_pct)
storm-brake-triggers-band-url = bande hôte : relay-health.{ $site }.<zone> (voir docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Remarque : un simple vidage de la signalisation ne rétablit PAS le maillage. Les couches 1-2 (backoff client + délestage de la signalisation) sont le remède ; le frein gagne du temps pour la récupération.
storm-brake-confirm = Déclencher le frein anti-tempête sur { $user }@{ $host } ? Redémarre le conteneur de signalisation et met en pause les éditeurs déclarés. [o/N]
storm-brake-aborted = frein anti-tempête annulé (pas de confirmation)
storm-brake-executing = déclenchement du frein anti-tempête sur { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = échec
storm-brake-outcome-skipped = ignoré
storm-brake-summary = frein anti-tempête terminé : { $ok } ok, { $failed } en échec, { $skipped } ignoré(s)
storm-brake-flush-failed = échec du FLUSH — le conteneur de signalisation n'a pas redémarré, la récupération du maillage n'est donc pas attendue : { $reason }
storm-brake-mgmt-unresolved = hôte mgmt non résolu — passer --mgmt-host, définir mgmt_ssh_host dans le profile.toml du contexte, ou exporter GOAT_MGMT_SSH_HOST
storm-brake-audit-note = action opérateur enregistrée dans la piste d'audit (acteur { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = le portail a répondu HTTP 404 pour `{ $path }` : non enregistré à cet endroit
error-portal-rate-limited-summary = le portail a répondu HTTP 429 pour `{ $path }` : trop de requêtes, réessayez plus tard
error-portal-server-error-summary = le portail a répondu HTTP { $status } pour `{ $path }` : une panne du côté du portail
error-portal-refused-summary = le portail a répondu HTTP { $status } pour `{ $path }` : requête refusée

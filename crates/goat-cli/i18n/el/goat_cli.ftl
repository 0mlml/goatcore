### goat-cli — Greek (el). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = δεν υπάρχει προφίλ για το πλαίσιο `{ $context }`
error-off-mesh-summary = εκτός ZTOL — χωρίς σύνδεση στο mesh
error-router-unreachable-summary = ο δρομολογητής Zenoh στο `{ $endpoint }` δεν είναι προσβάσιμος
error-cert-expired-summary = το πιστοποιητικό mTLS έληξε ή απορρίφθηκε από τον δρομολογητή
error-portal-unreachable-summary = το API της πύλης στο `{ $endpoint }` δεν είναι προσβάσιμο
error-token-invalid-summary = η πύλη απέρριψε το διακριτικό Bearer
error-topic-conflict-summary = το topic `{ $topic }` υπάρχει ήδη
error-bundle-invalid-summary = η επαλήθευση υπογραφής ή λήξης του bundle απέτυχε

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = εσφαλμένη μορφή τελικού σημείου δρομολογητή
doctor-step1-endpoint-malformed-hint = αναμενόταν `<proto>/<host>:<port>`· ελήφθη `{ $endpoint }`
doctor-step1-ok-summary = η σύνδεση TCP στον κεντρικό υπολογιστή δρομολογητή { $host }:{ $port } πέτυχε
doctor-step1-conn-failed-summary = η σύνδεση TCP στον κεντρικό υπολογιστή δρομολογητή απέτυχε
doctor-step1-conn-failed-hint = { $err } — εκκινήστε το netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`)· ελέγξτε `netbird status`
doctor-step1-timeout-summary = λήξη χρονικού ορίου σύνδεσης TCP στον κεντρικό υπολογιστή δρομολογητή
doctor-step1-timeout-hint = ο κεντρικός υπολογιστής δρομολογητή { $host }:{ $port } δεν είναι προσβάσιμος σε { $ms }ms — πιθανώς εκτός mesh
doctor-step2-endpoint-malformed-hint = διορθώστε τον εκδότη του bundle (το --goat-cli-router-endpoint πρέπει να είναι `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = το PEM ριζών CA λείπει ή δεν είναι αναγνώσιμο
doctor-step2-ca-missing-hint = { $err } — εκτελέστε ξανά `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = ο δρομολογητής { $host }:{ $port } δέχεται TCP (το TLS επικυρώθηκε στο βήμα 3)
doctor-step2-refused-summary = το TCP του δρομολογητή απορρίφθηκε
doctor-step2-refused-hint = { $err } — ο δρομολογητής μπορεί να είναι εκτός λειτουργίας· ελέγξτε την ανάπτυξη Zenoh στο mgmt-host
doctor-step2-timeout-summary = λήξη χρονικού ορίου TCP του δρομολογητή
doctor-step2-timeout-hint = ο δρομολογητής { $host }:{ $port } δεν είναι προσβάσιμος σε { $ms }ms
doctor-step3-config-failed-summary = αποτυχία δημιουργίας διαμόρφωσης zenoh
doctor-step3-config-failed-hint = μη έγκυρο τελικό σημείο `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = αποτυχία δημιουργίας διαμόρφωσης TLS zenoh
doctor-step3-tls-config-failed-hint = { $err } — πιθανώς απόκλιση έκδοσης zenoh· επαληθεύστε ότι οι εξαρτήσεις ταιριάζουν
doctor-step3-ok-summary = η συνεδρία mTLS άνοιξε — το πιστοποιητικό έγινε αποδεκτό από την ACL του δρομολογητή
doctor-step3-open-failed-summary = αποτυχία ανοίγματος συνεδρίας zenoh
doctor-step3-open-failed-hint = { $err } — το πιστοποιητικό μπορεί να έχει λήξει (εκτελέστε `goat profile show`) ή να απορρίφθηκε από την ACL του δρομολογητή (αναντιστοιχία URI-SAN)
doctor-step3-timeout-summary = λήξη χρονικού ορίου ανοίγματος συνεδρίας zenoh
doctor-step3-timeout-hint = ο δρομολογητής δεν ολοκλήρωσε τη χειραψία σε { $secs }s — απόκλιση ρολογιού, λάθος πιστοποιητικό ή συμφόρηση
doctor-step4-ok-summary = η πύλη επέστρεψε { $n } γραμμή(ές) οργανισμού
doctor-step4-token-rejected-summary = η πύλη απέρριψε το διακριτικό Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — το διακριτικό έληξε ή ανακλήθηκε· επανεγγραφείτε μέσω operator-onboard.yml
doctor-step4-request-failed-summary = το αίτημα προς την πύλη απέτυχε
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = λήξη χρονικού ορίου αιτήματος προς την πύλη
doctor-step4-timeout-hint = καμία απόκριση σε { $secs }s — ελέγξτε ότι το { $endpoint } είναι προσβάσιμο από το mesh
doctor-skipped-summary = παραλείφθηκε — το προηγούμενο βήμα απέτυχε (εκτελέστε ξανά με --all για εξαναγκασμό)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Ενοποιημένη γραμμή εντολών χειριστή για το data fabric goat.
help-long-about = goat — CLI για χειριστές που συνθέτει το panoscope-core σε ένα εργαλείο: αρχικοποίηση/επαναχρησιμοποίηση προφίλ, doctor προσβασιμότητας με επίγνωση ZTOL, περιήγηση στην πύλη οργανισμών, CRUD topic, pub/sub, ανάθεση σε stethoscope, διαδραστικό συνθέτη εντολών και αφήγηση κλιμάκωσης substrate.
help-flag-profile-dir = Παράκαμψη του καταλόγου προφίλ (προεπιλογή: $GOAT_PROFILE_DIR ή ~/.goat).
help-flag-context = Παράκαμψη του ενεργού πλαισίου (προεπιλογή: μεταβλητή $GOAT_CONTEXT, μετά current_context στο config.toml, μετά "default").
help-flag-narrate = Εμφάνιση της κλιμάκωσης substrate που πυροδοτεί μια εντολή μεταβολής.
help-flag-verbose = Ενεργοποίηση αναλυτικής εξόδου εντοπισμού σφαλμάτων (ή ορίστε RUST_LOG=goat=debug).
help-flag-color = Λειτουργία χρώματος για την έξοδο τερματικού (auto|always|never).
help-flag-lang = Γλώσσα για το κείμενο που απευθύνεται σε ανθρώπους (π.χ. en, uk). Επιστρέφει στο GOAT_LANG, μετά στις τοπικές ρυθμίσεις του OS, μετά στα Αγγλικά.
help-cmd-profile = Διαχείριση του τοπικού προφίλ goat (πιστοποιητικό mTLS + διακριτικό πύλης + σύνολο τελικών σημείων).
help-cmd-context = Διαχείριση πλαισίων (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Εκτέλεση βηματικού ανιχνευτή προσβασιμότητας έναντι του fabric.
help-cmd-doctor-long = Βηματικός ανιχνευτής προσβασιμότητας έναντι του fabric: συμμετοχή ZTOL → δρομολογητής Zenoh → αποδοχή πιστοποιητικού → πύλη. Κάθε βήμα που αποτυγχάνει εκπέμπει μια συγκεκριμένη υπόδειξη αποκατάστασης.
help-cmd-vendor = Περιήγηση στους οργανισμούς που είναι καταχωρισμένοι στην πύλη.
help-cmd-topic = Παράθεση, εμφάνιση, δημιουργία ή διαγραφή topic.
help-cmd-pub = Δημοσίευση ωφέλιμου φορτίου σε ένα κλειδί topic.
help-cmd-sub = Εγγραφή σε μια έκφραση κλειδιού topic.
help-cmd-inspect = Ανάθεση αιτήματος διαγνωστικής ετυμηγορίας στο stethoscope.
help-cmd-ui = Διαδραστικός συγχωνευμένος επιλογέας: περιηγηθείτε στη δηλωμένη ⊕ ζωντανή λίστα και μεταβείτε στον φακό Diagnose / Tail στον επιλεγμένο στόχο.
help-cmd-compose = Διαδραστικός δημιουργός εντολών TUI.
help-cmd-narrate = Αναστοχασμός κλιμάκωσης substrate επί της πρόσφατης δραστηριότητας. Δεν είναι ακόμη διαθέσιμο (η καθημερινή διαδρομή είναι το --narrate σε μια εντολή μεταβολής).
help-cmd-completion = Εκτύπωση σεναρίου συμπλήρωσης κελύφους στο stdout.
help-cmd-explain = Αναζήτηση της σημασίας ενός κωδικού σφάλματος + αποκατάσταση στο ενσωματωμένο έγγραφο `goat-cli-error-codes.md` (μοτίβο cargo --explain).
help-cmd-mock = Δημοσίευση συνθετικών δειγμάτων σε ένα topic. Προεπιλογή: τετριμμένο hello-world JSON στο 1Hz. Περάστε `--format <.fds>` + `--message <Name>` για να κωδικοποιήσετε protobuf που παρέχεται από τον χειριστή.
help-cmd-status = Σύνθεση κατάστασης υγείας: προσβασιμότητα ZTOL + /api/health πύλης + τοπική λήξη πιστοποιητικού, τρεις γραμμές glyph ή ένα αντικείμενο --json.
help-cmd-services = Ανίχνευση κάθε φιλοξενούμενης υπηρεσίας στο `ops/serve/services.yaml` και απόδοση μιας γραμμής glyph ανά υπηρεσία (ή ενός αντικειμένου --json).
help-cmd-client = Τύλιξη του τοπικού daemon goat-clientd μέσω της υποδοχής JSON-RPC IPC (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = Ανακάλυψη επεκτάσεων βάσει PATH (στυλ kubectl). Οποιοδήποτε εκτελέσιμο `goat-<name>` στο PATH μπορεί να κληθεί ως `goat <name>`· το `goat plugin list` απαριθμεί τα εγκατεστημένα plugin, το `goat plugin which <name>` επιλύει τη διαδρομή.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = εσφαλμένη μορφή τελικού σημείου δρομολογητή: `{ $endpoint }`
status-ztol-refused = TCP απορρίφθηκε προς τον δρομολογητή { $host }:{ $port }: { $err }
status-ztol-unreachable = εκτός mesh (δρομολογητής { $host }:{ $port } μη προσβάσιμος σε { $ms }ms)
status-portal-ok = πύλη /api/health 2xx
status-portal-failed = πύλη /api/health απέτυχε: { $err }
status-portal-timeout = πύλη /api/health έληξε χρονικά σε { $secs }s
status-cert-read-failed = ανάγνωση πιστοποιητικού απέτυχε: { $err }
status-cert-pem-parse-failed = ανάλυση PEM πιστοποιητικού απέτυχε: { $err }
status-cert-wrong-tag = το πρώτο μπλοκ PEM είναι { $tag }, αναμένεται CERTIFICATE
status-cert-x509-parse-failed = ανάλυση x509 απέτυχε: { $err }
status-cert-expired = το πιστοποιητικό ΕΛΗΞΕ στο unix={ $not_after }, τώρα={ $now }
status-cert-valid = πιστοποιητικό έγκυρο· λήγει σε { $days_left }d (unix={ $not_after })
inspect-finding-label = εύρημα ({ $severity }): { $label } — { $detail }
inspect-remedy-label = αποκατάσταση: { $title } — { $detail }
inspect-no-outcomes = (χωρίς αποτελέσματα — ο στόχος δεν ταίριαξε σε κανέναν έλεγχο)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put έγινε αποδεκτό στο `{ $key }` ({ $bytes } bytes)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = η κλιμάκωση κατασταλάχθηκε — { $summary } ({ $n } { $n ->
        [one] συνέπεια
       *[other] συνέπειες
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Οδηγός πρώτης εκτέλεσης: επιλέξτε sandbox ή πραγματικό, αποδείξτε ότι λειτουργεί, δείξτε τον tutor.
help-cmd-tutor = Μια αυτορρυθμιζόμενη, πρακτική περιήγηση στο fabric (στυλ vimtutor· συνεχιζόμενη).
help-cmd-docs = Περιηγηθείτε στα έγγραφα του goat ή εκπέμψτε την πλήρη αναφορά εντολών (--markdown).
help-cmd-hello = Διεκπεραιώστε ένα hello μέσω του fabric από άκρο σε άκρο (περιβάλλον sandbox ή πραγματικό).
flourish-all-green = 🐐 όλα πράσινα — το fabric λειτουργεί κανονικά

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = ενέργεια μόνο για χειριστή — δεν είστε εξουσιοδοτημένος (`goat admin` είναι ανενεργό για μη-διαχειριστές)
help-flag-narrate-json = Εκπομπή του substrate cascade ως NDJSON (ένα αντικείμενο ανά συνέπεια) για αρχεία συμβάντων + scripting. Ενεργοποιεί το --narrate.
help-cmd-admin = Διαχείριση μόνο για χειριστή: κύκλος ζωής οργανισμού, onboard/revoke/rotate, αποκοπή mesh-peer, προμήθεια συσκευών + operator-tier, διάγνωση LAN. Εξουσιοδοτείται από τα διαπιστευτήρια χειριστή σας (server-side μέσω portal/NetBird token scope)· ανενεργό για μη-διαχειριστές — μη εξουσιοδοτημένη κλήση δεν αλλάζει τίποτα και αποτυγχάνει με `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = το προφίλ για το context `{ $context }` υπάρχει αλλά δεν είναι αναγνώσιμο από αυτόν τον χρήστη (άρνηση πρόσβασης)
error-portal-bad-response-summary = το portal απάντησε αλλά αυτό το build του goat δεν μπόρεσε να αναλύσει την απάντηση
doctor-step0-ok-summary = η τοπική ρύθμιση είναι έτοιμη για network probes
doctor-step0-warn-summary = η τοπική ρύθμιση έχει ελέγχους που δεν επιβεβαιώθηκαν
doctor-step0-fail-summary = η τοπική ρύθμιση χρειάζεται προσοχή
doctor-step0-profile-absent-summary = δεν υπάρχει προφίλ goat για το context `{ $context }`
doctor-step0-profile-new-hint = ελέγξτε με `goat context ls`· αν είναι η πρώτη σας ρύθμιση, εκτελέστε `goat ca add <bundle-ca.pem>` και συγκρίνετε το fingerprint με αυτό του operator σας, μετά `goat profile init <bundle.cbor>`· εκτελέστε ξανά `goat doctor`
doctor-step0-profile-absent-hint = ελέγξτε με `goat context ls`· επιλέξτε υπάρχον context με `goat context use <name>`, ή εκτελέστε `goat profile init <bundle.cbor>` για αυτό το context· εκτελέστε ξανά `goat doctor`
doctor-step0-profile-denied-summary = το προφίλ goat στο { $path } δεν είναι αναγνώσιμο
doctor-step0-profile-denied-hint = διορθώστε την ιδιοκτησία του προφίλ ή εκτελέστε ως ο χρήστης-ιδιοκτήτης· μην κάνετε επανεκκίνηση (reinitialize) υπάρχοντος προφίλ
doctor-step0-profile-incomplete-summary = το αρχείο προφίλ goat { $file } λείπει ή δεν είναι αναγνώσιμο
doctor-step0-profile-incomplete-hint = επαναφέρετε το αρχείο που αναφέρεται από το bundle που εκδόθηκε, ή χρησιμοποιήστε `goat profile rotate <bundle.cbor>` για να αντικαταστήσετε αυτό το context· εκτελέστε ξανά `goat doctor`
doctor-step0-profile-invalid-summary = το προφίλ goat έχει μη αναγνώσιμα ή ελλείποντα endpoints
doctor-step0-profile-empty-token-summary = το προφίλ goat έχει κενό portal token
doctor-step0-profile-ok-summary = το goat context `{ $context }` έχει αναγνώσιμο υλικό προφίλ
doctor-step0-client-absent-summary = δεν απάντησε κανένας υποστηριζόμενος mesh client
doctor-step0-client-absent-hint = εγκαταστήστε τον mesh client που παρέχει το goatnet σας, ξεκινήστε το daemon του και εκτελέστε ξανά `goat doctor`
doctor-step0-client-stopped-summary = ο εγκατεστημένος mesh client δεν επέστρεψε χρήσιμη κατάσταση
doctor-step0-client-stopped-hint = ελέγξτε με `netbird status -d` ή `goat client status --detail`· ξεκινήστε το εγκατεστημένο daemon (`sudo netbird service start` για NetBird) και εκτελέστε ξανά `goat doctor`
doctor-step0-netbird-ok-summary = το daemon του NetBird απάντησε (αναφερόμενο προφίλ `{ $profile }`)
doctor-step0-goat-client-ok-summary = το goat-clientd απάντησε (κατάσταση `{ $state }`)
doctor-step0-management-connected-summary = το NetBird αναφέρει management συνδεδεμένο
doctor-step0-management-disconnected-summary = το NetBird αναφέρει management αποσυνδεδεμένο
doctor-step0-management-disconnected-hint = ελέγξτε το αναφερόμενο management URL και το `netbird status -d`· ένα ήδη εδραιωμένο tunnel μπορεί να λειτουργεί κατά τη διακοπή του management
doctor-step0-management-unknown-summary = το ρυθμισμένο management endpoint δεν μπόρεσε να επιβεβαιωθεί τοπικά
doctor-step0-management-unknown-hint = έλεγξε το `netbird status -d` ή το `goat client diagnostics`· το doctor δεν θα μαντέψει διεύθυνση URL διαχείρισης
doctor-step0-tunnel-down-summary = το NetBird δεν αναφέρει διεύθυνση mesh
doctor-step0-tunnel-down-hint = εκτέλεσε `netbird status -d`· αν η συσκευή είναι εγγεγραμμένη και το daemon αποκρίνεται, εκτέλεσε `goat heal` και μετά ξανά `goat doctor`
doctor-step0-tunnel-address-summary = το NetBird αναφέρει διεύθυνση mesh { $address }· ακολουθεί έλεγχος προσβασιμότητας του router
doctor-step0-goat-client-no-bundle-summary = το goat-clientd δεν έχει εισηγμένο bundle
doctor-step0-goat-client-no-bundle-hint = εκτέλεσε `goat client bundle import <bundle.cbor>` με το bundle εγγραφής του client, συνδέσου και ξανατρέξε `goat doctor`
doctor-step0-goat-client-connected-summary = το goat-clientd αναφέρει συνδεδεμένο τούνελ· ακολουθεί έλεγχος προσβασιμότητας του router
doctor-step0-goat-client-outer-disabled-summary = το εξωτερικό τούνελ του goat-clientd είναι απενεργοποιημένο σε λειτουργία netbird-only
doctor-step0-goat-client-down-summary = το goat-clientd αναφέρει κατάσταση τούνελ `{ $state }`
doctor-step0-goat-client-down-hint = εκτέλεσε `goat client diagnostics` και μετά `goat client up` αν το bundle είναι φορτωμένο· ξανατρέξε `goat doctor`
doctor-step0-goat-client-inner-ok-summary = το goat-clientd αναφέρει συνδεδεμένο εσωτερικό mesh
doctor-step0-goat-client-inner-disabled-summary = το εσωτερικό mesh του goat-clientd είναι απενεργοποιημένο σε λειτουργία wg-cp0-only
doctor-step0-goat-client-inner-down-summary = το goat-clientd αναφέρει κατάσταση εσωτερικού mesh `{ $state }`
doctor-step0-goat-client-inner-down-hint = εκτέλεσε `goat client diagnostics` για έλεγχο του εσωτερικού mesh και ξανατρέξε `goat doctor` μετά την αποκατάστασή του
doctor-step0-management-reachable-summary = το endpoint διαχείρισης απάντησε HTTP { $status }
doctor-step0-management-auth-summary = το HTTPS endpoint διαχείρισης είναι προσβάσιμο (το HTTP { $status } απαιτεί εξουσιοδότηση για αυτόν τον έλεγχο)
doctor-step0-management-unexpected-summary = το endpoint διαχείρισης επέστρεψε μη αναμενόμενο HTTP { $status }
doctor-step0-management-unexpected-hint = έλεγξε τη ρυθμισμένη διεύθυνση URL διαχείρισης και ζήτα από τον χειριστή του goatnet να ελέγξει αυτή την απάντηση
doctor-step0-management-unreachable-summary = το ρυθμισμένο endpoint διαχείρισης δεν ήταν προσβάσιμο: { $error }
doctor-step0-management-unreachable-hint = έλεγξε το DNS και την πρόσβαση δικτύου προς το URL που αναφέρει ο client· ένα υπάρχον τούνελ μπορεί να λειτουργεί ακόμη
doctor-step0-clock-ok-summary = το τοπικό ρολόι απέχει το πολύ { $seconds } δευτερόλεπτα από την ώρα HTTP της διαχείρισης
doctor-step0-clock-skew-summary = το τοπικό ρολόι διαφέρει από την ώρα HTTP της διαχείρισης κατά { $seconds } δευτερόλεπτα
doctor-step0-clock-skew-hint = συγχρόνισε το ρολόι της συσκευής και ξανατρέξε `goat doctor`· η επικύρωση πιστοποιητικών μπορεί να αποτύχει όσο το ρολόι είναι λάθος
doctor-step0-clock-unknown-summary = δεν ήταν δυνατή η μέτρηση της απόκλισης ρολογιού από κεφαλίδα HTTP Date της διαχείρισης
doctor-step0-clock-unknown-hint = έλεγξε τον συγχρονισμό ώρας της συσκευής αν αποτύχουν οι έλεγχοι TLS ή πιστοποιητικών
error-clock-skew-summary = το ρολόι της συσκευής διαφέρει υπερβολικά από την ώρα διαχείρισης
error-doctor-inconclusive-summary = το doctor παρατήρησε αποτυχία χωρίς αποδεδειγμένη αιτία
error-portal-forbidden-summary = το portal απέρριψε την πρόσβαση αυτού του λογαριασμού στο API που ελέγχθηκε
doctor-step4-forbidden-summary = το portal επέστρεψε HTTP 403 για αυτό το προφίλ
doctor-step4-forbidden-hint = αυτό το διαπιστευτήριο έφτασε στο portal αλλά δεν έχει πρόσβαση στο API που ελέγχθηκε· δες `goat profile show` και ζήτα από τον χειριστή του goatnet να επαληθεύσει την παραχώρηση
doctor-step4-decode-summary = το portal απάντησε με απόκριση που αυτό το goat δεν μπορεί να διαβάσει
doctor-step4-decode-hint = { $err } — ενημέρωσε το goat στην έκδοση που εξυπηρετεί αυτό το goatnet και τρέξε ξανά `goat doctor`
doctor-step1-control-only-summary = συνδέθηκε μόνο με τον διακομιστή ελέγχου — κανένα τούνελ προς κανέναν από τους { $total } peers
doctor-step1-control-only-hint = αυτό το δίκτυο επιτρέπει έξοδο TCP 443 αλλά μπλοκάρει τις θύρες δεδομένων του mesh, οπότε τίποτα στο mesh (συμπεριλαμβανομένων των σελίδων σύνδεσης) δεν είναι προσβάσιμο από αυτή τη συσκευή. Ζήτα από την ομάδα δικτύου να επιτρέψει εξερχόμενο TCP και UDP προς { $relays } και UDP 3478 προς { $stun }· συνιστάται επίσης άμεσο UDP μεταξύ συσκευών. Για επιβεβαίωση, σύνδεση σε hotspot κινητού και τρέξε `netbird status`: τότε οι peers θα πρέπει να συνδεθούν.
doctor-offmesh-dashboard-hint = στόλος relay (εξωτερική οπτική): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = το τοπικό πιστοποιητικό, το αντίστοιχο κλειδί και το πακέτο CA διαβάζονται σωστά· το πιστοποιητικό είναι εντός του διαστήματος ισχύος
doctor-step2-material-failed-summary = το τοπικό υλικό TLS { $file } δεν μπόρεσε να χρησιμοποιηθεί
doctor-step2-material-failed-hint = { $err } — έλεγξε το συγκεκριμένο αρχείο· επανάφερέ το από το εκδοθέν πακέτο ή κάνε rotate το προφίλ
doctor-step2-material-invalid-hint = το { $file } δεν είναι έγκυρο πιστοποιητικό X.509 PEM· επανάφερε το εκδοθέν υλικό ή τρέξε `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = το mtls.key.pem δεν είναι έγκυρο ή δεν ταιριάζει με το mtls.cert.pem· επανάφερε το εκδοθέν υλικό ή τρέξε `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = το τοπικό πιστοποιητικό είναι εκτός του διαστήματος ισχύος
doctor-step2-cert-time-hint = τρέξε `goat profile show`· έλεγξε το αποτέλεσμα του ρολογιού παραπάνω, μετά ζήτα νέο πακέτο από τον χειριστή και τρέξε `goat profile rotate <new-bundle.cbor>` αν το πιστοποιητικό έχει λήξει
doctor-skipped-no-endpoint-summary = παραλείφθηκε — στο profile.toml λείπει το { $field }
doctor-skipped-no-profile-summary = παραλείφθηκε — δεν υπάρχουν αναγνώσιμα endpoints προφίλ για αυτό το context
doctor-skipped-no-portal-token-summary = παραλείφθηκε — δεν υπάρχει αναγνώσιμο portal token για αυτό το context
status-ztol-tcp-ok = η θύρα του router { $host }:{ $port } δέχεται TCP (μόνο θύρα — δες session)
status-session-ok = η συνεδρία zenoh mTLS άνοιξε σε { $ms }ms (ο router δέχτηκε αυτό το πιστοποιητικό)
status-session-skipped = δεν επιχειρήθηκε συνεδρία (εκτός mesh· δες ztol)
status-session-material-missing = δεν επιχειρήθηκε συνεδρία zenoh: { $detail }
status-session-config-failed = η ρύθμιση της συνεδρίας zenoh απέτυχε: { $err }
status-session-failed = η συνεδρία zenoh δεν άνοιξε: { $err } — τρέξε `goat doctor`
status-session-timeout = η συνεδρία zenoh δεν άνοιξε σε { $secs }s — η θύρα του router δέχεται TCP αλλά καμία συνεδρία δεν ολοκληρώνεται (router κολλημένος, απορριφθέν πιστοποιητικό ή απόκλιση ρολογιού)· τρέξε `goat doctor`
status-names-live = ονόματα live (απάντησε το mesh DNS)
status-names-ip-only = ονόματα μη ελεγμένα (τα endpoints του προφίλ είναι IP literals)
status-names-snapshot = ονόματα από υπογεγραμμένο snapshot με serial { $serial }, ηλικία { $age } (το mesh DNS δεν είναι προσβάσιμο)
status-names-snapshot-aging = ονόματα από υπογεγραμμένο snapshot με serial { $serial }, ηλικία { $age } — ΠΑΛΙΟ (το mesh DNS δεν είναι προσβάσιμο· διόρθωσε το goat-coredns και τα ονόματα θα ανανεωθούν αυτόματα)
status-names-snapshot-daemon = ονόματα από υπογεγραμμένο snapshot με serial { $serial }, ηλικία { $age } — αποθήκη daemon (το mesh DNS δεν είναι προσβάσιμο)
status-names-snapshot-daemon-aging = ονόματα από υπογεγραμμένο snapshot με serial { $serial }, ηλικία { $age } — αποθήκη daemon, ΠΑΛΙΟ (το mesh DNS δεν είναι προσβάσιμο· διόρθωσε το goat-coredns και τα ονόματα θα ανανεωθούν αυτόματα)
status-names-failed = το mesh DNS δεν είναι προσβάσιμο και το εφεδρικό snapshot δεν είναι διαθέσιμο — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ΕΝΕΡΓΟ — { $qr }/{ $connected } συνδεδεμένοι peers ανθεκτικοί σε κβαντικές επιθέσεις
status-pqc-off = wireguard PQC (rosenpass) ΑΝΕΝΕΡΓΟ — κλασικά handshakes wireguard
status-pqc-unknown = wireguard PQC άγνωστο — { $reason }
status-device-none = προστασία ονομάτων σε επίπεδο συσκευής ΑΝΕΝΕΡΓΗ — δεν υπάρχει αποθήκη ονομάτων goat-clientd (οι εντολές goat έχουν ακόμα fallback· ενεργοποίηση βλ. name-fallback-resolver.md §9.2)
status-device-broken = υπάρχει αποθήκη ονομάτων goat-clientd (serial { $serial }) αλλά ο forwarder της ΔΕΝ απαντά στο { $addr } — daemon εκτός λειτουργίας ή κολλημένο υποσύστημα ονομάτων
status-device-active = προστασία ονομάτων σε επίπεδο συσκευής ΕΝΕΡΓΗ — το goat-clientd μπροστά από το mesh DNS του λειτουργικού στο { $addr } (αποθήκη serial { $serial }, { $grade })
status-device-not-fronted = ο forwarder goat-clientd λειτουργεί στο { $addr } (αποθήκη serial { $serial }, { $grade }) — το λειτουργικό δεν περνά από αυτόν· οι browsers χρησιμοποιούν απευθείας το mesh DNS
status-device-fronting-unknown = ο forwarder goat-clientd λειτουργεί στο { $addr } (αποθήκη serial { $serial }, { $grade }) — η κατάσταση fronting του λειτουργικού δεν ανιχνεύεται σε αυτήν την πλατφόρμα
status-names-observed = ονόματα ΜΗ ΚΑΝΟΝΙΚΑ (ad hoc): παρατηρημένη εγγραφή, ηλικία { $age } — δεν υπάρχει στο υπογεγραμμένο snapshot (το mesh DNS δεν είναι προσβάσιμο· κωδικοποίησε αυτήν την εγγραφή)
status-names-selfclaim = ονόματα μέσω επαληθευμένου SELF-CLAIM, ηλικία { $age } — υπογεγραμμένο από το δικό του anchor του ονόματος (το mesh DNS δεν είναι προσβάσιμο)
doctor-names-note = μέσω snapshot serial { $serial }, ηλικία { $age }
doctor-names-selfclaim-note = μέσω επαληθευμένου SELF-CLAIM, ηλικία { $age } — υπογεγραμμένο από το δικό του anchor του ονόματος
doctor-names-selfclaim-hint = το mesh DNS δεν είναι προσβάσιμο — το βήμα επιλύθηκε μέσω επαληθευμένου SELF-CLAIM (ηλικία { $age }): το δικό του anchor του ονόματος υπέγραψε αυτή τη σύνδεση και τα peer_bindings του αποθηκευμένου snapshot την εγκρίνουν. Υπογεγραμμένο αλλά όχι live· διόρθωσε το mesh DNS για να φύγει αυτό.
doctor-names-observed-note = μέσω ΠΑΡΑΤΗΡΗΜΕΝΗΣ (ad hoc) εγγραφής, ηλικία { $age } — μη κανονική
doctor-names-observed-hint = το mesh DNS δεν είναι προσβάσιμο — το βήμα επιλύθηκε μέσω ΜΗ ΚΑΝΟΝΙΚΗΣ παρατηρημένης εγγραφής (ηλικία { $age }): το όνομα υπάρχει μόνο live, δεν έχει κωδικοποιηθεί ποτέ στο site registry. Κωδικοποίησέ το (group_vars registry → generator → re-sign) αλλιώς λήγει· διόρθωσε το mesh DNS για να φύγει αυτό.
doctor-names-hint = το mesh DNS δεν είναι προσβάσιμο — το βήμα επιλύθηκε μέσω του υπογεγραμμένου snapshot ονομάτων (serial { $serial }, ηλικία { $age }). Το fabric παραμένει προσβάσιμο με εφεδρικά ονόματα· διόρθωσε το mesh DNS (goat-coredns στον mgmt host) για να φύγει αυτό.
storm-brake-plan-header = Σχέδιο storm brake (ADR 1081 Layer 4 — χειροκίνητο):
storm-brake-plan-target = mgmt host { $user }@{ $host }  (context { $context })
storm-brake-step-flush = επανεκκίνηση του signal container `{ $container }` (απορρίπτει τα zombie streams)
storm-brake-step-tighten = περιορισμός του L2 limiter στο `{ $container }` (μισό rate-limit, αύξηση neg-cache TTL) — { $window } λεπτά, η επαναφορά γίνεται από τον χειριστή· παραλείπεται αν δεν έχει γίνει deploy του L2
storm-brake-step-shed = παύση της μη απαραίτητης μονάδας publisher `{ $unit }`
storm-brake-shed-none = (δεν έχουν δηλωθεί μονάδες shed — ορίστε storm_brake_shed_units στο profile.toml ή δώστε --shed-unit)
storm-brake-announce-note = εγγραφή ενός OperatorActionV1 στο audit trail με τα αποτελέσματα ανά βήμα
storm-brake-dryrun-note = dry run — δεν εκτελέστηκε τίποτα.
storm-brake-triggers-header = Ενεργοποίηση όταν η ζώνη host-net του relay-health δείχνει:
storm-brake-trigger-futility = λόγος ματαιότητας signaling >= 0.5 με >= 100 msgs/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = απώλεια uplink >= 5% για 10 λεπτά συνεχόμενα (wan.loss_pct)
storm-brake-triggers-band-url = host band: relay-health.{ $site }.<zone> (δείτε docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Σημείωση: το signal flush από μόνο του ΔΕΝ αποκαθιστά το mesh. Η θεραπεία είναι τα Layers 1-2 (client backoff + signal shedding)· το brake κερδίζει χρόνο για ανάκαμψη.
storm-brake-confirm = Ενεργοποίηση του storm brake στο { $user }@{ $host }; Επανεκκινεί το signal container και θέτει σε παύση τους δηλωμένους publishers. [y/N]
storm-brake-aborted = το storm brake ακυρώθηκε (χωρίς επιβεβαίωση)
storm-brake-executing = ενεργοποίηση storm brake στο { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = απέτυχε
storm-brake-outcome-skipped = παραλείφθηκε
storm-brake-summary = storm brake ολοκληρώθηκε: { $ok } ok, { $failed } απέτυχαν, { $skipped } παραλείφθηκαν
storm-brake-flush-failed = FLUSH απέτυχε — το signal container δεν επανεκκινήθηκε, άρα δεν αναμένεται ανάκαμψη του mesh: { $reason }
storm-brake-mgmt-unresolved = το mgmt host δεν επιλύθηκε — δώστε --mgmt-host, ορίστε mgmt_ssh_host στο profile.toml του context ή κάντε export το GOAT_MGMT_SSH_HOST
storm-brake-audit-note = η ενέργεια χειριστή καταγράφηκε στο audit trail (actor { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = η πύλη απάντησε HTTP 404 για `{ $path }`: δεν είναι καταχωρισμένο εκεί
error-portal-rate-limited-summary = η πύλη απάντησε HTTP 429 για `{ $path }`: πάρα πολλά αιτήματα, δοκιμάστε αργότερα
error-portal-server-error-summary = η πύλη απάντησε HTTP { $status } για `{ $path }`: σφάλμα από την πλευρά της πύλης
error-portal-refused-summary = η πύλη απάντησε HTTP { $status } για `{ $path }`: το αίτημα απορρίφθηκε

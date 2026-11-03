### goat-cli — Polish (pl). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = brak profilu dla kontekstu `{ $context }`
error-off-mesh-summary = poza ZTOL — brak połączenia z siecią mesh
error-router-unreachable-summary = router Zenoh pod adresem `{ $endpoint }` jest nieosiągalny
error-cert-expired-summary = certyfikat mTLS wygasł lub został odrzucony przez router
error-portal-unreachable-summary = interfejs API portalu pod adresem `{ $endpoint }` jest nieosiągalny
error-token-invalid-summary = portal odrzucił token Bearer
error-topic-conflict-summary = topic `{ $topic }` już istnieje
error-bundle-invalid-summary = weryfikacja podpisu lub ważności pakietu bundle nie powiodła się

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = nieprawidłowy punkt końcowy routera
doctor-step1-endpoint-malformed-hint = oczekiwano `<proto>/<host>:<port>`; otrzymano `{ $endpoint }`
doctor-step1-ok-summary = połączenie TCP z hostem routera { $host }:{ $port } powiodło się
doctor-step1-conn-failed-summary = połączenie TCP z hostem routera nie powiodło się
doctor-step1-conn-failed-hint = { $err } — uruchom netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); sprawdź `netbird status`
doctor-step1-timeout-summary = przekroczono limit czasu połączenia TCP z hostem routera
doctor-step1-timeout-hint = host routera { $host }:{ $port } nieosiągalny w { $ms }ms — prawdopodobnie poza siecią mesh
doctor-step2-endpoint-malformed-hint = popraw wystawcę pakietu bundle (--goat-cli-router-endpoint musi być `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = brak lub nieczytelny PEM korzeni CA
doctor-step2-ca-missing-hint = { $err } — uruchom ponownie `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = router { $host }:{ $port } akceptuje TCP (TLS zweryfikowano w kroku 3)
doctor-step2-refused-summary = odrzucono TCP routera
doctor-step2-refused-hint = { $err } — router może być wyłączony; sprawdź wdrożenie Zenoh na mgmt-host
doctor-step2-timeout-summary = przekroczono limit czasu TCP routera
doctor-step2-timeout-hint = router { $host }:{ $port } nieosiągalny w { $ms }ms
doctor-step3-config-failed-summary = nie udało się zbudować konfiguracji zenoh
doctor-step3-config-failed-hint = nieprawidłowy punkt końcowy `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = nie udało się zbudować konfiguracji TLS zenoh
doctor-step3-tls-config-failed-hint = { $err } — prawdopodobnie rozbieżność wersji zenoh; sprawdź zgodność zależności
doctor-step3-ok-summary = sesja mTLS otwarta — certyfikat zaakceptowany przez ACL routera
doctor-step3-open-failed-summary = nie udało się otworzyć sesji zenoh
doctor-step3-open-failed-hint = { $err } — certyfikat może być wygasły (uruchom `goat profile show`) lub odrzucony przez ACL routera (niezgodność URI-SAN)
doctor-step3-timeout-summary = przekroczono limit czasu otwierania sesji zenoh
doctor-step3-timeout-hint = router nie ukończył uzgadniania w { $secs }s — przesunięcie zegara, zły certyfikat lub przeciążenie
doctor-step4-ok-summary = portal zwrócił { $n } wierszy organizacji
doctor-step4-token-rejected-summary = portal odrzucił token Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — token wygasł lub został odwołany; ponów onboarding przez operator-onboard.yml
doctor-step4-request-failed-summary = żądanie do portalu nie powiodło się
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = przekroczono limit czasu żądania do portalu
doctor-step4-timeout-hint = brak odpowiedzi w { $secs }s — sprawdź, czy { $endpoint } jest osiągalny z sieci mesh
doctor-skipped-summary = pominięto — poprzedni krok nie powiódł się (uruchom ponownie z --all, aby wymusić)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Zunifikowany interfejs wiersza poleceń operatora dla struktury danych goat.
help-long-about = goat — CLI skierowane do operatora, łączące panoscope-core w jedno narzędzie: inicjalizacja/ponowne użycie profilu, świadomy ZTOL doctor osiągalności, przeglądanie portalu organizacji, CRUD topic, pub/sub, delegowanie stethoscope, interaktywny kreator poleceń oraz narracja kaskady struktury.
help-flag-profile-dir = Zastąp katalog profilu (domyślnie: $GOAT_PROFILE_DIR lub ~/.goat).
help-flag-context = Zastąp aktywny kontekst (domyślnie: zmienna $GOAT_CONTEXT, następnie current_context w config.toml, następnie „default").
help-flag-narrate = Pokaż kaskadę struktury wywołaną przez polecenie modyfikujące.
help-flag-verbose = Włącz szczegółowe wyjście debugowania (lub ustaw RUST_LOG=goat=debug).
help-flag-color = Tryb kolorów dla wyjścia terminala (auto|always|never).
help-flag-lang = Język tekstu skierowanego do ludzi (np. en, uk). Wraca do GOAT_LANG, następnie do ustawień regionalnych systemu, następnie do angielskiego.
help-cmd-profile = Zarządzaj lokalnym profilem goat (certyfikat mTLS + token portalu + zestaw punktów końcowych).
help-cmd-context = Zarządzaj kontekstami (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Uruchom krokowy próbnik osiągalności względem struktury.
help-cmd-doctor-long = Krokowy próbnik osiągalności względem struktury: członkostwo ZTOL → router Zenoh → akceptacja certyfikatu → portal. Każdy nieudany krok generuje konkretną wskazówkę naprawczą.
help-cmd-vendor = Przeglądaj organizacje zarejestrowane w portalu.
help-cmd-topic = Wyświetl, pokaż, utwórz lub usuń topic.
help-cmd-pub = Opublikuj ładunek na kluczu topic.
help-cmd-sub = Subskrybuj wyrażenie klucza topic.
help-cmd-inspect = Deleguj żądanie diagnostycznego werdyktu do stethoscope.
help-cmd-ui = Interaktywny scalony selektor: przeglądaj listę zadeklarowanych ⊕ aktywnych i przejdź do soczewki Diagnose / Tail na wybranym celu.
help-cmd-compose = Interaktywny TUI do budowania poleceń.
help-cmd-narrate = Refleksja kaskady struktury nad ostatnią aktywnością. Jeszcze niedostępne (codzienna ścieżka to --narrate na poleceniu modyfikującym).
help-cmd-completion = Wypisz skrypt dopełniania powłoki na stdout.
help-cmd-explain = Wyszukaj znaczenie kodu błędu + naprawę we wbudowanym dokumencie `goat-cli-error-codes.md` (wzorzec cargo --explain).
help-cmd-mock = Opublikuj syntetyczne próbki na topic. Domyślnie: trywialny hello-world JSON z częstotliwością 1Hz. Podaj `--format <.fds>` + `--message <Name>`, aby zamiast tego zakodować protobuf dostarczony przez operatora.
help-cmd-status = Synteza kondycji: osiągalność ZTOL + portal /api/health + wygaśnięcie lokalnego certyfikatu, trzy linie glyph lub jeden obiekt --json.
help-cmd-services = Sprawdź każdą hostowaną usługę w `ops/serve/services.yaml` i wyrenderuj jedną linię glyph na usługę (lub jeden obiekt --json).
help-cmd-client = Opakuj lokalny daemon goat-clientd przez jego gniazdo JSON-RPC IPC (`~/.goat-client/goat-clientd.sock`): status / up / down / import bundle.
help-cmd-plugin = Wykrywanie rozszerzeń oparte na PATH (styl kubectl). Dowolny plik wykonywalny `goat-<name>` na PATH można wywołać jako `goat <name>`; `goat plugin list` wylicza zainstalowane wtyczki, `goat plugin which <name>` rozwiązuje ścieżkę.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = nieprawidłowy punkt końcowy routera: `{ $endpoint }`
status-ztol-refused = odrzucono TCP do routera { $host }:{ $port }: { $err }
status-ztol-unreachable = poza siecią mesh (router { $host }:{ $port } nieosiągalny w { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health nie powiódł się: { $err }
status-portal-timeout = przekroczono limit czasu portal /api/health w { $secs }s
status-cert-read-failed = odczyt certyfikatu nie powiódł się: { $err }
status-cert-pem-parse-failed = analiza PEM certyfikatu nie powiodła się: { $err }
status-cert-wrong-tag = pierwszy blok PEM to { $tag }, oczekiwano CERTIFICATE
status-cert-x509-parse-failed = analiza x509 nie powiodła się: { $err }
status-cert-expired = certyfikat WYGASŁ o unix={ $not_after }, teraz={ $now }
status-cert-valid = certyfikat ważny; wygasa za { $days_left }d (unix={ $not_after })
inspect-finding-label = ustalenie ({ $severity }): { $label } — { $detail }
inspect-remedy-label = naprawa: { $title } — { $detail }
inspect-no-outcomes = (brak wyników — cel nie pasuje do żadnej kontroli)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put zaakceptowany na `{ $key }` ({ $bytes } bajtów)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskada ustabilizowana — { $summary } ({ $n } { $n ->
        [one] konsekwencja
        [few] konsekwencje
        [many] konsekwencji
       *[other] konsekwencji
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Kreator pierwszego uruchomienia: wybierz sandbox lub środowisko rzeczywiste, sprawdź działanie, skieruj do samouczka.
help-cmd-tutor = Samodzielna, praktyczna wycieczka po strukturze (w stylu vimtutor; wznawialna).
help-cmd-docs = Przeglądaj dokumentację goat lub wygeneruj pełny wykaz poleceń (--markdown).
help-cmd-hello = Przeprowadź hello przez strukturę od początku do końca (sandbox lub środowisko rzeczywiste).
flourish-all-green = 🐐 wszystko zielone — struktura sprawna

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = akcja tylko dla operatora — nie masz uprawnień (`goat admin` jest bezczynnościowe dla nie-administratorów)
help-flag-narrate-json = Emituje kaskadę substratu jako NDJSON (jeden obiekt na konsekwencję) do logów zdarzeń i skryptowania. Implikuje --narrate.
help-cmd-admin = Administracja tylko dla operatora: cykl życia organizacji, onboarding/odwoływanie/rotacja, cięcie mesh-peer, provisioning urządzeń i poziomu operatora, triage LAN. Autoryzowane przez poświadczenia operatora (po stronie serwera przez portal/zakres tokenu NetBird); bezczynnościowe dla nie-administratorów — nieautoryzowane wywołanie nic nie zmienia i kończy się błędem `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = profil dla kontekstu `{ $context }` istnieje, ale ten użytkownik nie ma uprawnień do jego odczytu (brak dostępu)
error-portal-bad-response-summary = portal odpowiedział, ale ta wersja goat nie potrafiła przetworzyć odpowiedzi
doctor-step0-ok-summary = konfiguracja lokalna gotowa do sond sieciowych
doctor-step0-warn-summary = konfiguracja lokalna zawiera elementy, których nie udało się potwierdzić
doctor-step0-fail-summary = konfiguracja lokalna wymaga uwagi
doctor-step0-profile-absent-summary = brak profilu goat dla kontekstu `{ $context }`
doctor-step0-profile-new-hint = sprawdź `goat context ls`; przy pierwszej konfiguracji uruchom `goat ca add <bundle-ca.pem>` i porównaj jego odcisk z odciskiem od operatora, następnie `goat profile init <bundle.cbor>`; uruchom ponownie `goat doctor`
doctor-step0-profile-absent-hint = sprawdź `goat context ls`; wybierz istniejący kontekst przez `goat context use <name>` lub uruchom `goat profile init <bundle.cbor>` dla tego kontekstu; uruchom ponownie `goat doctor`
doctor-step0-profile-denied-summary = profil goat w { $path } jest nieczytelny
doctor-step0-profile-denied-hint = napraw właściciela profilu lub uruchom jako jego właściciel; nie inicjalizuj ponownie istniejącego profilu
doctor-step0-profile-incomplete-summary = plik profilu goat { $file } nie istnieje lub jest nieczytelny
doctor-step0-profile-incomplete-hint = przywróć wskazany plik z wydanego pakietu lub użyj `goat profile rotate <bundle.cbor>`, aby zastąpić ten kontekst; uruchom ponownie `goat doctor`
doctor-step0-profile-invalid-summary = profil goat ma nieczytelne lub brakujące endpointy
doctor-step0-profile-empty-token-summary = profil goat ma pusty token portalu
doctor-step0-profile-ok-summary = kontekst goat `{ $context }` ma czytelne dane profilu
doctor-step0-client-absent-summary = żaden obsługiwany klient mesh nie odpowiedział
doctor-step0-client-absent-hint = zainstaluj klienta mesh dostarczonego przez twój goatnet, uruchom jego demon, a następnie ponownie `goat doctor`
doctor-step0-client-stopped-summary = zainstalowany klient mesh nie zwrócił użytecznego statusu
doctor-step0-client-stopped-hint = sprawdź `netbird status -d` lub `goat client status --detail`; uruchom zainstalowany demon (`sudo netbird service start` dla NetBird), a następnie ponownie `goat doctor`
doctor-step0-netbird-ok-summary = demon NetBird odpowiedział (zgłoszony profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd odpowiedział (stan `{ $state }`)
doctor-step0-management-connected-summary = NetBird zgłasza połączenie z management
doctor-step0-management-disconnected-summary = NetBird zgłasza brak połączenia z management
doctor-step0-management-disconnected-hint = sprawdź zgłoszony URL management i `netbird status -d`; ustanowiony tunel może nadal działać podczas awarii management
doctor-step0-management-unknown-summary = nie udało się lokalnie ustalić skonfigurowanego endpointu management
doctor-step0-management-unknown-hint = sprawdź `netbird status -d` lub `goat client diagnostics`; doctor nie będzie zgadywać adresu URL zarządzania
doctor-step0-tunnel-down-summary = NetBird nie zgłasza adresu mesh
doctor-step0-tunnel-down-hint = uruchom `netbird status -d`; jeśli urządzenie jest zarejestrowane, a demon odpowiada, uruchom `goat heal`, potem ponownie `goat doctor`
doctor-step0-tunnel-address-summary = NetBird zgłasza adres mesh { $address }; następnie sprawdzana jest osiągalność routera
doctor-step0-goat-client-no-bundle-summary = goat-clientd nie ma zaimportowanego pakietu
doctor-step0-goat-client-no-bundle-hint = uruchom `goat client bundle import <bundle.cbor>` z pakietem rejestracyjnym klienta, połącz klienta, potem ponownie `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd zgłasza połączony tunel; następnie sprawdzana jest osiągalność routera
doctor-step0-goat-client-outer-disabled-summary = zewnętrzny tunel goat-clientd jest wyłączony w trybie netbird-only
doctor-step0-goat-client-down-summary = goat-clientd zgłasza stan tunelu `{ $state }`
doctor-step0-goat-client-down-hint = uruchom `goat client diagnostics`, a potem `goat client up`, jeśli pakiet jest załadowany; ponownie uruchom `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd zgłasza połączony wewnętrzny mesh
doctor-step0-goat-client-inner-disabled-summary = wewnętrzny mesh goat-clientd jest wyłączony w trybie wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd zgłasza stan wewnętrznego mesh `{ $state }`
doctor-step0-goat-client-inner-down-hint = uruchom `goat client diagnostics`, aby sprawdzić wewnętrzny mesh, a po jego przywróceniu ponownie `goat doctor`
doctor-step0-management-reachable-summary = punkt końcowy zarządzania odpowiedział HTTP { $status }
doctor-step0-management-auth-summary = punkt końcowy HTTPS zarządzania jest osiągalny (HTTP { $status } wymaga autoryzacji dla tej sondy)
doctor-step0-management-unexpected-summary = punkt końcowy zarządzania zwrócił nieoczekiwany HTTP { $status }
doctor-step0-management-unexpected-hint = sprawdź skonfigurowany adres URL zarządzania i poproś operatora goatnet o weryfikację tej odpowiedzi
doctor-step0-management-unreachable-summary = nie można połączyć się ze skonfigurowanym punktem końcowym zarządzania: { $error }
doctor-step0-management-unreachable-hint = sprawdź DNS i dostęp sieciowy do adresu URL zgłoszonego przez klienta; istniejący tunel może nadal działać
doctor-step0-clock-ok-summary = zegar lokalny mieści się w granicach { $seconds } s od czasu HTTP zarządzania
doctor-step0-clock-skew-summary = zegar lokalny różni się od czasu HTTP zarządzania o { $seconds } s
doctor-step0-clock-skew-hint = zsynchronizuj zegar urządzenia, potem ponownie uruchom `goat doctor`; przy błędnym zegarze walidacja certyfikatów może się nie powieść
doctor-step0-clock-unknown-summary = nie udało się zmierzyć przesunięcia zegara z nagłówka HTTP Date zarządzania
doctor-step0-clock-unknown-hint = sprawdź synchronizację czasu urządzenia, jeśli kontrole TLS lub certyfikatów zawodzą
error-clock-skew-summary = zegar urządzenia zbyt mocno odbiega od czasu zarządzania
error-doctor-inconclusive-summary = doctor wykrył awarię bez ustalonej przyczyny
error-portal-forbidden-summary = portal odmówił temu kontu dostępu do sprawdzanego API
doctor-step4-forbidden-summary = portal zwrócił HTTP 403 dla tego profilu
doctor-step4-forbidden-hint = to poświadczenie dotarło do portalu, ale nie ma dostępu do sprawdzanego API; sprawdź `goat profile show` i poproś operatora goatnet o weryfikację uprawnień
doctor-step4-decode-summary = portal zwrócił odpowiedź, której ten goat nie potrafi odczytać
doctor-step4-decode-hint = { $err } — zaktualizuj goat do wersji obsługiwanej przez tę goatnet, a potem uruchom ponownie `goat doctor`
doctor-step1-control-only-summary = połączono tylko z serwerem kontrolnym — brak tunelu do któregokolwiek z { $total } peerów
doctor-step1-control-only-hint = ta sieć przepuszcza TCP 443, ale blokuje porty danych mesh, więc nic w mesh (w tym strony logowania) nie jest osiągalne z tego urządzenia. Poproś zespół sieciowy o zezwolenie na wychodzący TCP i UDP do { $relays } oraz UDP 3478 do { $stun }; zalecany jest też bezpośredni UDP między urządzeniami. Aby potwierdzić, połącz się z hotspotem w telefonie i uruchom `netbird status`: peery powinny się wtedy połączyć.
doctor-offmesh-dashboard-hint = flota relayów (widok zewnętrzny): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = lokalny certyfikat, pasujący klucz i pakiet CA są poprawnie wczytane; certyfikat mieści się w okresie ważności
doctor-step2-material-failed-summary = nie można użyć lokalnego materiału TLS { $file }
doctor-step2-material-failed-hint = { $err } — sprawdź wskazany plik; przywróć go z wydanego pakietu lub zrotuj profil
doctor-step2-material-invalid-hint = { $file } nie jest prawidłowym certyfikatem X.509 PEM; przywróć wydany materiał lub uruchom `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem jest nieprawidłowy lub nie pasuje do mtls.cert.pem; przywróć wydany materiał lub uruchom `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = lokalny certyfikat jest poza okresem ważności
doctor-step2-cert-time-hint = uruchom `goat profile show`; sprawdź wynik zegara powyżej, a potem poproś operatora o nowy pakiet i uruchom `goat profile rotate <new-bundle.cbor>`, jeśli certyfikat wygasł
doctor-skipped-no-endpoint-summary = pominięto — w profile.toml brak { $field }
doctor-skipped-no-profile-summary = pominięto — brak czytelnych endpointów profilu dla tego kontekstu
doctor-skipped-no-portal-token-summary = pominięto — brak czytelnego tokenu portalu dla tego kontekstu
status-ztol-tcp-ok = port routera { $host }:{ $port } przyjmuje TCP (tylko port — zob. sesja)
status-session-ok = sesja zenoh mTLS otwarta w { $ms }ms (router zaakceptował ten certyfikat)
status-session-skipped = sesji nie próbowano (poza mesh; zob. ztol)
status-session-material-missing = sesji zenoh nie próbowano: { $detail }
status-session-config-failed = konfiguracja sesji zenoh nie powiodła się: { $err }
status-session-failed = sesja zenoh nie została otwarta: { $err } — uruchom `goat doctor`
status-session-timeout = sesja zenoh nie została otwarta w { $secs }s — port routera przyjmuje TCP, ale żadna sesja się nie kończy (router zawieszony, certyfikat odrzucony lub przesunięty zegar); uruchom `goat doctor`
status-names-live = nazwy aktywne (mesh DNS odpowiada)
status-names-ip-only = nazwy nieużyte (endpointy profilu to adresy IP)
status-names-snapshot = nazwy ze snapshotu podpisanego, serial { $serial }, wiek { $age } (mesh DNS nieosiągalny)
status-names-snapshot-aging = nazwy ze snapshotu podpisanego, serial { $serial }, wiek { $age } — PRZESTARZAŁE (mesh DNS nieosiągalny; napraw goat-coredns, nazwy odświeżą się automatycznie)
status-names-snapshot-daemon = nazwy ze snapshotu podpisanego, serial { $serial }, wiek { $age } — magazyn demona (mesh DNS nieosiągalny)
status-names-snapshot-daemon-aging = nazwy ze snapshotu podpisanego, serial { $serial }, wiek { $age } — magazyn demona, PRZESTARZAŁE (mesh DNS nieosiągalny; napraw goat-coredns, nazwy odświeżą się automatycznie)
status-names-failed = mesh DNS nieosiągalny, a awaryjny snapshot niedostępny — { $reason }
status-pqc-on = wireguard PQC (rosenpass) WŁĄCZONE — { $qr }/{ $connected } połączonych peerów odpornych kwantowo
status-pqc-off = wireguard PQC (rosenpass) WYŁĄCZONE — klasyczne uzgodnienia wireguard
status-pqc-unknown = wireguard PQC nieznane — { $reason }
status-device-none = ochrona nazw na całym urządzeniu WYŁĄCZONA — brak magazynu nazw goat-clientd (polecenia goat nadal mają fallback; włącz wg name-fallback-resolver.md §9.2)
status-device-broken = magazyn nazw goat-clientd istnieje (serial { $serial }), ale jego forwarder NIE odpowiada na { $addr } — demon nie działa lub podsystem nazw jest zawieszony
status-device-active = ochrona nazw na całym urządzeniu AKTYWNA — goat-clientd przejmuje mesh DNS systemu na { $addr } (magazyn serial { $serial }, { $grade })
status-device-not-fronted = forwarder goat-clientd działa na { $addr } (magazyn serial { $serial }, { $grade }) — system nie jest przez niego obsługiwany; przeglądarki używają bezpośrednio mesh DNS
status-device-fronting-unknown = forwarder goat-clientd działa na { $addr } (magazyn serial { $serial }, { $grade }) — stan przejęcia DNS systemu niewykrywalny na tej platformie
status-names-observed = nazwy NIEKANONICZNE (ad hoc): zaobserwowany rekord, wiek { $age } — brak w podpisanym snapshocie (mesh DNS nieosiągalny; skodyfikuj ten rekord)
status-names-selfclaim = nazwy przez zweryfikowany SELF-CLAIM, wiek { $age } — podpisany przez własny anchor nazwy (mesh DNS nieosiągalny)
doctor-names-note = ze snapshotu serial { $serial }, wiek { $age }
doctor-names-selfclaim-note = przez zweryfikowany SELF-CLAIM, wiek { $age } — podpisany przez własny anchor nazwy
doctor-names-selfclaim-hint = mesh DNS jest nieosiągalny — ten krok rozwiązano przez zweryfikowany SELF-CLAIM (wiek { $age }): własny anchor nazwy podpisał to powiązanie, a peer_bindings z buforowanego snapshotu je autoryzuje. Podpisane, ale nie na żywo; napraw mesh DNS, aby to usunąć.
doctor-names-observed-note = przez zaobserwowany rekord (ad hoc), wiek { $age } — niekanoniczny
doctor-names-observed-hint = mesh DNS jest nieosiągalny — ten krok rozwiązano przez NIEKANONICZNY zaobserwowany rekord (wiek { $age }): nazwa istnieje tylko na żywo, nigdy nie skodyfikowano jej w rejestrze witryny. Skodyfikuj ją (rejestr group_vars → generator → ponowne podpisanie), inaczej wygaśnie; napraw mesh DNS, aby to usunąć.
doctor-names-hint = mesh DNS jest nieosiągalny — ten krok rozwiązano przez podpisany snapshot nazw (serial { $serial }, wiek { $age }). Sieć pozostaje osiągalna przez nazwy awaryjne; napraw mesh DNS (goat-coredns na hoście mgmt), aby to usunąć.
storm-brake-plan-header = Plan hamulca burzy (ADR 1081 warstwa 4 — ręczny):
storm-brake-plan-target = host mgmt { $user }@{ $host }  (kontekst { $context })
storm-brake-step-flush = restart kontenera sygnalizacji `{ $container }` (zrywa zombie strumienie)
storm-brake-step-tighten = przycisk limitera L2 na `{ $container }` (limit szybkości o połowę, wyższy TTL neg-cache) — { $window } min, cofa operator; pomijane, jeśli L2 niewdrożony
storm-brake-step-shed = wstrzymaj nieistotną jednostkę publikującą `{ $unit }`
storm-brake-shed-none = (brak zadeklarowanych jednostek do odciążenia — ustaw storm_brake_shed_units w profile.toml lub podaj --shed-unit)
storm-brake-announce-note = zapisz jedno OperatorActionV1 w ścieżce audytu z wynikami poszczególnych kroków
storm-brake-dryrun-note = próba — nic nie wykonano.
storm-brake-triggers-header = Uruchom, gdy pasmo host-net relay-health pokazuje:
storm-brake-trigger-futility = wskaźnik daremności sygnalizacji >= 0.5 przy >= 100 wiad./min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = utrata uplinku >= 5% przez 10 min (wan.loss_pct)
storm-brake-triggers-band-url = pasmo hosta: relay-health.{ $site }.<zone> (zob. docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Uwaga: samo opróżnienie sygnalizacji NIE przywraca mesh. Lekiem są warstwy 1-2 (backoff klienta + odciążanie sygnalizacji); hamulec daje tylko czas na odzyskanie.
storm-brake-confirm = Uruchomić hamulec burzy na { $user }@{ $host }? Restartuje kontener sygnalizacji i wstrzymuje zadeklarowanych publisherów. [y/N]
storm-brake-aborted = hamulec burzy przerwany (brak potwierdzenia)
storm-brake-executing = uruchamiam hamulec burzy na { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = błąd
storm-brake-outcome-skipped = pominięto
storm-brake-summary = hamulec burzy zakończony: { $ok } ok, { $failed } błędów, { $skipped } pominiętych
storm-brake-flush-failed = FLUSH nieudany — kontener sygnalizacji nie zrestartował się, więc odzyskanie mesh nie jest spodziewane: { $reason }
storm-brake-mgmt-unresolved = nie ustalono hosta mgmt — podaj --mgmt-host, ustaw mgmt_ssh_host w profile.toml kontekstu lub wyeksportuj GOAT_MGMT_SSH_HOST
storm-brake-audit-note = akcję operatora zapisano w ścieżce audytu (aktor { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portal odpowiedział HTTP 404 dla `{ $path }`: nie zarejestrowano tam
error-portal-rate-limited-summary = portal odpowiedział HTTP 429 dla `{ $path }`: zbyt wiele żądań, spróbuj później
error-portal-server-error-summary = portal odpowiedział HTTP { $status } dla `{ $path }`: błąd po stronie portalu
error-portal-refused-summary = portal odpowiedział HTTP { $status } dla `{ $path }`: żądanie odrzucone

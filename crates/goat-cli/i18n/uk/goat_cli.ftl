### goat-cli — Ukrainian (uk). Tier-1 seed, PARTIAL by design.
### source: machine-assisted (Claude), NOT native-reviewed
### review: machine
### reconciled-against-en: introduced with track/goat-cli-i18n (this change-set)
### coverage: partial — only the keys below; every other id falls back
###           to English by the i18n-embed fallback (ADR 1024 decision 1).
###
### This catalog is deliberately incomplete: it proves negotiation +
### per-message English fallback end-to-end. Untranslated summaries
### (cert-expired, portal-unreachable, token-invalid, topic-conflict,
### bundle-invalid) render in English until added in a later, native-
### reviewed pass. `ZTOL` is kept as the contract acronym, not localized.

error-profile-missing-summary = немає профілю для контексту `{ $context }`
error-off-mesh-summary = поза ZTOL — немає підключення до мережі
error-router-unreachable-summary = маршрутизатор Zenoh за адресою `{ $endpoint }` недоступний

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = неправильний формат кінцевої точки маршрутизатора
doctor-step1-endpoint-malformed-hint = очікувалося `<proto>/<host>:<port>`; отримано `{ $endpoint }`
doctor-step1-ok-summary = TCP-з'єднання з хостом маршрутизатора { $host }:{ $port } успішне
doctor-step1-conn-failed-summary = не вдалося встановити TCP-з'єднання з хостом маршрутизатора
doctor-step1-conn-failed-hint = { $err } — запустіть netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); перевірте `netbird status`
doctor-step1-timeout-summary = час очікування TCP-з'єднання з хостом маршрутизатора вичерпано
doctor-step1-timeout-hint = хост маршрутизатора { $host }:{ $port } недоступний за { $ms }мс — імовірно, поза мережею mesh
doctor-step2-endpoint-malformed-hint = виправте емітента bundle (--goat-cli-router-endpoint має бути `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM кореневих CA відсутній або нечитабельний
doctor-step2-ca-missing-hint = { $err } — повторно виконайте `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = маршрутизатор { $host }:{ $port } приймає TCP (TLS перевірено на кроці 3)
doctor-step2-refused-summary = TCP маршрутизатора відхилено
doctor-step2-refused-hint = { $err } — маршрутизатор може бути вимкнений; перевірте розгортання Zenoh на mgmt-host
doctor-step2-timeout-summary = час очікування TCP маршрутизатора вичерпано
doctor-step2-timeout-hint = маршрутизатор { $host }:{ $port } недоступний за { $ms }мс
doctor-step3-config-failed-summary = не вдалося побудувати конфігурацію zenoh
doctor-step3-config-failed-hint = недійсна кінцева точка `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = не вдалося побудувати конфігурацію TLS zenoh
doctor-step3-tls-config-failed-hint = { $err } — імовірно, розбіжність версій zenoh; перевірте відповідність залежностей
doctor-step3-ok-summary = сесію mTLS відкрито — сертифікат прийнято ACL маршрутизатора
doctor-step3-open-failed-summary = не вдалося відкрити сесію zenoh
doctor-step3-open-failed-hint = { $err } — сертифікат може бути прострочений (виконайте `goat profile show`) або відхилений ACL маршрутизатора (невідповідність URI-SAN)
doctor-step3-timeout-summary = час очікування відкриття сесії zenoh вичерпано
doctor-step3-timeout-hint = маршрутизатор не завершив рукостискання за { $secs }с — зсув годинника, неправильний сертифікат або перевантаження
doctor-step4-ok-summary = портал повернув { $n } рядків організації
doctor-step4-token-rejected-summary = портал відхилив токен Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — токен прострочений або відкликаний; повторно пройдіть онбординг через operator-onboard.yml
doctor-step4-request-failed-summary = запит до порталу не вдався
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = час очікування запиту до порталу вичерпано
doctor-step4-timeout-hint = немає відповіді за { $secs }с — перевірте, чи { $endpoint } доступний з мережі mesh
doctor-skipped-summary = пропущено — попередній крок не вдався (повторіть із --all, щоб примусити)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Уніфікований операторський інтерфейс командного рядка для структури даних goat.
help-long-about = goat — орієнтований на оператора CLI, що поєднує panoscope-core в єдиний інструмент: ініціалізація/повторне використання профілю, ZTOL-обізнаний doctor для перевірки досяжності, перегляд порталу організацій, CRUD над topic, pub/sub, делегування до stethoscope, інтерактивний конструктор команд і розповідь про каскад змін у структурі.
help-flag-profile-dir = Перевизначити каталог профілю (типово: $GOAT_PROFILE_DIR або ~/.goat).
help-flag-context = Перевизначити активний контекст (типово: змінна $GOAT_CONTEXT, потім current_context у config.toml, потім «default»).
help-flag-narrate = Показати каскад у структурі, який спричиняє команда, що змінює стан.
help-flag-verbose = Увімкнути докладний діагностичний вивід (або встановіть RUST_LOG=goat=debug).
help-flag-color = Режим кольору для виводу в термінал (auto|always|never).
help-flag-lang = Мова для тексту, призначеного людям (напр. en, uk). Повертається до GOAT_LANG, потім до локалі ОС, потім до англійської.
help-cmd-profile = Керування локальним профілем goat (сертифікат mTLS + токен порталу + набір кінцевих точок).
help-cmd-context = Керування контекстами (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Запустити покроковий зонд досяжності щодо структури.
help-cmd-doctor-long = Покроковий зонд досяжності щодо структури: членство в ZTOL → маршрутизатор Zenoh → прийняття сертифіката → portal. Кожен невдалий крок видає конкретну підказку щодо усунення.
help-cmd-vendor = Перегляд організацій, зареєстрованих у порталі.
help-cmd-topic = Переглянути список, показати, створити або видалити topic.
help-cmd-pub = Опублікувати корисне навантаження на ключ topic.
help-cmd-sub = Підписатися на вираз ключа topic.
help-cmd-inspect = Делегувати запит на діагностичний вердикт до stethoscope.
help-cmd-ui = Інтерактивний об'єднаний вибір: переглядайте реєстр оголошених ⊕ активних і переходьте до лінзи Diagnose / Tail на вибраній цілі.
help-cmd-compose = Інтерактивний TUI для конструювання команд.
help-cmd-narrate = Рефлексія каскаду в структурі над нещодавньою активністю. Ще недоступно (щоденний шлях — це --narrate на команді, що змінює стан).
help-cmd-completion = Вивести скрипт доповнення оболонки у stdout.
help-cmd-explain = Знайти значення коду помилки + спосіб усунення у вбудованому документі `goat-cli-error-codes.md` (за шаблоном cargo --explain).
help-cmd-mock = Опублікувати синтетичні зразки на topic. Типово: тривіальний hello-world JSON із частотою 1Hz. Передайте `--format <.fds>` + `--message <Name>`, щоб натомість закодувати protobuf, наданий оператором.
help-cmd-status = Синтез стану: досяжність ZTOL + portal /api/health + термін дії локального сертифіката, три рядки glyph або один об'єкт --json.
help-cmd-services = Перевірити кожну розміщену службу в `ops/serve/services.yaml` і відобразити один рядок glyph на службу (або один об'єкт --json).
help-cmd-client = Обгорнути локальний daemon goat-clientd через його JSON-RPC IPC socket (`~/.goat-client/goat-clientd.sock`): status / up / down / імпорт bundle.
help-cmd-plugin = Виявлення розширень на основі PATH (у стилі kubectl). Будь-який виконуваний файл `goat-<name>` у PATH можна викликати як `goat <name>`; `goat plugin list` перелічує встановлені плагіни, `goat plugin which <name>` визначає шлях.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = неправильний формат кінцевої точки маршрутизатора: `{ $endpoint }`
status-ztol-refused = TCP відхилено маршрутизатором { $host }:{ $port }: { $err }
status-ztol-unreachable = поза мережею mesh (маршрутизатор { $host }:{ $port } недоступний за { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health не вдався: { $err }
status-portal-timeout = час очікування portal /api/health вичерпано за { $secs }s
status-cert-read-failed = не вдалося прочитати сертифікат: { $err }
status-cert-pem-parse-failed = не вдалося розібрати PEM сертифіката: { $err }
status-cert-wrong-tag = перший блок PEM — { $tag }, очікувалося CERTIFICATE
status-cert-x509-parse-failed = не вдалося розібрати x509: { $err }
status-cert-expired = сертифікат ПРОСТРОЧЕНО на unix={ $not_after }, зараз={ $now }
status-cert-valid = сертифікат дійсний; спливає за { $days_left }d (unix={ $not_after })
inspect-finding-label = знахідка ({ $severity }): { $label } — { $detail }
inspect-remedy-label = усунення: { $title } — { $detail }
inspect-no-outcomes = (немає результатів — ціль не відповідає жодній перевірці)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put прийнято на `{ $key }` ({ $bytes } байтів)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = каскад завершено — { $summary } ({ $n } { $n ->
        [one] наслідок
        [few] наслідки
        [many] наслідків
       *[other] наслідку
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Майстер першого запуску: оберіть sandbox або реальне середовище, переконайтеся, що працює, перейдіть до тьютора.
help-cmd-tutor = Самостійна практична екскурсія структурою (у стилі vimtutor; відновлювана).
help-cmd-docs = Перегляньте документацію goat або згенеруйте повний довідник команд (--markdown).
help-cmd-hello = Проведіть hello крізь структуру наскрізно (sandbox або реальне середовище).
flourish-all-green = 🐐 усе зелене — структура в нормі

# machine-translated additions (2026-06-10) — review per i18n-provenance.toml
error-cert-expired-summary = mTLS-сертифікат прострочено або відхилено маршрутизатором
error-portal-unreachable-summary = portal API за адресою `{ $endpoint }` недоступний
error-token-invalid-summary = portal відхилив Bearer-токен
error-topic-conflict-summary = топік `{ $topic }` вже існує
error-bundle-invalid-summary = перевірка підпису або терміну дії бандла не пройдена

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = дія лише для оператора — у вас немає прав (`goat admin` не діє для не-адміністраторів)
help-flag-narrate-json = Виводити каскад субстрату як NDJSON (один об'єкт на наслідок) для журналів інцидентів та скриптів. Має на увазі --narrate.
help-cmd-admin = Адміністрування лише для оператора: життєвий цикл організації, onboard/revoke/rotate, відключення mesh-пірів, провізія пристроїв і операторського рівня, триаж LAN. Авторизується вашими обліковими даними оператора (на стороні сервера через portal/NetBird token scope); не діє для не-адміністраторів — неавторизований виклик нічого не змінює і завершується з `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = профіль для контексту `{ $context }` існує, але цей користувач не може його прочитати (доступ заборонено)
error-portal-bad-response-summary = портал відповів, але ця збірка goat не змогла розібрати відповідь
doctor-step0-ok-summary = локальне налаштування готове до мережевих перевірок
doctor-step0-warn-summary = у локальному налаштуванні є перевірки, які не вдалося підтвердити
doctor-step0-fail-summary = локальне налаштування потребує уваги
doctor-step0-profile-absent-summary = немає профілю goat для контексту `{ $context }`
doctor-step0-profile-new-hint = перевірте `goat context ls`; якщо це перше налаштування, виконайте `goat ca add <bundle-ca.pem>` і порівняйте його відбиток із відбитком від оператора, потім `goat profile init <bundle.cbor>`; повторно запустіть `goat doctor`
doctor-step0-profile-absent-hint = перевірте `goat context ls`; виберіть наявний контекст через `goat context use <name>` або виконайте `goat profile init <bundle.cbor>` для цього контексту; повторно запустіть `goat doctor`
doctor-step0-profile-denied-summary = профіль goat у { $path } не читається
doctor-step0-profile-denied-hint = виправте власника профілю або запустіть від імені власника; не ініціалізуйте наявний профіль повторно
doctor-step0-profile-incomplete-summary = файл профілю goat { $file } відсутній або не читається
doctor-step0-profile-incomplete-hint = відновіть вказаний файл із виданого пакета або скористайтеся `goat profile rotate <bundle.cbor>`, щоб замінити цей контекст; повторно запустіть `goat doctor`
doctor-step0-profile-invalid-summary = у профілі goat кінцеві точки нечитабельні або відсутні
doctor-step0-profile-empty-token-summary = у профілі goat порожній токен порталу
doctor-step0-profile-ok-summary = контекст goat `{ $context }` має читабельні дані профілю
doctor-step0-client-absent-summary = жоден підтримуваний mesh-клієнт не відповів
doctor-step0-client-absent-hint = встановіть mesh-клієнт, який постачає ваш goatnet, запустіть його демон і повторно запустіть `goat doctor`
doctor-step0-client-stopped-summary = встановлений mesh-клієнт не повернув придатного статусу
doctor-step0-client-stopped-hint = перевірте `netbird status -d` або `goat client status --detail`; запустіть встановлений демон (`sudo netbird service start` для NetBird), потім повторно запустіть `goat doctor`
doctor-step0-netbird-ok-summary = демон NetBird відповів (повідомлений профіль `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd відповів (стан `{ $state }`)
doctor-step0-management-connected-summary = NetBird повідомляє, що management підключено
doctor-step0-management-disconnected-summary = NetBird повідомляє, що management відключено
doctor-step0-management-disconnected-hint = перевірте повідомлений URL management і `netbird status -d`; встановлений тунель може працювати і під час збою management
doctor-step0-management-unknown-summary = налаштовану кінцеву точку management не вдалося встановити локально
doctor-step0-management-unknown-hint = перевірте `netbird status -d` або `goat client diagnostics`; doctor не вгадуватиме URL керування
doctor-step0-tunnel-down-summary = NetBird не повідомляє mesh-адресу
doctor-step0-tunnel-down-hint = виконайте `netbird status -d`; якщо пристрій зареєстровано і демон відповідає, виконайте `goat heal`, потім повторіть `goat doctor`
doctor-step0-tunnel-address-summary = NetBird повідомляє mesh-адресу { $address }; далі перевіряється доступність маршрутизатора
doctor-step0-goat-client-no-bundle-summary = у goat-clientd немає імпортованого бандла
doctor-step0-goat-client-no-bundle-hint = виконайте `goat client bundle import <bundle.cbor>` з бандлом реєстрації клієнта, підключіть клієнт, потім повторіть `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd повідомляє про підключений тунель; далі перевіряється доступність маршрутизатора
doctor-step0-goat-client-outer-disabled-summary = зовнішній тунель goat-clientd вимкнено в режимі netbird-only
doctor-step0-goat-client-down-summary = goat-clientd повідомляє стан тунелю `{ $state }`
doctor-step0-goat-client-down-hint = виконайте `goat client diagnostics`, потім `goat client up`, якщо бандл завантажено; повторіть `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd повідомляє, що внутрішній mesh підключено
doctor-step0-goat-client-inner-disabled-summary = внутрішній mesh goat-clientd вимкнено в режимі wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd повідомляє стан внутрішнього mesh `{ $state }`
doctor-step0-goat-client-inner-down-hint = виконайте `goat client diagnostics`, щоб перевірити внутрішній mesh, після відновлення повторіть `goat doctor`
doctor-step0-management-reachable-summary = кінцева точка керування відповіла HTTP { $status }
doctor-step0-management-auth-summary = HTTPS-точка керування доступна (HTTP { $status }: для цієї перевірки потрібна авторизація)
doctor-step0-management-unexpected-summary = кінцева точка керування повернула неочікуваний HTTP { $status }
doctor-step0-management-unexpected-hint = перевірте налаштований URL керування і попросіть оператора goatnet перевірити цю відповідь
doctor-step0-management-unreachable-summary = налаштована кінцева точка керування недоступна: { $error }
doctor-step0-management-unreachable-hint = перевірте DNS і мережевий доступ до URL, який повідомив клієнт; наявний тунель може все ще працювати
doctor-step0-clock-ok-summary = локальний годинник відрізняється від HTTP-часу керування не більше ніж на { $seconds } с
doctor-step0-clock-skew-summary = локальний годинник відрізняється від HTTP-часу керування на { $seconds } с
doctor-step0-clock-skew-hint = синхронізуйте годинник пристрою, потім повторіть `goat doctor`; перевірка сертифікатів може не проходити, поки годинник неточний
doctor-step0-clock-unknown-summary = не вдалося виміряти розбіжність годинника за заголовком HTTP Date від керування
doctor-step0-clock-unknown-hint = якщо збоїть TLS або перевірка сертифікатів, перевірте синхронізацію часу на пристрої
error-clock-skew-summary = годинник пристрою надто відхиляється від часу керування
error-doctor-inconclusive-summary = doctor зафіксував збій без доведеної причини
error-portal-forbidden-summary = портал заборонив цьому обліковому запису доступ до перевіреного API
doctor-step4-forbidden-summary = портал повернув HTTP 403 для цього профілю
doctor-step4-forbidden-hint = ці облікові дані дійшли до порталу, але не мають доступу до перевіреного API; перевірте `goat profile show` і попросіть оператора goatnet перевірити надання доступу
doctor-step4-decode-summary = портал відповів так, що цей goat не може прочитати
doctor-step4-decode-hint = { $err } — оновіть goat до версії, яку обслуговує ця goatnet, і повторно запустіть `goat doctor`
doctor-step1-control-only-summary = з'єднано лише з сервером керування — немає тунелю до жодного з { $total } пірів
doctor-step1-control-only-hint = ця мережа пропускає TCP 443, але блокує порти даних mesh, тому з цього пристрою нічого в mesh (зокрема сторінки входу) недоступне. Попросіть мережевий відділ дозволити вихідні TCP і UDP до { $relays }, а також UDP 3478 до { $stun }; прямий UDP між пристроями теж бажаний. Щоб перевірити, підключіться до точки доступу телефона й виконайте `netbird status`: піри мають з'єднатися.
doctor-offmesh-dashboard-hint = парк ретрансляторів (зовнішня точка спостереження): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = локальний сертифікат, відповідний ключ і пакет CA розбираються; сертифікат у межах строку дії
doctor-step2-material-failed-summary = не вдалося використати локальний матеріал TLS { $file }
doctor-step2-material-failed-hint = { $err } — перевірте вказаний файл; відновіть його з виданого пакета або виконайте ротацію профілю
doctor-step2-material-invalid-hint = { $file } — не чинний сертифікат X.509 PEM; відновіть виданий матеріал або виконайте `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem недійсний або не відповідає mtls.cert.pem; відновіть виданий матеріал або виконайте `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = локальний сертифікат поза строком дії
doctor-step2-cert-time-hint = виконайте `goat profile show`; перевірте результат перевірки годинника вище, потім попросіть оператора видати новий пакет і, якщо сертифікат прострочений, виконайте `goat profile rotate <new-bundle.cbor>`
doctor-skipped-no-endpoint-summary = пропущено — у profile.toml відсутнє { $field }
doctor-skipped-no-profile-summary = пропущено — немає придатних для читання кінцевих точок профілю для цього контексту
doctor-skipped-no-portal-token-summary = пропущено — немає придатного для читання токена порталу для цього контексту
status-ztol-tcp-ok = порт маршрутизатора { $host }:{ $port } приймає TCP (лише порт — див. сесію)
status-session-ok = сесію zenoh mTLS відкрито за { $ms } мс (маршрутизатор прийняв цей сертифікат)
status-session-skipped = сесію не намагались відкрити (поза mesh; див. ztol)
status-session-material-missing = сесію zenoh не намагались відкрити: { $detail }
status-session-config-failed = помилка конфігурації сесії zenoh: { $err }
status-session-failed = сесію zenoh не відкрито: { $err } — виконайте `goat doctor`
status-session-timeout = сесію zenoh не відкрито за { $secs } с — порт маршрутизатора приймає TCP, але жодна сесія не завершується (маршрутизатор завис, сертифікат відхилено або розбіжність годинників); виконайте `goat doctor`
status-names-live = імена живі (mesh DNS відповів)
status-names-ip-only = імена не перевірялися (кінцеві точки профілю — IP-адреси)
status-names-snapshot = імена за підписаним знімком, серійний номер { $serial }, вік { $age } (mesh DNS недоступний)
status-names-snapshot-aging = імена за підписаним знімком, серійний номер { $serial }, вік { $age } — ЗАСТАРІЛО (mesh DNS недоступний; виправте goat-coredns, після чого імена оновляться автоматично)
status-names-snapshot-daemon = імена за підписаним знімком, серійний номер { $serial }, вік { $age } — сховище демона (mesh DNS недоступний)
status-names-snapshot-daemon-aging = імена за підписаним знімком, серійний номер { $serial }, вік { $age } — сховище демона, ЗАСТАРІЛО (mesh DNS недоступний; виправте goat-coredns, після чого імена оновляться автоматично)
status-names-failed = mesh DNS недоступний, резервний знімок відсутній — { $reason }
status-pqc-on = wireguard PQC (rosenpass) УВІМКНЕНО — { $qr }/{ $connected } підключених вузлів зі стійкістю до квантових атак
status-pqc-off = wireguard PQC (rosenpass) ВИМКНЕНО — класичні рукостискання wireguard
status-pqc-unknown = wireguard PQC невідомо — { $reason }
status-device-none = захист імен на рівні пристрою ВИМКНЕНО — немає сховища імен goat-clientd (команди goat усе одно мають резервний варіант; увімкнення — за name-fallback-resolver.md §9.2)
status-device-broken = сховище імен goat-clientd є (серійний номер { $serial }), але його forwarder НЕ відповідає на { $addr } — демон не працює або підсистема імен зависла
status-device-active = захист імен на рівні пристрою АКТИВНИЙ — goat-clientd перехоплює mesh DNS ОС на { $addr } (серійний номер сховища { $serial }, { $grade })
status-device-not-fronted = forwarder goat-clientd працює на { $addr } (серійний номер сховища { $serial }, { $grade }) — ОС не перенаправлено; браузери використовують mesh DNS напряму
status-device-fronting-unknown = forwarder goat-clientd працює на { $addr } (серійний номер сховища { $serial }, { $grade }) — стан перенаправлення ОС на цій платформі визначити неможливо
status-names-observed = імена НЕКАНОНІЧНІ (ad hoc): спостережений запис, вік { $age } — відсутній у підписаному знімку (mesh DNS недоступний; закодифікуйте цей запис)
status-names-selfclaim = імена через перевірене САМОЗАЯВЛЕННЯ, вік { $age } — підписано власним якорем імені (mesh DNS недоступний)
doctor-names-note = через знімок із серійним номером { $serial }, вік { $age }
doctor-names-selfclaim-note = через перевірене САМОЗАЯВЛЕННЯ, вік { $age } — підписано власним якорем імені
doctor-names-selfclaim-hint = mesh DNS недоступний — цей крок розв'язано через перевірене САМОЗАЯВЛЕННЯ (вік { $age }): власний якір імені підписав цю прив'язку, а peer_bindings кешованого знімка її авторизують. Підписано, але не наживо; щоб це прибрати, виправте mesh DNS.
doctor-names-observed-note = через СПОСТЕРЕЖЕНИЙ (ad hoc) запис, вік { $age } — неканонічний
doctor-names-observed-hint = mesh DNS недоступний — цей крок розв'язано через НЕКАНОНІЧНИЙ спостережений запис (вік { $age }): ім'я існує лише наживо і ніколи не кодифікувалося в реєстрі сайту. Закодифікуйте його (реєстр group_vars → генератор → повторний підпис), інакше він застаріє й зникне; щоб це прибрати, виправте mesh DNS.
doctor-names-hint = mesh DNS недоступний — цей крок розв'язано через підписаний знімок імен (серійний номер { $serial }, вік { $age }). Фабрика лишається досяжною за резервними іменами; щоб це прибрати, виправте mesh DNS (goat-coredns на хості mgmt).
storm-brake-plan-header = План storm brake (ADR 1081, рівень 4 — ручний):
storm-brake-plan-target = mgmt-хост { $user }@{ $host }  (контекст { $context })
storm-brake-step-flush = перезапустити signal-контейнер `{ $container }` (скидає zombie-потоки)
storm-brake-step-tighten = обмежити L2 limiter на `{ $container }` (удвічі знизити rate-limit, підняти TTL neg-cache) — { $window } хв, повернення вручну оператором; пропускається, якщо L2 не розгорнуто
storm-brake-step-shed = призупинити неосновний publisher-юніт `{ $unit }`
storm-brake-shed-none = (shed-юніти не задані — вкажіть storm_brake_shed_units у profile.toml або передайте --shed-unit)
storm-brake-announce-note = записати один OperatorActionV1 в audit trail з результатами кожного кроку
storm-brake-dryrun-note = пробний запуск — нічого не виконано.
storm-brake-triggers-header = Запускати, коли host-net-смуга relay-health показує:
storm-brake-trigger-futility = signaling futility ratio >= 0.5 при >= 100 повідомлень/хв (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = втрати на uplink >= 5% протягом 10 хв (wan.loss_pct)
storm-brake-triggers-band-url = host-смуга: relay-health.{ $site }.<zone> (див. docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Примітка: сам лише signal flush НЕ відновлює mesh. Лікують рівні 1-2 (client backoff + signal shedding); brake лише виграє час на відновлення.
storm-brake-confirm = Запустити storm brake проти { $user }@{ $host }? Перезапустить signal-контейнер і призупинить задані publisher-и. [y/N]
storm-brake-aborted = storm brake скасовано (без підтвердження)
storm-brake-executing = запуск storm brake проти { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = помилка
storm-brake-outcome-skipped = пропущено
storm-brake-summary = storm brake завершено: { $ok } ok, { $failed } з помилкою, { $skipped } пропущено
storm-brake-flush-failed = FLUSH не вдався — signal-контейнер не перезапустився, відновлення mesh не очікується: { $reason }
storm-brake-mgmt-unresolved = mgmt-хост не визначено — передайте --mgmt-host, задайте mgmt_ssh_host у profile.toml контексту або експортуйте GOAT_MGMT_SSH_HOST
storm-brake-audit-note = дію оператора записано в audit trail (actor { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = портал відповів HTTP 404 для `{ $path }`: там не зареєстровано
error-portal-rate-limited-summary = портал відповів HTTP 429 для `{ $path }`: забагато запитів, спробуйте пізніше
error-portal-server-error-summary = портал відповів HTTP { $status } для `{ $path }`: збій на боці порталу
error-portal-refused-summary = портал відповів HTTP { $status } для `{ $path }`: запит відхилено

### goat-cli — Bulgarian (bg). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = няма профил за контекста `{ $context }`
error-off-mesh-summary = извън ZTOL — няма връзка с mesh мрежата
error-router-unreachable-summary = Zenoh маршрутизаторът на `{ $endpoint }` е недостъпен
error-cert-expired-summary = mTLS сертификатът е изтекъл или е отхвърлен от маршрутизатора
error-portal-unreachable-summary = API на портала на `{ $endpoint }` е недостъпно
error-token-invalid-summary = порталът отхвърли Bearer токена
error-topic-conflict-summary = topic `{ $topic }` вече съществува
error-bundle-invalid-summary = проверката на подписа или валидността на bundle бе неуспешна

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = крайната точка на маршрутизатора е с неправилен формат
doctor-step1-endpoint-malformed-hint = очаквано `<proto>/<host>:<port>`; получено `{ $endpoint }`
doctor-step1-ok-summary = TCP връзката към хоста на маршрутизатора { $host }:{ $port } е успешна
doctor-step1-conn-failed-summary = TCP връзката към хоста на маршрутизатора е неуспешна
doctor-step1-conn-failed-hint = { $err } — стартирайте netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); проверете `netbird status`
doctor-step1-timeout-summary = изтекло време за TCP връзка към хоста на маршрутизатора
doctor-step1-timeout-hint = хостът на маршрутизатора { $host }:{ $port } е недостъпен за { $ms }ms — вероятно извън mesh
doctor-step2-endpoint-malformed-hint = поправете издателя на bundle (--goat-cli-router-endpoint трябва да е `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM на CA корените липсва или е нечетим
doctor-step2-ca-missing-hint = { $err } — изпълнете отново `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = маршрутизаторът { $host }:{ $port } приема TCP (TLS проверено в стъпка 3)
doctor-step2-refused-summary = TCP на маршрутизатора отказано
doctor-step2-refused-hint = { $err } — маршрутизаторът може да е спрял; проверете внедряването на Zenoh на mgmt-host
doctor-step2-timeout-summary = изтекло време за TCP на маршрутизатора
doctor-step2-timeout-hint = маршрутизаторът { $host }:{ $port } е недостъпен за { $ms }ms
doctor-step3-config-failed-summary = изграждането на конфигурацията на zenoh е неуспешно
doctor-step3-config-failed-hint = невалидна крайна точка `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = изграждането на TLS конфигурацията на zenoh е неуспешно
doctor-step3-tls-config-failed-hint = { $err } — вероятно несъответствие на версията на zenoh; проверете съответствието на зависимостите
doctor-step3-ok-summary = mTLS сесията е отворена — сертификатът е приет от ACL на маршрутизатора
doctor-step3-open-failed-summary = отварянето на zenoh сесия е неуспешно
doctor-step3-open-failed-hint = { $err } — сертификатът може да е изтекъл (изпълнете `goat profile show`) или отхвърлен от ACL на маршрутизатора (несъответствие URI-SAN)
doctor-step3-timeout-summary = изтекло време за отваряне на zenoh сесия
doctor-step3-timeout-hint = маршрутизаторът не завърши ръкостискането за { $secs }s — отместване на часовника, грешен сертификат или претоварване
doctor-step4-ok-summary = порталът върна { $n } реда(ове) на организация
doctor-step4-token-rejected-summary = порталът отхвърли Bearer токена
doctor-step4-token-rejected-hint = HTTP { $status } — токенът е изтекъл или отменен; регистрирайте се отново чрез operator-onboard.yml
doctor-step4-request-failed-summary = заявката към портала е неуспешна
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = изтекло време за заявка към портала
doctor-step4-timeout-hint = няма отговор за { $secs }s — проверете дали { $endpoint } е достъпен от mesh
doctor-skipped-summary = пропуснато — предишната стъпка е неуспешна (изпълнете отново с --all за принуда)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Унифициран команден ред за оператора на data fabric goat.
help-long-about = goat — CLI, насочен към оператора, който съчетава panoscope-core в един инструмент: init/повторно използване на профил, doctor за достъпност, отчитащ ZTOL, разглеждане на портала за организации, CRUD на topic, pub/sub, делегиране към stethoscope, интерактивен композитор на команди и разказ за каскадата на субстрата.
help-flag-profile-dir = Заменя директорията на профила (по подразбиране: $GOAT_PROFILE_DIR или ~/.goat).
help-flag-context = Заменя активния контекст (по подразбиране: променливата $GOAT_CONTEXT, после current_context в config.toml, после "default").
help-flag-narrate = Показва каскадата на субстрата, задействана от променяща команда.
help-flag-verbose = Активира подробен изход за отстраняване на грешки (или задайте RUST_LOG=goat=debug).
help-flag-color = Цветови режим за изхода в терминала (auto|always|never).
help-flag-lang = Език за текста, насочен към потребителя (напр. en, uk). Връща се към GOAT_LANG, после към локала на операционната система, после към английски.
help-cmd-profile = Управлява локалния профил goat (mTLS сертификат + токен за портала + набор от endpoint-и).
help-cmd-context = Управлява контекстите (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Изпълнява стъпков prober за достъпност срещу fabric.
help-cmd-doctor-long = Стъпков prober за достъпност срещу fabric: членство в ZTOL → Zenoh router → приемане на сертификат → портал. Всяка неуспешна стъпка излъчва конкретен съвет за отстраняване.
help-cmd-vendor = Разглежда организациите, регистрирани в портала.
help-cmd-topic = Изброява, показва, създава или изтрива topic.
help-cmd-pub = Публикува payload към ключ на topic.
help-cmd-sub = Абонира се за израз на ключ на topic.
help-cmd-inspect = Делегира заявка за диагностична присъда към stethoscope.
help-cmd-ui = Интерактивен обединен избор: разглежда декларирания ⊕ списък на живо и преминава към лещата Diagnose / Tail върху избраната цел.
help-cmd-compose = Интерактивен TUI за изграждане на команди.
help-cmd-narrate = Отражение на каскадата на субстрата върху скорошната активност. Все още не е налично (ежедневният път е --narrate върху променяща команда).
help-cmd-completion = Отпечатва скрипта за автоматично допълване на shell към stdout.
help-cmd-explain = Търси значението + отстраняването на код на грешка във вградения документ `goat-cli-error-codes.md` (модел cargo --explain).
help-cmd-mock = Публикува синтетични мостри към topic. По подразбиране: тривиален JSON hello-world при 1Hz. Подайте `--format <.fds>` + `--message <Name>`, за да кодирате вместо това protobuf, предоставен от оператора.
help-cmd-status = Синтез на състоянието: достъпност на ZTOL + /api/health на портала + изтичане на локалния сертификат, три реда glyph или един обект --json.
help-cmd-services = Сондира всяка хоствана услуга в `ops/serve/services.yaml` и рендира по един ред glyph на услуга (или един обект --json).
help-cmd-client = Обвива локалния daemon goat-clientd чрез неговия JSON-RPC IPC сокет (`~/.goat-client/goat-clientd.sock`): status / up / down / импортиране на bundle.
help-cmd-plugin = Откриване на разширения, базирано на PATH (в стил kubectl). Всеки изпълним файл `goat-<name>` в PATH може да се извика като `goat <name>`; `goat plugin list` изброява инсталираните plugin-и, `goat plugin which <name>` разрешава пътя.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = крайната точка на маршрутизатора е с неправилен формат: `{ $endpoint }`
status-ztol-refused = TCP отказано към маршрутизатора { $host }:{ $port }: { $err }
status-ztol-unreachable = извън mesh мрежата (маршрутизаторът { $host }:{ $port } е недостъпен за { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health е неуспешен: { $err }
status-portal-timeout = изтекло време за portal /api/health за { $secs }s
status-cert-read-failed = четенето на сертификата е неуспешно: { $err }
status-cert-pem-parse-failed = разборът на PEM на сертификата е неуспешен: { $err }
status-cert-wrong-tag = първият PEM блок е { $tag }, а се очаква CERTIFICATE
status-cert-x509-parse-failed = разборът на x509 е неуспешен: { $err }
status-cert-expired = сертификатът е ИЗТЕКЪЛ на unix={ $not_after }, сега={ $now }
status-cert-valid = сертификатът е валиден; изтича след { $days_left }d (unix={ $not_after })
inspect-finding-label = находка ({ $severity }): { $label } — { $detail }
inspect-remedy-label = отстраняване: { $title } — { $detail }
inspect-no-outcomes = (няма резултати — целта не съответства на нито една проверка)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put прието на `{ $key }` ({ $bytes } байта)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = каскадата приключи — { $summary } ({ $n } { $n ->
        [one] последствие
       *[other] последствия
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Съветник за първо стартиране: изберете sandbox или реална среда, докажете, че работи, насочете към наставника.
help-cmd-tutor = Самостоятелна практическа обиколка на fabric (в стил vimtutor; възобновяема).
help-cmd-docs = Разгледайте документацията на goat или генерирайте пълния справочник на командите (--markdown).
help-cmd-hello = Прекарайте hello през fabric от край до край (sandbox или реална среда).
flourish-all-green = 🐐 всичко е зелено — fabric е в норма

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = действие само за оператори — нямате права (`goat admin` е неактивна за не-администратори)
help-flag-narrate-json = Излъчва субстратната каскада като NDJSON (един обект на последствие) за инцидентни логове + скриптиране. Подразбира --narrate.
help-cmd-admin = Администрация само за оператори: жизнен цикъл на организация, включване/отмяна/ротация, прекъсване на mesh-партньор, осигуряване на устройства + операторско ниво, LAN диагностика. Оторизира се чрез вашите операторски идентификационни данни (от страна на сървъра чрез обхват на токена portal/NetBird); неактивна за не-администратори — неоторизирано извикване не променя нищо и се проваля с `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = профилът за контекст `{ $context }` съществува, но този потребител не може да го чете (отказан достъп)
error-portal-bad-response-summary = порталът отговори, но тази версия на goat не можа да анализира отговора
doctor-step0-ok-summary = локалната настройка е готова за мрежови проби
doctor-step0-warn-summary = част от проверките на локалната настройка не можаха да бъдат потвърдени
doctor-step0-fail-summary = локалната настройка изисква внимание
doctor-step0-profile-absent-summary = няма goat профил за контекст `{ $context }`
doctor-step0-profile-new-hint = проверете `goat context ls`; ако това е първата ви настройка, изпълнете `goat ca add <bundle-ca.pem>` и сравнете отпечатъка му с този на вашия оператор, после `goat profile init <bundle.cbor>`; изпълнете отново `goat doctor`
doctor-step0-profile-absent-hint = проверете `goat context ls`; изберете съществуващ контекст с `goat context use <name>` или изпълнете `goat profile init <bundle.cbor>` за този контекст; изпълнете отново `goat doctor`
doctor-step0-profile-denied-summary = goat профилът в { $path } не може да се чете
doctor-step0-profile-denied-hint = поправете собствеността на профила или работете като собственика му; не инициализирайте отново съществуващ профил
doctor-step0-profile-incomplete-summary = файлът { $file } на goat профила липсва или не може да се чете
doctor-step0-profile-incomplete-hint = възстановете посочения файл от издадения пакет или използвайте `goat profile rotate <bundle.cbor>`, за да замените този контекст; изпълнете отново `goat doctor`
doctor-step0-profile-invalid-summary = goat профилът има нечетими или липсващи крайни точки
doctor-step0-profile-empty-token-summary = goat профилът има празен токен на портала
doctor-step0-profile-ok-summary = goat контекст `{ $context }` има четим профилен материал
doctor-step0-client-absent-summary = не отговори поддържан mesh клиент
doctor-step0-client-absent-hint = инсталирайте mesh клиента, предоставен от вашия goatnet, стартирайте демона му и изпълнете отново `goat doctor`
doctor-step0-client-stopped-summary = инсталираният mesh клиент не върна използваем статус
doctor-step0-client-stopped-hint = проверете `netbird status -d` или `goat client status --detail`; стартирайте инсталирания демон (`sudo netbird service start` за NetBird), после изпълнете отново `goat doctor`
doctor-step0-netbird-ok-summary = демонът на NetBird отговори (отчетен профил `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd отговори (състояние `{ $state }`)
doctor-step0-management-connected-summary = NetBird отчита свързан management
doctor-step0-management-disconnected-summary = NetBird отчита разкачен management
doctor-step0-management-disconnected-hint = проверете отчетения management URL и `netbird status -d`; вече установен тунел може да работи и при срив на management
doctor-step0-management-unknown-summary = конфигурираната management крайна точка не можа да бъде установена локално
doctor-step0-management-unknown-hint = прегледайте `netbird status -d` или `goat client diagnostics`; doctor няма да налучква URL на management
doctor-step0-tunnel-down-summary = NetBird не отчита mesh адрес
doctor-step0-tunnel-down-hint = изпълнете `netbird status -d`; ако устройството е записано и демонът отговаря, изпълнете `goat heal`, после повторете `goat doctor`
doctor-step0-tunnel-address-summary = NetBird отчита mesh адрес { $address }; следва проверка на достъпността на рутера
doctor-step0-goat-client-no-bundle-summary = goat-clientd няма импортиран bundle
doctor-step0-goat-client-no-bundle-hint = изпълнете `goat client bundle import <bundle.cbor>` с enrollment bundle на клиента, свържете клиента, после повторете `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd отчита свързан тунел; следва проверка на достъпността на рутера
doctor-step0-goat-client-outer-disabled-summary = външният тунел на goat-clientd е изключен в режим netbird-only
doctor-step0-goat-client-down-summary = goat-clientd отчита състояние на тунела `{ $state }`
doctor-step0-goat-client-down-hint = изпълнете `goat client diagnostics`, после `goat client up`, ако bundle е зареден; повторете `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd отчита свързан вътрешен mesh
doctor-step0-goat-client-inner-disabled-summary = вътрешният mesh на goat-clientd е изключен в режим wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd отчита състояние на вътрешния mesh `{ $state }`
doctor-step0-goat-client-inner-down-hint = изпълнете `goat client diagnostics`, за да прегледате вътрешния mesh, после повторете `goat doctor`, след като го възстановите
doctor-step0-management-reachable-summary = management endpoint отговори с HTTP { $status }
doctor-step0-management-auth-summary = management HTTPS endpoint е достъпен (HTTP { $status } изисква авторизация за тази проверка)
doctor-step0-management-unexpected-summary = management endpoint върна неочакван HTTP { $status }
doctor-step0-management-unexpected-hint = проверете конфигурирания URL на management и помолете оператора на goatnet да прегледа този отговор
doctor-step0-management-unreachable-summary = конфигурираният management endpoint е недостъпен: { $error }
doctor-step0-management-unreachable-hint = проверете DNS и мрежовия достъп до URL, подаден от клиента; съществуващ тунел може още да работи
doctor-step0-clock-ok-summary = локалният часовник е в рамките на { $seconds } секунди от HTTP времето на management
doctor-step0-clock-skew-summary = локалният часовник се различава от HTTP времето на management с { $seconds } секунди
doctor-step0-clock-skew-hint = синхронизирайте часовника на устройството, после повторете `goat doctor`; проверката на сертификати може да се провали при грешен часовник
doctor-step0-clock-unknown-summary = отклонението на часовника не може да се измери от HTTP заглавие Date на management
doctor-step0-clock-unknown-hint = проверете синхронизацията на времето на устройството, ако TLS или проверките на сертификати се провалят
error-clock-skew-summary = часовникът на устройството е твърде далеч от времето на управлението
error-doctor-inconclusive-summary = doctor установи грешка без доказана причина
error-portal-forbidden-summary = порталът отказа достъп на този акаунт до тествания API
doctor-step4-forbidden-summary = порталът върна HTTP 403 за този профил
doctor-step4-forbidden-hint = този идентификационен документ достигна портала, но няма достъп до тествания API; проверете `goat profile show` и помолете оператора на goatnet да провери предоставения достъп
doctor-step4-decode-summary = порталът отговори с отговор, който този goat не може да прочете
doctor-step4-decode-hint = { $err } — обновете goat до версията, предоставяна от този goatnet, и стартирайте `goat doctor` отново
doctor-step1-control-only-summary = свързано само с контролния сървър — няма тунел до нито един от { $total } peer-а
doctor-step1-control-only-hint = тази мрежа пропуска TCP 443, но блокира портовете за данни на mesh, затова нищо в mesh (включително страниците за вход) не е достъпно от това устройство. Помолете мрежовия екип да разреши изходящ TCP и UDP към { $relays } и UDP 3478 към { $stun }; препоръчва се и директен UDP между устройствата. За потвърждение се свържете към hotspot на телефон и изпълнете `netbird status`: peer-овете трябва да се свържат.
doctor-offmesh-dashboard-hint = relay флот (външна гледна точка): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = локалният сертификат, съответстващият ключ и CA пакетът се четат; сертификатът е в срока на валидност
doctor-step2-material-failed-summary = локалният TLS материал { $file } не може да се използва
doctor-step2-material-failed-hint = { $err } — проверете посочения файл; възстановете го от издадения пакет или сменете профила
doctor-step2-material-invalid-hint = { $file } не е валиден X.509 PEM сертификат; възстановете издадения материал или изпълнете `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem е невалиден или не съответства на mtls.cert.pem; възстановете издадения материал или изпълнете `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = локалният сертификат е извън срока си на валидност
doctor-step2-cert-time-hint = изпълнете `goat profile show`; проверете резултата за часовника по-горе, след това поискайте нов пакет от оператора и изпълнете `goat profile rotate <new-bundle.cbor>`, ако сертификатът е изтекъл
doctor-skipped-no-endpoint-summary = пропуснато — в profile.toml липсва { $field }
doctor-skipped-no-profile-summary = пропуснато — няма четими endpoint-и в профила за този контекст
doctor-skipped-no-portal-token-summary = пропуснато — няма четим токен за портала за този контекст
status-ztol-tcp-ok = портът на рутера { $host }:{ $port } приема TCP (само порт — вижте сесията)
status-session-ok = zenoh mTLS сесията е отворена за { $ms }ms (рутерът прие този сертификат)
status-session-skipped = сесията не е опитвана (извън mesh; вижте ztol)
status-session-material-missing = zenoh сесията не е опитвана: { $detail }
status-session-config-failed = конфигурацията на zenoh сесията е неуспешна: { $err }
status-session-failed = zenoh сесията не се отвори: { $err } — изпълнете `goat doctor`
status-session-timeout = zenoh сесията не се отвори за { $secs }s — портът на рутера приема TCP, но нито една сесия не се завършва (рутерът е блокирал, сертификатът е отхвърлен или има разминаване в часовника); изпълнете `goat doctor`
status-names-live = имената са живи (mesh DNS отговори)
status-names-ip-only = имената не са проверени (крайните точки в профила са IP адреси)
status-names-snapshot = имена по подписан снимков запис със сериен номер { $serial }, възраст { $age } (mesh DNS е недостъпен)
status-names-snapshot-aging = имена по подписан снимков запис със сериен номер { $serial }, възраст { $age } — ОСТАРЯЛ (mesh DNS е недостъпен; поправете goat-coredns, след което имената се обновяват автоматично)
status-names-snapshot-daemon = имена по подписан снимков запис със сериен номер { $serial }, възраст { $age } — хранилище на демона (mesh DNS е недостъпен)
status-names-snapshot-daemon-aging = имена по подписан снимков запис със сериен номер { $serial }, възраст { $age } — хранилище на демона, ОСТАРЯЛО (mesh DNS е недостъпен; поправете goat-coredns, след което имената се обновяват автоматично)
status-names-failed = mesh DNS е недостъпен и резервният снимков запис не е наличен — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ВКЛ. — { $qr }/{ $connected } свързани пира са устойчиви на квантови атаки
status-pqc-off = wireguard PQC (rosenpass) ИЗКЛ. — класически wireguard ръкостискания
status-pqc-unknown = wireguard PQC е неизвестен — { $reason }
status-device-none = защитата на имената за цялото устройство е ИЗКЛ. — няма хранилище за имена на goat-clientd (командите на goat пак имат резервен вариант; включва се според name-fallback-resolver.md §9.2)
status-device-broken = хранилището за имена на goat-clientd съществува (сериен номер { $serial }), но препращачът му НЕ отговаря на { $addr } — демонът е спрял или подсистемата за имена е блокирала
status-device-active = защитата на имената за цялото устройство е АКТИВНА — goat-clientd е пред mesh DNS на ОС на { $addr } (сериен номер на хранилището { $serial }, { $grade })
status-device-not-fronted = препращачът на goat-clientd работи на { $addr } (сериен номер на хранилището { $serial }, { $grade }) — ОС не е зад него; браузърите ползват директно mesh DNS
status-device-fronting-unknown = препращачът на goat-clientd работи на { $addr } (сериен номер на хранилището { $serial }, { $grade }) — състоянието на препращане на ОС не може да се определи на тази платформа
status-names-observed = имена НЕКАНОНИЧНИ (ad hoc): наблюдаван запис, възраст { $age } — не е в подписания снимков запис (mesh DNS е недостъпен; кодифицирайте този запис)
status-names-selfclaim = имена чрез проверено САМОУТВЪРЖДЕНИЕ, възраст { $age } — подписано от собствения anchor на името (mesh DNS е недостъпен)
doctor-names-note = чрез снимков запис със сериен номер { $serial }, възраст { $age }
doctor-names-selfclaim-note = чрез проверено САМОУТВЪРЖДЕНИЕ, възраст { $age } — подписано от собствения anchor на името
doctor-names-selfclaim-hint = mesh DNS е недостъпен — тази стъпка е разрешена чрез проверено САМОУТВЪРЖДЕНИЕ (възраст { $age }): собственият anchor на името е подписал тази връзка и peer_bindings в кешираната снимка я разрешават. Подписано, но не и живо; поправете mesh DNS, за да изчезне това.
doctor-names-observed-note = чрез НАБЛЮДАВАН (ad hoc) запис, възраст { $age } — неканоничен
doctor-names-observed-hint = mesh DNS е недостъпен — тази стъпка е разрешена чрез НЕКАНОНИЧЕН наблюдаван запис (възраст { $age }): името е само живо и никога не е кодифицирано в регистъра на обекта. Кодифицирайте го (регистър group_vars → генератор → повторно подписване), иначе ще изтече; поправете mesh DNS, за да изчезне това.
doctor-names-hint = mesh DNS е недостъпен — тази стъпка е разрешена чрез подписания снимков запис на имената (сериен номер { $serial }, възраст { $age }). Фабриката остава достъпна чрез резервни имена; поправете mesh DNS (goat-coredns на mgmt хоста), за да изчезне това.
storm-brake-plan-header = План за storm brake (ADR 1081, слой 4 — ръчен):
storm-brake-plan-target = mgmt хост { $user }@{ $host }  (контекст { $context })
storm-brake-step-flush = рестарт на signal контейнера `{ $container }` (прекъсва зомби потоците)
storm-brake-step-tighten = ограничаване на L2 лимитера на `{ $container }` (наполовина rate-limit, по-висок neg-cache TTL) — { $window } мин, връща се ръчно от оператор; пропуска се, ако L2 не е разгърнат
storm-brake-step-shed = пауза на несъществения publisher модул `{ $unit }`
storm-brake-shed-none = (няма зададени модули за спиране — задайте storm_brake_shed_units в profile.toml или подайте --shed-unit)
storm-brake-announce-note = записва един OperatorActionV1 в одитния дневник с резултата от всяка стъпка
storm-brake-dryrun-note = сух пуск — нищо не е изпълнено.
storm-brake-triggers-header = Задействайте, когато host-net лентата на relay-health показва:
storm-brake-trigger-futility = signaling futility ratio >= 0.5 при >= 100 msgs/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = загуби по uplink >= 5% за 10 мин (wan.loss_pct)
storm-brake-triggers-band-url = host лента: relay-health.{ $site }.<zone> (вж. docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Бележка: само изчистване на signal не възстановява mesh. Слоеве 1-2 (client backoff + signal shedding) са лекарството; brake само печели време за възстановяване.
storm-brake-confirm = Да се задейства ли storm brake срещу { $user }@{ $host }? Рестартира signal контейнера и паузира зададените publisher-и. [y/N]
storm-brake-aborted = storm brake прекратен (няма потвърждение)
storm-brake-executing = задействане на storm brake срещу { $user }@{ $host } ...
storm-brake-outcome-ok = ок
storm-brake-outcome-failed = неуспех
storm-brake-outcome-skipped = пропуснато
storm-brake-summary = storm brake приключи: { $ok } ок, { $failed } неуспешни, { $skipped } пропуснати
storm-brake-flush-failed = FLUSH неуспешен — signal контейнерът не се рестартира, възстановяване на mesh не се очаква: { $reason }
storm-brake-mgmt-unresolved = mgmt хостът не е определен — подайте --mgmt-host, задайте mgmt_ssh_host в profile.toml на контекста или експортирайте GOAT_MGMT_SSH_HOST
storm-brake-audit-note = операторското действие е записано в одитния дневник (актьор { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = порталът отговори с HTTP 404 за `{ $path }`: не е регистрирано там
error-portal-rate-limited-summary = порталът отговори с HTTP 429 за `{ $path }`: твърде много заявки, опитайте по-късно
error-portal-server-error-summary = порталът отговори с HTTP { $status } за `{ $path }`: грешка от страната на портала
error-portal-refused-summary = порталът отговори с HTTP { $status } за `{ $path }`: заявката е отказана

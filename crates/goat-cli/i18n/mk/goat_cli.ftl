### goat-cli — Macedonian (mk). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = нема профил за контекстот `{ $context }`
error-off-mesh-summary = надвор од ZTOL — без врска со mesh мрежата
error-router-unreachable-summary = Zenoh рутерот на `{ $endpoint }` е недостапен
error-cert-expired-summary = mTLS сертификатот истече или рутерот го одби
error-portal-unreachable-summary = API на порталот на `{ $endpoint }` е недостапно
error-token-invalid-summary = порталот го одби Bearer токенот
error-topic-conflict-summary = topic `{ $topic }` веќе постои
error-bundle-invalid-summary = проверката на потписот или важноста на bundle не успеа

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = крајната точка на рутерот е со неправилен формат
doctor-step1-endpoint-malformed-hint = очекувано `<proto>/<host>:<port>`; примено `{ $endpoint }`
doctor-step1-ok-summary = TCP врската до хостот на рутерот { $host }:{ $port } успешна
doctor-step1-conn-failed-summary = TCP врската до хостот на рутерот не успеа
doctor-step1-conn-failed-hint = { $err } — стартувајте netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); проверете `netbird status`
doctor-step1-timeout-summary = истече времето за TCP врска до хостот на рутерот
doctor-step1-timeout-hint = хостот на рутерот { $host }:{ $port } недостапен за { $ms }ms — веројатно надвор од mesh
doctor-step2-endpoint-malformed-hint = поправете го издавачот на bundle (--goat-cli-router-endpoint мора да биде `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM на CA корените недостасува или е нечитлив
doctor-step2-ca-missing-hint = { $err } — повторно извршете `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = рутерот { $host }:{ $port } прифаќа TCP (TLS проверено во чекор 3)
doctor-step2-refused-summary = TCP на рутерот одбиен
doctor-step2-refused-hint = { $err } — рутерот можеби е исклучен; проверете го распоредувањето на Zenoh на mgmt-host
doctor-step2-timeout-summary = истече времето за TCP на рутерот
doctor-step2-timeout-hint = рутерот { $host }:{ $port } недостапен за { $ms }ms
doctor-step3-config-failed-summary = изградбата на конфигурацијата на zenoh не успеа
doctor-step3-config-failed-hint = неважечка крајна точка `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = изградбата на TLS конфигурацијата на zenoh не успеа
doctor-step3-tls-config-failed-hint = { $err } — веројатно отстапување на верзијата на zenoh; проверете го совпаѓањето на зависностите
doctor-step3-ok-summary = mTLS сесијата е отворена — сертификатот е прифатен од ACL на рутерот
doctor-step3-open-failed-summary = отворањето на zenoh сесија не успеа
doctor-step3-open-failed-hint = { $err } — сертификатот можеби е истечен (извршете `goat profile show`) или одбиен од ACL на рутерот (несовпаѓање URI-SAN)
doctor-step3-timeout-summary = истече времето за отворање на zenoh сесија
doctor-step3-timeout-hint = рутерот не го заврши ракувањето за { $secs }s — поместување на часовникот, погрешен сертификат или презаситеност
doctor-step4-ok-summary = порталот врати { $n } редови на организација
doctor-step4-token-rejected-summary = порталот го одби Bearer токенот
doctor-step4-token-rejected-hint = HTTP { $status } — токенот е истечен или отповикан; повторно регистрирајте се преку operator-onboard.yml
doctor-step4-request-failed-summary = барањето до порталот не успеа
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = истече времето за барање до порталот
doctor-step4-timeout-hint = нема одговор за { $secs }s — проверете дали { $endpoint } е достапен од mesh
doctor-skipped-summary = прескокнато — претходниот чекор не успеа (повторно извршете со --all за принуда)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Унифицирана операторска командна линија за data fabric goat.
help-long-about = goat — CLI наменет за оператори што го составува panoscope-core во една алатка: иницијализација/повторна употреба на профил, ZTOL-свесен doctor за достапност, прелистување на порталот на организации, CRUD на topic, pub/sub, делегирање на stethoscope, интерактивен составувач на команди и нарација на каскадата на substrate.
help-flag-profile-dir = Прегази го директориумот на профилот (стандардно: $GOAT_PROFILE_DIR или ~/.goat).
help-flag-context = Прегази го активниот контекст (стандардно: околина $GOAT_CONTEXT, потоа current_context во config.toml, потоа „default").
help-flag-narrate = Прикажи ја каскадата на substrate што ја предизвикува командата што менува состојба.
help-flag-verbose = Овозможи детален дебаг-излез (или постави RUST_LOG=goat=debug).
help-flag-color = Режим на боја за терминалскиот излез (auto|always|never).
help-flag-lang = Јазик за текст наменет за луѓе (на пр. en, uk). Се враќа на GOAT_LANG, потоа на локалот на оперативниот систем, потоа на англиски.
help-cmd-profile = Управувај со локалниот goat профил (mTLS сертификат + токен на портал + збир крајни точки).
help-cmd-context = Управувај со контексти (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Изврши чекорест испитувач на достапност спрема fabric.
help-cmd-doctor-long = Чекорест испитувач на достапност спрема fabric: членство во ZTOL → Zenoh рутер → прифаќање сертификат → portal. Секој неуспешен чекор испраќа специфичен совет за поправка.
help-cmd-vendor = Прелистувај организации регистрирани во порталот.
help-cmd-topic = Излистај, прикажи, создади или избриши topic-и.
help-cmd-pub = Објави полнеж на клуч на topic.
help-cmd-sub = Претплати се на израз на клуч на topic.
help-cmd-inspect = Делегирај барање за дијагностичка пресуда до stethoscope.
help-cmd-ui = Интерактивен споен избирач: прелистувај го пописот декларирани ⊕ активни и скокни до лензата Diagnose / Tail на избраната цел.
help-cmd-compose = Интерактивен TUI за градење команди.
help-cmd-narrate = Рефлексија на каскадата на substrate врз неодамнешната активност. Сè уште не е достапно (секојдневниот пат е --narrate на команда што менува состојба).
help-cmd-completion = Испечати ја скриптата за дополнување на школка на stdout.
help-cmd-explain = Побарај го значењето на код на грешка + поправка во вградениот документ `goat-cli-error-codes.md` (шема cargo --explain).
help-cmd-mock = Објави синтетички примероци на topic. Стандардно: тривијален hello-world JSON на 1Hz. Подај `--format <.fds>` + `--message <Name>` за да кодираш protobuf доставен од операторот наместо тоа.
help-cmd-status = Синтеза на здравје: достапност на ZTOL + portal /api/health + истек на локалниот сертификат, три glyph линии или еден --json објект.
help-cmd-services = Испитај ја секоја хостирана услуга во `ops/serve/services.yaml` и прикажи по една glyph линија за услуга (или еден --json објект).
help-cmd-client = Обвиткај го локалниот goat-clientd daemon преку неговиот JSON-RPC IPC socket (`~/.goat-client/goat-clientd.sock`): status / up / down / увоз на bundle.
help-cmd-plugin = Откривање на проширувања базирано на PATH (стил kubectl). Секоја извршна датотека `goat-<name>` на PATH може да се повика како `goat <name>`; `goat plugin list` ги набројува инсталираните приклучоци, `goat plugin which <name>` ја разрешува патеката.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = крајната точка на рутерот е со неправилен формат: `{ $endpoint }`
status-ztol-refused = TCP одбиено кон рутерот { $host }:{ $port }: { $err }
status-ztol-unreachable = надвор од mesh мрежата (рутерот { $host }:{ $port } е недостапен за { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health не успеа: { $err }
status-portal-timeout = истече времето за portal /api/health за { $secs }s
status-cert-read-failed = читањето на сертификатот не успеа: { $err }
status-cert-pem-parse-failed = парсирањето на PEM на сертификатот не успеа: { $err }
status-cert-wrong-tag = првиот PEM блок е { $tag }, а се очекува CERTIFICATE
status-cert-x509-parse-failed = парсирањето на x509 не успеа: { $err }
status-cert-expired = сертификатот е ИСТЕЧЕН на unix={ $not_after }, сега={ $now }
status-cert-valid = сертификатот е важечки; истекува за { $days_left }d (unix={ $not_after })
inspect-finding-label = наод ({ $severity }): { $label } — { $detail }
inspect-remedy-label = поправка: { $title } — { $detail }
inspect-no-outcomes = (нема резултати — целта не одговара на ниту една проверка)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put прифатено на `{ $key }` ({ $bytes } бајти)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = каскадата заврши — { $summary } ({ $n } { $n ->
        [one] последица
       *[other] последици
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Волшебник за прво стартување: избери sandbox или реална средина, докажи дека работи, упати кон туторот.
help-cmd-tutor = Самостојна практична тура низ fabric (во стил vimtutor; продолжлива).
help-cmd-docs = Прелистувај ја документацијата на goat или генерирај го целосниот преглед на команди (--markdown).
help-cmd-hello = Спроведи hello низ fabric од крај до крај (sandbox или реална средина).
flourish-all-green = 🐐 сè зелено — fabric во норма

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = акција само за оператори — немате авторизација (`goat admin` е неактивен за не-администратори)
help-flag-narrate-json = Емитирај го substrate каскадот како NDJSON (еден објект по последица) за инцидент логови + скриптирање. Имплицира --narrate.
help-cmd-admin = Администрација само за оператори: животен циклус на организации, onboard/revoke/rotate, прекин на mesh-peer, обезбедување на уреди + оператор-ниво, LAN тријажа. Авторизирано со вашите оператор акредитиви (server-side преку portal/NetBird token scope); неактивен за не-администратори — неовластен повик не менува ништо и не успева со `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = профилот за контекстот `{ $context }` постои, но овој корисник не може да го чита (пристапот е одбиен)
error-portal-bad-response-summary = порталот одговори, но оваа верзија на goat не можеше да го обработи одговорот
doctor-step0-ok-summary = локалната поставеност е подготвена за мрежни проби
doctor-step0-warn-summary = локалната поставеност има проверки што не можеа да се потврдат
doctor-step0-fail-summary = локалната поставеност бара внимание
doctor-step0-profile-absent-summary = нема goat профил за контекстот `{ $context }`
doctor-step0-profile-new-hint = провери со `goat context ls`; ако ова е првата поставеност, изврши `goat ca add <bundle-ca.pem>` и спореди го неговиот отпечаток со оној од твојот оператор, потоа `goat profile init <bundle.cbor>`; повторно изврши `goat doctor`
doctor-step0-profile-absent-hint = провери со `goat context ls`; избери постоечки контекст со `goat context use <name>`, или изврши `goat profile init <bundle.cbor>` за овој контекст; повторно изврши `goat doctor`
doctor-step0-profile-denied-summary = goat профилот на { $path } не може да се прочита
doctor-step0-profile-denied-hint = поправи ја сопственоста на профилот или работи како корисникот-сопственик; не реиницијализирај постоечки профил
doctor-step0-profile-incomplete-summary = датотеката { $file } од goat профилот недостасува или не може да се прочита
doctor-step0-profile-incomplete-hint = врати ја наведената датотека од издадениот пакет, или користи `goat profile rotate <bundle.cbor>` за да го замениш овој контекст; повторно изврши `goat doctor`
doctor-step0-profile-invalid-summary = goat профилот има нечитливи или отсутни крајни точки
doctor-step0-profile-empty-token-summary = goat профилот има празен токен за порталот
doctor-step0-profile-ok-summary = goat контекстот `{ $context }` има читлив профилен материјал
doctor-step0-client-absent-summary = ниеден поддржан mesh клиент не одговори
doctor-step0-client-absent-hint = инсталирај го mesh клиентот што го обезбедува твојот goatnet, стартувај го неговиот демон, па повторно изврши `goat doctor`
doctor-step0-client-stopped-summary = инсталираниот mesh клиент не врати употреблив статус
doctor-step0-client-stopped-hint = провери со `netbird status -d` или `goat client status --detail`; стартувај го инсталираниот демон (`sudo netbird service start` за NetBird), па повторно изврши `goat doctor`
doctor-step0-netbird-ok-summary = NetBird демонот одговори (пријавен профил `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd одговори (состојба `{ $state }`)
doctor-step0-management-connected-summary = NetBird пријавува дека management е поврзан
doctor-step0-management-disconnected-summary = NetBird пријавува дека management е неповрзан
doctor-step0-management-disconnected-hint = провери го пријавениот management URL и `netbird status -d`; воспоставен тунел може да работи и при испад на management
doctor-step0-management-unknown-summary = конфигурираната management крајна точка не можеше да се утврди локално
doctor-step0-management-unknown-hint = провери со `netbird status -d` или `goat client diagnostics`; doctor нема да погодува URL на management
doctor-step0-tunnel-down-summary = NetBird не пријавува mesh адреса
doctor-step0-tunnel-down-hint = изврши `netbird status -d`; ако е запишан и демонот одговара, изврши `goat heal`, па повторно `goat doctor`
doctor-step0-tunnel-address-summary = NetBird пријавува mesh адреса { $address }; следно се проверува достапноста на рутерот
doctor-step0-goat-client-no-bundle-summary = goat-clientd нема увезен bundle
doctor-step0-goat-client-no-bundle-hint = изврши `goat client bundle import <bundle.cbor>` со bundle-от за запишување на клиентот, поврзи го клиентот, па повторно `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd пријавува поврзан тунел; следно се проверува достапноста на рутерот
doctor-step0-goat-client-outer-disabled-summary = надворешниот тунел на goat-clientd е исклучен во режим netbird-only
doctor-step0-goat-client-down-summary = goat-clientd пријавува состојба на тунелот `{ $state }`
doctor-step0-goat-client-down-hint = изврши `goat client diagnostics`, па `goat client up` ако bundle-от е вчитан; повторно изврши `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd пријавува поврзан внатрешен mesh
doctor-step0-goat-client-inner-disabled-summary = внатрешниот mesh на goat-clientd е исклучен во режим wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd пријавува состојба на внатрешниот mesh `{ $state }`
doctor-step0-goat-client-inner-down-hint = изврши `goat client diagnostics` за преглед на внатрешниот mesh, па повторно `goat doctor` откако ќе се поправи
doctor-step0-management-reachable-summary = management endpoint-от одговори со HTTP { $status }
doctor-step0-management-auth-summary = management HTTPS endpoint-от е достапен (HTTP { $status } бара авторизација за оваа проба)
doctor-step0-management-unexpected-summary = management endpoint-от врати неочекуван HTTP { $status }
doctor-step0-management-unexpected-hint = провери го конфигурираниот management URL и замоли го твојот goatnet оператор да го провери овој одговор
doctor-step0-management-unreachable-summary = конфигурираниот management endpoint не можеше да се достигне: { $error }
doctor-step0-management-unreachable-hint = провери DNS и мрежен пристап до URL-то што го пријавува клиентот; постоечки тунел сè уште може да работи
doctor-step0-clock-ok-summary = локалниот часовник е во рамки од { $seconds } секунди од HTTP времето на management
doctor-step0-clock-skew-summary = локалниот часовник се разликува од HTTP времето на management за { $seconds } секунди
doctor-step0-clock-skew-hint = синхронизирај го часовникот на уредот, па повторно `goat doctor`; проверката на сертификати може да не успее додека часовникот е погрешен
doctor-step0-clock-unknown-summary = отстапувањето на часовникот не можеше да се измери од HTTP Date заглавието на management
doctor-step0-clock-unknown-hint = провери ја синхронизацијата на времето на уредот ако TLS или проверките на сертификати не успеваат
error-clock-skew-summary = часовникот на уредот отстапува премногу од времето на управувањето
error-doctor-inconclusive-summary = doctor забележа грешка без докажана причина
error-portal-forbidden-summary = порталот му го одби пристапот на овој акаунт до проверуваниот API
doctor-step4-forbidden-summary = порталот врати HTTP 403 за овој профил
doctor-step4-forbidden-hint = овој акредитив стигна до порталот, но нема пристап до проверуваниот API; провери со `goat profile show` и побарај од операторот на goatnet да ја потврди дозволата
doctor-step4-decode-summary = порталот врати одговор што овој goat не може да го прочита
doctor-step4-decode-hint = { $err } — ажурирај го goat на верзијата што ја служи овој goatnet, па повтори `goat doctor`
doctor-step1-control-only-summary = поврзан само со контролниот сервер — нема тунел до ниту еден од { $total } peer-ови
doctor-step1-control-only-hint = оваа мрежа пропушта TCP 443 нанадвор, но ги блокира data портите на mesh-от, па ништо на mesh-от (вклучувајќи ги страниците за најава) не е достапно од овој уред. Побарај од мрежниот тим да дозволи излезен TCP и UDP кон { $relays }, и UDP 3478 кон { $stun }; препорачливо е и директно UDP меѓу уредите. За потврда, приклучи се на hotspot од телефон и изврши `netbird status`: peer-овите тогаш треба да се поврзат.
doctor-offmesh-dashboard-hint = relay флота (надворешна гледна точка): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = локалниот сертификат, неговиот клуч и CA пакетот се читливи; сертификатот е во рамките на рокот на важност
doctor-step2-material-failed-summary = локалниот TLS материјал { $file } не може да се употреби
doctor-step2-material-failed-hint = { $err } — прегледај ја именуваната датотека; врати ја од издадениот пакет или ротирај го профилот
doctor-step2-material-invalid-hint = { $file } не е важечки X.509 PEM сертификат; врати го издадениот материјал или изврши `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem е невалиден или не одговара на mtls.cert.pem; врати го издадениот материјал или изврши `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = локалниот сертификат е надвор од рокот на важност
doctor-step2-cert-time-hint = изврши `goat profile show`; провери го резултатот за часовникот погоре, па побарај нов пакет од операторот и изврши `goat profile rotate <new-bundle.cbor>` ако сертификатот истекол
doctor-skipped-no-endpoint-summary = прескокнато — во profile.toml недостасува { $field }
doctor-skipped-no-profile-summary = прескокнато — нема читливи endpoint-и на профилот за овој контекст
doctor-skipped-no-portal-token-summary = прескокнато — нема читлив токен за порталот за овој контекст
status-ztol-tcp-ok = портата на рутерот { $host }:{ $port } прима TCP (само портата — види сесија)
status-session-ok = zenoh mTLS сесијата е отворена за { $ms }ms (рутерот го прифати овој сертификат)
status-session-skipped = сесијата не е обидена (надвор од mesh; види ztol)
status-session-material-missing = zenoh сесијата не е обидена: { $detail }
status-session-config-failed = конфигурацијата на zenoh сесијата не успеа: { $err }
status-session-failed = zenoh сесијата не се отвори: { $err } — извршете `goat doctor`
status-session-timeout = zenoh сесијата не се отвори за { $secs }s — портот на рутерот прима TCP, но ниту една сесија не завршува (рутерот е заглавен, сертификатот е одбиен или има отстапување на часовникот); извршете `goat doctor`
status-names-live = имињата се активни (mesh DNS одговори)
status-names-ip-only = имињата не се испитани (endpoint-ите во профилот се IP адреси)
status-names-snapshot = имиња според потпишан snapshot со сериски број { $serial }, старост { $age } (mesh DNS недостапен)
status-names-snapshot-aging = имиња според потпишан snapshot со сериски број { $serial }, старост { $age } — ЗАСТАРЕНО (mesh DNS недостапен; поправете го goat-coredns, потоа имињата се освежуваат автоматски)
status-names-snapshot-daemon = имиња според потпишан snapshot со сериски број { $serial }, старост { $age } — складиште на демонот (mesh DNS недостапен)
status-names-snapshot-daemon-aging = имиња според потпишан snapshot со сериски број { $serial }, старост { $age } — складиште на демонот, ЗАСТАРЕНО (mesh DNS недостапен; поправете го goat-coredns, потоа имињата се освежуваат автоматски)
status-names-failed = mesh DNS е недостапен и snapshot резервата не е достапна — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ВКЛУЧЕН — { $qr }/{ $connected } поврзани peer-и се отпорни на квантни напади
status-pqc-off = wireguard PQC (rosenpass) ИСКЛУЧЕН — класични wireguard handshake-ови
status-pqc-unknown = wireguard PQC непознат — { $reason }
status-device-none = заштитата на имиња за целиот уред е ИСКЛУЧЕНА — нема goat-clientd складиште на имиња (goat командите сепак имаат резерва; овозможете според name-fallback-resolver.md §9.2)
status-device-broken = goat-clientd складиштето на имиња постои (сериски број { $serial }), но неговиот forwarder НЕ одговара на { $addr } — демонот е запрен или поткомпонентата за имиња е заглавена
status-device-active = заштитата на имиња за целиот уред е АКТИВНА — goat-clientd е пред OS mesh DNS на { $addr } (сериски број на складиште { $serial }, { $grade })
status-device-not-fronted = goat-clientd forwarder работи на { $addr } (сериски број на складиште { $serial }, { $grade }) — OS не е зад него; прелистувачите користат директен mesh DNS
status-device-fronting-unknown = goat-clientd forwarder работи на { $addr } (сериски број на складиште { $serial }, { $grade }) — состојбата на OS пренасочувањето не може да се открие на оваа платформа
status-names-observed = имиња НЕКАНОНСКИ (ad hoc): забележан запис, старост { $age } — не е во потпишан snapshot (mesh DNS недостапен; кодифицирајте го записот)
status-names-selfclaim = имиња преку проверен SELF-CLAIM, старост { $age } — потпишан од сопствениот anchor на името (mesh DNS недостапен)
doctor-names-note = преку snapshot со сериски број { $serial }, старост { $age }
doctor-names-selfclaim-note = преку проверен SELF-CLAIM, старост { $age } — потпишан од сопствениот anchor на името
doctor-names-selfclaim-hint = mesh DNS е недостапен — овој чекор се разреши преку проверен SELF-CLAIM (старост { $age }): сопствениот anchor на името го потпиша ова врзување, а peer_bindings од кешираниот snapshot го овластуваат. Потпишано, но не и активно; поправете го mesh DNS за да исчезне ова.
doctor-names-observed-note = преку ЗАБЕЛЕЖАН (ad hoc) запис, старост { $age } — неканонски
doctor-names-observed-hint = mesh DNS е недостапен — овој чекор се разреши преку НЕКАНОНСКИ забележан запис (старост { $age }): името постои само во живо и никогаш не е кодифицирано во регистарот на локацијата. Кодифицирајте го (group_vars регистар → генератор → повторно потпишување) или ќе истече; поправете го mesh DNS за да исчезне ова.
doctor-names-hint = mesh DNS е недостапен — овој чекор се разреши преку потпишаниот snapshot на имиња (сериски број { $serial }, старост { $age }). Фабриката останува достапна преку резервни имиња; поправете го mesh DNS (goat-coredns на mgmt хостот) за да исчезне ова.
storm-brake-plan-header = План за storm brake (ADR 1081 Layer 4 — рачно):
storm-brake-plan-target = mgmt хост { $user }@{ $host }  (контекст { $context })
storm-brake-step-flush = рестартирај го signal контејнерот `{ $container }` (ги отфрла zombie стримовите)
storm-brake-step-tighten = стесни го L2 limiter на `{ $container }` (преполови го rate-limit, зголеми го neg-cache TTL) — { $window } мин, враќање рачно од операторот; прескокнато ако L2 не е распореден
storm-brake-step-shed = паузирај ја неесенцијалната publisher единица `{ $unit }`
storm-brake-shed-none = (нема декларирани shed единици — постави storm_brake_shed_units во profile.toml или користи --shed-unit)
storm-brake-announce-note = испрати еден OperatorActionV1 во audit trail со исходи по чекор
storm-brake-dryrun-note = проба без извршување — ништо не беше извршено.
storm-brake-triggers-header = Активирај кога host-net опсегот на relay-health покажува:
storm-brake-trigger-futility = signaling futility ratio >= 0.5 при >= 100 пораки/мин (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = загуба на uplink >= 5% во траење од 10 мин (wan.loss_pct)
storm-brake-triggers-band-url = host опсег: relay-health.{ $site }.<zone> (види docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Забелешка: само signal flush НЕ ја враќа мрежата. Лек се Layers 1-2 (client backoff + signal shedding); брејкот купува време за обнова.
storm-brake-confirm = Да се активира storm brake врз { $user }@{ $host }? Го рестартира signal контејнерот и ги паузира декларираните publisher-и. [y/N]
storm-brake-aborted = storm brake прекинат (без потврда)
storm-brake-executing = се активира storm brake врз { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = неуспешно
storm-brake-outcome-skipped = прескокнато
storm-brake-summary = storm brake завршен: { $ok } ok, { $failed } неуспешни, { $skipped } прескокнати
storm-brake-flush-failed = FLUSH неуспешен — signal контејнерот не се рестартираше, па обнова на мрежата не се очекува: { $reason }
storm-brake-mgmt-unresolved = mgmt хостот не е разрешен — користи --mgmt-host, постави mgmt_ssh_host во profile.toml на контекстот или извези GOAT_MGMT_SSH_HOST
storm-brake-audit-note = операторската акција е запишана во audit trail (actor { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = порталот одговори HTTP 404 за `{ $path }`: не е регистрирано таму
error-portal-rate-limited-summary = порталот одговори HTTP 429 за `{ $path }`: премногу барања, обидете се подоцна
error-portal-server-error-summary = порталот одговори HTTP { $status } за `{ $path }`: грешка на страната на порталот
error-portal-refused-summary = порталот одговори HTTP { $status } за `{ $path }`: барањето е одбиено

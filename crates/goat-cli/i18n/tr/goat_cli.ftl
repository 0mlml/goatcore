### goat-cli — Turkish (tr). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = `{ $context }` bağlamı için profil yok
error-off-mesh-summary = ZTOL dışında — mesh'e bağlı değil
error-router-unreachable-summary = `{ $endpoint }` adresindeki Zenoh yönlendiricisine erişilemiyor
error-cert-expired-summary = mTLS sertifikasının süresi doldu veya yönlendirici tarafından reddedildi
error-portal-unreachable-summary = `{ $endpoint }` adresindeki portal API'sine erişilemiyor
error-token-invalid-summary = portal Bearer belirtecini reddetti
error-topic-conflict-summary = `{ $topic }` topic'i zaten mevcut
error-bundle-invalid-summary = bundle imzası veya son kullanma doğrulaması başarısız oldu

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = yönlendirici uç noktası hatalı biçimlendirilmiş
doctor-step1-endpoint-malformed-hint = beklenen `<proto>/<host>:<port>`; alınan `{ $endpoint }`
doctor-step1-ok-summary = yönlendirici ana bilgisayarına { $host }:{ $port } TCP bağlantısı başarılı
doctor-step1-conn-failed-summary = yönlendirici ana bilgisayarına TCP bağlantısı başarısız
doctor-step1-conn-failed-hint = { $err } — netbird'i başlatın (`sudo netbird up --management-url <mgmt> --setup-key <key>`); `netbird status` denetleyin
doctor-step1-timeout-summary = yönlendirici ana bilgisayarına TCP bağlantısı zaman aşımına uğradı
doctor-step1-timeout-hint = yönlendirici ana bilgisayarı { $host }:{ $port } { $ms }ms içinde erişilemez — muhtemelen mesh dışında
doctor-step2-endpoint-malformed-hint = bundle yayıncısını düzeltin (--goat-cli-router-endpoint `<proto>/<host>:<port>` olmalıdır)
doctor-step2-ca-missing-summary = CA kökleri PEM eksik veya okunamıyor
doctor-step2-ca-missing-hint = { $err } — `goat profile init <bundle.cbor>` komutunu yeniden çalıştırın
doctor-step2-ok-summary = yönlendirici { $host }:{ $port } TCP kabul ediyor (TLS 3. adımda doğrulandı)
doctor-step2-refused-summary = yönlendirici TCP reddedildi
doctor-step2-refused-hint = { $err } — yönlendirici kapalı olabilir; mgmt-host üzerindeki Zenoh dağıtımını denetleyin
doctor-step2-timeout-summary = yönlendirici TCP zaman aşımına uğradı
doctor-step2-timeout-hint = yönlendirici { $host }:{ $port } { $ms }ms içinde erişilemez
doctor-step3-config-failed-summary = zenoh yapılandırması oluşturulamadı
doctor-step3-config-failed-hint = geçersiz uç nokta `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = zenoh TLS yapılandırması oluşturulamadı
doctor-step3-tls-config-failed-hint = { $err } — muhtemelen zenoh sürüm sapması; bağımlılıkların eşleştiğini doğrulayın
doctor-step3-ok-summary = mTLS oturumu açıldı — sertifika yönlendirici ACL tarafından kabul edildi
doctor-step3-open-failed-summary = zenoh oturumu açılamadı
doctor-step3-open-failed-hint = { $err } — sertifikanın süresi dolmuş (`goat profile show` çalıştırın) veya yönlendirici ACL tarafından reddedilmiş olabilir (URI-SAN uyuşmazlığı)
doctor-step3-timeout-summary = zenoh oturumu açılışı zaman aşımına uğradı
doctor-step3-timeout-hint = yönlendirici el sıkışmayı { $secs }s içinde tamamlamadı — saat kayması, yanlış sertifika veya tıkanıklık
doctor-step4-ok-summary = portal { $n } kuruluş satırı döndürdü
doctor-step4-token-rejected-summary = portal Bearer belirtecini reddetti
doctor-step4-token-rejected-hint = HTTP { $status } — belirtecin süresi dolmuş veya iptal edilmiş; operator-onboard.yml ile yeniden katılın
doctor-step4-request-failed-summary = portal isteği başarısız
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = portal isteği zaman aşımına uğradı
doctor-step4-timeout-hint = { $secs }s içinde yanıt yok — { $endpoint } adresinin mesh'ten erişilebilir olduğunu denetleyin
doctor-skipped-summary = atlandı — önceki adım başarısız oldu (zorlamak için --all ile yeniden çalıştırın)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = goat veri dokusu için birleşik operatör komut satırı.
help-long-about = goat — panoscope-core'u tek bir araçta birleştiren, operatöre yönelik CLI: profil başlatma/yeniden kullanma, ZTOL farkındalıklı erişilebilirlik doctor'u, kuruluş portalı gezintisi, topic CRUD, pub/sub, stethoscope devretme, etkileşimli bir komut oluşturucu ve substrate kademe anlatımı.
help-flag-profile-dir = Profil dizinini geçersiz kıl (varsayılan: $GOAT_PROFILE_DIR veya ~/.goat).
help-flag-context = Etkin bağlamı geçersiz kıl (varsayılan: $GOAT_CONTEXT değişkeni, ardından config.toml içindeki current_context, ardından "default").
help-flag-narrate = Değiştirici bir komutun başlattığı substrate kademesini göster.
help-flag-verbose = Ayrıntılı hata ayıklama çıktısını etkinleştir (veya RUST_LOG=goat=debug ayarla).
help-flag-color = Terminal çıktısı için renk modu (auto|always|never).
help-flag-lang = İnsana yönelik metin için dil (örn. en, uk). GOAT_LANG'a, ardından OS yereline, ardından İngilizceye geri döner.
help-cmd-profile = Yerel goat profilini yönet (mTLS sertifikası + portal belirteci + uç nokta kümesi).
help-cmd-context = Bağlamları yönet (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Dokuya karşı adımlı bir erişilebilirlik yoklayıcısı çalıştır.
help-cmd-doctor-long = Dokuya karşı adımlı erişilebilirlik yoklayıcısı: ZTOL üyeliği → Zenoh yönlendiricisi → sertifika kabulü → portal. Başarısız olan her adım özel bir düzeltme ipucu yayar.
help-cmd-vendor = Portala kayıtlı kuruluşlara göz at.
help-cmd-topic = topic'leri listele, göster, oluştur veya sil.
help-cmd-pub = Bir topic anahtarına yük yayınla.
help-cmd-sub = Bir topic anahtar ifadesine abone ol.
help-cmd-inspect = Bir tanılama kararı isteğini stethoscope'a devret.
help-cmd-ui = Etkileşimli birleştirilmiş seçici: bildirilen ⊕ canlı listeye göz at ve seçili hedefte Diagnose / Tail merceğine atla.
help-cmd-compose = Etkileşimli komut oluşturucu TUI.
help-cmd-narrate = Son etkinlik üzerinde substrate kademe yansıması. Henüz mevcut değil (günlük yol, değiştirici bir komut üzerindeki --narrate'tir).
help-cmd-completion = Kabuk tamamlama betiğini stdout'a yazdır.
help-cmd-explain = Bir hata kodunun anlamını + düzeltmesini gömülü `goat-cli-error-codes.md` belgesinde ara (cargo --explain modeli).
help-cmd-mock = Bir topic'e sentetik örnekler yayınla. Varsayılan: 1Hz'de basit hello-world JSON. Bunun yerine operatörün sağladığı protobuf'u kodlamak için `--format <.fds>` + `--message <Name>` ver.
help-cmd-status = Sağlık sentezi: ZTOL erişilebilirliği + portal /api/health + yerel sertifika sona erme süresi, üç glyph satırı veya bir --json nesnesi.
help-cmd-services = `ops/serve/services.yaml` içindeki her barındırılan hizmeti yokla ve hizmet başına bir glyph satırı (veya bir --json nesnesi) işle.
help-cmd-client = Yerel goat-clientd daemon'unu JSON-RPC IPC soketi (`~/.goat-client/goat-clientd.sock`) üzerinden sar: status / up / down / bundle import.
help-cmd-plugin = PATH tabanlı uzantı keşfi (kubectl tarzı). PATH üzerindeki herhangi bir `goat-<name>` yürütülebilir dosyası `goat <name>` olarak çağrılabilir; `goat plugin list` yüklü eklentileri sıralar, `goat plugin which <name>` yolu çözer.

# goat status / inspect / --narrate runtime prose (ADR 1024).
status-ztol-malformed = yönlendirici uç noktası hatalı biçimlendirilmiş: `{ $endpoint }`
status-ztol-refused = yönlendiriciye { $host }:{ $port } TCP reddedildi: { $err }
status-ztol-unreachable = mesh dışında (yönlendirici { $host }:{ $port } { $ms }ms içinde erişilemez)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health başarısız: { $err }
status-portal-timeout = portal /api/health { $secs }s içinde zaman aşımına uğradı
status-cert-read-failed = sertifika okuma başarısız: { $err }
status-cert-pem-parse-failed = sertifika PEM ayrıştırma başarısız: { $err }
status-cert-wrong-tag = ilk PEM bloğu { $tag }, CERTIFICATE bekleniyor
status-cert-x509-parse-failed = x509 ayrıştırma başarısız: { $err }
status-cert-expired = sertifikanın SÜRESİ DOLDU unix={ $not_after }, şimdi={ $now }
status-cert-valid = sertifika geçerli; { $days_left }d içinde sona eriyor (unix={ $not_after })
inspect-finding-label = bulgu ({ $severity }): { $label } — { $detail }
inspect-remedy-label = çözüm: { $title } — { $detail }
inspect-no-outcomes = (sonuç yok — hedef hiçbir denetimle eşleşmedi)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put `{ $key }` üzerinde kabul edildi ({ $bytes } bayt)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kademe yerleşti — { $summary } ({ $n } { $n ->
        [one] sonuç
       *[other] sonuç
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = İlk çalıştırma sihirbazı: sandbox veya gerçek seç, çalıştığını kanıtla, tutor'a yönlendir.
help-cmd-tutor = Dokunun kendi hızında, uygulamalı bir turu (vimtutor tarzı; sürdürülebilir).
help-cmd-docs = goat belgelerine göz at veya tam komut referansını yay (--markdown).
help-cmd-hello = Dokunun bir ucundan diğerine bir hello'yu gidiş-dönüş ilet (sandbox veya gerçek ortam).
flourish-all-green = 🐐 hepsi yeşil — doku normal

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = yalnızca operatör eylemi — yetkili değilsiniz (`goat admin` yönetici olmayanlar için etkisizdir)
help-flag-narrate-json = Olay günlükleri ve betik kullanımı için substrat basamağını NDJSON olarak yayar (sonuç başına bir nesne). --narrate seçeneğini kapsar.
help-cmd-admin = Yalnızca operatöre özgü yönetim: kuruluş yaşam döngüsü, ekleme/iptal/döndürme, mesh-peer kesimi, cihaz + operatör katmanı sağlama, LAN önceliklendirme. Operatör kimlik bilgilerinizle yetkilendirilir (portal/NetBird token kapsamı aracılığıyla sunucu tarafı); yönetici olmayanlar için etkisizdir — yetkisiz bir çağrı hiçbir şeyi değiştirmez ve `E_NOT_ADMIN` ile başarısız olur.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = `{ $context }` bağlamı için profil mevcut ancak bu kullanıcı tarafından okunamıyor (izin reddedildi)
error-portal-bad-response-summary = portal yanıt verdi ancak bu goat sürümü yanıtı ayrıştıramadı
doctor-step0-ok-summary = yerel kurulum ağ yoklamalarına hazır
doctor-step0-warn-summary = yerel kurulumda doğrulanamayan kontroller var
doctor-step0-fail-summary = yerel kurulum ilgi gerektiriyor
doctor-step0-profile-absent-summary = `{ $context }` bağlamı için goat profili yok
doctor-step0-profile-new-hint = `goat context ls` çıktısını kontrol edin; ilk kurulumunuzsa `goat ca add <bundle-ca.pem>` çalıştırıp parmak izini operatörünüzünkiyle karşılaştırın, ardından `goat profile init <bundle.cbor>` çalıştırın; `goat doctor` komutunu yeniden çalıştırın
doctor-step0-profile-absent-hint = `goat context ls` çıktısını kontrol edin; `goat context use <name>` ile mevcut bir bağlam seçin ya da bu bağlam için `goat profile init <bundle.cbor>` çalıştırın; `goat doctor` komutunu yeniden çalıştırın
doctor-step0-profile-denied-summary = { $path } konumundaki goat profili okunamıyor
doctor-step0-profile-denied-hint = profil sahipliğini düzeltin veya sahibi olan kullanıcı olarak çalıştırın; mevcut bir profili yeniden başlatmayın
doctor-step0-profile-incomplete-summary = goat profil dosyası { $file } eksik veya okunamıyor
doctor-step0-profile-incomplete-hint = adı geçen dosyayı verilen paketten geri yükleyin ya da bu bağlamı değiştirmek için `goat profile rotate <bundle.cbor>` kullanın; `goat doctor` komutunu yeniden çalıştırın
doctor-step0-profile-invalid-summary = goat profilinde okunamayan veya eksik uç noktalar var
doctor-step0-profile-empty-token-summary = goat profilinde boş portal belirteci var
doctor-step0-profile-ok-summary = `{ $context }` goat bağlamının okunabilir profil verisi var
doctor-step0-client-absent-summary = desteklenen hiçbir mesh istemcisi yanıt vermedi
doctor-step0-client-absent-hint = goatnet'inizin sağladığı mesh istemcisini kurun, daemon'ını başlatın, ardından `goat doctor` komutunu yeniden çalıştırın
doctor-step0-client-stopped-summary = kurulu mesh istemcisi kullanılabilir durum döndürmedi
doctor-step0-client-stopped-hint = `netbird status -d` veya `goat client status --detail` çıktısını kontrol edin; kurulu daemon'ı başlatın (NetBird için `sudo netbird service start`), ardından `goat doctor` komutunu yeniden çalıştırın
doctor-step0-netbird-ok-summary = NetBird daemon yanıt verdi (bildirilen profil `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd yanıt verdi (durum `{ $state }`)
doctor-step0-management-connected-summary = NetBird yönetim bağlantısının kurulu olduğunu bildiriyor
doctor-step0-management-disconnected-summary = NetBird yönetim bağlantısının kesik olduğunu bildiriyor
doctor-step0-management-disconnected-hint = bildirilen yönetim URL'sini ve `netbird status -d` çıktısını kontrol edin; kurulu bir tünel yönetim kesintisi sırasında da çalışabilir
doctor-step0-management-unknown-summary = yapılandırılmış yönetim uç noktasıyla yerel olarak bağlantı kurulamadı
doctor-step0-management-unknown-hint = `netbird status -d` veya `goat client diagnostics` ile inceleyin; doctor management URL'sini tahmin etmez
doctor-step0-tunnel-down-summary = NetBird mesh adresi bildirmiyor
doctor-step0-tunnel-down-hint = `netbird status -d` çalıştırın; kayıtlıysa ve daemon yanıt veriyorsa `goat heal` çalıştırın, ardından `goat doctor` komutunu yeniden çalıştırın
doctor-step0-tunnel-address-summary = NetBird mesh adresi { $address } bildiriyor; router erişilebilirliği sıradaki adımda denetlenir
doctor-step0-goat-client-no-bundle-summary = goat-clientd'de içe aktarılmış bundle yok
doctor-step0-goat-client-no-bundle-hint = istemci kayıt bundle'ı ile `goat client bundle import <bundle.cbor>` çalıştırın, istemciyi bağlayın, ardından `goat doctor` komutunu yeniden çalıştırın
doctor-step0-goat-client-connected-summary = goat-clientd bağlı bir tünel bildiriyor; router erişilebilirliği sıradaki adımda denetlenir
doctor-step0-goat-client-outer-disabled-summary = netbird-only modunda goat-clientd dış tüneli devre dışı
doctor-step0-goat-client-down-summary = goat-clientd tünel durumunu `{ $state }` olarak bildiriyor
doctor-step0-goat-client-down-hint = `goat client diagnostics` çalıştırın; bundle yüklüyse ardından `goat client up` çalıştırın; `goat doctor` komutunu yeniden çalıştırın
doctor-step0-goat-client-inner-ok-summary = goat-clientd iç mesh'in bağlı olduğunu bildiriyor
doctor-step0-goat-client-inner-disabled-summary = wg-cp0-only modunda goat-clientd iç mesh'i devre dışı
doctor-step0-goat-client-inner-down-summary = goat-clientd iç mesh durumunu `{ $state }` olarak bildiriyor
doctor-step0-goat-client-inner-down-hint = iç mesh'i incelemek için `goat client diagnostics` çalıştırın, düzelttikten sonra `goat doctor` komutunu yeniden çalıştırın
doctor-step0-management-reachable-summary = management uç noktası HTTP { $status } ile yanıt verdi
doctor-step0-management-auth-summary = management HTTPS uç noktasına erişilebiliyor (HTTP { $status }, bu yoklama için yetkilendirme gerektirir)
doctor-step0-management-unexpected-summary = management uç noktası beklenmeyen HTTP { $status } döndürdü
doctor-step0-management-unexpected-hint = yapılandırılmış management URL'sini inceleyin ve goatnet operatörünüzden bu yanıtı kontrol etmesini isteyin
doctor-step0-management-unreachable-summary = yapılandırılmış management uç noktasına erişilemedi: { $error }
doctor-step0-management-unreachable-hint = istemcinin bildirdiği URL için DNS ve ağ erişimini kontrol edin; mevcut tünel yine de çalışıyor olabilir
doctor-step0-clock-ok-summary = yerel saat, management HTTP zamanından en fazla { $seconds } saniye farklı
doctor-step0-clock-skew-summary = yerel saat management HTTP zamanından { $seconds } saniye farklı
doctor-step0-clock-skew-hint = cihaz saatini senkronize edin, ardından `goat doctor` komutunu yeniden çalıştırın; saat yanlışken sertifika doğrulaması başarısız olabilir
doctor-step0-clock-unknown-summary = saat sapması management HTTP Date başlığından ölçülemedi
doctor-step0-clock-unknown-hint = TLS veya sertifika denetimleri başarısız olursa cihazın zaman senkronizasyonunu kontrol edin
error-clock-skew-summary = cihaz saati yönetim zamanından çok uzak
error-doctor-inconclusive-summary = doctor, nedeni kanıtlanmamış bir hata gözlemledi
error-portal-forbidden-summary = portal, bu hesabın sorgulanan API'ye erişimini reddetti
doctor-step4-forbidden-summary = portal bu profil için HTTP 403 döndürdü
doctor-step4-forbidden-hint = bu kimlik bilgisi portala ulaştı ama sorgulanan API'ye erişimi yok; `goat profile show` çıktısını kontrol edin ve goatnet operatörünüzden yetkiyi doğrulamasını isteyin
doctor-step4-decode-summary = portal, bu goat'un okuyamadığı bir yanıt verdi
doctor-step4-decode-hint = { $err } — goat'u bu goatnet'in sunduğu sürüme güncelleyin, sonra `goat doctor` komutunu yeniden çalıştırın
doctor-step1-control-only-summary = yalnızca kontrol sunucusuna bağlı — { $total } eşin hiçbirine tünel yok
doctor-step1-control-only-hint = bu ağ TCP 443'ten çıkışa izin veriyor ama mesh veri portlarını engelliyor; bu yüzden mesh üzerindeki hiçbir şeye (oturum açma sayfaları dahil) bu cihazdan erişilemiyor. Ağ ekibinden { $relays } adreslerine giden TCP ve UDP'ye, ayrıca { $stun } adresine UDP 3478'e izin vermesini isteyin; cihazlar arası doğrudan UDP de önerilir. Doğrulamak için telefon hotspot'una bağlanıp `netbird status` çalıştırın: eşler bu durumda bağlanmalı.
doctor-offmesh-dashboard-hint = röle filosu (harici bakış noktası): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = yerel sertifika, eşleşen anahtar ve CA paketi okunabiliyor; sertifika geçerlilik aralığında
doctor-step2-material-failed-summary = yerel TLS materyali { $file } kullanılamadı
doctor-step2-material-failed-hint = { $err } — adı geçen dosyayı inceleyin; verilen paketten geri yükleyin veya profili yenileyin
doctor-step2-material-invalid-hint = { $file } geçerli bir X.509 PEM sertifikası değil; verilen materyali geri yükleyin veya `goat profile rotate <bundle.cbor>` çalıştırın
doctor-step2-key-invalid-hint = mtls.key.pem geçersiz veya mtls.cert.pem ile eşleşmiyor; verilen materyali geri yükleyin veya `goat profile rotate <bundle.cbor>` çalıştırın
doctor-step2-cert-time-summary = yerel sertifika geçerlilik aralığının dışında
doctor-step2-cert-time-hint = `goat profile show` çalıştırın; yukarıdaki saat sonucunu kontrol edin, sertifikanın süresi dolduysa operatörünüzden yeni bir paket isteyin ve `goat profile rotate <new-bundle.cbor>` çalıştırın
doctor-skipped-no-endpoint-summary = atlandı — profile.toml içinde { $field } eksik
doctor-skipped-no-profile-summary = atlandı — bu bağlam için okunabilir profil uç noktası yok
doctor-skipped-no-portal-token-summary = atlandı — bu bağlam için okunabilir portal belirteci yok
status-ztol-tcp-ok = yönlendirici portu { $host }:{ $port } TCP kabul ediyor (yalnızca port — bkz. oturum)
status-session-ok = zenoh mTLS oturumu { $ms }ms içinde açıldı (yönlendirici bu sertifikayı kabul etti)
status-session-skipped = oturum denenmedi (mesh dışında; bkz. ztol)
status-session-material-missing = zenoh oturumu denenmedi: { $detail }
status-session-config-failed = zenoh oturum yapılandırması başarısız: { $err }
status-session-failed = zenoh oturumu açılmadı: { $err } — `goat doctor` çalıştırın
status-session-timeout = zenoh oturumu { $secs } sn içinde açılmadı — router portu TCP kabul ediyor ama hiçbir oturum tamamlanmıyor (router kilitlenmiş, sertifika reddedilmiş veya saat kayması olabilir); `goat doctor` çalıştırın
status-names-live = isimler canlı (mesh DNS yanıt verdi)
status-names-ip-only = isimler denenmedi (profil uç noktaları IP değeri)
status-names-snapshot = isimler imzalı anlık görüntü seri { $serial } kaynaklı, yaş { $age } (mesh DNS erişilemiyor)
status-names-snapshot-aging = isimler imzalı anlık görüntü seri { $serial } kaynaklı, yaş { $age } — ESKİ (mesh DNS erişilemiyor; goat-coredns'u düzeltin, isimler sonra otomatik yenilenir)
status-names-snapshot-daemon = isimler imzalı anlık görüntü seri { $serial } kaynaklı, yaş { $age } — daemon deposu (mesh DNS erişilemiyor)
status-names-snapshot-daemon-aging = isimler imzalı anlık görüntü seri { $serial } kaynaklı, yaş { $age } — daemon deposu, ESKİ (mesh DNS erişilemiyor; goat-coredns'u düzeltin, isimler sonra otomatik yenilenir)
status-names-failed = mesh DNS erişilemiyor ve anlık görüntü yedeği kullanılamıyor — { $reason }
status-pqc-on = wireguard PQC (rosenpass) AÇIK — bağlı { $connected } eşten { $qr } tanesi kuantum dirençli
status-pqc-off = wireguard PQC (rosenpass) KAPALI — klasik wireguard el sıkışmaları
status-pqc-unknown = wireguard PQC bilinmiyor — { $reason }
status-device-none = cihaz genelinde isim koruması KAPALI — goat-clientd isim deposu yok (goat komutlarında yedek yine var; name-fallback-resolver.md §9.2'ye göre etkinleştirin)
status-device-broken = goat-clientd isim deposu var (seri { $serial }) ama yönlendiricisi { $addr } adresinde YANIT VERMİYOR — daemon çalışmıyor veya isim alt sistemi kilitlenmiş
status-device-active = cihaz genelinde isim koruması AKTİF — goat-clientd, { $addr } adresinde işletim sisteminin mesh DNS'inin önünde (depo seri { $serial }, { $grade })
status-device-not-fronted = goat-clientd yönlendiricisi { $addr } adresinde çalışıyor (depo seri { $serial }, { $grade }) — işletim sistemi önüne alınmadı; tarayıcılar doğrudan mesh DNS kullanır
status-device-fronting-unknown = goat-clientd yönlendiricisi { $addr } adresinde çalışıyor (depo seri { $serial }, { $grade }) — işletim sisteminin önüne alınma durumu bu platformda tespit edilemiyor
status-names-observed = isimler STANDART DIŞI (ad hoc): gözlenen kayıt, yaş { $age } — imzalı anlık görüntüde yok (mesh DNS erişilemiyor; bu kaydı kodlayın)
status-names-selfclaim = isimler doğrulanmış KENDİ BEYANI ile, yaş { $age } — ismin kendi çapası imzaladı (mesh DNS erişilemiyor)
doctor-names-note = anlık görüntü seri { $serial } üzerinden, yaş { $age }
doctor-names-selfclaim-note = doğrulanmış KENDİ BEYANI üzerinden, yaş { $age } — ismin kendi çapası imzaladı
doctor-names-selfclaim-hint = mesh DNS erişilemiyor — bu adım doğrulanmış bir KENDİ BEYANI ile çözüldü (yaş { $age }): bu bağlamayı ismin kendi çapası imzaladı ve önbellekteki anlık görüntünün peer_bindings alanı yetkilendiriyor. İmzalı ama canlı değil; temizlemek için mesh DNS'i düzeltin.
doctor-names-observed-note = GÖZLENEN (ad hoc) kayıt üzerinden, yaş { $age } — standart dışı
doctor-names-observed-hint = mesh DNS erişilemiyor — bu adım STANDART DIŞI gözlenen bir kayıtla çözüldü (yaş { $age }): isim yalnızca canlı, site kayıt defterinde hiç kodlanmamış. Kodlayın (group_vars registry → generator → yeniden imzala) yoksa zamanla silinir; temizlemek için mesh DNS'i düzeltin.
doctor-names-hint = mesh DNS erişilemiyor — bu adım imzalı isim anlık görüntüsüyle çözüldü (seri { $serial }, yaş { $age }). Fabric yedek isimlerle erişilebilir kalır; temizlemek için mesh DNS'i (yönetim ana makinesindeki goat-coredns) düzeltin.
storm-brake-plan-header = Fırtına freni planı (ADR 1081 Katman 4 — manuel):
storm-brake-plan-target = yönetim ana makinesi { $user }@{ $host }  (bağlam { $context })
storm-brake-step-flush = sinyal konteyneri `{ $container }` yeniden başlatılır (zombi akışları düşürür)
storm-brake-step-tighten = `{ $container }` üzerindeki L2 sınırlayıcıyı kıs (hız sınırını yarıya indir, neg-cache TTL'yi artır) — { $window } dk, operatör geri alır; L2 dağıtılmamışsa atlanır
storm-brake-step-shed = temel olmayan yayıncı birimi `{ $unit }` duraklatılır
storm-brake-shed-none = (tanımlı kapatılacak birim yok — profile.toml içinde storm_brake_shed_units ayarlayın veya --shed-unit verin)
storm-brake-announce-note = adım sonuçlarıyla birlikte denetim kaydına bir OperatorActionV1 yaz
storm-brake-dryrun-note = deneme çalıştırması — hiçbir şey yürütülmedi.
storm-brake-triggers-header = relay-health ana makine-ağ bandı şunu gösterdiğinde çalıştırın:
storm-brake-trigger-futility = sinyalleşme boşa gitme oranı >= 0.5, >= 100 msj/dk hızında (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = uplink kaybı >= %5, 10 dk boyunca sürekli (wan.loss_pct)
storm-brake-triggers-band-url = ana makine bandı: relay-health.{ $site }.<zone> (bkz. docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Not: tek başına sinyal flush'ı mesh'i geri getirmez. Çözüm Katman 1-2'dir (istemci backoff + sinyal kısma); fren yalnızca toparlanma süresi kazandırır.
storm-brake-confirm = Fırtına freni { $user }@{ $host } üzerinde çalıştırılsın mı? Sinyal konteynerini yeniden başlatır ve tanımlı yayıncıları duraklatır. [y/N]
storm-brake-aborted = fırtına freni iptal edildi (onay yok)
storm-brake-executing = fırtına freni { $user }@{ $host } üzerinde çalıştırılıyor ...
storm-brake-outcome-ok = tamam
storm-brake-outcome-failed = başarısız
storm-brake-outcome-skipped = atlandı
storm-brake-summary = fırtına freni tamamlandı: { $ok } tamam, { $failed } başarısız, { $skipped } atlandı
storm-brake-flush-failed = FLUSH başarısız — sinyal konteyneri yeniden başlamadı, mesh'in toparlanması beklenmez: { $reason }
storm-brake-mgmt-unresolved = yönetim ana makinesi çözümlenemedi — --mgmt-host verin, bağlam profile.toml içinde mgmt_ssh_host ayarlayın veya GOAT_MGMT_SSH_HOST dışa aktarın
storm-brake-audit-note = operatör eylemi denetim kaydına yazıldı (aktör { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portal `{ $path }` için HTTP 404 yanıtı verdi: orada kayıtlı değil
error-portal-rate-limited-summary = portal `{ $path }` için HTTP 429 yanıtı verdi: çok fazla istek, daha sonra yeniden deneyin
error-portal-server-error-summary = portal `{ $path }` için HTTP { $status } yanıtı verdi: portal tarafında bir arıza
error-portal-refused-summary = portal `{ $path }` için HTTP { $status } yanıtı verdi: istek reddedildi

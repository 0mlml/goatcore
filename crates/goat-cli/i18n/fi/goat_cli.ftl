### goat-cli — Finnish (fi). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = ei profiilia kontekstille `{ $context }`
error-off-mesh-summary = ZTOL:n ulkopuolella — ei yhteyttä mesh-verkkoon
error-router-unreachable-summary = Zenoh-reititin osoitteessa `{ $endpoint }` ei ole tavoitettavissa
error-cert-expired-summary = mTLS-varmenne on vanhentunut tai reititin hylkäsi sen
error-portal-unreachable-summary = portaalin API osoitteessa `{ $endpoint }` ei ole tavoitettavissa
error-token-invalid-summary = portaali hylkäsi Bearer-tunnuksen
error-topic-conflict-summary = topic `{ $topic }` on jo olemassa
error-bundle-invalid-summary = bundlen allekirjoituksen tai voimassaolon tarkistus epäonnistui

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = reitittimen päätepiste on virheellisesti muotoiltu
doctor-step1-endpoint-malformed-hint = odotettiin `<proto>/<host>:<port>`; saatiin `{ $endpoint }`
doctor-step1-ok-summary = TCP-yhteys reititinisäntään { $host }:{ $port } onnistui
doctor-step1-conn-failed-summary = TCP-yhteys reititinisäntään epäonnistui
doctor-step1-conn-failed-hint = { $err } — käynnistä netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); tarkista `netbird status`
doctor-step1-timeout-summary = TCP-yhteyden aikakatkaisu reititinisäntään
doctor-step1-timeout-hint = reititinisäntä { $host }:{ $port } ei tavoitettavissa { $ms }ms:ssa — todennäköisesti mesh-verkon ulkopuolella
doctor-step2-endpoint-malformed-hint = korjaa bundlen myöntäjä (--goat-cli-router-endpoint on oltava `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = CA-juurien PEM puuttuu tai ei ole luettavissa
doctor-step2-ca-missing-hint = { $err } — suorita `goat profile init <bundle.cbor>` uudelleen
doctor-step2-ok-summary = reititin { $host }:{ $port } hyväksyy TCP:n (TLS vahvistettu vaiheessa 3)
doctor-step2-refused-summary = reitittimen TCP hylätty
doctor-step2-refused-hint = { $err } — reititin voi olla alhaalla; tarkista Zenoh-käyttöönotto mgmt-hostissa
doctor-step2-timeout-summary = reitittimen TCP:n aikakatkaisu
doctor-step2-timeout-hint = reititin { $host }:{ $port } ei tavoitettavissa { $ms }ms:ssa
doctor-step3-config-failed-summary = zenoh-määrityksen rakentaminen epäonnistui
doctor-step3-config-failed-hint = virheellinen päätepiste `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = zenoh-TLS-määrityksen rakentaminen epäonnistui
doctor-step3-tls-config-failed-hint = { $err } — todennäköisesti zenoh-versiopoikkeama; tarkista että riippuvuudet täsmäävät
doctor-step3-ok-summary = mTLS-istunto avattu — varmenne hyväksytty reitittimen ACL:ssä
doctor-step3-open-failed-summary = zenoh-istunnon avaaminen epäonnistui
doctor-step3-open-failed-hint = { $err } — varmenne voi olla vanhentunut (suorita `goat profile show`) tai reitittimen ACL hylkäsi sen (URI-SAN-ristiriita)
doctor-step3-timeout-summary = zenoh-istunnon avaamisen aikakatkaisu
doctor-step3-timeout-hint = reititin ei suorittanut kättelyä { $secs }s:ssa — kellopoikkeama, väärä varmenne tai ruuhka
doctor-step4-ok-summary = portaali palautti { $n } organisaatioriviä
doctor-step4-token-rejected-summary = portaali hylkäsi Bearer-tunnuksen
doctor-step4-token-rejected-hint = HTTP { $status } — tunnus vanhentunut tai peruutettu; liity uudelleen operator-onboard.yml:n kautta
doctor-step4-request-failed-summary = portaalipyyntö epäonnistui
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = portaalipyynnön aikakatkaisu
doctor-step4-timeout-hint = ei vastausta { $secs }s:ssa — tarkista että { $endpoint } on tavoitettavissa meshistä
doctor-skipped-summary = ohitettu — edellinen vaihe epäonnistui (suorita uudelleen --all pakottaaksesi)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Yhtenäinen operaattorin komentorivi goat-datakudokselle.
help-long-about = goat — operaattorille suunnattu CLI, joka kokoaa panoscope-coren yhdeksi työkaluksi: profiilin init/uudelleenkäyttö, ZTOL-tietoinen tavoitettavuuden doctor, organisaatioportaalin selaus, topic CRUD, pub/sub, stethoscope-delegointi, interaktiivinen komentojen kokoaja ja substraatin kaskadin kerronta.
help-flag-profile-dir = Ohita profiilihakemisto (oletus: $GOAT_PROFILE_DIR tai ~/.goat).
help-flag-context = Ohita aktiivinen konteksti (oletus: $GOAT_CONTEXT-ympäristömuuttuja, sitten current_context tiedostossa config.toml, sitten "default").
help-flag-narrate = Näytä substraatin kaskadi, jonka muuttava komento käynnistää.
help-flag-verbose = Ota käyttöön monisanainen virheenkorjaustuloste (tai aseta RUST_LOG=goat=debug).
help-flag-color = Väritila terminaalitulosteelle (auto|always|never).
help-flag-lang = Ihmiselle suunnatun tekstin kieli (esim. en, uk). Varavaihtoehtoina GOAT_LANG, sitten käyttöjärjestelmän kielialue, sitten englanti.
help-cmd-profile = Hallitse paikallista goat-profiilia (mTLS-varmenne + portaalitunnus + päätepistejoukko).
help-cmd-context = Hallitse konteksteja (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Suorita vaiheittainen tavoitettavuuden prober kudosta vasten.
help-cmd-doctor-long = Vaiheittainen tavoitettavuuden prober kudosta vasten: ZTOL-jäsenyys → Zenoh-reititin → varmenteen hyväksyntä → portaali. Jokainen epäonnistunut vaihe antaa erityisen korjausvihjeen.
help-cmd-vendor = Selaa portaaliin rekisteröityjä organisaatioita.
help-cmd-topic = Listaa, näytä, luo tai poista topic-aiheita.
help-cmd-pub = Julkaise hyötykuorma topic-avaimeen.
help-cmd-sub = Tilaa topic-avainlauseke.
help-cmd-inspect = Delegoi diagnostinen verdikti-pyyntö stethoscopelle.
help-cmd-ui = Interaktiivinen yhdistetty valitsin: selaa ilmoitettua ⊕ live-luetteloa ja hyppää valitun kohteen Diagnose / Tail -linssiin.
help-cmd-compose = Interaktiivinen komentojen rakentaja-TUI.
help-cmd-narrate = Substraatin kaskadin heijastus viimeaikaisen toiminnan yli. Ei vielä saatavilla (jokapäiväinen tapa on --narrate muuttavassa komennossa).
help-cmd-completion = Tulosta kuoren täydennysskripti stdoutiin.
help-cmd-explain = Etsi virhekoodin merkitys + korjaus upotetusta `goat-cli-error-codes.md`-dokumentista (cargo --explain -malli).
help-cmd-mock = Julkaise synteettisiä näytteitä topic-aiheeseen. Oletus: triviaali hello-world JSON taajuudella 1Hz. Anna `--format <.fds>` + `--message <Name>` koodataksesi operaattorin toimittaman protobufin sen sijaan.
help-cmd-status = Terveyden synteesi: ZTOL-tavoitettavuus + portaalin /api/health + paikallisen varmenteen vanheneminen, kolme glyph-riviä tai yksi --json-objekti.
help-cmd-services = Tutki jokaista isännöityä palvelua tiedostossa `ops/serve/services.yaml` ja renderöi yksi glyph-rivi palvelua kohti (tai yksi --json-objekti).
help-cmd-client = Käytä paikallista goat-clientd-daemonia sen JSON-RPC IPC -soketin kautta (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = PATH-pohjainen laajennusten löytäminen (kubectl-tyylinen). Mikä tahansa PATHissa oleva `goat-<name>`-suoritettava on kutsuttavissa muodossa `goat <name>`; `goat plugin list` luettelee asennetut liitännäiset, `goat plugin which <name>` ratkaisee polun.

# ---------------------------------------------------------------------
# goat status / inspect / --narrate -ajonaikainen teksti (machine-translated; ADR 1024).
# Contract tokens `{ $var }`, takakenot, → —, ja kiinteät literaalit
# (/api/health, 2xx, PEM, CERTIFICATE, unix=, TCP, x509, Zenoh,
# stethoscope, (portal-confirmed)) toistettu sanatarkasti.
# ---------------------------------------------------------------------
status-ztol-malformed = reitittimen päätepiste on virheellisesti muotoiltu: `{ $endpoint }`
status-ztol-refused = TCP hylätty reitittimelle { $host }:{ $port }: { $err }
status-ztol-unreachable = mesh-verkon ulkopuolella (reititin { $host }:{ $port } ei tavoitettavissa { $ms }ms:ssa)
status-portal-ok = portaalin /api/health 2xx
status-portal-failed = portaalin /api/health epäonnistui: { $err }
status-portal-timeout = portaalin /api/health aikakatkaisu { $secs }s:ssa
status-cert-read-failed = varmenteen luku epäonnistui: { $err }
status-cert-pem-parse-failed = varmenteen PEM-jäsennys epäonnistui: { $err }
status-cert-wrong-tag = ensimmäinen PEM-lohko on { $tag }, vaaditaan CERTIFICATE
status-cert-x509-parse-failed = x509-jäsennys epäonnistui: { $err }
status-cert-expired = varmenne VANHENTUNUT hetkellä unix={ $not_after }, nyt={ $now }
status-cert-valid = varmenne voimassa; vanhenee { $days_left }d kuluttua (unix={ $not_after })
inspect-finding-label = löydös ({ $severity }): { $label } — { $detail }
inspect-remedy-label = korjaus: { $title } — { $detail }
inspect-no-outcomes = (ei tuloksia — kohde ei vastannut yhtäkään tarkistusta)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = Zenoh put hyväksytty avaimeen `{ $key }` ({ $bytes } tavua)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = kaskadi asettui — { $summary } ({ $n } { $n ->
        [one] seuraus
       *[other] seurausta
    })

# goat onboarding/docs help one-liners + the all-green flourish (ADR 1024).
help-cmd-init = Ensikäynnistyksen ohjattu toiminto: valitse sandbox tai oikea, todista että toimii, ohjaa opastajalle.
help-cmd-tutor = Omatahtinen, käytännönläheinen kierros kudoksessa (vimtutor-tyylinen; jatkettavissa).
help-cmd-docs = Selaa goat-dokumentteja tai tulosta täydellinen komentohakemisto (--markdown).
help-cmd-hello = Lähetä hello kudoksen läpi päästä päähän (sandbox tai oikea ympäristö).
flourish-all-green = 🐐 kaikki vihreää — kudos kunnossa

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = vain operaattoritoiminto — sinulla ei ole valtuuksia (`goat admin` on toimimaton ei-admineille)
help-flag-narrate-json = Tulosta substraattikaskadi NDJSON-muodossa (yksi objekti per seuraus) häiriölokeja ja skriptausta varten. Edellyttää --narrate.
help-cmd-admin = Vain operaattoreille: organisaation elinkaari, käyttöönotto/peruutus/rotaatio, mesh-peer-katkaisu, laite- ja operaattoritason provisiointi, LAN-triage. Valtuutetaan operaattoritunnuksillasi (palvelinpuolella portaalin/NetBird-tokenin laajuuden kautta); toimimaton ei-admineille — luvaton kutsu ei muuta mitään ja epäonnistuu koodilla `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = kontekstin `{ $context }` profiili on olemassa, mutta tämä käyttäjä ei voi lukea sitä (käyttö estetty)
error-portal-bad-response-summary = portaali vastasi, mutta tämä goat-versio ei pystynyt jäsentämään vastausta
doctor-step0-ok-summary = paikallinen asennus on valmis verkkotesteihin
doctor-step0-warn-summary = paikallisessa asennuksessa on tarkistuksia, joita ei voitu vahvistaa
doctor-step0-fail-summary = paikallinen asennus vaatii toimenpiteitä
doctor-step0-profile-absent-summary = kontekstille `{ $context }` ei ole goat-profiilia
doctor-step0-profile-new-hint = tarkista `goat context ls`; jos tämä on ensimmäinen asennuksesi, suorita `goat ca add <bundle-ca.pem>` ja vertaa sen sormenjälkeä operaattorisi antamaan, sitten `goat profile init <bundle.cbor>`; suorita `goat doctor` uudelleen
doctor-step0-profile-absent-hint = tarkista `goat context ls`; valitse olemassa oleva konteksti komennolla `goat context use <name>` tai suorita `goat profile init <bundle.cbor>` tälle kontekstille; suorita `goat doctor` uudelleen
doctor-step0-profile-denied-summary = goat-profiilia polussa { $path } ei voi lukea
doctor-step0-profile-denied-hint = korjaa profiilin omistajuus tai aja omistajakäyttäjänä; älä alusta olemassa olevaa profiilia uudelleen
doctor-step0-profile-incomplete-summary = goat-profiilin tiedosto { $file } puuttuu tai sitä ei voi lukea
doctor-step0-profile-incomplete-hint = palauta mainittu tiedosto myönnetystä bundlesta tai korvaa tämä konteksti komennolla `goat profile rotate <bundle.cbor>`; suorita `goat doctor` uudelleen
doctor-step0-profile-invalid-summary = goat-profiilin päätepisteet ovat lukukelvottomia tai puuttuvat
doctor-step0-profile-empty-token-summary = goat-profiilin portaalitunniste on tyhjä
doctor-step0-profile-ok-summary = goat-kontekstin `{ $context }` profiiliaineisto on luettavissa
doctor-step0-client-absent-summary = yksikään tuettu mesh-asiakas ei vastannut
doctor-step0-client-absent-hint = asenna goatnetisi toimittama mesh-asiakas, käynnistä sen daemon ja suorita `goat doctor` uudelleen
doctor-step0-client-stopped-summary = asennettu mesh-asiakas ei palauttanut käyttökelpoista tilaa
doctor-step0-client-stopped-hint = tarkista `netbird status -d` tai `goat client status --detail`; käynnistä asennettu daemon (`sudo netbird service start` NetBirdille) ja suorita `goat doctor` uudelleen
doctor-step0-netbird-ok-summary = NetBird-daemon vastasi (ilmoitettu profiili `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd vastasi (tila `{ $state }`)
doctor-step0-management-connected-summary = NetBird ilmoittaa hallinnan yhdistetyksi
doctor-step0-management-disconnected-summary = NetBird ilmoittaa hallinnan yhteyden katkenneeksi
doctor-step0-management-disconnected-hint = tarkista ilmoitettu hallinnan URL ja `netbird status -d`; muodostettu tunneli voi toimia hallinnan katkoksen aikana
doctor-step0-management-unknown-summary = määritettyä hallinnan päätepistettä ei voitu vahvistaa paikallisesti
doctor-step0-management-unknown-hint = tarkista `netbird status -d` tai `goat client diagnostics`; doctor ei arvaa management-URL-osoitetta
doctor-step0-tunnel-down-summary = NetBird ei ilmoita mesh-osoitetta
doctor-step0-tunnel-down-hint = suorita `netbird status -d`; jos laite on rekisteröity ja daemon vastaa, suorita `goat heal` ja sen jälkeen `goat doctor` uudelleen
doctor-step0-tunnel-address-summary = NetBird ilmoittaa mesh-osoitteen { $address }; reitittimen tavoitettavuus tarkistetaan seuraavaksi
doctor-step0-goat-client-no-bundle-summary = goat-clientd:llä ei ole tuotua bundlea
doctor-step0-goat-client-no-bundle-hint = suorita `goat client bundle import <bundle.cbor>` asiakkaan enrollment-bundlella, yhdistä asiakas ja suorita `goat doctor` uudelleen
doctor-step0-goat-client-connected-summary = goat-clientd ilmoittaa tunnelin olevan yhdistetty; reitittimen tavoitettavuus tarkistetaan seuraavaksi
doctor-step0-goat-client-outer-disabled-summary = goat-clientd:n ulompi tunneli on pois käytöstä netbird-only-tilassa
doctor-step0-goat-client-down-summary = goat-clientd ilmoittaa tunnelin tilaksi `{ $state }`
doctor-step0-goat-client-down-hint = suorita `goat client diagnostics`, sitten `goat client up`, jos bundle on ladattu; suorita `goat doctor` uudelleen
doctor-step0-goat-client-inner-ok-summary = goat-clientd ilmoittaa sisemmän meshin olevan yhdistetty
doctor-step0-goat-client-inner-disabled-summary = goat-clientd:n sisempi mesh on pois käytöstä wg-cp0-only-tilassa
doctor-step0-goat-client-inner-down-summary = goat-clientd ilmoittaa sisemmän meshin tilaksi `{ $state }`
doctor-step0-goat-client-inner-down-hint = tarkista sisempi mesh komennolla `goat client diagnostics` ja suorita `goat doctor` uudelleen, kun mesh on palautettu
doctor-step0-management-reachable-summary = management-päätepiste vastasi: HTTP { $status }
doctor-step0-management-auth-summary = management HTTPS -päätepiste on tavoitettavissa (HTTP { $status } vaatii valtuutuksen tässä tarkistuksessa)
doctor-step0-management-unexpected-summary = management-päätepiste palautti odottamattoman vastauksen: HTTP { $status }
doctor-step0-management-unexpected-hint = tarkista määritetty management-URL ja pyydä goatnet-operaattoria tarkistamaan tämä vastaus
doctor-step0-management-unreachable-summary = määritettyä management-päätepistettä ei tavoitettu: { $error }
doctor-step0-management-unreachable-hint = tarkista DNS ja verkkoyhteys asiakkaan ilmoittamaan URL-osoitteeseen; olemassa oleva tunneli voi silti toimia
doctor-step0-clock-ok-summary = paikallinen kello on { $seconds } sekunnin sisällä management-HTTP-ajasta
doctor-step0-clock-skew-summary = paikallinen kello poikkeaa management-HTTP-ajasta { $seconds } sekuntia
doctor-step0-clock-skew-hint = synkronoi laitteen kello ja suorita `goat doctor` uudelleen; varmenteen validointi voi epäonnistua, kun kello on väärässä
doctor-step0-clock-unknown-summary = kellon poikkeamaa ei voitu mitata management-HTTP:n Date-otsakkeesta
doctor-step0-clock-unknown-hint = tarkista laitteen ajan synkronointi, jos TLS- tai varmennetarkistukset epäonnistuvat
error-clock-skew-summary = laitteen kello poikkeaa liikaa hallinnan ajasta
error-doctor-inconclusive-summary = doctor havaitsi virheen, jonka syytä ei todennettu
error-portal-forbidden-summary = portaali esti tämän tilin pääsyn tutkittuun API:in
doctor-step4-forbidden-summary = portaali palautti HTTP 403 tälle profiilille
doctor-step4-forbidden-hint = tämä tunniste tavoitti portaalin, mutta sillä ei ole pääsyä tutkittuun API:in; tarkista `goat profile show` ja pyydä goatnet-operaattoria varmistamaan oikeudet
doctor-step4-decode-summary = portaali vastasi vastauksella, jota tämä goat ei osaa lukea
doctor-step4-decode-hint = { $err } — päivitä goat tämän goatnetin tarjoamaan versioon ja aja `goat doctor` uudelleen
doctor-step1-control-only-summary = yhteys vain control-palvelimeen — ei tunnelia yhteenkään { $total } vertaiseen
doctor-step1-control-only-hint = tämä verkko päästää TCP 443 ulos mutta estää mesh-dataportit, joten mikään meshissä (kirjautumissivut mukaan lukien) ei ole tavoitettavissa tältä laitteelta. Pyydä verkkotiimiä sallimaan lähtevä TCP ja UDP kohteisiin { $relays } sekä UDP 3478 kohteeseen { $stun }; suora UDP laitteiden välillä on myös suositeltava. Varmista liittymällä puhelimen hotspotiin ja aja `netbird status`: vertaisten pitäisi nyt yhdistyä.
doctor-offmesh-dashboard-hint = relay-laivasto (ulkoinen näkökulma): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = paikallinen varmenne, vastaava avain ja CA-paketti jäsentyvät; varmenne on voimassaoloaikansa sisällä
doctor-step2-material-failed-summary = paikallista TLS-materiaalia { $file } ei voitu käyttää
doctor-step2-material-failed-hint = { $err } — tarkista mainittu tiedosto; palauta se myönnetystä paketista tai kierrätä profiili
doctor-step2-material-invalid-hint = { $file } ei ole kelvollinen X.509 PEM -varmenne; palauta myönnetty materiaali tai aja `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem on virheellinen tai ei vastaa tiedostoa mtls.cert.pem; palauta myönnetty materiaali tai aja `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = paikallinen varmenne on voimassaoloaikansa ulkopuolella
doctor-step2-cert-time-hint = aja `goat profile show`; tarkista kellotulos yllä, pyydä sitten operaattorilta uusi paketti ja aja `goat profile rotate <new-bundle.cbor>`, jos varmenne on vanhentunut
doctor-skipped-no-endpoint-summary = ohitettu — profile.toml:stä puuttuu { $field }
doctor-skipped-no-profile-summary = ohitettu — tälle kontekstille ei ole luettavia profiilin päätepisteitä
doctor-skipped-no-portal-token-summary = ohitettu — tälle kontekstille ei ole luettavaa portaalitunnusta
status-ztol-tcp-ok = reitittimen portti { $host }:{ $port } hyväksyy TCP:n (vain portti — katso sessio)
status-session-ok = zenoh mTLS -sessio avattu ajassa { $ms }ms (reititin hyväksyi tämän varmenteen)
status-session-skipped = sessiota ei yritetty (mesh-verkon ulkopuolella; katso ztol)
status-session-material-missing = zenoh-sessiota ei yritetty: { $detail }
status-session-config-failed = zenoh-session konfigurointi epäonnistui: { $err }
status-session-failed = zenoh-istunto ei avautunut: { $err } — suorita `goat doctor`
status-session-timeout = zenoh-istunto ei avautunut { $secs } s:ssa — reitittimen portti hyväksyy TCP:n, mutta istunto ei valmistu (reititin jumissa, varmenne hylätty tai kellon ero); suorita `goat doctor`
status-names-live = nimet livenä (mesh-DNS vastasi)
status-names-ip-only = nimiä ei kokeiltu (profiilin päätepisteet ovat IP-literaaleja)
status-names-snapshot = nimet allekirjoitetun tilannekuvan mukaan, serial { $serial }, ikä { $age } (mesh-DNS ei tavoitettavissa)
status-names-snapshot-aging = nimet allekirjoitetun tilannekuvan mukaan, serial { $serial }, ikä { $age } — VANHENTUNUT (mesh-DNS ei tavoitettavissa; korjaa goat-coredns, jonka jälkeen nimet päivittyvät automaattisesti)
status-names-snapshot-daemon = nimet allekirjoitetun tilannekuvan mukaan, serial { $serial }, ikä { $age } — daemonin tallenne (mesh-DNS ei tavoitettavissa)
status-names-snapshot-daemon-aging = nimet allekirjoitetun tilannekuvan mukaan, serial { $serial }, ikä { $age } — daemonin tallenne, VANHENTUNUT (mesh-DNS ei tavoitettavissa; korjaa goat-coredns, jonka jälkeen nimet päivittyvät automaattisesti)
status-names-failed = mesh-DNS ei tavoitettavissa eikä tilannekuvavaravaihtoehto käytettävissä — { $reason }
status-pqc-on = wireguard PQC (rosenpass) PÄÄLLÄ — { $qr }/{ $connected } yhdistettyä vertaista kvanttiturvallista
status-pqc-off = wireguard PQC (rosenpass) POIS — klassiset wireguard-kättelyt
status-pqc-unknown = wireguard PQC tuntematon — { $reason }
status-device-none = laitekohtainen nimisuoja POIS — goat-clientd-nimitallennetta ei ole (goat-komennoilla on silti varamekanismi; ota käyttöön kohdan name-fallback-resolver.md §9.2 mukaan)
status-device-broken = goat-clientd-nimitallenne löytyy (serial { $serial }), mutta sen välittäjä EI vastaa osoitteessa { $addr } — daemon alhaalla tai nimialijärjestelmä jumissa
status-device-active = laitekohtainen nimisuoja AKTIIVINEN — goat-clientd on käyttöjärjestelmän mesh-DNS:n edessä osoitteessa { $addr } (tallenteen serial { $serial }, { $grade })
status-device-not-fronted = goat-clientd-välittäjä käynnissä osoitteessa { $addr } (tallenteen serial { $serial }, { $grade }) — käyttöjärjestelmä ei sen takana; selaimet käyttävät suoraan mesh-DNS:ää
status-device-fronting-unknown = goat-clientd-välittäjä käynnissä osoitteessa { $addr } (tallenteen serial { $serial }, { $grade }) — käyttöjärjestelmän ohjausta ei voi havaita tällä alustalla
status-names-observed = nimet EI-KANONISIA (ad hoc): havaittu tietue, ikä { $age } — ei allekirjoitetussa tilannekuvassa (mesh-DNS ei tavoitettavissa; kodifioi tämä tietue)
status-names-selfclaim = nimet vahvistetun ITSEVÄITTÄMÄN kautta, ikä { $age } — nimen oman ankkurin allekirjoittama (mesh-DNS ei tavoitettavissa)
doctor-names-note = tilannekuvan serial { $serial } kautta, ikä { $age }
doctor-names-selfclaim-note = vahvistetun ITSEVÄITTÄMÄN kautta, ikä { $age } — nimen oman ankkurin allekirjoittama
doctor-names-selfclaim-hint = mesh-DNS ei ole tavoitettavissa — tämä vaihe ratkesi vahvistetun ITSEVÄITTÄMÄN kautta (ikä { $age }): nimen oma ankkuri allekirjoitti tämän sidoksen, ja välimuistissa olevan tilannekuvan peer_bindings valtuuttaa sen. Allekirjoitettu mutta ei live; korjaa mesh-DNS, niin tämä poistuu.
doctor-names-observed-note = HAVAITUN (ad hoc) tietueen kautta, ikä { $age } — ei-kanoninen
doctor-names-observed-hint = mesh-DNS ei ole tavoitettavissa — tämä vaihe ratkesi EI-KANONISEN havaitun tietueen kautta (ikä { $age }): nimi on vain livenä, sitä ei ole koskaan kodifioitu sivuston rekisteriin. Kodifioi se (group_vars-rekisteri → generaattori → uudelleenallekirjoitus), tai se vanhenee pois; korjaa mesh-DNS, niin tämä poistuu.
doctor-names-hint = mesh-DNS ei ole tavoitettavissa — tämä vaihe ratkesi allekirjoitetun nimitilannekuvan kautta (serial { $serial }, ikä { $age }). Verkko pysyy tavoitettavana varanimillä; korjaa mesh-DNS (goat-coredns hallintaisännällä), niin tämä poistuu.
storm-brake-plan-header = Myrskyjarrun suunnitelma (ADR 1081 kerros 4 — manuaalinen):
storm-brake-plan-target = hallintaisäntä { $user }@{ $host }  (konteksti { $context })
storm-brake-step-flush = käynnistä signalointikontti `{ $container }` uudelleen (pudottaa zombie-virrat)
storm-brake-step-tighten = kuristetaan L2-rajoitin kohteessa `{ $container }` (puolita rate-limit, nosta neg-cache TTL) — { $window } min, operaattori palauttaa; ohitetaan, jos L2 ei ole käytössä
storm-brake-step-shed = keskeytä ei-välttämätön julkaisijayksikkö `{ $unit }`
storm-brake-shed-none = (ei karsittavia yksiköitä — aseta storm_brake_shed_units tiedostossa profile.toml tai anna --shed-unit)
storm-brake-announce-note = kirjoita yksi OperatorActionV1 auditointilokiin askelkohtaisine tuloksineen
storm-brake-dryrun-note = kuivajoo — mitään ei suoritettu.
storm-brake-triggers-header = Laukaise, kun relay-health host-net -kaista näyttää:
storm-brake-trigger-futility = signaloinnin turhuussuhde >= 0.5 vähintään 100 viestiä/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = uplink-häviö >= 5 % yhtäjaksoisesti 10 min (wan.loss_pct)
storm-brake-triggers-band-url = isäntäkaista: relay-health.{ $site }.<zone> (ks. docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Huom: pelkkä signaalien tyhjennys EI palauta meshiä. Kerrokset 1–2 (asiakkaan backoff + signaloinnin karsinta) ovat parannus; jarru ostaa palautumisaikaa.
storm-brake-confirm = Laukaistaanko myrskyjarru kohteessa { $user }@{ $host }? Käynnistää signalointikontin uudelleen ja keskeyttää määritellyt julkaisijat. [y/N]
storm-brake-aborted = myrskyjarru keskeytetty (ei vahvistusta)
storm-brake-executing = laukaistaan myrskyjarru kohteessa { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = epäonnistui
storm-brake-outcome-skipped = ohitettu
storm-brake-summary = myrskyjarru valmis: { $ok } ok, { $failed } epäonnistui, { $skipped } ohitettu
storm-brake-flush-failed = FLUSH epäonnistui — signalointikontti ei käynnistynyt uudelleen, joten meshin palautumista ei odoteta: { $reason }
storm-brake-mgmt-unresolved = hallintaisäntää ei selvitetty — anna --mgmt-host, aseta mgmt_ssh_host kontekstin tiedostossa profile.toml tai vie GOAT_MGMT_SSH_HOST
storm-brake-audit-note = operaattoritoiminto kirjattu auditointilokiin (toimija { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portaali vastasi HTTP 404 kohteelle `{ $path }`: ei rekisteröity sinne
error-portal-rate-limited-summary = portaali vastasi HTTP 429 kohteelle `{ $path }`: liikaa pyyntöjä, yritä myöhemmin uudelleen
error-portal-server-error-summary = portaali vastasi HTTP { $status } kohteelle `{ $path }`: vika portaalin päässä
error-portal-refused-summary = portaali vastasi HTTP { $status } kohteelle `{ $path }`: pyyntö hylättiin

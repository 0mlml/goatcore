### goat-cli — Portuguese (pt). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = nenhum perfil para o contexto `{ $context }`
error-off-mesh-summary = fora da ZTOL — sem ligação à mesh
error-router-unreachable-summary = o router Zenoh em `{ $endpoint }` está inacessível
error-cert-expired-summary = certificado mTLS expirado ou rejeitado pelo router
error-portal-unreachable-summary = a API do portal em `{ $endpoint }` está inacessível
error-token-invalid-summary = o portal rejeitou o token Bearer
error-topic-conflict-summary = o topic `{ $topic }` já existe
error-bundle-invalid-summary = falha na verificação da assinatura ou validade do bundle

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = endpoint do router malformado
doctor-step1-endpoint-malformed-hint = esperado `<proto>/<host>:<port>`; recebido `{ $endpoint }`
doctor-step1-ok-summary = ligação TCP ao host do router { $host }:{ $port } bem-sucedida
doctor-step1-conn-failed-summary = falha na ligação TCP ao host do router
doctor-step1-conn-failed-hint = { $err } — inicie o netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); verifique `netbird status`
doctor-step1-timeout-summary = tempo esgotado na ligação TCP ao host do router
doctor-step1-timeout-hint = host do router { $host }:{ $port } inacessível em { $ms }ms — provavelmente fora da malha
doctor-step2-endpoint-malformed-hint = corrija o emissor do bundle (--goat-cli-router-endpoint tem de ser `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM das raízes CA em falta ou ilegível
doctor-step2-ca-missing-hint = { $err } — execute novamente `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = router { $host }:{ $port } a aceitar TCP (TLS validado no passo 3)
doctor-step2-refused-summary = TCP do router recusado
doctor-step2-refused-hint = { $err } — o router pode estar em baixo; verifique a implantação do Zenoh no mgmt-host
doctor-step2-timeout-summary = tempo esgotado no TCP do router
doctor-step2-timeout-hint = router { $host }:{ $port } inacessível em { $ms }ms
doctor-step3-config-failed-summary = falha ao construir a configuração zenoh
doctor-step3-config-failed-hint = endpoint inválido `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = falha ao construir a configuração TLS zenoh
doctor-step3-tls-config-failed-hint = { $err } — provável desvio de versão do zenoh; verifique a correspondência das dependências
doctor-step3-ok-summary = sessão mTLS aberta — certificado aceite pela ACL do router
doctor-step3-open-failed-summary = falha ao abrir a sessão zenoh
doctor-step3-open-failed-hint = { $err } — o certificado pode estar expirado (execute `goat profile show`) ou rejeitado pela ACL do router (incompatibilidade URI-SAN)
doctor-step3-timeout-summary = tempo esgotado ao abrir a sessão zenoh
doctor-step3-timeout-hint = o router não concluiu o handshake em { $secs }s — desvio de relógio, certificado errado ou congestão
doctor-step4-ok-summary = o portal devolveu { $n } linha(s) de organização
doctor-step4-token-rejected-summary = o portal rejeitou o token Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — token expirado ou revogado; refaça o onboarding via operator-onboard.yml
doctor-step4-request-failed-summary = falha no pedido ao portal
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = tempo esgotado no pedido ao portal
doctor-step4-timeout-hint = sem resposta em { $secs }s — verifique se { $endpoint } está acessível a partir da malha
doctor-skipped-summary = ignorado — o passo anterior falhou (execute novamente com --all para forçar)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Linha de comandos de operador unificada para a fabric de dados goat.
help-long-about = goat — CLI orientada ao operador que compõe panoscope-core numa única ferramenta: inicialização/reutilização de perfil, doctor de acessibilidade ciente de ZTOL, navegação no portal de organizações, CRUD de topic, pub/sub, delegação ao stethoscope, um compositor de comandos interativo e narração da cascata do substrato.
help-flag-profile-dir = Substituir o diretório de perfil (predefinição: $GOAT_PROFILE_DIR ou ~/.goat).
help-flag-context = Substituir o contexto ativo (predefinição: variável $GOAT_CONTEXT, depois current_context em config.toml, depois "default").
help-flag-narrate = Mostrar a cascata do substrato que um comando mutador desencadeia.
help-flag-verbose = Ativar saída de depuração detalhada (ou definir RUST_LOG=goat=debug).
help-flag-color = Modo de cor para a saída do terminal (auto|always|never).
help-flag-lang = Idioma do texto destinado a pessoas (p. ex. en, uk). Recorre a GOAT_LANG, depois à configuração regional do SO, depois ao inglês.
help-cmd-profile = Gerir o perfil goat local (certificado mTLS + token de portal + conjunto de endpoints).
help-cmd-context = Gerir contextos (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Executar um sondador de acessibilidade por etapas contra a fabric.
help-cmd-doctor-long = Sondador de acessibilidade por etapas contra a fabric: pertença a ZTOL → router Zenoh → aceitação do certificado → portal. Cada etapa falhada emite uma sugestão de correção específica.
help-cmd-vendor = Navegar pelas organizações registadas no portal.
help-cmd-topic = Listar, mostrar, criar ou eliminar topics.
help-cmd-pub = Publicar um payload numa chave de topic.
help-cmd-sub = Subscrever uma expressão de chave de topic.
help-cmd-inspect = Delegar um pedido de veredicto de diagnóstico ao stethoscope.
help-cmd-ui = Seletor combinado interativo: navegar pelo registo declared ⊕ live e saltar para a lente Diagnose / Tail no alvo selecionado.
help-cmd-compose = TUI interativa de construção de comandos.
help-cmd-narrate = Reflexão sobre a cascata do substrato a partir da atividade recente. Ainda não disponível (a via habitual é --narrate num comando mutador).
help-cmd-completion = Imprimir o script de autocompletar da shell em stdout.
help-cmd-explain = Procurar o significado de um código de erro + a sua correção no documento `goat-cli-error-codes.md` incorporado (padrão cargo --explain).
help-cmd-mock = Publicar amostras sintéticas num topic. Predefinição: um JSON hello-world trivial a 1Hz. Passe `--format <.fds>` + `--message <Name>` para codificar em vez disso protobuf fornecido pelo operador.
help-cmd-status = Síntese do estado de saúde: acessibilidade ZTOL + portal /api/health + validade do certificado local, três linhas de glyph ou um objeto --json.
help-cmd-services = Sondar cada serviço alojado em `ops/serve/services.yaml` e apresentar uma linha de glyph por serviço (ou um objeto --json).
help-cmd-client = Encapsular o daemon goat-clientd local através do seu socket IPC JSON-RPC (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = Descoberta de extensões baseada no PATH (estilo kubectl). Qualquer executável `goat-<name>` no PATH é invocável como `goat <name>`; `goat plugin list` enumera os plugins instalados, `goat plugin which <name>` resolve o caminho.

# goat status / inspect / --narrate prosa de runtime (ADR 1024).
status-ztol-malformed = endpoint do router malformado: `{ $endpoint }`
status-ztol-refused = TCP recusado para o router { $host }:{ $port }: { $err }
status-ztol-unreachable = fora da mesh (router { $host }:{ $port } inacessível em { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = portal /api/health falhou: { $err }
status-portal-timeout = tempo esgotado no portal /api/health em { $secs }s
status-cert-read-failed = falha na leitura do certificado: { $err }
status-cert-pem-parse-failed = falha na análise PEM do certificado: { $err }
status-cert-wrong-tag = o primeiro bloco PEM é { $tag }, esperado CERTIFICATE
status-cert-x509-parse-failed = falha na análise x509: { $err }
status-cert-expired = certificado EXPIRADO em unix={ $not_after }, agora={ $now }
status-cert-valid = certificado válido; expira em { $days_left }d (unix={ $not_after })
inspect-finding-label = constatação ({ $severity }): { $label } — { $detail }
inspect-remedy-label = correção: { $title } — { $detail }
inspect-no-outcomes = (sem resultados — o alvo não correspondeu a nenhuma verificação)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = put do Zenoh aceite em `{ $key }` ({ $bytes } bytes)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = cascata estabilizada — { $summary } ({ $n } { $n ->
        [one] consequência
       *[other] consequências
    })

# Ajuda de uma linha para o onboarding/a documentação do goat + o arremate tudo-verde (ADR 1024).
help-cmd-init = Assistente de primeiro arranque: escolha sandbox ou real, confirme que funciona, aponte para o tutor.
help-cmd-tutor = Um percurso prático e ao seu próprio ritmo pela fabric (estilo vimtutor; retomável).
help-cmd-docs = Navegar pela documentação do goat, ou emitir a referência completa de comandos (--markdown).
help-cmd-hello = Fazer um hello percorrer a fabric de ponta a ponta, ida e volta (ambiente sandbox ou real).
flourish-all-green = 🐐 tudo verde — fabric nominal

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = ação restrita a operadores — você não está autorizado (`goat admin` não tem efeito para não-administradores)
help-flag-narrate-json = Emite a cascata de substrato como NDJSON (um objeto por consequência) para logs de incidentes + scripts. Implica --narrate.
help-cmd-admin = Administração restrita a operadores: ciclo de vida de organizações, onboard/revoke/rotate, corte de mesh-peer, provisionamento de dispositivos + nível de operador, triagem de LAN. Autorizado pelas suas credenciais de operador (lado servidor via escopo de token portal/NetBird); sem efeito para não-administradores — uma chamada não autorizada não altera nada e falha com `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = o perfil do contexto `{ $context }` existe, mas não pode ser lido por este usuário (permissão negada)
error-portal-bad-response-summary = o portal respondeu, mas esta build do goat não conseguiu interpretar a resposta
doctor-step0-ok-summary = a configuração local está pronta para testes de rede
doctor-step0-warn-summary = a configuração local tem verificações que não puderam ser confirmadas
doctor-step0-fail-summary = a configuração local precisa de atenção
doctor-step0-profile-absent-summary = não há perfil goat para o contexto `{ $context }`
doctor-step0-profile-new-hint = verifique `goat context ls`; se esta é sua primeira configuração, execute `goat ca add <bundle-ca.pem>` e compare a impressão digital com a do seu operador, depois `goat profile init <bundle.cbor>`; execute `goat doctor` novamente
doctor-step0-profile-absent-hint = verifique `goat context ls`; selecione um contexto existente com `goat context use <name>`, ou execute `goat profile init <bundle.cbor>` para este contexto; execute `goat doctor` novamente
doctor-step0-profile-denied-summary = o perfil goat em { $path } não pode ser lido
doctor-step0-profile-denied-hint = corrija o proprietário do perfil ou execute como o usuário proprietário; não reinicialize um perfil existente
doctor-step0-profile-incomplete-summary = o arquivo { $file } do perfil goat está ausente ou ilegível
doctor-step0-profile-incomplete-hint = restaure o arquivo indicado a partir do bundle emitido, ou use `goat profile rotate <bundle.cbor>` para substituir este contexto; execute `goat doctor` novamente
doctor-step0-profile-invalid-summary = o perfil goat tem endpoints ilegíveis ou ausentes
doctor-step0-profile-empty-token-summary = o perfil goat tem um token de portal vazio
doctor-step0-profile-ok-summary = o contexto goat `{ $context }` tem material de perfil legível
doctor-step0-client-absent-summary = nenhum cliente mesh compatível respondeu
doctor-step0-client-absent-hint = instale o cliente mesh fornecido pela sua goatnet, inicie o daemon dele e execute `goat doctor` novamente
doctor-step0-client-stopped-summary = o cliente mesh instalado não retornou um status utilizável
doctor-step0-client-stopped-hint = verifique `netbird status -d` ou `goat client status --detail`; inicie o daemon instalado (`sudo netbird service start` para NetBird) e execute `goat doctor` novamente
doctor-step0-netbird-ok-summary = o daemon NetBird respondeu (perfil informado `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd respondeu (estado `{ $state }`)
doctor-step0-management-connected-summary = o NetBird informa management conectado
doctor-step0-management-disconnected-summary = o NetBird informa management desconectado
doctor-step0-management-disconnected-hint = verifique a URL de management informada e `netbird status -d`; um túnel já estabelecido pode continuar funcionando durante uma queda do management
doctor-step0-management-unknown-summary = não foi possível estabelecer localmente o endpoint de management configurado
doctor-step0-management-unknown-hint = inspecione `netbird status -d` ou `goat client diagnostics`; o doctor não adivinha a URL de gerenciamento
doctor-step0-tunnel-down-summary = NetBird não informa endereço de malha
doctor-step0-tunnel-down-hint = execute `netbird status -d`; se estiver registrado e o daemon responder, execute `goat heal` e depois `goat doctor` novamente
doctor-step0-tunnel-address-summary = NetBird informa o endereço de malha { $address }; a alcançabilidade do roteador é verificada a seguir
doctor-step0-goat-client-no-bundle-summary = goat-clientd não tem bundle importado
doctor-step0-goat-client-no-bundle-hint = execute `goat client bundle import <bundle.cbor>` com o bundle de registro do cliente, conecte o cliente e depois execute `goat doctor` novamente
doctor-step0-goat-client-connected-summary = goat-clientd informa túnel conectado; a alcançabilidade do roteador é verificada a seguir
doctor-step0-goat-client-outer-disabled-summary = o túnel externo do goat-clientd está desativado no modo netbird-only
doctor-step0-goat-client-down-summary = goat-clientd informa estado do túnel `{ $state }`
doctor-step0-goat-client-down-hint = execute `goat client diagnostics` e, se o bundle estiver carregado, `goat client up`; execute `goat doctor` novamente
doctor-step0-goat-client-inner-ok-summary = goat-clientd informa malha interna conectada
doctor-step0-goat-client-inner-disabled-summary = a malha interna do goat-clientd está desativada no modo wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd informa estado da malha interna `{ $state }`
doctor-step0-goat-client-inner-down-hint = execute `goat client diagnostics` para inspecionar a malha interna e, depois de restaurá-la, execute `goat doctor` novamente
doctor-step0-management-reachable-summary = o endpoint de gerenciamento respondeu HTTP { $status }
doctor-step0-management-auth-summary = o endpoint HTTPS de gerenciamento está acessível (HTTP { $status } exige autorização neste teste)
doctor-step0-management-unexpected-summary = o endpoint de gerenciamento retornou HTTP { $status } inesperado
doctor-step0-management-unexpected-hint = inspecione a URL de gerenciamento configurada e peça ao operador da goatnet que verifique esta resposta
doctor-step0-management-unreachable-summary = não foi possível alcançar o endpoint de gerenciamento configurado: { $error }
doctor-step0-management-unreachable-hint = verifique o DNS e o acesso de rede à URL informada pelo cliente; um túnel existente ainda pode funcionar
doctor-step0-clock-ok-summary = o relógio local está a até { $seconds } segundos da hora HTTP do gerenciamento
doctor-step0-clock-skew-summary = o relógio local difere da hora HTTP do gerenciamento em { $seconds } segundos
doctor-step0-clock-skew-hint = sincronize o relógio do dispositivo e execute `goat doctor` novamente; a validação de certificado pode falhar enquanto o relógio estiver errado
doctor-step0-clock-unknown-summary = não foi possível medir o desvio do relógio a partir do cabeçalho HTTP Date do gerenciamento
doctor-step0-clock-unknown-hint = verifique a sincronização de hora do dispositivo se as verificações de TLS ou de certificado falharem
error-clock-skew-summary = o relógio do dispositivo está muito distante da hora de gerenciamento
error-doctor-inconclusive-summary = o doctor observou uma falha sem causa comprovada
error-portal-forbidden-summary = o portal negou a esta conta acesso à API testada
doctor-step4-forbidden-summary = o portal retornou HTTP 403 para este perfil
doctor-step4-forbidden-hint = esta credencial chegou ao portal, mas não tem acesso à API testada; verifique `goat profile show` e peça ao operador da goatnet para conferir a permissão
doctor-step4-decode-summary = o portal respondeu com uma resposta que este goat não consegue ler
doctor-step4-decode-hint = { $err } — atualize o goat para a versão servida por esta goatnet e execute `goat doctor` novamente
doctor-step1-control-only-summary = conectado apenas ao servidor de controle — sem túnel para nenhum dos { $total } peers
doctor-step1-control-only-hint = esta rede permite a saída de TCP 443, mas bloqueia as portas de dados da malha, então nada na malha (inclusive páginas de login) é acessível a partir deste dispositivo. Peça à equipe de rede para liberar TCP e UDP de saída para { $relays } e UDP 3478 para { $stun }; UDP direto entre dispositivos também é recomendado. Para confirmar, conecte-se ao hotspot de um celular e execute `netbird status`: os peers devem conectar.
doctor-offmesh-dashboard-hint = frota de relays (ponto de vista externo): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = certificado local, chave correspondente e pacote de CA são válidos; certificado dentro do período de validade
doctor-step2-material-failed-summary = não foi possível usar o material TLS local { $file }
doctor-step2-material-failed-hint = { $err } — inspecione o arquivo indicado; restaure-o a partir do pacote emitido ou rotacione o perfil
doctor-step2-material-invalid-hint = { $file } não é um certificado X.509 PEM válido; restaure o material emitido ou execute `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem é inválida ou não corresponde a mtls.cert.pem; restaure o material emitido ou execute `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = o certificado local está fora do período de validade
doctor-step2-cert-time-hint = execute `goat profile show`; confira o resultado do relógio acima, depois peça um novo pacote ao operador e execute `goat profile rotate <new-bundle.cbor>` se o certificado tiver expirado
doctor-skipped-no-endpoint-summary = ignorado — profile.toml sem { $field }
doctor-skipped-no-profile-summary = ignorado — nenhum endpoint de perfil legível neste contexto
doctor-skipped-no-portal-token-summary = ignorado — nenhum token do portal legível neste contexto
status-ztol-tcp-ok = porta do roteador { $host }:{ $port } aceita TCP (apenas a porta — veja a sessão)
status-session-ok = sessão zenoh mTLS aberta em { $ms }ms (o roteador aceitou este certificado)
status-session-skipped = sessão não tentada (fora da malha; veja ztol)
status-session-material-missing = sessão zenoh não tentada: { $detail }
status-session-config-failed = falha na configuração da sessão zenoh: { $err }
status-session-failed = a sessão zenoh não abriu: { $err } — execute `goat doctor`
status-session-timeout = a sessão zenoh não abriu em { $secs }s — a porta do router aceita TCP, mas nenhuma sessão se completa (router travado, certificado rejeitado ou desvio de relógio); execute `goat doctor`
status-names-live = nomes ao vivo (DNS da malha respondeu)
status-names-ip-only = nomes não exercitados (os endpoints do perfil são IPs literais)
status-names-snapshot = nomes conforme snapshot assinado, série { $serial }, idade { $age } (DNS da malha inacessível)
status-names-snapshot-aging = nomes conforme snapshot assinado, série { $serial }, idade { $age } — DESATUALIZADO (DNS da malha inacessível; corrija o goat-coredns e os nomes se atualizam sozinhos)
status-names-snapshot-daemon = nomes conforme snapshot assinado, série { $serial }, idade { $age } — armazenamento do daemon (DNS da malha inacessível)
status-names-snapshot-daemon-aging = nomes conforme snapshot assinado, série { $serial }, idade { $age } — armazenamento do daemon, DESATUALIZADO (DNS da malha inacessível; corrija o goat-coredns e os nomes se atualizam sozinhos)
status-names-failed = DNS da malha inacessível e fallback de snapshot indisponível — { $reason }
status-pqc-on = PQC do wireguard (rosenpass) ATIVO — { $qr }/{ $connected } peers conectados resistentes a ataques quânticos
status-pqc-off = PQC do wireguard (rosenpass) DESATIVADO — handshakes wireguard clássicos
status-pqc-unknown = PQC do wireguard desconhecido — { $reason }
status-device-none = proteção de nomes em todo o dispositivo DESATIVADA — sem armazenamento de nomes do goat-clientd (os comandos goat ainda têm fallback; ative conforme name-fallback-resolver.md §9.2)
status-device-broken = armazenamento de nomes do goat-clientd presente (série { $serial }), mas o forwarder NÃO responde em { $addr } — daemon parado ou subsistema de nomes travado
status-device-active = proteção de nomes em todo o dispositivo ATIVA — goat-clientd à frente do DNS da malha do SO em { $addr } (série do armazenamento { $serial }, { $grade })
status-device-not-fronted = forwarder do goat-clientd ativo em { $addr } (série do armazenamento { $serial }, { $grade }) — SO sem proxy; navegadores usam o DNS da malha direto
status-device-fronting-unknown = forwarder do goat-clientd ativo em { $addr } (série do armazenamento { $serial }, { $grade }) — estado de proxy do SO não detectável nesta plataforma
status-names-observed = nomes NÃO CANÔNICOS (ad hoc): registro observado, idade { $age } — fora do snapshot assinado (DNS da malha inacessível; codifique este registro)
status-names-selfclaim = nomes via AUTODECLARAÇÃO verificada, idade { $age } — assinada pela âncora do próprio nome (DNS da malha inacessível)
doctor-names-note = via snapshot série { $serial }, idade { $age }
doctor-names-selfclaim-note = via AUTODECLARAÇÃO verificada, idade { $age } — assinada pela âncora do próprio nome
doctor-names-selfclaim-hint = o DNS da malha está inacessível — este passo resolveu via AUTODECLARAÇÃO verificada (idade { $age }): a âncora do próprio nome assinou este vínculo e os peer_bindings do snapshot em cache o autorizam. Assinado, mas não ao vivo; corrija o DNS da malha para limpar isto.
doctor-names-observed-note = via registro OBSERVADO (ad hoc), idade { $age } — não canônico
doctor-names-observed-hint = o DNS da malha está inacessível — este passo resolveu via um registro observado NÃO CANÔNICO (idade { $age }): o nome existe só ao vivo, nunca foi codificado no registro do site. Codifique-o (registro em group_vars → gerador → reassinar) ou ele expira; corrija o DNS da malha para limpar isto.
doctor-names-hint = o DNS da malha está inacessível — este passo resolveu via o snapshot de nomes assinado (série { $serial }, idade { $age }). A malha continua acessível pelos nomes de fallback; corrija o DNS da malha (goat-coredns no host mgmt) para limpar isto.
storm-brake-plan-header = Plano do freio de tempestade (ADR 1081 Camada 4 — manual):
storm-brake-plan-target = host mgmt { $user }@{ $host }  (contexto { $context })
storm-brake-step-flush = reiniciar o contêiner de sinalização `{ $container }` (descarta streams zumbis)
storm-brake-step-tighten = limitar o limiter L2 em `{ $container }` (reduz o rate-limit à metade, aumenta o TTL do neg-cache) — { $window } min, revertido pelo operador; ignorado se L2 não estiver implantado
storm-brake-step-shed = pausar a unidade publicadora não essencial `{ $unit }`
storm-brake-shed-none = (nenhuma unidade de shed declarada — defina storm_brake_shed_units em profile.toml ou passe --shed-unit)
storm-brake-announce-note = emitir um OperatorActionV1 para a trilha de auditoria com o resultado de cada passo
storm-brake-dryrun-note = simulação — nada foi executado.
storm-brake-triggers-header = Acionar quando a faixa host-net do relay-health mostrar:
storm-brake-trigger-futility = razão de futilidade da sinalização >= 0.5 com >= 100 msgs/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = perda no uplink >= 5% por 10 min seguidos (wan.loss_pct)
storm-brake-triggers-band-url = faixa do host: relay-health.{ $site }.<zone> (ver docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Nota: só o flush da sinalização NÃO restaura a malha. As Camadas 1-2 (backoff do cliente + shed de sinalização) são a cura; o freio ganha tempo de recuperação.
storm-brake-confirm = Acionar o freio de tempestade em { $user }@{ $host }? Reinicia o contêiner de sinalização e pausa os publicadores declarados. [y/N]
storm-brake-aborted = freio de tempestade abortado (sem confirmação)
storm-brake-executing = acionando o freio de tempestade em { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = falhou
storm-brake-outcome-skipped = ignorado
storm-brake-summary = freio de tempestade concluído: { $ok } ok, { $failed } falhou, { $skipped } ignorado
storm-brake-flush-failed = FLUSH falhou — o contêiner de sinalização não reiniciou, então não se espera recuperação da malha: { $reason }
storm-brake-mgmt-unresolved = host mgmt não resolvido — passe --mgmt-host, defina mgmt_ssh_host no profile.toml do contexto ou exporte GOAT_MGMT_SSH_HOST
storm-brake-audit-note = ação do operador registrada na trilha de auditoria (ator { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = o portal respondeu HTTP 404 para `{ $path }`: não registado lá
error-portal-rate-limited-summary = o portal respondeu HTTP 429 para `{ $path }`: demasiados pedidos, tente mais tarde
error-portal-server-error-summary = o portal respondeu HTTP { $status } para `{ $path }`: uma falha do lado do portal
error-portal-refused-summary = o portal respondeu HTTP { $status } para `{ $path }`: pedido recusado

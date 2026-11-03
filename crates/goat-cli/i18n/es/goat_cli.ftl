### goat-cli — Spanish (es). NATO-member language, Tier 2.
### source: machine-assisted (Claude); review: machine — NOT native-reviewed.
### Contract/domain terms kept verbatim: ZTOL, mTLS, Bearer, bundle, topic, Zenoh.
error-profile-missing-summary = no hay perfil para el contexto `{ $context }`
error-off-mesh-summary = fuera de la ZTOL — sin conexión a la mesh
error-router-unreachable-summary = el router Zenoh en `{ $endpoint }` es inaccesible
error-cert-expired-summary = certificado mTLS caducado o rechazado por el router
error-portal-unreachable-summary = la API del portal en `{ $endpoint }` es inaccesible
error-token-invalid-summary = el portal rechazó el token Bearer
error-topic-conflict-summary = el topic `{ $topic }` ya existe
error-bundle-invalid-summary = error al verificar la firma o la caducidad del bundle

# goat doctor (machine-translated; contract tokens + backticked commands verbatim)
doctor-endpoint-malformed-summary = punto de conexión del router mal formado
doctor-step1-endpoint-malformed-hint = se esperaba `<proto>/<host>:<port>`; se recibió `{ $endpoint }`
doctor-step1-ok-summary = conexión TCP al host del router { $host }:{ $port } correcta
doctor-step1-conn-failed-summary = falló la conexión TCP al host del router
doctor-step1-conn-failed-hint = { $err } — inicie netbird (`sudo netbird up --management-url <mgmt> --setup-key <key>`); compruebe `netbird status`
doctor-step1-timeout-summary = se agotó el tiempo de conexión TCP al host del router
doctor-step1-timeout-hint = host del router { $host }:{ $port } inalcanzable en { $ms }ms — probablemente fuera de la malla
doctor-step2-endpoint-malformed-hint = corrija el emisor del bundle (--goat-cli-router-endpoint debe ser `<proto>/<host>:<port>`)
doctor-step2-ca-missing-summary = PEM de raíces CA ausente o ilegible
doctor-step2-ca-missing-hint = { $err } — vuelva a ejecutar `goat profile init <bundle.cbor>`
doctor-step2-ok-summary = router { $host }:{ $port } acepta TCP (TLS validado en el paso 3)
doctor-step2-refused-summary = TCP del router rechazado
doctor-step2-refused-hint = { $err } — el router puede estar caído; compruebe el despliegue de Zenoh en el mgmt-host
doctor-step2-timeout-summary = se agotó el tiempo de TCP del router
doctor-step2-timeout-hint = router { $host }:{ $port } inalcanzable en { $ms }ms
doctor-step3-config-failed-summary = falló la construcción de la configuración de zenoh
doctor-step3-config-failed-hint = punto de conexión no válido `{ $endpoint }`: { $err }
doctor-step3-tls-config-failed-summary = falló la construcción de la configuración TLS de zenoh
doctor-step3-tls-config-failed-hint = { $err } — probablemente desajuste de versión de zenoh; verifique que las dependencias coincidan
doctor-step3-ok-summary = sesión mTLS abierta — certificado aceptado por la ACL del router
doctor-step3-open-failed-summary = falló la apertura de la sesión zenoh
doctor-step3-open-failed-hint = { $err } — el certificado puede estar caducado (ejecute `goat profile show`) o rechazado por la ACL del router (discrepancia URI-SAN)
doctor-step3-timeout-summary = se agotó el tiempo de apertura de la sesión zenoh
doctor-step3-timeout-hint = el router no completó el saludo en { $secs }s — desfase de reloj, certificado incorrecto o congestión
doctor-step4-ok-summary = el portal devolvió { $n } fila(s) de organización
doctor-step4-token-rejected-summary = el portal rechazó el token Bearer
doctor-step4-token-rejected-hint = HTTP { $status } — token caducado o revocado; vuelva a incorporarse mediante operator-onboard.yml
doctor-step4-request-failed-summary = falló la solicitud al portal
doctor-step4-request-failed-hint = { $err }
doctor-step4-timeout-summary = se agotó el tiempo de la solicitud al portal
doctor-step4-timeout-hint = sin respuesta en { $secs }s — compruebe que { $endpoint } sea alcanzable desde la malla
doctor-skipped-summary = omitido — el paso anterior falló (vuelva a ejecutar con --all para forzar)

# ---------------------------------------------------------------------
# `goat --help` command-tree help (machine-translated; ADR 1024).
# Backticked commands, flag names, paths, env vars ($GOAT_LANG), arrows
# → ⊕, and `{ $var }` reproduced verbatim. Domain terms kept as-is:
# ZTOL, mTLS, Bearer, Zenoh, topic, bundle, portal, mesh, glyph.
# ---------------------------------------------------------------------
help-about = Línea de comandos de operador unificada para la fabric de datos goat.
help-long-about = goat — CLI orientada al operador que compone panoscope-core en una sola herramienta: inicialización/reutilización de perfil, doctor de alcanzabilidad consciente de ZTOL, navegación por el portal de organizaciones, CRUD de topic, pub/sub, delegación a stethoscope, un compositor de comandos interactivo y narración de la cascada del sustrato.
help-flag-profile-dir = Anular el directorio de perfil (por defecto: $GOAT_PROFILE_DIR o ~/.goat).
help-flag-context = Anular el contexto activo (por defecto: variable $GOAT_CONTEXT, luego current_context en config.toml, luego "default").
help-flag-narrate = Mostrar la cascada del sustrato que desencadena un comando mutador.
help-flag-verbose = Habilitar salida de depuración detallada (o definir RUST_LOG=goat=debug).
help-flag-color = Modo de color para la salida del terminal (auto|always|never).
help-flag-lang = Idioma del texto destinado a las personas (p. ej. en, uk). Recurre a GOAT_LANG, luego a la configuración regional del SO, luego al inglés.
help-cmd-profile = Gestionar el perfil goat local (certificado mTLS + token de portal + conjunto de puntos de conexión).
help-cmd-context = Gestionar contextos (`ls` / `use` / `show` / `rm`).
help-cmd-doctor = Ejecutar un sondeador de alcanzabilidad por pasos contra la fabric.
help-cmd-doctor-long = Sondeador de alcanzabilidad por pasos contra la fabric: pertenencia a ZTOL → router Zenoh → aceptación del certificado → portal. Cada paso fallido emite una sugerencia de remediación específica.
help-cmd-vendor = Navegar por las organizaciones registradas en el portal.
help-cmd-topic = Listar, mostrar, crear o eliminar topics.
help-cmd-pub = Publicar una carga útil en una clave de topic.
help-cmd-sub = Suscribirse a una expresión de clave de topic.
help-cmd-inspect = Delegar una solicitud de veredicto de diagnóstico a stethoscope.
help-cmd-ui = Selector combinado interactivo: navegar por el registro declared ⊕ live y saltar a la lente Diagnose / Tail sobre el objetivo seleccionado.
help-cmd-compose = TUI interactiva de construcción de comandos.
help-cmd-narrate = Reflexión sobre la cascada del sustrato a partir de la actividad reciente. Aún no disponible (la vía habitual es --narrate en un comando mutador).
help-cmd-completion = Imprimir el script de autocompletado del shell en stdout.
help-cmd-explain = Buscar el significado de un código de error + su remediación en el documento `goat-cli-error-codes.md` incrustado (patrón cargo --explain).
help-cmd-mock = Publicar muestras sintéticas en un topic. Por defecto: un JSON hello-world trivial a 1Hz. Pase `--format <.fds>` + `--message <Name>` para codificar en su lugar protobuf proporcionado por el operador.
help-cmd-status = Síntesis de estado de salud: alcanzabilidad ZTOL + portal /api/health + caducidad del certificado local, tres líneas de glyph o un objeto --json.
help-cmd-services = Sondear cada servicio alojado en `ops/serve/services.yaml` y mostrar una línea de glyph por servicio (o un objeto --json).
help-cmd-client = Envolver el daemon goat-clientd local a través de su socket IPC JSON-RPC (`~/.goat-client/goat-clientd.sock`): status / up / down / bundle import.
help-cmd-plugin = Descubrimiento de extensiones basado en el PATH (estilo kubectl). Cualquier ejecutable `goat-<name>` en el PATH se puede invocar como `goat <name>`; `goat plugin list` enumera los plugins instalados, `goat plugin which <name>` resuelve la ruta.

# goat status / inspect / --narrate prosa de ejecución (ADR 1024).
status-ztol-malformed = punto de conexión del router mal formado: `{ $endpoint }`
status-ztol-refused = TCP rechazado hacia el router { $host }:{ $port }: { $err }
status-ztol-unreachable = fuera de la malla (router { $host }:{ $port } inalcanzable en { $ms }ms)
status-portal-ok = portal /api/health 2xx
status-portal-failed = falló el portal /api/health: { $err }
status-portal-timeout = se agotó el tiempo del portal /api/health en { $secs }s
status-cert-read-failed = falló la lectura del certificado: { $err }
status-cert-pem-parse-failed = falló el análisis PEM del certificado: { $err }
status-cert-wrong-tag = el primer bloque PEM es { $tag }, se esperaba CERTIFICATE
status-cert-x509-parse-failed = falló el análisis x509: { $err }
status-cert-expired = certificado CADUCADO en unix={ $not_after }, ahora={ $now }
status-cert-valid = certificado válido; caduca en { $days_left }d (unix={ $not_after })
inspect-finding-label = hallazgo ({ $severity }): { $label } — { $detail }
inspect-remedy-label = remedio: { $title } — { $detail }
inspect-no-outcomes = (sin resultados — el objetivo no coincidió con ninguna comprobación)
narrator-portal-confirmed = { $method } { $path } → { $http_status } { $outcome } (portal-confirmed)
narrator-zenoh-put = put de Zenoh aceptado en `{ $key }` ({ $bytes } bytes)
narrator-stethoscope = stethoscope: { $check_id } → { $verdict } ({ $summary })
narrator-settled = cascada estabilizada — { $summary } ({ $n } { $n ->
        [one] consecuencia
       *[other] consecuencias
    })

# Ayuda de una línea para el onboarding/la doc de goat + el broche final todo-verde (ADR 1024).
help-cmd-init = Asistente de primer arranque: elija sandbox o real, compruebe que funciona, apunte al tutor.
help-cmd-tutor = Un recorrido práctico y a su propio ritmo por la fabric (estilo vimtutor; reanudable).
help-cmd-docs = Navegar por la doc de goat, o emitir la referencia completa de comandos (--markdown).
help-cmd-hello = Hacer ida y vuelta de un hello a través de la fabric de extremo a extremo (entorno sandbox o real).
flourish-all-green = 🐐 todo en verde — fabric nominal

# machine-translated additions (2026-06-17) — review per i18n-provenance.toml
error-not-admin-summary = acción solo para operadores — no estás autorizado (`goat admin` es inerte para no administradores)
help-flag-narrate-json = Emite la cascada de sustrato como NDJSON (un objeto por consecuencia) para registros de incidentes + scripting. Implica --narrate.
help-cmd-admin = Administración solo para operadores: ciclo de vida de organización, incorporación/revocación/rotación, corte de mesh-peer, aprovisionamiento de dispositivos + nivel de operador, triaje de LAN. Autorizado por tus credenciales de operador (lado servidor vía portal/token de NetBird); inerte para no administradores — una llamada no autorizada no cambia nada y falla con `E_NOT_ADMIN`.

# machine-translated additions (2026-10-29) — review per i18n-provenance.toml
error-profile-denied-summary = el perfil del contexto `{ $context }` existe pero este usuario no puede leerlo (permiso denegado)
error-portal-bad-response-summary = el portal respondió pero esta versión de goat no pudo interpretar la respuesta
doctor-step0-ok-summary = la configuración local está lista para las pruebas de red
doctor-step0-warn-summary = la configuración local tiene comprobaciones que no se pudieron confirmar
doctor-step0-fail-summary = la configuración local requiere atención
doctor-step0-profile-absent-summary = no hay perfil de goat para el contexto `{ $context }`
doctor-step0-profile-new-hint = revisa `goat context ls`; si es tu primera configuración, ejecuta `goat ca add <bundle-ca.pem>` y compara su huella con la de tu operador, luego `goat profile init <bundle.cbor>`; vuelve a ejecutar `goat doctor`
doctor-step0-profile-absent-hint = revisa `goat context ls`; selecciona un contexto existente con `goat context use <name>`, o ejecuta `goat profile init <bundle.cbor>` para este contexto; vuelve a ejecutar `goat doctor`
doctor-step0-profile-denied-summary = el perfil de goat en { $path } no se puede leer
doctor-step0-profile-denied-hint = corrige el propietario del perfil o ejecuta como el usuario propietario; no reinicialices un perfil existente
doctor-step0-profile-incomplete-summary = el archivo de perfil de goat { $file } falta o no se puede leer
doctor-step0-profile-incomplete-hint = restaura el archivo indicado desde el bundle emitido, o usa `goat profile rotate <bundle.cbor>` para reemplazar este contexto; vuelve a ejecutar `goat doctor`
doctor-step0-profile-invalid-summary = el perfil de goat tiene endpoints ilegibles o ausentes
doctor-step0-profile-empty-token-summary = el perfil de goat tiene un token de portal vacío
doctor-step0-profile-ok-summary = el contexto de goat `{ $context }` tiene material de perfil legible
doctor-step0-client-absent-summary = ningún cliente de malla compatible respondió
doctor-step0-client-absent-hint = instala el cliente de malla que provee tu goatnet, inicia su daemon y vuelve a ejecutar `goat doctor`
doctor-step0-client-stopped-summary = el cliente de malla instalado no devolvió un estado utilizable
doctor-step0-client-stopped-hint = revisa `netbird status -d` o `goat client status --detail`; inicia el daemon instalado (`sudo netbird service start` para NetBird) y vuelve a ejecutar `goat doctor`
doctor-step0-netbird-ok-summary = el daemon de NetBird respondió (perfil reportado `{ $profile }`)
doctor-step0-goat-client-ok-summary = goat-clientd respondió (estado `{ $state }`)
doctor-step0-management-connected-summary = NetBird reporta management conectado
doctor-step0-management-disconnected-summary = NetBird reporta management desconectado
doctor-step0-management-disconnected-hint = revisa la URL de management reportada y `netbird status -d`; un túnel ya establecido puede seguir funcionando durante una caída de management
doctor-step0-management-unknown-summary = no se pudo establecer localmente el endpoint de management configurado
doctor-step0-management-unknown-hint = inspecciona `netbird status -d` o `goat client diagnostics`; doctor no adivinará una URL de gestión
doctor-step0-tunnel-down-summary = NetBird no informa ninguna dirección de malla
doctor-step0-tunnel-down-hint = ejecuta `netbird status -d`; si está inscrito y el daemon responde, ejecuta `goat heal` y luego repite `goat doctor`
doctor-step0-tunnel-address-summary = NetBird informa la dirección de malla { $address }; a continuación se comprueba la alcanzabilidad del router
doctor-step0-goat-client-no-bundle-summary = goat-clientd no tiene ningún bundle importado
doctor-step0-goat-client-no-bundle-hint = ejecuta `goat client bundle import <bundle.cbor>` con el bundle de inscripción del cliente, conecta el cliente y repite `goat doctor`
doctor-step0-goat-client-connected-summary = goat-clientd informa un túnel conectado; a continuación se comprueba la alcanzabilidad del router
doctor-step0-goat-client-outer-disabled-summary = el túnel externo de goat-clientd está deshabilitado en modo netbird-only
doctor-step0-goat-client-down-summary = goat-clientd informa el estado de túnel `{ $state }`
doctor-step0-goat-client-down-hint = ejecuta `goat client diagnostics` y luego `goat client up` si el bundle está cargado; repite `goat doctor`
doctor-step0-goat-client-inner-ok-summary = goat-clientd informa su malla interna conectada
doctor-step0-goat-client-inner-disabled-summary = la malla interna de goat-clientd está deshabilitada en modo wg-cp0-only
doctor-step0-goat-client-inner-down-summary = goat-clientd informa el estado de malla interna `{ $state }`
doctor-step0-goat-client-inner-down-hint = ejecuta `goat client diagnostics` para inspeccionar la malla interna y repite `goat doctor` tras restaurarla
doctor-step0-management-reachable-summary = el endpoint de gestión respondió HTTP { $status }
doctor-step0-management-auth-summary = el endpoint HTTPS de gestión es alcanzable (HTTP { $status } exige autorización para esta sonda)
doctor-step0-management-unexpected-summary = el endpoint de gestión devolvió un HTTP { $status } inesperado
doctor-step0-management-unexpected-hint = inspecciona la URL de gestión configurada y pide a tu operador de goatnet que revise esta respuesta
doctor-step0-management-unreachable-summary = no se pudo alcanzar el endpoint de gestión configurado: { $error }
doctor-step0-management-unreachable-hint = revisa el DNS y el acceso de red a la URL que informa el cliente; es posible que un túnel existente aún funcione
doctor-step0-clock-ok-summary = el reloj local está a menos de { $seconds } segundos de la hora HTTP de gestión
doctor-step0-clock-skew-summary = el reloj local difiere de la hora HTTP de gestión en { $seconds } segundos
doctor-step0-clock-skew-hint = sincroniza el reloj del dispositivo y repite `goat doctor`; la validación de certificados puede fallar mientras el reloj esté mal
doctor-step0-clock-unknown-summary = no se pudo medir el desfase del reloj a partir de una cabecera HTTP Date de gestión
doctor-step0-clock-unknown-hint = revisa la sincronización horaria del dispositivo si fallan las comprobaciones de TLS o de certificados
error-clock-skew-summary = el reloj del dispositivo está demasiado desfasado respecto a la hora de gestión
error-doctor-inconclusive-summary = doctor observó un fallo sin causa demostrada
error-portal-forbidden-summary = el portal denegó a esta cuenta el acceso a la API sondeada
doctor-step4-forbidden-summary = el portal devolvió HTTP 403 para este perfil
doctor-step4-forbidden-hint = esta credencial llegó al portal pero no tiene acceso a la API sondeada; revisa `goat profile show` y pide a tu operador de goatnet que verifique la concesión
doctor-step4-decode-summary = el portal respondió con una respuesta que este goat no puede leer
doctor-step4-decode-hint = { $err } — actualiza goat a la versión que sirve esta goatnet y vuelve a ejecutar `goat doctor`
doctor-step1-control-only-summary = conectado solo al servidor de control — sin túnel a ninguno de los { $total } peers
doctor-step1-control-only-hint = esta red deja salir TCP 443 pero bloquea los puertos de datos de la malla, así que nada en la malla (incluidas las páginas de inicio de sesión) es accesible desde este dispositivo. Pide al equipo de red que permita TCP y UDP de salida a { $relays }, y UDP 3478 a { $stun }; también se recomienda UDP directo entre dispositivos. Para confirmarlo, conéctate a un punto de acceso del móvil y ejecuta `netbird status`: los peers deberían conectarse.
doctor-offmesh-dashboard-hint = flota de relays (punto de vista externo): https://relay-health.sandbox.zagros.goat.example
doctor-step2-material-ok-summary = el certificado local, su clave correspondiente y el paquete de CA se leen correctamente; el certificado está dentro de su intervalo de validez
doctor-step2-material-failed-summary = no se pudo usar el material TLS local { $file }
doctor-step2-material-failed-hint = { $err } — inspecciona el archivo indicado; restáuralo desde el paquete emitido o rota el perfil
doctor-step2-material-invalid-hint = { $file } no es un certificado X.509 PEM válido; restaura el material emitido o ejecuta `goat profile rotate <bundle.cbor>`
doctor-step2-key-invalid-hint = mtls.key.pem no es válida o no coincide con mtls.cert.pem; restaura el material emitido o ejecuta `goat profile rotate <bundle.cbor>`
doctor-step2-cert-time-summary = el certificado local está fuera de su intervalo de validez
doctor-step2-cert-time-hint = ejecuta `goat profile show`; revisa el resultado del reloj de arriba, luego pide un paquete nuevo a tu operador y ejecuta `goat profile rotate <new-bundle.cbor>` si el certificado ha caducado
doctor-skipped-no-endpoint-summary = omitido — a profile.toml le falta { $field }
doctor-skipped-no-profile-summary = omitido — no hay endpoints de perfil legibles para este contexto
doctor-skipped-no-portal-token-summary = omitido — no hay token de portal legible para este contexto
status-ztol-tcp-ok = el puerto del router { $host }:{ $port } acepta TCP (solo el puerto — ver sesión)
status-session-ok = sesión mTLS de zenoh abierta en { $ms }ms (el router aceptó este certificado)
status-session-skipped = sesión no intentada (fuera de la malla; ver ztol)
status-session-material-missing = sesión de zenoh no intentada: { $detail }
status-session-config-failed = falló la configuración de la sesión de zenoh: { $err }
status-session-failed = la sesión zenoh no se abrió: { $err } — ejecute `goat doctor`
status-session-timeout = la sesión zenoh no se abrió en { $secs }s — el puerto del router acepta TCP pero ninguna sesión se completa (router bloqueado, certificado rechazado o desfase de reloj); ejecute `goat doctor`
status-names-live = nombres en vivo (DNS de malla respondió)
status-names-ip-only = nombres no probados (los endpoints del perfil son IP literales)
status-names-snapshot = nombres según snapshot firmado serial { $serial }, antigüedad { $age } (DNS de malla inaccesible)
status-names-snapshot-aging = nombres según snapshot firmado serial { $serial }, antigüedad { $age } — OBSOLETO (DNS de malla inaccesible; repare goat-coredns y los nombres se actualizarán solos)
status-names-snapshot-daemon = nombres según snapshot firmado serial { $serial }, antigüedad { $age } — almacén del daemon (DNS de malla inaccesible)
status-names-snapshot-daemon-aging = nombres según snapshot firmado serial { $serial }, antigüedad { $age } — almacén del daemon, OBSOLETO (DNS de malla inaccesible; repare goat-coredns y los nombres se actualizarán solos)
status-names-failed = DNS de malla inaccesible y sin snapshot de respaldo — { $reason }
status-pqc-on = wireguard PQC (rosenpass) ACTIVO — { $qr }/{ $connected } peers conectados resistentes a cuántica
status-pqc-off = wireguard PQC (rosenpass) INACTIVO — handshakes wireguard clásicos
status-pqc-unknown = wireguard PQC desconocido — { $reason }
status-device-none = protección de nombres a nivel de dispositivo INACTIVA — sin almacén de nombres goat-clientd (los comandos goat conservan el respaldo; actívela según name-fallback-resolver.md §9.2)
status-device-broken = almacén de nombres de goat-clientd presente (serial { $serial }) pero su forwarder NO responde en { $addr } — daemon caído o subsistema de nombres bloqueado
status-device-active = protección de nombres a nivel de dispositivo ACTIVA — goat-clientd al frente del DNS de malla del SO en { $addr } (serial del almacén { $serial }, { $grade })
status-device-not-fronted = forwarder de goat-clientd activo en { $addr } (serial del almacén { $serial }, { $grade }) — SO sin front; los navegadores usan DNS de malla directo
status-device-fronting-unknown = forwarder de goat-clientd activo en { $addr } (serial del almacén { $serial }, { $grade }) — estado de front del SO no detectable en esta plataforma
status-names-observed = nombres NO CANÓNICOS (ad hoc): registro observado, antigüedad { $age } — no está en el snapshot firmado (DNS de malla inaccesible; codifique este registro)
status-names-selfclaim = nombres vía AUTORRECLAMACIÓN verificada, antigüedad { $age } — firmada por el ancla del propio nombre (DNS de malla inaccesible)
doctor-names-note = vía snapshot serial { $serial }, antigüedad { $age }
doctor-names-selfclaim-note = vía AUTORRECLAMACIÓN verificada, antigüedad { $age } — firmada por el ancla del propio nombre
doctor-names-selfclaim-hint = el DNS de malla está inaccesible — este paso se resolvió vía una AUTORRECLAMACIÓN verificada (antigüedad { $age }): el ancla del propio nombre firmó este vínculo y los peer_bindings del snapshot en caché lo autorizan. Firmado pero no en vivo; repare el DNS de malla para borrar este aviso.
doctor-names-observed-note = vía registro OBSERVADO (ad hoc), antigüedad { $age } — no canónico
doctor-names-observed-hint = el DNS de malla está inaccesible — este paso se resolvió vía un registro observado NO CANÓNICO (antigüedad { $age }): el nombre es solo en vivo, nunca codificado en el registro del sitio. Codifíquelo (registro group_vars → generador → refirmar) o caducará; repare el DNS de malla para borrar este aviso.
doctor-names-hint = el DNS de malla está inaccesible — este paso se resolvió vía el snapshot de nombres firmado (serial { $serial }, antigüedad { $age }). La fabric sigue accesible con nombres de respaldo; repare el DNS de malla (goat-coredns en el host mgmt) para borrar este aviso.
storm-brake-plan-header = Plan de freno de tormenta (ADR 1081 Capa 4 — manual):
storm-brake-plan-target = host de gestión { $user }@{ $host }  (contexto { $context })
storm-brake-step-flush = reiniciar contenedor de señalización `{ $container }` (descarta streams zombi)
storm-brake-step-tighten = limitar el limitador L2 en `{ $container }` (reduce a la mitad el rate-limit, sube el TTL de neg-cache) — { $window } min, reversión manual por el operador; se omite si L2 no está desplegado
storm-brake-step-shed = pausar unidad publicadora no esencial `{ $unit }`
storm-brake-shed-none = (sin unidades de shed declaradas — define storm_brake_shed_units en profile.toml o pasa --shed-unit)
storm-brake-announce-note = emitir un OperatorActionV1 a la pista de auditoría con el resultado de cada paso
storm-brake-dryrun-note = simulacro — no se ejecutó nada.
storm-brake-triggers-header = Activar cuando la banda host-net de relay-health muestre:
storm-brake-trigger-futility = ratio de futilidad de señalización >= 0.5 con >= 100 msgs/min (control.signal_notfound_60s / signal_msgs_60s)
storm-brake-trigger-loss = pérdida de enlace ascendente >= 5% sostenida 10 min (wan.loss_pct)
storm-brake-triggers-band-url = banda de host: relay-health.{ $site }.<zone> (ver docs/design/relay-health-dashboard.md)
storm-brake-not-cure = Nota: un flush de señalización por sí solo NO restablece la malla. Las capas 1-2 (backoff de cliente + shedding de señalización) son la cura; el freno solo gana tiempo de recuperación.
storm-brake-confirm = ¿Activar el freno de tormenta contra { $user }@{ $host }? Reinicia el contenedor de señalización y pausa los publicadores declarados. [y/N]
storm-brake-aborted = freno de tormenta abortado (sin confirmación)
storm-brake-executing = activando freno de tormenta contra { $user }@{ $host } ...
storm-brake-outcome-ok = ok
storm-brake-outcome-failed = fallido
storm-brake-outcome-skipped = omitido
storm-brake-summary = freno de tormenta completo: { $ok } ok, { $failed } fallidos, { $skipped } omitidos
storm-brake-flush-failed = FLUSH falló — el contenedor de señalización no se reinició, por lo que no se espera recuperación de la malla: { $reason }
storm-brake-mgmt-unresolved = host de gestión sin resolver — pasa --mgmt-host, define mgmt_ssh_host en el profile.toml del contexto, o exporta GOAT_MGMT_SSH_HOST
storm-brake-audit-note = acción de operador registrada en la pista de auditoría (actor { $actor })

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = el portal respondió HTTP 404 para `{ $path }`: no está registrado allí
error-portal-rate-limited-summary = el portal respondió HTTP 429 para `{ $path }`: demasiadas solicitudes, reintente más tarde
error-portal-server-error-summary = el portal respondió HTTP { $status } para `{ $path }`: un fallo del lado del portal
error-portal-refused-summary = el portal respondió HTTP { $status } para `{ $path }`: solicitud rechazada

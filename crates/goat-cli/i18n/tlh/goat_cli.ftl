### goat-cli — Klingon / tlhIngan Hol (tlh). An OPT-IN easter-egg locale
### (ADR 1024 / docs/design/goat-cli-i18n.md). Affectionate, phrasebook-
### grade, and explicitly NOT native-reviewed — tlhIngan Hol speakers are
### warmly invited to PR corrections (that invitation is half the fun).
###
### `tlh` is a real ISO-639 code, so it passes the langid test like any
### other catalog, but it is NEVER auto-selected from the OS locale —
### reachable only via an explicit `--lang tlh` / `GOAT_LANG=tlh`. It is a
### PARTIAL catalog: only the marquee strings are rendered here; every
### other key falls back to the English arbiter, exactly like a
### half-translated human locale.
###
### Boundary discipline is identical to every other catalog: the contract
### tokens — `{ $var }` interpolations, backticks, the → / — glyphs, and
### the literals `/api/health`, `CERTIFICATE`, `unix=`, `Zenoh`,
### `stethoscope` — are reproduced VERBATIM and never localized. The
### machine surface (--json, E_* codes, exit codes) never reaches this
### file. Qapla'!

# --- goat status verdicts — Qapla' (triumph) when the path is good. ---
status-ztol-unreachable = qoghvo' SoH — Qaw'wI' { $host }:{ $port } Sambe'lu' ({ $ms }ms)
status-portal-ok = portal /api/health Qapla'
status-cert-valid = nID lI'taH; { $days_left }d 'ej Hegh (unix={ $not_after })
status-cert-expired = nID Heghpu' (unix={ $not_after }, DaH={ $now })
inspect-no-outcomes = (pagh Qum — pagh waw' lutu'lu')

# All-green flourish — Qapla' (success/triumph) is the only fitting word.
flourish-all-green = 🐐 Qapla'! Hoch QaQ — qogh chep

# --- Marquee error summaries — luj (to fail, to lose). ---
error-off-mesh-summary = ZTOL Daparbe' — qogh DamuvHa'pu'
error-cert-expired-summary = mTLS nID Heghpu' pagh Qaw'wI' lajQo'
error-token-invalid-summary = portal Bearer naQ lajQo'

# --- --narrate — when the dust settles. ---
narrator-settled = Such tagh — { $summary } ({ $n } wanI'pu')

# --- The not-native-reviewed notice, with a wink. The { $lang } and
# { $channels } interpolations pass through verbatim. ---
i18n-machine-translation-warning = note: { $lang } jatlh jonta'mey — tlhIngan Hol jatlhwI', DaSammeH qaqIH! DI'onmey { $channels } Daja'. GOAT_I18N_NO_WARN=1 yIlo'.

# --- Portal answers that are not "unreachable" (#2087). ---
error-portal-not-found-summary = portal jang HTTP 404 `{ $path }`: pa' ghItlhbe'lu'
error-portal-rate-limited-summary = portal jang HTTP 429 `{ $path }`: tlhoy law' tlhobmey, tugh yInIDqa'
error-portal-server-error-summary = portal jang HTTP { $status } `{ $path }`: portal Dop Qagh
error-portal-refused-summary = portal jang HTTP { $status } `{ $path }`: tlhob lajQo'

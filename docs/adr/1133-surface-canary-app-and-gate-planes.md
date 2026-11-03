# ADR 1133 — Surface canary: the app plane and the gate plane are different measurements

- **Status:** Accepted
- **Date:** 2026-08-14
- **Deciders:** platform
- **Related:** ADR 1128 (mesh bandwidth attribution: two planes, never summed),
  ADR 1123 (client demand must not amplify), ADR 1036 (environment-health
  surface), ADR 1058 (relay-health token-gated ingest), ADR 1073 (RBAC
  adherence — the oauth2-proxy sidecar in front of every surface),
  F-263 (self-reported surfaces that stay green while a node is unreachable),
  F-330 (a converged host and a stale container),
  surface-canary.md,
  [network-health-monitoring.md](../design/network-health-monitoring.md),
  relay-health-dashboard.md

## Context

The operator's question was short: *"why isn't there a simple thing that just
shows if high-load web services are loading well… panoscope is data hungry, why
is that nowhere here."*

It was right, and the situation was worse than it looked. The heavy inspection
surfaces — panoscope, goat-map, flashlight, netvis — were watched from
**neither** direction:

- **No passive traffic.** netview's representative set is "services with
  requests in the last hour". These four had zero, so the selection rule
  excluded precisely the services worth watching. A canary you only see when
  somebody happens to click it is not a canary.
- **No active probe.** `blackbox-https` probed six targets — the zone apex,
  grafana, dashboard, auth, oauth2, mgmt. None of these four.

Measuring the existing six turned up the second problem:
**three of them had been permanently red.** `https://mgmt.<zone>/`,
`https://oauth2.<zone>/` and the zone apex each returned **404 in under 200 ms**
— Traefik answering promptly that no route matched. They were not outages. They
were wrong paths: the service registry `isitup_services` already declared
mgmt's health path as `/api/users` and oauth2's as `/health/ready`, and the
hand-kept probe list simply disagreed with the registry. Probing every service
at its root was a single-goatnet-shaped assumption that survived the F-188 per-site
templating.

Standing red is worse than no probe. It trains everyone to ignore the colour,
so a real outage arrives as one more red among the reds nobody looks at.

### The trap that decided the design

The obvious fix — add the four surfaces to `blackbox-https` — produces a
**confidently wrong** answer. Measured, not reasoned:

```
https://panoscope.<site>.<zone>/healthz
  module http_2xx  ->  probe_success=1  probe_http_status_code=200  195 ms
```

That 200 is **Ory's login page**. `http_2xx` follows redirects; panoscope's
oauth2-proxy sidecar returns 302 to `oauth2.<zone>`, and blackbox chased it,
timed the identity provider, and filed the result under panoscope's name. The
195 ms was real — it was just a measurement of something else. netvis behaved
identically at 459 ms.

A canary that reports the wrong service as healthy is strictly worse than no
canary, because it converts an unknown into a false known.

Two facts make the honest version cheap:

1. **oauth2-proxy is in front of every gated surface, and there is no
   `SKIP_AUTH_ROUTES` anywhere in `ops/`.** So *through Traefik*, no path on a
   gated surface reaches the app without a session. Not `/healthz`, not
   anything.
2. **blackbox-exporter is already on the shared `mgmt` docker network**, which
   is upstream of both Traefik and oauth2-proxy. isitup's `check.sh` has probed
   container IPs across that seam all along. Measured: every surface answers
   its own health path in **3–5 ms** there, with no credential.

## Decision

### 1. Two planes, never conflated

Directly parallel to ADR 1128, and for the same reason: two numbers that
describe different amounts of infrastructure must not be averaged, compared or
summed.

| Plane | Probe | Passes through | Answers |
|---|---|---|---|
| **app** | `http://<container>:<port><health_path>` over `mgmt` | nothing | Does the surface's own process answer, and how fast? |
| **gate** | `https://<fqdn><health_path>`, **redirects not followed** | DNS → mesh path → TLS → Traefik → oauth2-proxy | Can a browser get to the front door at all? |

`follow_redirects: false` on the gate module is load-bearing, not a
preference. Whatever answers first **is** the reading. Consequently a 302 at
the gate is a **success** whose meaning is exact: DNS resolved, the mesh path
worked, TLS verified, Traefik routed, the auth gate is alive — and the surface
behind it is **unmeasured**.

### 2. The verdict vocabulary is small, and each word means one thing

`up` (app answered, gate answered *as the surface*) · `gate-only` (app
answered, gate answered *as the gate*) · `surface-down` (gate fine, app not
answering — the state a gate-only probe **misses**) · `unreachable` (the gate
did not answer) · `unmeasured` (no series; never rendered as a colour).

`gate-only` is not a warning. It is the honest statement that the probe holds
no credential and therefore cannot speak for the logged-in experience.

### 3. Targets are rendered from the service registry, never hand-kept

`isitup_services` in group_vars already declares, once per service, the fqdn,
the container, the port and the health path. It drives the isitup page and the
generated mesh DNS zone. The probe now reads the same rows, so it **cannot
disagree with the declaration** — the failure that produced three permanently
red targets is structurally gone, and a new service is watched the day it is
declared rather than the day someone remembers.

Opt-outs are **per plane** (`probe_gate` / `probe_app`), not global. zenoh-router
is deliberately not Traefik-routed and 404s at the gate, but its app-plane
probe is the only useful signal it has; one flag would have taken both.

### 4. What this rung deliberately does NOT measure

`/healthz` on panoscope is a constant handler. A 200 proves the process
answers and — because `main.rs` opens the zenoh session *before* binding HTTP —
that the session came up at start. It runs **no fabric query**.

So this is **not yet** the ADR 1123 fabric-pressure signal that makes panoscope
the most interesting canary on the fleet. Measuring that needs an authenticated
deep path; it is designed in surface-canary.md §5 and deliberately not built
here. The distinction is carried in the emitted document as
`exercises_fabric: false` so the page cannot overclaim, and so the day someone
adds a deep probe the honesty flag has to change with it.

### 5. Cost, stated out loud

ADR 1123 cuts both ways: panoscope's latency is valuable *because* a request
becomes a fabric query, which means a canary that measured it would itself
apply fabric pressure. At this rung the probes hit constant handlers at a
**30 s** interval, so the canary applies **no fabric load**. Any future rung
that changes that must state its new interval and its new cost in this ADR.

## Alternatives rejected

- **Add the four surfaces to the existing `blackbox-https` job.** The obvious
  fix, and the one measured above: `http_2xx` follows redirects, so panoscope
  would have reported Ory's login page at 195 ms under its own name. Wrong
  confidently is worse than absent.
- **One `probe: false` opt-out per service.** zenoh-router needs the gate plane
  off and the app plane on; a single flag takes both and would have silenced
  the only signal it has.
- **Keep the hand-kept target list and just correct the three paths.** Fixes
  today's three and leaves the mechanism that produced them, so the next
  service declared is watched whenever someone remembers.
- **Probe through a held credential so the gate plane sees the real surface.**
  That is the §4 rung and it needs a credential the prober does not have; a
  canary holding a session cookie is a new secret on the obs host. Deliberately
  deferred, not dismissed.

## Consequences

- The heavy surfaces are watched whether or not anyone clicks them.
- `blackbox-https` target `instance` values change for the rows whose path was
  wrong. Those series were permanently `probe_success=0`, so no useful history
  is lost. The `PortalProbeDown` alert matches `https://portal\..*` and is
  unaffected.
- A goatnet without `isitup_services` renders zero probe targets rather than a
  hand-me-down list of another goatnet's hostnames.
- `prometheus.yml` is an Ansible template, so promtool cannot parse it and the
  scrape config had **no CI check of any kind**. `ops/ci/check-prometheus-template-renders.sh`
  now renders it per goatnet and asserts the probe jobs survive with targets
  and their relabel block.
- The gate plane still cannot see past the login for a gated surface. That is a
  stated limit, not a defect, and §4 is where it gets lifted.
- The app plane can only reach containers on the `mgmt` network. Services in
  their own compose project (zenoh-router on `<zenoh_stack>_default`, minio on
  `minio-net`) are unreachable by name however healthy they are, and carry
  `probe_app: false` with the reason rather than a permanent red. isitup still
  covers both — its `check.sh` resolves the container IP through the docker
  socket, a reach the blackbox job does not have.

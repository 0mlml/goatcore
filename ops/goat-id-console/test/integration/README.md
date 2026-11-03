# goat-id-console integration harness

This directory contains the integration harness for **goat-id-console**: a
strictly loopback-bound ephemeral identity stack, a safety guard that keeps
every endpoint on the local host, and assertions that need a real Kratos
rather than a fake one.

## Scope

Covered:

- An ephemeral ORY Kratos stack (`v1.3.1`) started via `docker compose`.
- Kratos admin and public ports bound to loopback only (`127.0.0.1`).
- A pre-flight guard (`RequireLoopback`) that enforces loopback-only values for:
  - `IDP_TENANT_KRATOS_ADMIN_URL`
  - `IDP_TENANT_HYDRA_ADMIN_URL`
  - `IDP_OPERATOR_KRATOS_ADMIN_URL`
  - `IDP_OPERATOR_HYDRA_ADMIN_URL`
- Baseline 2FA behaviour that explicitly **does not** treat the Kratos
  webauthn placeholder as a real second factor.
- Group patches against the v1.3.1 operation allowlist, including migrated,
  absent, and null `metadata_public` shapes, plus the real `goat admin` route.
- A seeder that enrols a **genuine TOTP factor** through the self-service
  settings flow, and the two tests that depends on: the `✓ 2FA → —` reset flip
  and the real `?aal=aal2&refresh=true` step-up login leg.

Recovery codes, multi-device TOTP management, and the console-side step-up
round trip are **not** covered — see "Not covered yet" below.

## Kratos stack

The stack is defined in `ops/goat-id-console/test/integration/docker-compose.yml`
with configuration in `ops/goat-id-console/test/integration/kratos.yaml`.

Key properties:

- Kratos version: `v1.3.1`.
- Admin base URL: `http://127.0.0.1:4434`.
- Public base URL: `http://127.0.0.1:4433`.
- TOTP is enabled (`selfservice.methods.totp.enabled = true`).
- WebAuthn runs in 2FA mode (`passwordless = false`).
- The container healthcheck runs `kratos health ready --endpoint
  http://127.0.0.1:4434`. CI additionally polls
  `http://127.0.0.1:4434/admin/health/ready` — deliberately not
  `/health/ready`, which 307-redirects on the admin port, so a bare `curl -f`
  would treat the redirect as success and pass before Kratos was ready.

## Safety guard

The loopback guard is implemented in `guard.go` as:

- `func RequireLoopback(vars map[string]string) error`

`RequireLoopback` validates exactly the endpoints it is handed;
`RequireLoopbackEnv` gathers the required set from the process environment for
the CI pre-flight and fails closed when one is unset. An error is returned
when:

- A required variable is missing or empty.
- The value is not a valid URL.
- The URL is schemeless or has an empty host.
- The host is not loopback-only (`localhost`, `127.0.0.0/8`, or `::1`).

- The guard is handed nothing at all (refusing rather than passing vacuously).

Untagged unit tests in `guard_test.go` cover acceptable loopback values and
reject external hostnames, schemeless URLs, empty hosts, and missing
variables — including a regression lock proving the guard validates the keys
it is *given* rather than a hardcoded list.

## Integration tests

The tagged integration tests live in `console_test.go` and are compiled only
when the `integration` build tag is set.

They verify these HTTP contracts against the pinned Kratos:

- **Linkage** (`TestIntegration_HealthzUsesRealServer`) — drives `GET /healthz`
  through a `handlers.NewServer` instance, proving the suite exercises the real
  console rather than a local reimplementation of it.
- **WebAuthn placeholder locking**
  (`TestIntegration_HasSecondFactorWebauthnPlaceholderRealKratos`) — creates a
  fresh identity against the ephemeral Kratos, asserts the auto-created
  `webauthn` credential **is present with zero enrolled keys**, and only then
  asserts `HasSecondFactor()` is false. The precondition assertions matter: an
  identity with no webauthn credential at all would satisfy the conclusion for
  the wrong reason and lock nothing.
- **Group-patch compatibility** (`TestIntegration_PatchGroupsRealKratosV131`)
  — proves a single child-path `add` is accepted, creates absent/null parents
  and keys, stores `[]` rather than null, and preserves sibling authorization
  metadata. `TestIntegration_ApiGroupsRouteRealKratosV131` drives the same
  operation through the production `POST /api/users/groups` route and re-reads
  the persisted identity.

### Seeded second factors (`seeder.go`, `totp_test.go`)

Kratos v1.3.1 admin identity-import **cannot** create a second factor. Verified
against a live v1.3.1: posting `credentials.totp` to `POST /admin/identities`
is rejected with `json: unknown field "totp"` — the import payload takes
`password` and `oidc` only. A real factor therefore has to be enrolled through
the self-service settings flow, which is what `SeedTOTPIdentity` does:

1. create the identity with a password via the admin API;
2. `GET /self-service/login/api` → `POST /self-service/login?flow=…` for a session token;
3. `GET /self-service/settings/api` and read the base32 seed from the node whose
   `attributes.id == "totp_secret_key"` (at `attributes.text.context.secret`);
4. compute an RFC 6238 code and `POST /self-service/settings?flow=…` with
   `{"method":"totp","totp_code":…}`, requiring the flow to report
   `state: success` (a single submission — there is no retry loop; the code is
   minted away from a 30s step boundary so it cannot expire in flight).

RFC 6238 is implemented in-file with stdlib crypto — roughly fifteen lines —
rather than pulled from a module, so a test-only dependency does not land in
the console's production module graph.

That unlocks two tests nothing else can reach:

- **`TestIntegration_RealTOTPFactorFlipsAndResets`** — the canon gate from epic
  #161. A genuinely enrolled factor makes `HasSecondFactor()` true, and clearing
  the credential flips it back (`✓ 2FA → —`).
- **`TestIntegration_AAL2StepUpLoginLeg`** — the leg PR #187 could not prove. A
  real `?aal=aal2&refresh=true` login completed with a TOTP code raises the
  session to `aal2` **and** records a per-method `authentication_methods[]`
  entry carrying `aal` and `completed_at`, which is the live signal the console
  reads to decide AAL and freshness.

**Worth knowing before the #188 rollout:** enrolling TOTP through the settings
flow *itself* records a `totp` / `aal2` authentication method. A freshly-enrolled
admin can therefore already look AAL2-fresh, which affects the ordering of the
"enrol, then flip `STEP_UP_ENFORCED`" procedure.

### Not covered yet

Nothing here asserts the following; they remain designed but not built:

- Reset-2FA HTTP contract: 404 unknown identity, 303 redirect on success
  (`/users/{id}?idp={idp}&reset2fa=ok` — the 204 in the original issue text is
  wrong; the merged handler returns `http.StatusSeeOther`), 400 on CSRF failure.
- Fail-closed behaviour for gated mutations: 502 on Kratos 5xx, 502 and no
  mutation when Kratos is unreachable.
- Authorisation: non-admin requests yielding 403.
- The console-side step-up round trip end to end — interstitial, one-shot nonce
  replay, operator binding, and the freshness window lapsing. The Kratos half of
  that flow is now proven; the console half is still only unit-tested against a
  fake Kratos.

## Running the tests locally

From the repository root:

```sh
cd ops/goat-id-console/test/integration

# Start the local Kratos stack
docker compose up -d

# In another shell, run the tests from the goat-id-console module root
cd ../../..
export IDP_TENANT_KRATOS_ADMIN_URL=http://127.0.0.1:4434
export IDP_TENANT_HYDRA_ADMIN_URL=http://127.0.0.1:4445
export IDP_OPERATOR_KRATOS_ADMIN_URL=http://127.0.0.1:4434
export IDP_OPERATOR_HYDRA_ADMIN_URL=http://127.0.0.1:4445
# The seeder drives the self-service flows, which live on the PUBLIC port.
export KRATOS_PUBLIC_URL=http://127.0.0.1:4433

# Run the safety guard unit tests
go test ./test/integration -run 'TestRequireLoopback'

# Run the tagged integration tests
go test ./test/integration -tags=integration -run .
```

Note that port 4434 is a common default; if something else on your machine is
already bound to it the stack will not come up. Every endpoint stays on
loopback — the guard refuses anything else, so the suite cannot be pointed at
a deployed goatnet.

# ADR 0413 — No ORM in our own Go code (raw SQL via sqlx/pgx)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0410-go-cli-peer-enrollment.md`](0410-go-cli-peer-enrollment.md), [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), [`0801-postgresql-spock-multi-master.md`](0801-postgresql-spock-multi-master.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0105-adr-as-sca-r-enablement.md`](0105-adr-as-sca-r-enablement.md), `../design/identity-architecture.md` |

## TL;DR (for a layperson)

Object-Relational Mappers (ORMs) are a popular tool that hides the
SQL database behind Go structs — they look convenient but tend to
create subtle, runtime-only bugs when struct definitions and database
column types drift apart. We've watched netbird (which uses GORM,
the most popular Go ORM) burn us multiple times today on exactly
this class of issue. For our own code we'll stick to writing plain
SQL (with `sqlx` or `pgx` for ergonomics), which keeps the data
contract visible, auditor-friendly, and free of hidden serialization
behavior.

## Context

On 2026-04-25, attempting to seed netbird's `accounts` and
`setup_keys` tables for the cgnat-validation env, we hit a long
chain of GORM-induced runtime errors that the Go type system
couldn't catch:

- `cannot scan NULL into *string` — pointer-vs-value type decisions
  in netbird's GORM model determine NULL semantics, surfacing only
  on row scan.
- `invalid character '.' after top-level value` — `gorm:"serializer:json"`
  on a `net.IPNet` field meant the column had to contain a JSON-
  marshaled IPNet (mixed format: `IP` as quoted string, `Mask` as
  base64), not a CIDR string.
- `cannot unmarshal object into Go value of type *netip.Prefix` —
  same class, different field, different JSON shape expectations.
- `invalid IP address: ZEAAAA==` — Go's `net.IP.UnmarshalJSON`
  expects a quoted IP string, not base64 bytes; we'd guessed wrong.

Each of these was discoverable only after running the live mgmt
server, watching it crash on a row scan, and reverse-engineering
what shape the model expected. None were caught at compile time.
None were obvious from the schema. None were documented in the
column type. The full hour-by-hour trail is in
`../reports/2026-04-24-cgnat-validation-attempt-1-findings.md`
§1.3 + the IdP strategy spike.

This is not unique to GORM — every Go ORM (ent, sqlboiler, gorm,
xorm) has variants of the same failure mode because they all hide
the SQL contract behind an in-language abstraction that the Go
type checker can't fully validate against the live schema.

We accept netbird's GORM dependency because we use upstream
netbird. We don't have to repeat the choice in our own code.

## Decision

For all new Go code we write (Go CLI for peer enrollment per ADR
0410, Block 17A.5 policy engine, Block 18 services, future internal
admin tools, future data-services daemons), **we use raw SQL via
[`sqlx`](https://github.com/jmoiron/sqlx) or
[`pgx`](https://github.com/jackc/pgx) — not an ORM**.

**Specifically:**

- **Queries are written as plain SQL strings** in the source. They
  can be code-reviewed, grepped, audited, and explained directly.
- **Row scanning** uses `sqlx.StructScan` or `pgx.RowToStructByName`
  — these tools fail loudly at compile-time-ish boundaries (column-
  to-struct mapping is one explicit call) rather than hiding it in
  reflection-driven model layers.
- **Schema migrations** are managed by [`golang-migrate`](https://github.com/golang-migrate/migrate)
  or `sqlc`-style tooling — explicit `up`/`down` SQL files in the
  repo, not GORM AutoMigrate.
- **JSON columns** (where unavoidable) are serialized + deserialized
  with explicit `json.Marshal`/`json.Unmarshal` calls in code, not
  via ORM serializer tags. The exact JSON shape lives in the code,
  not in tag annotations.
- **Type-safe query generation**: `sqlc` is acceptable when teams
  prefer compile-time-checked queries — it generates Go from SQL,
  which is the inverse of an ORM (SQL is the source of truth, Go is
  generated). We treat `sqlc` as a *codegen tool*, not an ORM.

**Exception:** code we *call into* (netbird, third-party libraries
we don't fork) can use whatever ORM they want. We accept that as
their architectural choice. The Go CLI per ADR 0410 imports
netbird's `management/server/types` package and lets netbird's own
GORM do its thing internally — that's *netbird's* problem, not
ours, and we get correctness for free because we're invoking the
same code path netbird's API uses.

## Alternatives considered

- **GORM.** The dominant Go ORM. Already discussed — runtime-only
  errors, tag annotations that hide JSON shape, AutoMigrate that
  drifts. Rejected for our own code.
- **ent (Facebook's ORM).** Schema-as-Go-code with code generation,
  somewhat better than GORM. Still hides the SQL contract behind a
  generated query builder. Rejected — we'd rather just write SQL.
- **sqlboiler.** Code-generates Go from a live database. Inverse of
  GORM (DB-first), better story than struct-first ORMs. Acceptable
  alternative to `sqlc` if it fits a team's preference. Either is
  fine; both are codegen tools that keep SQL as the source of
  truth.
- **bun, gorp, beego/orm, others.** All have variants of GORM's
  failure mode. Rejected.
- **Raw `database/sql`.** Underlying standard library. Fine for the
  simplest cases, but `sqlx` adds tiny ergonomic wins (named
  parameters, struct scanning) without hiding anything. We use
  `sqlx` over plain `database/sql` for that reason.

## Consequences

**Positive.** Data contract between Go code and Postgres schema is
visible in the code. Auditors (SCA-R) can read the SQL directly and
understand exactly what queries the service runs. Schema migrations
are explicit + reversible. JSON serialization shape is in code
(reviewable), not in struct tag annotations (hidden). Compile-time
errors catch column name typos with `sqlc`; runtime errors with
`sqlx` are at least scoped to the explicit `StructScan` call.

**Negative.** More boilerplate per query than GORM's `db.First(&user)`-
style code. We trade ergonomics for transparency. For a small team
this is the right trade because the *cost of a runtime SQL error in
production* dwarfs the savings of a 3-line query becoming a 1-line
one.

## Assessor view (SCA-R)

This decision satisfies AU-2 (auditable events) and CM-3 (configuration
change control) by making data-access behavior code-readable. The
elimination of ORM-driven implicit serialization removes a class of
"hidden behavior at the code/data boundary" that complicates security
review. Schema migrations under `golang-migrate` or equivalent are
fully version-controlled + reversible, satisfying CM-4 (security
impact analysis of changes). The decision is consistent with NIST
SP 800-160 Vol. 2 Cyber Resiliency principle of *minimize attack
surface* — every ORM is additional supply-chain surface and an
implicit-behavior surface we don't otherwise need to carry.

## Trigger conditions for revisit

- A team adopts a Go ORM with a fundamentally different failure-mode
  profile (e.g., compile-time exhaustive type checking against the
  live schema). Re-evaluate, but skeptically.
- We outgrow `sqlx` ergonomics on a particular service (e.g., very
  complex query composition). At that scope, `sqlc` becomes more
  attractive than ORM adoption.
- The cost of writing SQL by hand becomes a measurable bottleneck
  on developer velocity. (Expected to never trigger.)

---

*See `GLOSSARY.md` for terms and acronyms.*

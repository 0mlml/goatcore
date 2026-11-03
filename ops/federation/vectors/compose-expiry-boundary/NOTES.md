# compose-expiry-boundary — `expires` exactly equal to `now`

§6 step 3 refuses a package whose `expires` is **before** `now`, and the boundary is
**inclusive**: `expires == now` is valid that day and composes.

`package-xab.canonical.json` carries `"expires":"2026-09-05"`, which is exactly the `now` in
`../now.txt`. `package-xaa.canonical.json` is byte-identical to the `compose-full` one and
expires `2026-12-05`.

## What this vector pins

| | |
|---|---|
| Verdict | **Composes.** An implementation that refuses here has an exclusive boundary |
| `expires` | `2026-09-05` — the earlier of the two, which is also the boundary package's |
| Everything else | Identical to `compose-full`. The two agreements differ in one member |

The boundary package is deliberately also the one that decides the agreement's `expires`, so a
wrong boundary is visible twice: as a refusal that should not happen, and — for an implementation
that composes but rounds the term — as a different digest.

## Why this vector exists

`now` has a source and a timezone (§6), and neither is visible in development. Every other
vector's expiry is months away, so an off-by-one at the boundary passes the whole suite and
appears once, in production, on one specific day. This is the fixture that fires on it.

## Digest

`sha256:32580792386d28ca02f50a6833aaf631b870333c7c7e8f8ca8d7d4a3baf598ed`

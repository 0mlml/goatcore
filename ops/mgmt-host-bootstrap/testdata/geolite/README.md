# Geolocation fixture

netbird-mgmt builds its geolocation layer **during startup, before it serves
HTTP**. With this directory empty it fetches ~82 MB from `pkgs.netbird.io`
(a 32 MB MaxMind tarball plus a 50 MB CSV zip it imports into SQLite) inside
the integration test's readiness window.

That download is not a netbird property — it is a third-party CDN's throughput
on the day. Measured on the same unchanged files (both `Last-Modified:
2026-06-02`): warm edge, `TestEndToEnd_OTLPEmit` passes in **19 s**; cold edge,
the first file alone took **3 m 50 s** and the test failed on a deadline
(2026-08-22 and 2026-08-23, `--- FAIL: TestEndToEnd_OTLPEmit (254.78s)`).

So the tests seed the databases instead of downloading them. `NewGeolocation`
skips any database already present in the Datadir, so a seeded stack never
touches the network for this.

## Warming it

```bash
cd ops/mgmt-host-bootstrap
./testdata/warm-geolite.sh
```

Brings the fixture stack up once, waits for netbird to write both databases
here, tears it down. Ten to twenty minutes on a cold CDN, seconds on a warm
one — but only ever once per published database version.

CI does the same thing, guarded by an `actions/cache` keyed on the version
`pkgs.netbird.io` currently serves, so a version bump re-warms by itself. The
key follows the filename netbird derives (a HEAD of the tarball URL, its
`Content-Disposition`), not the sha256 pointer beside it: the two can
disagree, and netbird asks for the former. `geolite-version.sh` reads that
header the way netbird's `mime.ParseMediaType` does (the value bare or
quoted, parameters after it, the `filename*=` form) and prints the pointer's
version beside it; a spelling it cannot read falls back to the pointer under
a warning on the run, never silently. `tests/geolite-version.bats` pins each
spelling. An older pair is restored on a bump (`restore-keys`), the warm step
re-warms when it is not the version netbird will ask for (`GEOLITE_VERSION`),
and the cache is saved only after a warm that succeeded, so an empty
directory is never stored as a seed.

## When the mirror disagrees with itself

On 2026-09-08 the mirror moved both sha256 pointers to `20260908` while the
tarball and the CSV zip stayed at `20260602`. netbird 0.68 takes the filename
from the tarball, verifies the download against the pointer, and treats the
mismatch as fatal, so a cache miss could not be warmed for as long as that
lasted ([#1069](https://github.com/usareur-af/goatcore/issues/1069)).

`warm-geolite.sh` fails within a poll interval when management dies during
startup and names the reason on `$GITHUB_OUTPUT`: `mirror-checksum`,
`management-died` or `timeout` (`tests/warm-geolite.bats`). Only the first,
the mirror's own doing, sends CI on: the integration test then runs with
`NB_DISABLE_GEOLOCATION=true`, netbird's own switch, under a warning
annotation, and enrollment is still graded against the pinned image while
geolocation is not. A warm that failed for any other reason fails the run,
so a geolocation regression in the pinned image cannot hide behind the
mirror's outage. An unseeded local run still downloads, as below.

## Absent is fine

With this directory empty the tests still pass — they just fall back to the
download and log that they did. If a readiness timeout follows that log line,
the CDN is the suspect, not netbird.

## What lands here

| file | what it is |
|---|---|
| `GeoLite2-City_<date>.mmdb` | MaxMind city database, extracted from the tarball |
| `geonames_<date>.db` | SQLite built by netbird from the CSV zip — not a download |

Neither is committed. `<date>` is the version netbird resolved from the CDN.

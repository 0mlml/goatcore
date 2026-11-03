#!/usr/bin/env bash
# free-text-columns.sh — a row's `name` and `desc` survive the trip through
# services.env whatever punctuation they carry.
#
# Why this test exists: services.env is a bash array of double-quoted strings,
# and `name` + `desc` are operator-written prose templated straight into it.
# The template header said a `desc` "MUST NOT contain a `"`", and nothing
# enforced it. the Excalidraw external-service declaration said
#   press "Live collaboration", share the link…
# The inner quote closed the string; the space inside the quotes then split the
# entry into TWO array elements. The second began mid-sentence, so its columns
# landed wherever the pipes fell: id = the rest of the prose, name = the empty
# link_path, host = the probe_front_door column, the literal `true`. zagros's
# status page grew a nameless row reading "declared but true does not resolve
#" — a real-looking finding code on a service that does not exist.
#
# Renders the REAL template (render_services_env.py stubs the two ansible-only
# names it uses), sources the result the way check.sh does, runs check.sh for
# real with curl/docker/getent stubbed, and asserts on current.json: one row per
# declared service, none nameless, name + desc byte-identical to the fixture.
# `$` and a backtick are in the fixture too, because the same unescaped column
# would have EXECUTED them on every cron tick.
set -euo pipefail
cd "$(dirname "$0")"

if ! python3 -c 'import jinja2' >/dev/null 2>&1; then
  echo "[isitup/free-text-columns] SKIP (python3 jinja2 not importable)"
  exit 0
fi

CHECK_SH="$(cd ../files && pwd)/check.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/data"

cat >"$WORK/bin/docker" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat >"$WORK/bin/getent" <<'STUB'
#!/usr/bin/env bash
echo "172.20.0.2 $2"
STUB
cat >"$WORK/bin/curl" <<'STUB'
#!/usr/bin/env bash
echo -n "200"
STUB
# python3: check.sh pins its own PATH, and the history accumulator at its end
# needs python3. On a runner it is in /usr/bin and this shim changes nothing; on
# a laptop where it lives elsewhere, check.sh would die after writing
# current.json and fail this test for a reason that has nothing to do with it.
PY3="$(command -v python3)"
printf '#!/usr/bin/env bash\nexec "%s" "$@"\n' "$PY3" >"$WORK/bin/python3"
chmod +x "$WORK/bin"/*

python3 render_services_env.py "$WORK/services.env" >"$WORK/expected.json"

# HOME is pinned so an unescaped `$HOME` in the fixture would expand to
# something the assertion can name, rather than to a path that varies by runner.
HOME=/expanded-by-mistake \
ISITUP_TEST_BIN="$WORK/bin" ISITUP_DATA_DIR="$WORK/data" \
ISITUP_SERVICES_ENV="$WORK/services.env" bash "$CHECK_SH" >/dev/null

python3 - "$WORK/expected.json" "$WORK/data/current.json" <<'PY'
import json, sys

expected = json.load(open(sys.argv[1], encoding="utf-8"))["rows"]
try:
    got = json.load(open(sys.argv[2], encoding="utf-8"))["services"]
except ValueError as e:
    print("[isitup/free-text-columns] FAIL  current.json is not valid JSON: %s" % e)
    sys.exit(1)

fail = 0
def check(ok, msg):
    global fail
    print("[isitup/free-text-columns] %s  %s" % ("PASS" if ok else "FAIL", msg))
    fail |= (not ok)

check(len(got) == len(expected),
      "%d declared services -> %d rows (a split entry adds one)" % (len(expected), len(got)))
check(all(r["name"] for r in got), "no row is nameless")
check(not any("true does not resolve" in r["detail"] for r in got),
      "no row has the probe_front_door column as its hostname")
by_id = {r["id"]: r for r in got}
for want in expected:
    row = by_id.get(want["id"])
    if row is None:
        check(False, "row %r is present" % want["id"])
        continue
    check(row["name"] == want["name"], "row %r name round-trips: %r" % (want["id"], row["name"]))
    check(row["desc"] == want["desc"], "row %r desc round-trips" % want["id"])
sys.exit(fail)
PY

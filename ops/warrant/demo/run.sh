#!/usr/bin/env bash
# hello-galaxy — run the warrant generator against a policy that belongs to
# nobody. WORK IN PROGRESS; see README.md.
#
# This is the open-source seam (docs/project/warrant-plan.md §3A) being
# exercised for real: the generator is given --policy-dir pointing at this
# directory instead of the operator's, and produces that policy's ACL. Nothing
# in here touches any goatnet, and nothing in the real registry is read.
#
#   ops/warrant/demo/run.sh            # generate, print, and check it round-trips
#   ops/warrant/demo/run.sh --keep     # also leave the output on disk
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WARRANT="$(dirname "$HERE")"
OUT="$(mktemp -t hello-galaxy-acl.XXXXXX).yml"
KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

cleanup() { [ "$KEEP" = "0" ] && rm -f "$OUT"; }
trap cleanup EXIT

echo "== hello-galaxy: generating an ACL from a port-set registry =="
echo "   policy   : $HERE"
echo "   generator: $WARRANT/generate.py"
echo

python3 "$WARRANT/generate.py" --policy-dir "$HERE" --target "$OUT"

echo
echo "== what came out =="
# The interesting part is that the ports are NOWHERE in the source: they were
# resolved from the named set. Show one rule with its rendered ports next to
# the intent that produced it.
grep -n -A1 'protocol: tcp' "$OUT" | grep -E 'ports:' | head -5
echo

echo "== the same rules in the SOURCE carry no ports at all =="
grep -c 'port_set:' "$HERE/rule-intents/netbird-acl.yaml" | xargs printf '   %s rules name a port-set\n'
if grep -qE '^\s*ports:' "$HERE/rule-intents/netbird-acl.yaml"; then
  echo "   ERROR: a literal ports: line leaked into the source" >&2
  exit 1
fi
printf '   0 rules carry a literal ports: line  (this is the point)\n'
echo

echo "== change one port in one place, and every rule using it moves =="
TMP_POLICY="$(mktemp -d -t hello-galaxy-policy.XXXXXX)"
cp -R "$HERE/." "$TMP_POLICY/"
# Move the fabric's TCP port. Two sets carry 7447 (`substrate` and `fabric`),
# so a single edit to one of them must move exactly that set's rules.
sed -i.bak 's/"443", "53", "7447"/"443", "53", "9447"/' "$TMP_POLICY/port-sets.yaml"
OUT2="$(mktemp -t hello-galaxy-acl2.XXXXXX).yml"
python3 "$WARRANT/generate.py" --policy-dir "$TMP_POLICY" --target "$OUT2" >/dev/null
echo "   rules whose ports changed from one edit:"
diff "$OUT" "$OUT2" | grep -E '^[<>].*ports:' | sed 's/^/     /' || true
rm -rf "$TMP_POLICY" "$OUT2"
echo

echo "== the gate: is the generated file byte-identical to the registry? =="
python3 "$WARRANT/generate.py" --policy-dir "$HERE" --target "$OUT" --check && \
  echo "   (this is what CI runs on every push against the real declaration)"

[ "$KEEP" = "1" ] && echo && echo "output kept at: $OUT"
exit 0

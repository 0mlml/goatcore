#!/usr/bin/env bash
# zenoh-region-census.sh — read-only census of a zenoh router's REGION assignment.
#
# WHY. zenoh 1.9.0 assigns every session a Region (north / south:<id>:<mode>).
# No goat config sets `gateway`, so every router runs the `Preset(Auto)` default
# and the region split is decided by mode pairing alone — see
# docs/design/zenoh-data-fabric.md "Hierarchical routing". This script reads that
# live assignment back so the deployed behaviour can be checked rather than
# assumed, and so the link-state / route-table sizes in
# docs/measurements/M-111-zenoh-region-link-state-and-route-table-size.md can be
# re-measured.
#
# READ-ONLY. It issues adminspace GETs and changes nothing.
#
# THE TRAP THIS SCRIPT EXISTS TO AVOID:
#   NAMESPACE. The router runs in a container, and its zenoh SESSIONS are only
#   visible in that container's netns. `ss` on the host shows none and invites
#   the conclusion that peering is down. This script enters the netns so the
#   session view and the adminspace come back in one round trip.
#
#   NOT a trap, despite an earlier version of this comment saying so: the
#   adminspace REST listener IS published to host loopback
#   (8000/tcp -> 127.0.0.1:8000).
#   Anything that needs ONLY the adminspace can curl 127.0.0.1:8000 directly and
#   skip the netns entirely — which is what the goat-topology publisher does.
#
# usage: zenoh-region-census.sh <ssh-target> <container> [rest-port]
#   e.g. zenoh-region-census.sh operator@mesh-host.example zenoh-router
#
# Requires: passwordless sudo on the target (docker inspect + nsenter).
set -euo pipefail

TARGET="${1:?usage: $0 <ssh-target> <container> [rest-port]}"
CONTAINER="${2:?usage: $0 <ssh-target> <container> [rest-port]}"
PORT="${3:-8000}"

SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=15)

# One round trip: resolve the container PID, enter its netns, dump the adminspace.
raw=$(ssh "${SSH_OPTS[@]}" "$TARGET" \
  "PID=\$(sudo docker inspect -f '{{.State.Pid}}' '$CONTAINER'); \
   sudo nsenter -t \$PID -n curl -s --max-time 20 'http://127.0.0.1:$PORT/@/local/**'" 2>/dev/null)

[ -n "$raw" ] || { echo "no adminspace response from $TARGET/$CONTAINER" >&2; exit 1; }

# Stash the response in a temp file: the python program itself arrives on stdin
# (`python3 -`), so the JSON cannot also come down that pipe.
dump=$(mktemp); trap 'rm -f "$dump"' EXIT
printf '%s' "$raw" > "$dump"

python3 - "$TARGET" "$CONTAINER" "$dump" <<'PYEOF'
import collections, datetime, json, re, sys

target, container, dump = sys.argv[1], sys.argv[2], sys.argv[3]
with open(dump) as fh:
    d = json.load(fh)
now = datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')

router = next((x for x in d if x['key'].endswith('/router')), None)
if router is None:
    sys.exit('no @/<zid>/router key in the adminspace response')
zid = router['key'].split('/')[1]

print('host        %s  container %s' % (target, container))
print('observed    %s' % now)
print('router zid  %s' % zid)
for x in d:
    if x['key'].endswith('plugins/rest/version'):
        print('rest plugin %s' % x['value'])

print('\nSESSIONS BY REGION')
census = collections.Counter((s['whatami'], s['region']) for s in router['value']['sessions'])
for (mode, region), n in sorted(census.items()):
    print('  %4d  whatami=%-7s region=%s' % (n, mode, region))

print('\nNON-CLIENT SESSIONS (the ones that carry routing state)')
noncli = [s for s in router['value']['sessions'] if s['whatami'] != 'client']
for s in noncli:
    print('  %-7s %-16s %s  %s'
          % (s['whatami'], s['region'], s['peer'], ', '.join(l['dst'] for l in s['links'])))
if not noncli:
    print('  (none)')

print('\nLINK-STATE GRAPH PER REGION')
for x in sorted(d, key=lambda x: x['key']):
    if '/linkstate/' in x['key']:
        region = x['key'].split('/linkstate/', 1)[1]
        dot = x['value']
        nodes = re.findall(r'^\s+\d+ \[ label = "([0-9a-f]+)" \]', dot, re.M)
        edges = re.findall(r'^\s+(\d+) -- (\d+)', dot, re.M)
        print('  %-16s V=%d  E=%d' % (region, len(nodes), len(edges)))
        for n in nodes:
            print('      node %s%s' % (n, '  (self)' if n == zid else ''))

routes = [x for x in d if '/route/successor/' in x['key']]
north = next((x for x in d if x['key'].endswith('/linkstate/north')), None)
v = len(re.findall(r'label = "[0-9a-f]+"', north['value'])) if north else 0
print('\nROUTE/SUCCESSOR ENTRIES  %d   (V*(V-1) for north V=%d is %d)' % (len(routes), v, v * (v - 1)))
PYEOF

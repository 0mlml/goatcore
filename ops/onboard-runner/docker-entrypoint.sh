#!/bin/sh
# onboard-runner container entrypoint.
#
# The runner shells `goat admin onboard --native`, whose portal client (the
# Zenoh cert-mint leg) reads its endpoint + bearer token from a goat profile
# under $HOME/.goat. Rather than bind-mount secret files from the host (uid
# mismatch + a plaintext token on disk), we materialize a MINIMAL profile here
# at startup from the deploy env — just profile.toml (portal endpoint) +
# portal.token (the runner's OWN portal key). No mtls.* / ca-roots: the native
# onboard never uses them (the admin gate passes on an initialized profile, and
# the portal client is bearer-only). See the role's README.
set -eu

: "${PORTAL_URL:?PORTAL_URL required (goat profile portal_endpoint + runner --portal)}"
: "${ONBOARD_RUNNER_PORTAL_KEY:?ONBOARD_RUNNER_PORTAL_KEY required (the dedicated runner portal key)}"

ctx="${HOME}/.goat/contexts/default"
mkdir -p "$ctx"
umask 077
printf 'portal_endpoint = "%s"\n' "$PORTAL_URL" > "$ctx/profile.toml"
# router_endpoint — needed by the operator-action audit emit, not by the mint.
# `goat admin onboard` emits an OperatorActionV1 from the `goat admin` chokepoint
# (docs/design/operator-action-audit.md) and that emit reads router_endpoint from
# THIS profile. This file has only ever written portal_endpoint, so on every
# goatnet every runner mint printed
#
#   warning: operator-action audit emit failed (... missing router_endpoint ...);
#   recording to local fallback log
#
# and the per-person operator-action audit degraded to a file inside a container
# nothing collects. Optional on purpose: a goatnet that has not declared a router
# endpoint still starts, and the warning then states the true reason.
if [ -n "${ROUTER_ENDPOINT:-}" ]; then
  printf 'router_endpoint = "%s"\n' "$ROUTER_ENDPOINT" >> "$ctx/profile.toml"
fi

# Per-goatnet endpoints for the native onboard legs. goat reads these
# from THIS profile and falls back to its built-in table when a key is absent,
# so a goatnet the table has never heard of works from config alone — no
# goat-cli release. Every one mirrors a group_var; the role passes the value and
# nothing is invented here. Absent = keep whatever the table has.
for kv in \
  "netbird_mgmt_url=${NETBIRD_MGMT_URL:-}" \
  "netbird_token_env=${NETBIRD_TOKEN_ENV:-}" \
  "netbird_site_prefix=${NETBIRD_SITE_PREFIX:-}" \
  "netbird_rosenpass=${NETBIRD_ROSENPASS:-}" \
  "ory_auth_url=${ORY_AUTH_URL:-}" \
  "ory_ssh_host=${ORY_SSH_HOST:-}" \
  "ory_ssh_user=${ORY_SSH_USER:-}"; do
  k="${kv%%=*}"; v="${kv#*=}"
  [ -n "$v" ] && printf '%s = "%s"\n' "$k" "$v" >> "$ctx/profile.toml"
done
printf '%s' "$ONBOARD_RUNNER_PORTAL_KEY" > "$ctx/portal.token"

exec /usr/local/bin/onboard-runner "$@"

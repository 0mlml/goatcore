#!/bin/sh
# Custom entrypoint for the patched Netbird management server.
# POSIX sh (no bashisms) so the alpine final stage works without bash.
#
# Orchestrates AutoMigrate timing for multi-master Spock deployments by
# selectively setting NETBIRD_SKIP_MIGRATIONS based on a marker file or
# explicit env var.
#
# Designed to pair with:
#   - The skip-migrations patch (ops/netbird/patches/skip-migrations.patch)
#   - An Ansible-driven coordinated-upgrade playbook that writes/removes the
#     marker file on the designated migrator node during an upgrade window.
#
# Behavior on each container start:
#   - If NETBIRD_RUN_MIGRATIONS=true OR the marker file exists:
#       -> AutoMigrate runs (env var unset, upstream default behavior)
#   - Otherwise:
#       -> NETBIRD_SKIP_MIGRATIONS=true is exported, AutoMigrate is suppressed
#
# Marker file path (override with $NETBIRD_MIGRATION_MARKER):
#   /opt/netbird/migrations.allow
#
# See docs/design/ha-multi-site.md §5.8.1 and §6 for the full upgrade procedure.

set -eu  # no pipes in this script, pipefail unnecessary (and not POSIX)

MARKER_FILE="${NETBIRD_MIGRATION_MARKER:-/opt/netbird/migrations.allow}"

if [ "${NETBIRD_RUN_MIGRATIONS:-false}" = "true" ] || [ -f "$MARKER_FILE" ]; then
    echo "[netbird-entrypoint] migrations ENABLED for this startup (marker=$MARKER_FILE present or NETBIRD_RUN_MIGRATIONS=true)"
    unset NETBIRD_SKIP_MIGRATIONS
else
    echo "[netbird-entrypoint] migrations SKIPPED for this startup (set NETBIRD_RUN_MIGRATIONS=true or 'touch $MARKER_FILE' to enable)"
    export NETBIRD_SKIP_MIGRATIONS=true
fi

exec /usr/local/bin/netbird-mgmt "$@"

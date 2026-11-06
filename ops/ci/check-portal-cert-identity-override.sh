#!/usr/bin/env bash
# ops/ci/check-portal-cert-identity-override.sh — fail-closed guard so a NEW
# goatnet cannot silently inherit ANY part of the legacy portal identity.
#
# WHY: every per-goatnet portal identity value DEFAULTS to a legacy literal in
# the role defaults when a goatnet leaves it unset, so the parameterization is fail-OPEN — a goatnet
# that forgets to override any single value ships artifacts named for another
# goatnet: URI-SAN collisions (409s), or scripts that reference one CA filename
# while the tarball ships another.
#
# Rule: any group_vars/<env>/ that deploys the portal MUST explicitly set ALL of
# the per-goatnet identity values below, and none may equal the legacy default.
# nova-lan (a standalone LAN instance that keeps the legacy identity) is allowlisted.
#
# Class-A per docs/design/instance-variability-model.md §1.
# References: docs/design/portal-lexicon.md §"Cert identity layer".
#
# Full-tree (not diff-scoped): it asserts a property of the whole config set and
# passes today, so there are no grandfathered violations to scope around.
set -euo pipefail

# Environment discovery — read by ops/ci/check-env-coverage.sh, which asserts
# this glob still matches every group_vars/<env>/ dir. Keep it in sync with the
# `for dir in "$GV_ROOT"/*/` loop below.
# env-discovery: ops/ansible/group_vars/*/
GV_ROOT="${1:-ops/ansible/group_vars}"
# nova-lan is a standalone LAN instance that legitimately keeps the legacy identity.
ALLOWLIST_RE='^nova_lan_hosts$'
# A portal-deploying env sets at least one of these keys in its group_vars.
PORTAL_SIGNAL='^[[:space:]]*(portal_image|portal_cert_sign_[a-z_]+|portal_identity_model)[[:space:]]*:'

# The full per-goatnet identity set: "<group_vars key>|<legacy default>".
# Keep in lock-step with the legacy values in
# ops/ansible/roles/goat-zenoh-portal/defaults/main.yml.
IDENTITY_KEYS='
portal_cert_sign_uri_prefix|urn:goat-nova:vendor=
portal_cert_sign_cn_suffix|-trial-peer
portal_cert_sign_dns_suffix|nova.peer
portal_onboarding_root_cert_name|nova-ca-root.pem
portal_onboarding_packet_name|zenoh-trial-onboarding.md
portal_onboarding_tarball_stem|nova-onboarding
'

# extract_val <dir> <key> — last set value of <key> in <dir>, unquoted, comment-stripped.
# Trailing `|| true`: an unset key makes grep exit non-zero, which under
# `set -e`+pipefail would abort the script before the FAIL reason prints. Return
# empty + success instead so the caller's empty-check handles it.
extract_val() {
  command grep -rhE "^[[:space:]]*$2[[:space:]]*:" "$1" 2>/dev/null | tail -1 \
    | sed -E 's/^[^:]*:[[:space:]]*//; s/[[:space:]]*#.*$//; s/^["'"'"']//; s/["'"'"']$//' || true
}

fail=0
for dir in "$GV_ROOT"/*/; do
  env="$(basename "$dir")"
  command grep -rqE "$PORTAL_SIGNAL" "$dir" 2>/dev/null || continue   # not a portal env
  [[ "$env" =~ $ALLOWLIST_RE ]] && continue                            # nova-lan keeps the legacy identity

  while IFS='|' read -r key jbdefault; do
    [ -z "$key" ] && continue
    val="$(extract_val "$dir" "$key")"
    if [ -z "$val" ]; then
      echo "FAIL: '$env' deploys the portal but does not set $key."
      echo "      It would inherit the nova default '$jbdefault' into every minted bundle."
      fail=1
    elif [ "$val" = "$jbdefault" ]; then
      echo "FAIL: '$env' sets $key to the nova default '$jbdefault'; a non-nova goatnet must use its own."
      fail=1
    fi
  done <<< "$IDENTITY_KEYS"
done

if [ "$fail" -ne 0 ]; then
  echo
  echo "Per-goatnet portal identity is Class-A + fail-closed (docs/findings/F-212-*)."
  echo "Set ALL of these per goatnet, e.g. for an env <name>_hosts:"
  echo "  portal_cert_sign_uri_prefix:      \"urn:goat:<name>:org=\""
  echo "  portal_cert_sign_cn_suffix:       \"-peer\""
  echo "  portal_cert_sign_dns_suffix:      \"<name>.peer\""
  echo "  portal_onboarding_root_cert_name: \"<name>-ca-root.pem\""
  echo "  portal_onboarding_packet_name:    \"<name>-fabric-onboarding.md\""
  echo "  portal_onboarding_tarball_stem:   \"<name>-onboarding\""
  echo "See docs/design/portal-lexicon.md §\"Cert identity layer\"."
  exit 1
fi
echo "ok: every portal-deploying goatnet explicitly sets all 6 per-goatnet identity values"

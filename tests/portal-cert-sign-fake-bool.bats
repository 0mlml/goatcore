#!/usr/bin/env bats

# portal_cert_sign_fake is a bool that often arrives as a string. A command-line
# `-e portal_cert_sign_fake=false` is the string "false", which Jinja reads as
# true. A conditional that tests the bare variable then takes the fake-mint
# branch exactly when the operator forced real signing: the compose file passes
# --cert-sign-fake and skips the CA mounts while the role's tasks, which do
# filter the value, stage the real CA beside it. Every conditional in the portal
# role has to read the flag through `| bool`.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
ROLE="$REPO_ROOT/ops/ansible/roles/goat-zenoh-portal"

# Lines that TEST the flag: a `when:`, a list item under one, or a Jinja
# if/elif tag. Comments, the default, and fail_msg prose only name it.
conditional_uses() {
  grep -rnE '(^[[:space:]]*when:|^[[:space:]]*-[[:space:]]|\{%-?[[:space:]]*(if|elif)[[:space:]]).*portal_cert_sign_fake' \
    "$ROLE/tasks" "$ROLE/templates" | grep -vE '^[^:]+:[0-9]+:[[:space:]]*#'
}

@test "the portal role still tests portal_cert_sign_fake somewhere (the guard is not vacuous)" {
  run conditional_uses
  [ "$status" -eq 0 ]
  [ "$(printf '%s\n' "$output" | wc -l)" -ge 2 ]
}

@test "every conditional on portal_cert_sign_fake reads it through | bool" {
  bare="$(conditional_uses | grep -vE 'portal_cert_sign_fake[[:space:]]*\|[[:space:]]*bool' || true)"
  if [ -n "$bare" ]; then
    echo "bare truthiness test of portal_cert_sign_fake (the string \"false\" is true):" >&2
    printf '%s\n' "$bare" >&2
    return 1
  fi
}

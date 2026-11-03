# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Row K15. Class 5: OAuth2 clients declared in group_vars, written at one
# site, replicated.
# Pass beyond the common conditions: hydra_client equal at both sites.
#
# Driven by: the Hydra admin API at the hub (site A) while cut off, as
# ops/ansible/external-service.yml and the *-oidc-client roles do: a `GET`,
# then `POST /admin/clients` for a client that does not exist and a full
# `PUT /admin/clients/{id}` for one that does. Nothing runs at B.

EXPECT=""
CLIENT_BODY='{"client_name":"%s","grant_types":["authorization_code","refresh_token"],"response_types":["code"],
  "redirect_uris":["https://%s.harness.test/oauth2/callback"],"scope":"openid email profile",
  "token_endpoint_auth_method":"client_secret_basic"}'

seed() {
  # shellcheck disable=SC2059
  hapi POST /admin/clients "$(printf "$CLIENT_BODY" "harness-existing" "existing")" | jq -r .client_id > "$SCRATCH/existing"
}
write_a() {
  # shellcheck disable=SC2059
  hapi POST /admin/clients "$(printf "$CLIENT_BODY" "harness-new" "new")" | jq -r .client_id > "$SCRATCH/new"
  local id body; id=$(cat "$SCRATCH/existing")
  # shellcheck disable=SC2059
  body=$(printf "$CLIENT_BODY" "harness-existing-renamed" "existing" | jq -c --arg id "$id" '. + {client_id: $id}')
  hapi PUT "/admin/clients/$id" "$body" >/dev/null
}

check() {
  local new old; new=$(cat "$SCRATCH/new"); old=$(cat "$SCRATCH/existing")
  for s in a b; do
    expect_eq "$s: client created at the hub" "$(hy "$s" "SELECT count(*) FROM hydra_client WHERE id='$new';")" 1
    expect_eq "$s: client updated at the hub" "$(hy "$s" "SELECT client_name FROM hydra_client WHERE id='$old';")" harness-existing-renamed
  done
}

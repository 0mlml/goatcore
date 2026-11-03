#!/usr/bin/env bash
#
# spock-hub-directives.sh — carry out the hub's directives at this site (ADR 1205).
#
# The hub (the goatnet's highest-echelon site) acts on other sites only by
# inserting rows into goat_hub_directives in the netbird database. Every site
# runs this on a timer. For each directive it has not yet acted on:
#
#   1. the row must have been committed by the hub: goat_hub_directive_origin()
#      reads the replication origin of the commit, and the hub's node name
#      comes from this site's own configuration. Anything else is recorded as
#      refused_origin and counted in the alert metric;
#   2. sever_site / restore_site disable / enable this site's subscriptions
#      from the named node in every Spock database;
#   3. identity_* and transfer_owner are carried out through this site's
#      Kratos admin API, only when this site owns the identity
#      (metadata_admin.owner_site; none recorded = the hub). Otherwise the row
#      is recorded as not_owner and left to the owner.
#
# Inputs (from /etc/spock-hub-directives.env, or $HUB_DIRECTIVES_ENV_FILE):
#   HUB_DIRECTIVES_SITE       this site's Spock node name
#   HUB_DIRECTIVES_HUB        the hub's Spock node name (goatnet_hub_site)
#   HUB_DIRECTIVES_DBS        Spock databases, space-separated
#   HUB_DIRECTIVES_KRATOS     Kratos admin base URL; empty when no Kratos here
#   HUB_DIRECTIVES_TEXTFILE   Prometheus textfile output; empty to skip
#   HUB_DIRECTIVES_POSTGRES   the local Postgres container (postgres-a); psql runs
#                             inside it as postgres, as spock-mesh-peering does,
#                             so no database password is needed
#
# Exit: 0 when every pending directive was recorded, 1 on config error,
# 2 when a directive failed (recorded as failed; retried by hand, see README).

set -euo pipefail

ENV_FILE="${HUB_DIRECTIVES_ENV_FILE:-/etc/spock-hub-directives.env}"
[[ -r "$ENV_FILE" ]] || { echo "spock-hub-directives: $ENV_FILE not readable" >&2; exit 1; }
# shellcheck disable=SC1090
source "$ENV_FILE"
: "${HUB_DIRECTIVES_SITE:?missing}" "${HUB_DIRECTIVES_HUB:?missing}" "${HUB_DIRECTIVES_DBS:?missing}"
: "${HUB_DIRECTIVES_POSTGRES:?missing}"

psqlc() { docker exec -u postgres "$HUB_DIRECTIVES_POSTGRES" psql -U postgres -X -q -v ON_ERROR_STOP=1 "$@"; }
sql() { # sql <db> <statement>  -> unaligned, tuples only
  psqlc -At -d "$1" -c "$2"
}
lit() { # SQL string literal
  printf "'%s'" "${1//\'/\'\'}"
}
record() { # record <id> <verdict> <origin> <detail>
  sql netbird "INSERT INTO goat_hub_directive_applied (id, verdict, origin, detail)
               VALUES ($(lit "$1"), $(lit "$2"), $(lit "$3"), $(lit "$4")) ON CONFLICT (id) DO NOTHING;" >/dev/null
  echo "spock-hub-directives: $1 $2${4:+: $4}"
}
kratos() { # kratos <METHOD> <path> [json]
  curl -fsS -X "$1" "${HUB_DIRECTIVES_KRATOS%/}$2" -H 'Content-Type: application/json' ${3:+-d "$3"}
}

set_subs() { # set_subs <provider node> <enable|disable>
  local db sub n=0
  for db in $HUB_DIRECTIVES_DBS; do
    while read -r sub; do
      [[ -n "$sub" ]] || continue
      sql "$db" "SELECT spock.sub_$2($(lit "$sub"), true);" >/dev/null
      n=$((n+1))
    done < <(sql "$db" "SELECT s.sub_name FROM spock.subscription s JOIN spock.node n ON n.node_id = s.sub_origin
                         WHERE n.node_name = $(lit "$1");")
  done
  echo "$n subscription(s) $2d"
}

owner_of() { # owner_of <identity id>  -> owner node, or empty when the identity is not here
  local row
  row=$(kratos GET "/admin/identities/$1" 2>/dev/null) || return 0
  jq -r --arg hub "$HUB_DIRECTIVES_HUB" '.metadata_admin.owner_site // $hub' <<<"$row"
}

identity_act() { # identity_act <kind> <id> <body json>  -> detail on stdout
  local kind="$1" id="$2" body="$3" cur put
  case "$kind" in
    identity_edit)
      kratos PATCH "/admin/identities/$id" "$(jq -c .patch <<<"$body")" >/dev/null
      echo "patched" ;;
    identity_delete)
      kratos DELETE "/admin/identities/$id" >/dev/null
      echo "deleted" ;;
    identity_resave)
      # Re-issue the identity's rows with the same ids (goat_identity_resave,
      # in the Kratos revocation guard), so a site that discarded them at a
      # same-email collision receives them as fresh inserts.
      echo "identity_resave: $(sql kratos "SELECT goat_identity_resave($(lit "$id")::uuid);") row(s) re-issued" ;;
    transfer_owner)
      cur=$(kratos GET "/admin/identities/$id")
      put=$(jq -c --arg site "$(jq -r '.site // empty' <<<"$body")" '
        {schema_id, traits, state, metadata_public,
         metadata_admin: (if $site == "" then .metadata_admin
                          else ((.metadata_admin // {}) + {owner_site: $site}) end)}' <<<"$cur")
      kratos PUT "/admin/identities/$id" "$put" >/dev/null
      echo "$kind" ;;
  esac
}

failed=0 refused=0 applied=0 unknown=0
while IFS=$'\t' read -r id kind subject body origin; do
  [[ -n "$id" ]] || continue
  # No origin: the commit's timestamp and origin are not (yet) readable, so
  # nothing is decided; the directive stays pending for the next run.
  if [[ -z "$origin" ]]; then
    echo "spock-hub-directives: $id origin not readable; left pending"; unknown=$((unknown+1)); continue
  fi
  if [[ "$origin" != "$HUB_DIRECTIVES_HUB" ]]; then
    record "$id" refused_origin "$origin" "committed by $origin, not the hub $HUB_DIRECTIVES_HUB"
    refused=$((refused+1)); continue
  fi
  case "$kind" in
    sever_site|restore_site)
      if [[ "$subject" == "$HUB_DIRECTIVES_SITE" ]]; then
        record "$id" applied "$origin" "this site is the subject; nothing to change here"
      elif detail=$(set_subs "$subject" "$([[ "$kind" == sever_site ]] && echo disable || echo enable)" 2>&1); then
        record "$id" applied "$origin" "$kind $subject: $detail"; applied=$((applied+1))
      else
        record "$id" failed "$origin" "$detail"; failed=$((failed+1))
      fi ;;
    identity_*|transfer_owner)
      if [[ -z "${HUB_DIRECTIVES_KRATOS:-}" ]]; then
        record "$id" not_owner "$origin" "no Kratos at this site"; continue
      fi
      owner=$(owner_of "$subject")
      if [[ "$owner" != "$HUB_DIRECTIVES_SITE" ]]; then
        record "$id" not_owner "$origin" "owner is ${owner:-unknown (identity not here)}"; continue
      fi
      if detail=$(identity_act "$kind" "$subject" "$body" 2>&1); then
        record "$id" applied "$origin" "$detail"; applied=$((applied+1))
      else
        record "$id" failed "$origin" "$detail"; failed=$((failed+1))
      fi ;;
  esac
done < <(psqlc -At -F $'\t' -d netbird \
           -c "SELECT id, kind, subject, body::text, coalesce(origin, '') FROM goat_hub_directives_pending;")

if [[ -n "${HUB_DIRECTIVES_TEXTFILE:-}" ]]; then
  tmp=$(mktemp)
  {
    echo "# HELP goat_hub_directives_refused_total Directives this site refused because the hub did not commit them (ADR 1205)."
    echo "# TYPE goat_hub_directives_refused_total gauge"
    echo "goat_hub_directives_refused_total{site=\"$HUB_DIRECTIVES_SITE\"} $(sql netbird "SELECT count(*) FROM goat_hub_directive_applied WHERE verdict = 'refused_origin';")"
    echo "# HELP goat_hub_directives_failed_total Directives that failed at this site."
    echo "# TYPE goat_hub_directives_failed_total gauge"
    echo "goat_hub_directives_failed_total{site=\"$HUB_DIRECTIVES_SITE\"} $(sql netbird "SELECT count(*) FROM goat_hub_directive_applied WHERE verdict = 'failed';")"
    echo "# HELP goat_hub_directives_pending Directives this site has not yet decided on."
    echo "# TYPE goat_hub_directives_pending gauge"
    echo "goat_hub_directives_pending{site=\"$HUB_DIRECTIVES_SITE\"} $(sql netbird "SELECT count(*) FROM goat_hub_directives_pending;")"
    echo "# HELP goat_hub_directives_last_run_seconds Unix time of the last completed run."
    echo "# TYPE goat_hub_directives_last_run_seconds gauge"
    echo "goat_hub_directives_last_run_seconds{site=\"$HUB_DIRECTIVES_SITE\"} $(date +%s)"
  } > "$tmp"
  install -m 0644 "$tmp" "$HUB_DIRECTIVES_TEXTFILE"; rm -f "$tmp"
fi

echo "spock-hub-directives: $applied applied, $refused refused, $failed failed, $unknown left pending"
[[ "$failed" == 0 ]] || exit 2

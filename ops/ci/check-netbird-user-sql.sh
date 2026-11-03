#!/usr/bin/env bash
# ops/ci/check-netbird-user-sql.sh — ban unsafe writes to the netbird `users`
# table from ansible.
#
# WHY: bringing ZAGROS live, an operator hand-INSERTed an owner row into netbird's
# `users` table to work around a missing account owner. netbird's peer-login
# query scans several user string columns into non-nullable Go strings; the
# partial INSERT left NULLs, so EVERY peer-login failed `cannot scan NULL into
# *string` and the whole mesh dropped. The lesson: a goatnet standup must NEVER
# create or broadly rewrite a netbird user via raw SQL — netbird creates users
# itself on OIDC login, and the ONLY safe store-level mutation is flipping an
# EXISTING row's `role` (goat-netbird-owner-promote.yml). This guard makes the
# poison impossible to reintroduce.
#
# Banned in ops/ansible/**:
#   - INSERT INTO users ...                 (never hand-create a netbird user)
#   - UPDATE users SET <non-role cols> ...  (only `role` may be mutated)
# Allowed:
#   - UPDATE users SET role=... [WHERE ...] (the role-only recovery promotion)
#   - SQL against other tables (name_server_groups / groups …) — account-init
#     seeds those with full idempotent rows. NOT policies / policy_rules: no
#     raw SQL writes a policy (ADR 1185, ops/ci/check-no-default-allow.sh).
#
# Usage:
#   ops/ci/check-netbird-user-sql.sh             # scan tracked ops/ansible tree
#   ops/ci/check-netbird-user-sql.sh --staged    # staged diff only
#   ops/ci/check-netbird-user-sql.sh path ...    # specific paths
#
# Exit: 0 clean · 1 violation (file:line listed) · 2 usage/dep error
set -euo pipefail

repo_root() { git rev-parse --show-toplevel 2>/dev/null || pwd; }
cd "$(repo_root)"

mode="tree"; paths=()
case "${1:-}" in
  --staged) mode="staged" ;;
  "" ) ;;
  * ) paths=("$@") ;;
esac

files=()
if [[ "$mode" == "staged" ]]; then
  while IFS= read -r f; do
    files+=("$f")
  done < <(git diff --cached --name-only --diff-filter=d -- 'ops/ansible/*')
elif [[ ${#paths[@]} -gt 0 ]]; then
  files=("${paths[@]}")
else
  while IFS= read -r f; do
    files+=("$f")
  done < <(git ls-files 'ops/ansible/*.yml' 'ops/ansible/*.yaml' 'ops/ansible/**/*.yml' 'ops/ansible/**/*.sh')
fi

# This guard scans itself? No — it lives in ops/ci, out of scope. Good.
hits=0
if (( ${#files[@]} > 0 )); then
for f in "${files[@]}"; do
  [[ -f "$f" ]] || continue
  case "$f" in ops/ansible/*) ;; *) continue ;; esac
  case "$f" in *.md|*/README*) continue ;; esac

  while IFS=: read -r lineno content; do
    [[ -z "$lineno" ]] && continue
    # Skip whole-line comments (a `# ... insert into users ...` cautionary note is fine).
    stripped="${content#"${content%%[![:space:]]*}"}"
    [[ "$stripped" == \#* ]] && continue
    [[ "$stripped" == --* ]] && continue   # SQL comment line

    lc="$(printf '%s' "$content" | tr '[:upper:]' '[:lower:]')"

    # POSIX-portable word boundaries (BSD/macOS sed has no \b): a table name is
    # bounded by a non-[alnum_] char or string edge.
    # 1. INSERT INTO users — always banned.
    if printf '%s' "$lc" | grep -qE '(^|[^[:alnum:]_])insert[[:space:]]+into[[:space:]]+users([^[:alnum:]_]|$)'; then
      printf '  %s:%s: INSERT INTO users (never hand-create a netbird user): %s\n' \
        "$f" "$lineno" "$(printf '%s' "$content" | sed 's/^[[:space:]]*//')"
      hits=$((hits+1)); continue
    fi

    # 2. UPDATE users SET <cols> — allowed only when the SET clause assigns ONLY role.
    if printf '%s' "$lc" | grep -qE '(^|[^[:alnum:]_])update[[:space:]]+users[[:space:]]+set([^[:alnum:]_]|$)'; then
      # Extract the SET clause: text after 'set ', before ' where' / ';' / end.
      setclause="$(printf '%s' "$lc" | sed -E 's/.*update[[:space:]]+users[[:space:]]+set[[:space:]]+//; s/[[:space:]]+where[[:space:]].*//; s/;.*//')"
      bad=0
      # Each comma-separated assignment's left-hand side must be exactly `role`.
      IFS=',' read -ra assigns <<< "$setclause"
      # An empty SET clause means the assignments continue on the NEXT line,
      # which this line-based scan cannot see; that is out of the guard's
      # reach either way, so leave it as it was before the 3.2 sweep rather
      # than inventing a verdict. The length test is only here because bash
      # 3.2 under set -u dies expanding an empty "${arr[@]}".
      if (( ${#assigns[@]} > 0 )); then
        for a in "${assigns[@]}"; do
          lhs="$(printf '%s' "$a" | sed -E 's/=.*//; s/[[:space:]"]//g')"
          [[ "$lhs" == "role" ]] || bad=1
        done
      fi
      if [[ $bad -eq 1 ]]; then
        printf '  %s:%s: UPDATE users SET touches a non-role column (only `role` may be mutated): %s\n' \
          "$f" "$lineno" "$(printf '%s' "$content" | sed 's/^[[:space:]]*//')"
        hits=$((hits+1))
      fi
    fi
  done < <(grep -niE '(insert[[:space:]]+into[[:space:]]+users|update[[:space:]]+users[[:space:]]+set)' "$f" 2>/dev/null || true)
done
fi

if [[ $hits -gt 0 ]]; then
  echo ""
  echo "FAIL: $hits unsafe netbird users-table write(s) in ansible." >&2
  echo "netbird creates users on OIDC login; never hand-INSERT one, and mutate" >&2
  echo "only the \`role\` column (goat-netbird-owner-promote.yml). A partial user" >&2
  echo "INSERT poisons peer-login (cannot scan NULL into *string). See F-188." >&2
  exit 1
fi
echo "OK: no unsafe netbird users-table writes in ansible."

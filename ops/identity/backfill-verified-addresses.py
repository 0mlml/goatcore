#!/usr/bin/env python3
"""Mark the email address verified on identities the console minted by hand.

Kratos v1.3.1 verifies an address only when a SELF-SERVICE recovery code is
redeemed. The admin-minted welcome link the id-console issues never does, so
an identity created through the console's user-create or invite-accept path
carries `verifiable_addresses[].verified: false` for life, and every OIDC
relying party that checks `email_verified` (oauth2-proxy by default)
refuses it. The onboarding playbook and the site
bootstrap import the address verified at create time; the console now does
too. This script closes the gap for identities minted before that fix.

Runs ON the mgmt host (Kratos admin API on 127.0.0.1:4434 by default).
Stdlib only, no pip installs on the Thor. Dry-run by default; --apply writes.

Selection, deliberately narrow: by default only identities with an enrolled
first-factor credential: a password hash or a passwordless WebAuthn key,
both with a login identifier. Empty credential placeholders, second-factor-only
keys and password-migration hooks alone do not qualify. For an identity minted by the console's
user-create form or the onboarding playbook that link was mailed to the
address, so the person has read that mailbox. For a login minted through the
console's INVITE-ACCEPT page the link was shown on screen to whoever held the
invite and the address was typed by that person, so a redeemed link proves
nothing about the mailbox: list those identities from the audit trail
(`accept_invite` events) and pass each as --skip EMAIL, or confirm the address
with the person first. --all widens to every unverified address, on the
operator's say-so. --only EMAIL limits the run to one identity for a trial.

Examples:
  python3 backfill-verified-addresses.py --verify          # counts, exit 1 if any unverified
  python3 backfill-verified-addresses.py                   # dry-run: show planned changes
  python3 backfill-verified-addresses.py --only a@b.test --apply   # trial on one identity
  python3 backfill-verified-addresses.py --skip c@d.test --apply   # leave an invite-accept login alone
  python3 backfill-verified-addresses.py --apply           # mark every redeemed identity
"""

import argparse
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_ADMIN = "http://127.0.0.1:4434"
PAGE_SIZE = 500
FIRST_FACTORS = ("password", "webauthn")


def _req(method, url, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Accept", "application/json")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=30) as resp:
        raw = resp.read()
        return (json.loads(raw) if raw else None), dict(resp.headers)


def _next_link(headers):
    """The rel="next" path from Kratos's keyset-paginated Link header, or None."""
    link = headers.get("Link") or headers.get("link") or ""
    for part in link.split(","):
        m = re.search(r"<([^>]+)>\s*;\s*rel=\"?next\"?", part)
        if m:
            return m.group(1)
    return None


def enrolled_first_factors(credentials):
    """Reduce Kratos credential configs to enrolled first-factor type names.

    Kratos v1.3.1 initializes WebAuthn placeholders before any key is enrolled.
    Its first-factor counter requires a passwordless key and a login identifier;
    a second-factor-only key cannot establish a usable first factor. Password
    migration hooks are excluded: they can permit login without an enrolled hash.
    No hashes, identifiers or key material survive this reduction.
    """
    if not isinstance(credentials, dict):
        return []
    enrolled = []
    for kind in FIRST_FACTORS:
        entry = credentials.get(kind)
        if not isinstance(entry, dict) or entry.get("type") != kind:
            continue
        identifiers = entry.get("identifiers")
        if not isinstance(identifiers, list) or not any(
            isinstance(value, str) and value.strip() for value in identifiers
        ):
            continue
        config = entry.get("config")
        if not isinstance(config, dict):
            continue
        if kind == "password":
            hashed = config.get("hashed_password")
            if isinstance(hashed, str) and hashed.strip():
                enrolled.append(kind)
        else:
            keys = config.get("credentials")
            if isinstance(keys, list) and any(
                isinstance(key, dict) and key.get("is_passwordless") is True for key in keys
            ):
                enrolled.append(kind)
    return enrolled


def list_identities(admin):
    """Every identity, retaining only enrolled first-factor credential types.

    include_credential makes Kratos return the credential config too, and for
    password that config carries the hash. Evaluate enrollment before discarding
    the configs; the returned identities retain only nonsecret type names.
    """
    out = []
    query = "&".join(f"include_credential={c}" for c in FIRST_FACTORS)
    url = f"{admin}/admin/identities?page_size={PAGE_SIZE}&{query}"
    seen_tokens = set()
    while url:
        batch, headers = _req("GET", url)
        for identity in batch or []:
            identity["credentials"] = enrolled_first_factors(identity.get("credentials"))
            out.append(identity)
        nxt = _next_link(headers)
        if not nxt:
            break
        url = urllib.parse.urljoin(admin, nxt)
        tok = urllib.parse.parse_qs(urllib.parse.urlparse(url).query).get("page_token", [""])[0]
        if tok in seen_tokens or not batch or len(batch) < PAGE_SIZE:
            break
        seen_tokens.add(tok)
    return out


def email_address_index(identity):
    """The index of the identity's email verifiable address, or None."""
    email = str((identity.get("traits") or {}).get("email") or "").strip().lower()
    addresses = identity.get("verifiable_addresses") or []
    for index, address in enumerate(addresses):
        if address.get("via") == "email" and str(address.get("value", "")).lower() == email:
            return index
    for index, address in enumerate(addresses):
        if address.get("via") == "email":
            return index
    return None


def has_first_factor(identity):
    # list_identities has reduced configs to enrolled types, not mere map keys.
    return any(kind in FIRST_FACTORS for kind in identity.get("credentials") or [])


def plan(identities, only=None, mark_all=False, skip=()):
    """Return (actions, skipped). An action marks one address verified.

    skip names addresses the operator has ruled out, case-insensitively: the
    invite-accept logins whose welcome link was never mailed (see the module
    docstring). A skipped identity is reported, never silently dropped.
    """
    actions, skipped = [], []
    wanted = only.strip().lower() if only else None
    ruled_out = {address.strip().lower() for address in skip if address.strip()}
    for identity in identities:
        email = str((identity.get("traits") or {}).get("email") or "").strip().lower()
        ident = {"id": identity.get("id"), "email": email or "?"}
        if wanted and email != wanted:
            continue
        index = email_address_index(identity)
        if index is None:
            skipped.append({**ident, "why": "no email verifiable address on the identity"})
            continue
        address = identity["verifiable_addresses"][index]
        if address.get("verified"):
            skipped.append({**ident, "why": "already verified"})
            continue
        if email in ruled_out:
            skipped.append({**ident, "why": "--skip: the welcome link was not mailed to this address"})
            continue
        if not mark_all and not has_first_factor(identity):
            skipped.append(
                {**ident, "why": "no enrolled first-factor credential; welcome link not redeemed (use --all)"}
            )
            continue
        actions.append({**ident, "index": index, "credentials": identity.get("credentials") or []})
    return actions, skipped


def patch_verified(admin, action):
    """JSON Patch the one address. Kratos forbids patching /credentials and
    /id; verifiable_addresses is patchable and survives the schema pass
    because the value still matches the email trait."""
    base = f"/verifiable_addresses/{action['index']}"
    body = [
        {"op": "replace", "path": f"{base}/verified", "value": True},
        {"op": "replace", "path": f"{base}/status", "value": "completed"},
    ]
    url = f"{admin}/admin/identities/{urllib.parse.quote(action['id'])}"
    result, _ = _req("PATCH", url, body)
    return result


def read_back(admin, identity_id):
    result, _ = _req("GET", f"{admin}/admin/identities/{urllib.parse.quote(identity_id)}")
    index = email_address_index(result)
    return bool(index is not None and result["verifiable_addresses"][index].get("verified"))


def verify(identities):
    unverified = []
    for identity in identities:
        index = email_address_index(identity)
        if index is None or not identity["verifiable_addresses"][index].get("verified"):
            unverified.append(str((identity.get("traits") or {}).get("email") or identity.get("id")))
    return unverified


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--admin", default=DEFAULT_ADMIN, help="Kratos admin base URL")
    parser.add_argument("--apply", action="store_true", help="write; the default is a dry run")
    parser.add_argument("--all", dest="mark_all", action="store_true", help="also identities with no credential yet")
    parser.add_argument("--only", help="limit to one identity by email (a trial run)")
    parser.add_argument(
        "--skip",
        action="append",
        default=[],
        metavar="EMAIL",
        help="leave this identity alone (repeatable); for invite-accept logins, whose link was never mailed",
    )
    parser.add_argument("--verify", action="store_true", help="report counts; exit 1 if any address is unverified")
    args = parser.parse_args(argv)

    try:
        identities = list_identities(args.admin)
    except (urllib.error.URLError, OSError, ValueError) as exc:
        print(f"kratos admin unreachable at {args.admin}: {exc}", file=sys.stderr)
        return 2

    if args.verify:
        unverified = verify(identities)
        print(f"{len(identities)} identities; {len(unverified)} with an unverified email address")
        for email in unverified:
            print(f"  unverified  {email}")
        return 1 if unverified else 0

    actions, skipped = plan(identities, only=args.only, mark_all=args.mark_all, skip=args.skip)
    for row in skipped:
        print(f"skip   {row['email']:<44} {row['why']}")
    for action in actions:
        state = "mark" if args.apply else "would mark"
        print(f"{state:<6} {action['email']:<44} credentials {','.join(action['credentials']) or '-'}")
    if not args.apply:
        print(f"{len(actions)} to mark, {len(skipped)} skipped; dry run, nothing written")
        return 0

    failures = 0
    for action in actions:
        try:
            patch_verified(args.admin, action)
            ok = read_back(args.admin, action["id"])
        except (urllib.error.URLError, OSError, ValueError) as exc:
            print(f"error  {action['email']:<44} {exc}", file=sys.stderr)
            failures += 1
            continue
        if not ok:
            print(f"error  {action['email']:<44} patched but reads back unverified", file=sys.stderr)
            failures += 1
    print(f"{len(actions) - failures} marked, {failures} failed, {len(skipped)} skipped")
    return 2 if failures else 0


if __name__ == "__main__":
    sys.exit(main())

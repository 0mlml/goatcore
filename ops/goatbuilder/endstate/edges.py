"""edges.py — the zero-trust threads a service needs, graded (ADR 1206).

Three edge kinds, each with its positive check and a paired negative:

  zenoh    the router's live access_control grants a fabric client what its catalog row
           declares it publishes or subscribes, its certificate chains to the goatnet CA
           and its CN names an ACL subject; an operator-audit key and a key no service
           declares are refused to it.
  netbird  the site's declared NetBird matrix admits the paths peers use to reach the
           service (its `mesh.reach` ports, tcp/443 for a front door) and refuses its
           `mesh.internal` ports to participant groups; the live account holds every
           declared rule that admits the path, and nothing undeclared (ADR 1185).
  secrets  every secret the service's deploy play templates is available in the
           declared secrets posture (ADR 1204). Names only: no value is read.

Pure functions here grade facts; `endstate.py observe` gathers them. A fact that could
not be read grades UNKNOWN, never PASS.
"""
import json
import os
import re
import urllib.request

PASS, FAIL, UNKNOWN, UNOBSERVED = "PASS", "FAIL", "UNKNOWN", "UNOBSERVED"

# Keys every fabric client must be REFUSED: the operator-action audit trail and a
# root nobody declares. A grant that covers either is an over-grant.
ZENOH_NEGATIVES = ["ops/audit/operator/endstate-probe", "endstate/undeclared/probe"]
PARTICIPANT_GROUPS = ["OBSERVER", "DEVELOPER", "MONITOR"]


def _g(status, why):
    return {"status": status, "why": why}


# --- JSON5 (the router config) ---------------------------------------------------------
# Source text, so observe() can run the same parser on the host as the self-test runs here.

JSON5_LITE = r'''
def json5_lite(text):
    """The subset of JSON5 zenohd's config uses: // and /* */ comments, unquoted keys,
    single-quoted strings, trailing commas. Returns the parsed object."""
    import json, re
    out, i, n, last = [], 0, len(text), ""
    while i < n:
        c = text[i]
        if c in "\"'":
            q, j, buf = c, i + 1, []
            while j < n and text[j] != q:
                if text[j] == "\\" and j + 1 < n:
                    buf.append(text[j:j + 2]); j += 2; continue
                buf.append(text[j]); j += 1
            s = "".join(buf)
            if q == "'":
                s = s.replace('"', '\\"').replace("\\'", "'")
            out.append('"' + s + '"'); last = '"'; i = j + 1; continue
        if text.startswith("//", i):
            j = text.find("\n", i); i = n if j < 0 else j; continue
        if text.startswith("/*", i):
            j = text.find("*/", i + 2); i = n if j < 0 else j + 2; continue
        if c.isalpha() or c == "_":
            j = i
            while j < n and (text[j].isalnum() or text[j] in "_$"):
                j += 1
            word = text[i:j]
            k = j
            while k < n and text[k] in " \t\r\n":
                k += 1
            if k < n and text[k] == ":" and last in ("{", ",", ""):
                out.append('"' + word + '"')
            else:
                out.append(word)
            last = word[-1]; i = j; continue
        out.append(c)
        if not c.isspace():
            last = c
        i += 1
    js = re.sub(r",(\s*[}\]])", r"\1", "".join(out))
    return json.loads(js)
'''
exec(JSON5_LITE)  # defines json5_lite here too


# --- zenoh key expressions and the ACL decision ---------------------------------------

def _chunks_match(pat, key):
    if not pat:
        return not key
    p, rest = pat[0], pat[1:]
    if p == "**":
        if _chunks_match(rest, key):
            return True
        return bool(key) and not key[0].startswith("@") and _chunks_match(pat, key[1:])
    if not key:
        return False
    k = key[0]
    if k.startswith("@") or p.startswith("@"):
        ok = p == k
    elif p == "*":
        ok = True
    elif "$*" in p:
        ok = re.fullmatch(re.escape(p).replace(r"\$\*", ".*"), k) is not None
    else:
        ok = p == k
    return ok and _chunks_match(rest, key[1:])


def ke_includes(pattern, key):
    """Does key expression `pattern` include the concrete key `key` (zenoh 1.x rules:
    `*` one chunk, `**` zero or more, `$*` inside a chunk; a verbatim `@` chunk is matched
    only by itself)."""
    return _chunks_match(pattern.split("/"), key.split("/"))


def _subject_matches(subj, cn):
    if subj.get("usernames"):
        return False
    cns = subj.get("cert_common_names")
    if cns is not None and len(cns) == 0:
        return False   # present and empty: matches nobody (fail-closed)
    if cns and cn not in cns:
        return False
    lp = subj.get("link_protocols")
    if lp and "tls" not in lp:
        return False
    return True


def zenoh_decide(acl, cn, message, flow, key):
    """allow | deny for a client with certificate CN `cn`: within one subject a matching
    deny wins over that subject's allows; across subjects the first allow wins; with no
    decision, default_permission (zenohd 1.9.0 authorization.rs)."""
    rules = {r.get("id"): r for r in acl.get("rules") or []}
    subjects = {s.get("id"): s for s in acl.get("subjects") or []}
    by_subject = {}
    for pol in acl.get("policies") or []:
        for sid in pol.get("subjects") or []:
            by_subject.setdefault(sid, []).extend(pol.get("rules") or [])
    matched = [sid for sid, s in subjects.items() if _subject_matches(s, cn)]
    for sid in matched:
        allow = deny = False
        for rid in by_subject.get(sid, []):
            r = rules.get(rid) or {}
            if message not in (r.get("messages") or []):
                continue
            if flow not in (r.get("flows") or ["ingress", "egress"]):
                continue
            if not any(ke_includes(k, key) for k in r.get("key_exprs") or []):
                continue
            if r.get("permission") == "deny":
                deny = True
            else:
                allow = True
        if allow and not deny:
            return "allow", sid
    return (acl.get("default_permission") or "deny"), None


def prune_acl(acl, cns):
    """Keep only what bears on the given CNs (the slot pool can carry thousands of subjects)."""
    keep_s = [s for s in acl.get("subjects") or [] if any(_subject_matches(s, cn) for cn in cns)]
    ids = {s.get("id") for s in keep_s}
    keep_p = [p for p in acl.get("policies") or [] if ids & set(p.get("subjects") or [])]
    rids = {r for p in keep_p for r in p.get("rules") or []}
    return {"enabled": acl.get("enabled"), "default_permission": acl.get("default_permission"),
            "subjects": keep_s, "policies": keep_p,
            "rules": [r for r in acl.get("rules") or [] if r.get("id") in rids]}


def zenoh_edges(spec, fact):
    """spec: the service's catalog `zenoh` block, rendered; fact: observe()'s zenoh facts
    for this service ({cn, not_after, verify, err} or None) plus the router's pruned acl."""
    edges = []
    acl = (fact or {}).get("acl")
    cl = (fact or {}).get("client")
    if not cl or cl.get("err"):
        why = (cl or {}).get("err") or "the client's certificate could not be read"
        edges.append({"edge": "zenoh-cert", **_g(UNKNOWN, why)})
        for k in spec.get("put", []) + spec.get("sub", []):
            edges.append({"edge": f"zenoh-grant {k}", **_g(UNKNOWN, why)})
        return edges
    cn = cl.get("cn") or ""
    if cl.get("verify") != "OK":
        edges.append({"edge": "zenoh-cert", **_g(FAIL, f"certificate CN={cn} does not chain to the goatnet CA: {cl.get('verify')}")})
    elif cl.get("expired"):
        edges.append({"edge": "zenoh-cert", **_g(FAIL, f"certificate CN={cn} expired {cl.get('not_after')}")})
    elif acl is None:
        edges.append({"edge": "zenoh-cert", **_g(UNKNOWN, (fact or {}).get("acl_err") or "the router's access_control could not be read")})
    elif not any(_subject_matches(s, cn) and s.get("cert_common_names") for s in acl.get("subjects") or []):
        edges.append({"edge": "zenoh-cert", **_g(FAIL, f"no ACL subject names CN={cn}: the router treats it as an anonymous TLS peer")})
    else:
        edges.append({"edge": "zenoh-cert", **_g(PASS, f"CN={cn} chains to the goatnet CA, valid to {cl.get('not_after')}, named by an ACL subject")})
    if acl is None:
        for k in spec.get("put", []) + spec.get("sub", []):
            edges.append({"edge": f"zenoh-grant {k}", **_g(UNKNOWN, (fact or {}).get("acl_err") or "the router's access_control could not be read")})
        return edges
    if acl.get("enabled") is False:
        edges.append({"edge": "zenoh-acl", **_g(FAIL, "the router runs with access_control disabled: every peer may do anything")})
        return edges
    for k in spec.get("put", []):
        d, sid = zenoh_decide(acl, cn, "put", "ingress", k)
        edges.append({"edge": f"zenoh-grant put {k}", **(_g(PASS, f"allowed (subject {sid})") if d == "allow"
                      else _g(FAIL, f"the router refuses CN={cn} a put on {k}: its data never lands"))})
    for k in spec.get("sub", []):
        d, sid = zenoh_decide(acl, cn, "declare_subscriber", "ingress", k)
        edges.append({"edge": f"zenoh-grant sub {k}", **(_g(PASS, f"allowed (subject {sid})") if d == "allow"
                      else _g(FAIL, f"the router refuses CN={cn} a subscription on {k}"))})
    if spec.get("put"):
        for k in ZENOH_NEGATIVES:
            d, sid = zenoh_decide(acl, cn, "put", "ingress", k)
            edges.append({"edge": f"zenoh-denied put {k}", **(_g(PASS, "refused, as it must be") if d != "allow"
                          else _g(FAIL, f"over-granted: CN={cn} may put on {k} (subject {sid})"))})
    return edges


# --- NetBird paths ---------------------------------------------------------------------

def _port_ok(ports, port):
    if not ports:
        return True
    for p in ports:
        p = str(p)
        if "-" in p:
            a, b = p.split("-", 1)
            if a.isdigit() and b.isdigit() and int(a) <= port <= int(b):
                return True
        elif p == str(port):
            return True
    return False


def netbird_admitting(rules, src, dst, proto, port):
    """Names of the declared accept rules that admit src -> dst on proto/port."""
    out = []
    for r in rules or []:
        if r.get("action", "accept") != "accept" or r.get("enabled") is False:
            continue
        rp = r.get("protocol", "all")
        if rp not in ("all", proto):
            continue
        if not _port_ok(r.get("ports"), port):
            continue
        s, d = r.get("sources") or [], r.get("destinations") or []
        if (src in s and dst in d) or (r.get("bidirectional") and src in d and dst in s):
            out.append(r["name"])
    return out


def netbird_edges(mesh, fact):
    """mesh: {reach: [(proto, port)], from: [GROUP], internal: [(proto, port)], dest: GROUP};
    fact: {rules: declared rules or None, why, live: {name: enabled} or None, live_why}."""
    edges = []
    rules = (fact or {}).get("rules")
    live = (fact or {}).get("live")
    for proto, port in mesh.get("reach", []):
        for src in mesh.get("from") or ["PLATFORM-ADMIN"]:
            e = f"netbird-path {src}→{mesh['dest']} {proto}/{port}"
            if rules is None:
                edges.append({"edge": e, **_g(UNKNOWN, (fact or {}).get("why") or "no declared ACL for this site")})
                continue
            names = netbird_admitting(rules, src, mesh["dest"], proto, port)
            if not names:
                edges.append({"edge": e, **_g(FAIL, f"the declared matrix admits no {src} peer to {mesh['dest']} on {proto}/{port}: the service is unreachable over the mesh")})
            elif live is None:
                edges.append({"edge": e, **_g(UNKNOWN, f"declared by `{names[0]}`; the live account was not read: {(fact or {}).get('live_why')}")})
            elif not any(live.get(n) for n in names):
                edges.append({"edge": e, **_g(FAIL, f"declared by `{names[0]}`, but no enabled live policy carries it")})
            else:
                edges.append({"edge": e, **_g(PASS, f"`{next(n for n in names if live.get(n))}` (declared, live)")})
    for proto, port in mesh.get("internal", []):
        for src in PARTICIPANT_GROUPS:
            e = f"netbird-closed {src}→{mesh['dest']} {proto}/{port}"
            if rules is None:
                edges.append({"edge": e, **_g(UNKNOWN, (fact or {}).get("why") or "no declared ACL for this site")})
                continue
            names = netbird_admitting(rules, src, mesh["dest"], proto, port)
            edges.append({"edge": e, **(_g(PASS, "refused by the declared matrix") if not names
                          else _g(FAIL, f"`{names[0]}` lets a {src} peer reach {proto}/{port}, which no participant should"))})
    return edges


def netbird_site_edge(fact):
    """The account holds only the declared ACL and no All-group rule (ADR 1185)."""
    live = (fact or {}).get("live")
    if live is None:
        return {"edge": "netbird-acl declared-only", **_g(UNKNOWN, (fact or {}).get("live_why") or "the live account was not read")}
    extra = (fact or {}).get("undeclared") or []
    allall = (fact or {}).get("all_group") or []
    if allall:
        return {"edge": "netbird-acl declared-only", **_g(FAIL, f"an enabled All-group rule: {', '.join(allall)}")}
    if extra:
        return {"edge": "netbird-acl declared-only", **_g(FAIL, f"enabled policies the matrix does not declare: {', '.join(sorted(extra))}")}
    return {"edge": "netbird-acl declared-only", **_g(PASS, f"{sum(1 for v in live.values() if v)} enabled policies, all declared")}


def netbird_live(base_url, pat_env):
    """{name: enabled} plus the All-group offenders, read-only (GET /api/policies).
    `pat_env` names the environment variable holding the PAT; the value is read here,
    used in the one request header, and never returned or printed."""
    req = urllib.request.Request(base_url.rstrip("/") + "/api/policies",
                                 headers={"Authorization": "Token " + os.environ.get(pat_env, ""), "Accept": "application/json"})
    with urllib.request.urlopen(req, timeout=15) as r:
        pols = json.load(r)
    live, allg = {}, []
    for p in pols:
        live[p.get("name")] = bool(p.get("enabled"))
        for rule in p.get("rules") or []:
            names = {g.get("name") for side in ("sources", "destinations") for g in (rule.get(side) or []) if isinstance(g, dict)}
            if p.get("enabled") and rule.get("enabled", True) and rule.get("action", "accept") == "accept" and "All" in names:
                allg.append(p.get("name"))
    return live, sorted(set(allg))


# --- secrets ---------------------------------------------------------------------------

LOOKUP = re.compile(r"""^([a-z][a-z0-9_]*):\s.*lookup\(\s*['"]env['"]\s*,\s*['"]([A-Z][A-Z0-9_]*)['"]""")
WORD = re.compile(r"\b[a-z][a-z0-9_]+\b")


def env_bindings(repo, group):
    out = {}
    d = os.path.join(repo, "ops/ansible/group_vars", group)
    if not os.path.isdir(d):
        return out
    for fn in sorted(os.listdir(d)):
        if fn.endswith((".yml", ".yaml")):
            try:
                with open(os.path.join(d, fn), encoding="utf-8", errors="replace") as fh:
                    for line in fh:
                        m = LOOKUP.match(line)
                        if m:
                            out[m.group(1)] = m.group(2)
            except OSError:
                continue
    return out


def play_words(repo, play, roles):
    """Every lowercase identifier the play (one import_playbook hop) and the roles reference."""
    paths = [play]
    try:
        with open(play, encoding="utf-8", errors="replace") as fh:
            for line in fh:
                i = line.find("import_playbook:")
                if i >= 0:
                    paths.append(os.path.join(os.path.dirname(play), line[i + len("import_playbook:"):].strip()))
    except OSError:
        return play_words_of(paths, roles, repo)  # an unreadable play adds no import hop
    return play_words_of(paths, roles, repo)


def play_words_of(paths, roles, repo):
    paths = list(paths)
    for n in roles:
        for root, _d, files in os.walk(os.path.join(repo, "ops/ansible/roles", n)):
            paths += [os.path.join(root, f) for f in files]
    words = set()
    for p in paths:
        try:
            with open(p, encoding="utf-8", errors="replace") as fh:
                words |= set(WORD.findall(fh.read()))
        except OSError:
            continue
    return words


def deploy_inputs_edge(fact):
    """fact: {play, needed: {var: ENV}, have: [ENV] or None, store, err}."""
    if not fact:
        return {"edge": "secrets", **_g(UNKNOWN, "not derived")}
    if fact.get("err"):
        return {"edge": "secrets", **_g(UNKNOWN, fact["err"])}
    if not fact.get("play"):
        return {"edge": "secrets", **_g(UNOBSERVED, "no deploy play is known for it (catalog `play:`), so its secrets cannot be derived")}
    needed = fact.get("needed") or {}
    if not needed:
        return {"edge": "secrets", **_g(PASS, f"{os.path.basename(fact['play'])} templates no secret from the operator's environment")}
    have = fact.get("have")
    if have is None:
        return {"edge": "secrets", **_g(UNKNOWN, f"the {fact.get('store') or 'declared'} posture could not be read")}
    missing = sorted(env for env in needed.values() if env not in have)
    if missing:
        return {"edge": "secrets", **_g(FAIL, f"{len(missing)} of {len(needed)} not available in the {fact.get('store')} posture: {', '.join(missing)}")}
    legacy = fact.get("legacy") or []
    if legacy:
        # Supplied, so the deploy works; ADR 1186 retires plaintext files, so say which.
        return {"edge": "secrets", **_g(PASS, f"all {len(needed)} available; {len(legacy)} from the legacy plaintext .envrc.local.d/*.envrc, not yet a {fact.get('store')} reference: {', '.join(legacy)}")}
    return {"edge": "secrets", **_g(PASS, f"all {len(needed)} available in the {fact.get('store')} posture")}

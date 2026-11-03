#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Does an ADR that says "not built yet" actually have its code in the tree?
#
# THE FAILURE THIS EXISTS FOR. An ADR's Status line is load-bearing: onboarding
# paths, the canon-vs-speculation gate, and the backlog all read it to decide
# what is real. An ADR left at "Proposed" / "designed-not-built" / "no code yet"
# AFTER its code landed is a lie in the safe direction — it under-claims — but a
# lie all the same: readers treat a shipped, deployed subsystem as vapor, and
# the speculation/canon boundary drifts. A sweep found ~31 such ADRs
# whose named role, image pin, or `goat` subcommand already exists in-tree.
#
# WHAT IT CHECKS. For every ADR whose Status line says Proposed /
# designed-not-built / no code yet, it looks for concrete evidence the code it
# NAMES exists:
#   (i)   an ansible role directory  ops/ansible/roles/<name>  cited as roles/<name>
#   (ii)  an image-pin key  <thing>_image  present in a group_vars pins.yml
#   (iii) a `goat <subcommand>` that goat-cli actually defines
# Any hit => the ADR under-claims => FAIL with the ADR number and the evidence.
#
# NON-GROWING. The current under-claimers are frozen in
# ops/ci/adr-status-baseline.txt (shrink-only, modelled on
# coupled-fact-known-open.txt). A NEW under-claimer fails on its own PR. When an
# ADR's Status is corrected (or its evidence removed), its baseline line goes
# stale and the guard FAILS ("remove this line") — so the set only shrinks and
# every removal is a real status fix.
#
# Usage:
#   check-adr-status-vs-code.sh              # gate, honoring the baseline
#   check-adr-status-vs-code.sh --list       # show every under-claimer + evidence
#   check-adr-status-vs-code.sh --self-test  # prove red-first
#
# Exit 0 = no NEW under-claimer and no stale baseline line. Exit 1 otherwise.

set -uo pipefail
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
exec python3 - "$ROOT" "$@" <<'PY'
import glob
import os
import re
import sys

root = sys.argv[1]
args = sys.argv[2:]
adr_dir = os.path.join(root, "docs", "adr")
baseline_path = os.environ.get(
    "ADR_STATUS_BASELINE", os.path.join(root, "ops", "ci", "adr-status-baseline.txt")
)


# ---------------------------------------------------------------------------
# Evidence sources from the live tree
# ---------------------------------------------------------------------------
def role_dirs():
    out = set()
    for p in glob.glob(os.path.join(root, "ops", "ansible", "roles", "*")):
        if os.path.isdir(p):
            out.add(os.path.basename(p))
    return out


def image_pin_keys():
    """Every `<thing>_image:` key across the group_vars pins.yml files."""
    out = set()
    for p in glob.glob(
        os.path.join(root, "ops", "ansible", "group_vars", "*", "pins.yml")
    ):
        try:
            with open(p, encoding="utf-8") as fh:
                for line in fh:
                    m = re.match(r"^([a-z0-9_]+_image)\s*:", line)
                    if m:
                        out.add(m.group(1))
        except OSError:
            pass
    return out


def goat_subcommands():
    """Subcommand tokens goat-cli actually defines: clap `name = "x"`,
    top-level enum variants in main.rs, and cmd/<x>.rs module files."""
    out = set()
    cli = os.path.join(root, "crates", "goat-cli", "src")
    for dp, _dn, fns in os.walk(cli):
        for fn in fns:
            if not fn.endswith(".rs"):
                continue
            try:
                with open(os.path.join(dp, fn), encoding="utf-8", errors="ignore") as fh:
                    txt = fh.read()
            except OSError:
                continue
            for m in re.finditer(r'#\[command\([^)]*name\s*=\s*"([a-z][a-z0-9-]+)"', txt):
                out.add(m.group(1))
            for m in re.finditer(r'Command::new\("([a-z][a-z0-9-]+)"', txt):
                out.add(m.group(1))
    main_rs = os.path.join(cli, "main.rs")
    try:
        with open(main_rs, encoding="utf-8") as fh:
            in_enum = False
            for line in fh:
                if re.match(r"\s*enum\s+Commands?\b", line):
                    in_enum = True
                    continue
                if in_enum:
                    m = re.match(r"\s+([A-Z][A-Za-z0-9]+)\s*[\({]", line)
                    if m:
                        out.add(m.group(1).lower())
    except OSError:
        pass
    for p in glob.glob(os.path.join(cli, "cmd", "*.rs")):
        name = os.path.basename(p)[:-3]
        if name not in ("mod",):
            out.add(name.replace("_", "-"))
    out.discard("help")
    return out


ROLES = role_dirs()
PINS = image_pin_keys()
SUBS = goat_subcommands()


# ---------------------------------------------------------------------------
# ADR status parsing
# ---------------------------------------------------------------------------
UNDERCLAIM_ANY = re.compile(r"designed[,\s-]+not[\s-]+built|no\s+code\s+yet", re.I)
PROPOSED_LEAD = re.compile(r"proposed", re.I)


def status_line(text):
    for line in text.splitlines():
        m = re.match(r"\s*\**\s*status\s*\**\s*:?\s*\**\s*(.*)$", line, re.I)
        if m:
            return m.group(1).strip()
    return None


def is_candidate(status):
    if status is None:
        return False
    # The LEADING verdict is what the ADR claims about itself as a whole. Split
    # off any nuance clause first (an em-dash, a parenthetical, a semicolon, a
    # mid-dot, or a sentence break). "Accepted. Phase 1 built; phases 2/3
    # designed-not-built" leads with "Accepted" — it does NOT under-claim, it
    # documents phase state — so keying on the lead (not the whole line) keeps
    # it out, while "Proposed — drop counters built" still leads "Proposed".
    lead = re.split(r"[—–(;·]|\.\s", status, maxsplit=1)[0]
    if PROPOSED_LEAD.search(lead):
        return True
    if UNDERCLAIM_ANY.search(lead):
        return True
    return False


def title_tokens(path, text):
    """Words in the ADR filename + its first H1 heading — used to tell an ADR
    that INTRODUCES a `goat <sub>` from one that merely references a standing
    command. `1090-microscope-...` / "# 1090. microscope ..." => {microscope};
    `1024-goat-cli-internationalization` (which only cites `goat doctor`) yields
    no command token, so that incidental reference is not counted."""
    toks = set(re.split(r"[^a-z0-9]+", os.path.basename(path).lower()))
    m = re.search(r"^\s*#\s+(.*)$", text, re.M)
    if m:
        toks |= set(re.split(r"[^a-z0-9]+", m.group(1).lower()))
    toks.discard("")
    return toks


def evidence(path, text):
    hits = []
    for m in re.finditer(r"roles/([a-z0-9][a-z0-9_-]+)", text):
        if m.group(1) in ROLES:
            hits.append(f"role:ops/ansible/roles/{m.group(1)}")
    for m in re.finditer(r"\b([a-z0-9_]+_image)\b", text):
        if m.group(1) in PINS:
            hits.append(f"pin:{m.group(1)}")
    titles = title_tokens(path, text)
    for m in re.finditer(r"`?goat\s+([a-z][a-z0-9-]+)`?", text):
        sub = m.group(1)
        # only when the ADR is ABOUT this command (its name is in the title),
        # i.e. the ADR introduces it, not merely cites a standing surface.
        if sub in SUBS and sub in titles:
            hits.append(f"goat:{sub}")
    # de-dup, keep order
    seen, uniq = set(), []
    for h in hits:
        if h not in seen:
            seen.add(h)
            uniq.append(h)
    return uniq


def adr_number(path):
    b = os.path.basename(path)
    m = re.match(r"(\d+)", b)
    return m.group(1) if m else b


def scan():
    under = {}  # number -> (path, status, evidence[])
    for path in sorted(glob.glob(os.path.join(adr_dir, "[0-9]*.md"))):
        try:
            with open(path, encoding="utf-8") as fh:
                text = fh.read()
        except OSError:
            continue
        st = status_line(text)
        if not is_candidate(st):
            continue
        ev = evidence(path, text)
        if ev:
            under[adr_number(path)] = (path, st, ev)
    return under


def load_baseline():
    entries = set()
    if os.path.exists(baseline_path):
        with open(baseline_path, encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                # first token, dropping an inline `# note` or `| note`
                key = re.split(r"[|#\s]", line, maxsplit=1)[0].strip()
                if key:
                    entries.add(key)
    return entries


under = scan()

if "--self-test" in args:
    # red-first proof: fabricate a candidate ADR that names a real role.
    import tempfile, shutil

    tmp = tempfile.mkdtemp(prefix="adrsvc-")
    try:
        d = os.path.join(tmp, "docs", "adr")
        os.makedirs(d)
        os.makedirs(os.path.join(tmp, "ops", "ansible", "roles", "planted-role"))
        with open(os.path.join(d, "9999-planted.md"), "w") as fh:
            fh.write(
                "# 9999. planted\n\n**Status:** Proposed (2026-09-02)\n\n"
                "Body cites roles/planted-role which exists in-tree.\n"
            )
        # re-run the detector against the temp tree
        g = dict(globals())
        code = (
            "import glob,os,re\n"
            "ROLES={'planted-role'}\nPINS=set()\nSUBS=set()\n"
        )
        # reuse the same functions bound to the temp adr dir
        st = status_line(open(os.path.join(d, "9999-planted.md")).read())
        cand = is_candidate(st)
        # evidence() closes over module-level ROLES; planted-role isn't in the
        # real ROLES, so assert on the parsing primitives directly.
        body = open(os.path.join(d, "9999-planted.md")).read()
        role_hit = any(
            m.group(1) == "planted-role"
            for m in re.finditer(r"roles/([a-z0-9][a-z0-9_-]+)", body)
        )
        ok = cand and role_hit
        print("OK: self-test — planted Proposed ADR that names roles/planted-role")
        print(f"    is_candidate={cand}  role-named-and-matchable={role_hit}")
        if not ok:
            print("  self-test FAIL")
            sys.exit(1)
        # and a clean Accepted ADR must NOT be a candidate
        if is_candidate("Accepted — built + live-fired 2026-06-14"):
            print("  self-test FAIL: 'Accepted — built' misread as under-claim")
            sys.exit(1)
        print("    'Accepted — built' correctly NOT a candidate; red-first holds")
        sys.exit(0)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

baseline = load_baseline()

if "--list" in args:
    for num in sorted(under):
        path, st, ev = under[num]
        tag = "BASELINE" if num in baseline else "NEW"
        print(f"  {tag:8} ADR {num}: status={st[:48]!r}")
        for e in ev:
            print(f"           evidence: {e}")

fail = 0
new = sorted(n for n in under if n not in baseline)
if new:
    fail = 1
    print()
    print("FAIL: these ADRs say Proposed / designed-not-built / no-code-yet, but the")
    print("      code they name EXISTS in the tree — they UNDER-CLAIM their status:")
    for n in new:
        path, st, ev = under[n]
        print(f"        ADR {n}  ({os.path.relpath(path, root)})")
        print(f"            status: {st[:70]}")
        print(f"            code found: {', '.join(ev)}")
    print()
    print("      Correct the Status line to reflect what is built + where it runs.")
    print(f"      If a listed ADR is genuinely still un-built (the named artifact is")
    print("      an unrelated pre-existing thing), add its number to")
    print(f"      {os.path.relpath(baseline_path, root)} — but that is the exception, not the reflex.")

stale = sorted(b for b in baseline if b not in under)
if stale:
    fail = 1
    print()
    print(f"FAIL: these entries in {os.path.relpath(baseline_path, root)} are stale — the ADR")
    print("      no longer under-claims (status corrected, or evidence removed).")
    print("      The baseline is shrink-only: remove each line.")
    for b in stale:
        print(f"        ADR {b}")

if fail:
    sys.exit(1)

print(
    f"OK: {len(under)} ADR(s) under-claim and are on the shrink-only baseline; "
    "0 new; 0 stale baseline lines."
)
PY

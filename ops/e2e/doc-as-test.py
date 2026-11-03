#!/usr/bin/env python3
"""doc-as-test.py — run the fenced bash blocks of an operator page, as written, against the hermetic set.

    doc-as-test.py <page.md> --section "<heading text>" [--map FROM=TO]... [--only-dry-run] [--cwd DIR]

Takes every ```bash block under the named heading (until the next heading of
the same or higher level), applies the declared substitutions (the page names
a live goatnet and a live component; the hermetic set names its own — the map
is printed, so nothing is hidden), strips the secrets-injector prefixes
(`ops/scripts/with-secrets`, `goatbuilder secrets exec --`, `op run --env-file=… --`)
and `direnv exec . env … ` (the hermetic canary needs no secret), drops
comment lines, substitutes the harness's own name for a placeholder `--ack`
(goatbuilder refuses a placeholder in a ledger record), and runs each remaining
command line through bash. A line
beginning with `goatbuilder status` whose binary refuses on real hosts is
reported, not skipped: the page says it works, so the harness asks.

With --only-dry-run, only lines carrying `--dry-run` or `--verify-only` or
`validate`/`doctor` run (no host touched). Exit 1 when any command fails; the
page has drifted from the tool or the tool from the page, and either is a defect.
"""
import argparse
import os
import re
import subprocess
import sys



def read_text(path):
    with open(path, encoding="utf-8") as fh:
        return fh.read()

def blocks_under(text, heading):
    lines = text.splitlines()
    start = None
    level = 0
    for i, l in enumerate(lines):
        m = re.match(r"^(#+)\s+(.*)$", l)
        if m and m.group(2).strip() == heading:
            start, level = i, len(m.group(1))
            break
    if start is None:
        sys.exit(f"doc-as-test: no heading {heading!r} in the page")
    out, cur, infence = [], [], False
    for l in lines[start + 1:]:
        m = re.match(r"^(#+)\s+", l)
        if m and len(m.group(1)) <= level and not infence:
            break
        if l.startswith("```"):
            if infence:
                out.append("\n".join(cur)); cur = []
            infence = not infence and l.strip() in ("```bash", "```sh", "```shell")
            if not infence:
                cur = []
            continue
        if infence:
            cur.append(l)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("page")
    ap.add_argument("--section", required=True)
    ap.add_argument("--map", action="append", default=[])
    ap.add_argument("--only-dry-run", action="store_true")
    ap.add_argument("--cwd", default=None)
    a = ap.parse_args()
    text = read_text(a.page)
    maps = [m.split("=", 1) for m in a.map]
    print(f"first-run page: {os.path.relpath(a.page)} §{a.section!r}; substitutions: " + ", ".join(f"{f}->{t}" for f, t in maps))
    cmds = []
    for b in blocks_under(text, a.section):
        joined = re.sub(r"\\\n\s*", " ", b)
        for line in joined.splitlines():
            s = line.strip()
            if not s or s.startswith("#"):
                continue
            s = re.sub(r"^[A-Z_]+=\S+\s+op run --env-file=\S+\s+--\s+", "", s)
            s = re.sub(r"^op run --env-file=\S+\s+--\s+", "", s)
            s = re.sub(r"^(\./)?ops/scripts/with-secrets\s+", "", s)
            s = re.sub(r"^goatbuilder secrets exec --\s+", "", s)
            s = re.sub(r"^direnv exec \. env (\S+=\S+\s+)*", "", s)
            s = re.sub(r"^\$\s+", "", s)
            for f, t in maps:
                s = re.sub(rf"(?<![\w/.-]){re.escape(f)}(?![\w.-])", t, s)
            # The page writes the ack as a placeholder, in whatever words read
            # best there ("<name>", "<your name>"). goatbuilder refuses a
            # placeholder in a record, so the harness signs as itself.
            s = re.sub(r'--ack\s+"<[^"]*>"', '--ack "e2e harness"', s)
            # dry-run only: no line that reads a host (a --verify-only before any
            # deploy reads an empty ledger and fails by design)
            if a.only_dry_run and not re.search(r"--dry-run|\bvalidate\b|\bdoctor\b", s):
                continue
            if re.search(r"\b(deploy|release|rollback|fastlane)\b", s) and "--dry-run" not in s and "--verify-only" not in s and "--yes" not in s and " --list" not in s:
                s += " --yes"
            cmds.append(s)
    fails = 0
    for c in cmds:
        print(f"$ {c}")
        p = subprocess.run(["bash", "-c", c], cwd=a.cwd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        tail = "\n".join(p.stdout.splitlines()[-6:])
        print(tail)
        if p.returncode != 0:
            fails += 1
            print(f"doc-as-test: FAILED (exit {p.returncode}): the page says this works")
    print(f"first-run page: {len(cmds)} command(s), {fails} failed")
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())

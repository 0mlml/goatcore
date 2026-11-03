#!/usr/bin/env python3
"""Render an e2e run's report.jsonl as one page of Markdown and judge it.

    report.py <record dir> [--markdown OUT] [--check]

Exit 0 when every step passed or was an expected failure; 1 otherwise. With
--check the rows are also validated against ops/e2e/report.schema.json
(jsonschema required). The Markdown goes to --markdown, else stdout; when
$GITHUB_STEP_SUMMARY is set it is appended there too.
"""
import argparse
import json
import os
import sys


def read_text(path):
    with open(path, encoding="utf-8") as fh:
        return fh.read()

HERE = os.path.dirname(os.path.abspath(__file__))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("record_dir")
    ap.add_argument("--markdown")
    ap.add_argument("--check", action="store_true")
    ap.add_argument("--title", default="deployment e2e")
    a = ap.parse_args()
    path = os.path.join(a.record_dir, "report.jsonl")
    if not os.path.exists(path):
        print(f"report.py: no {path}", file=sys.stderr)
        return 1
    rows = [json.loads(l) for l in read_text(path).splitlines(True) if l.strip()]
    if a.check:
        import jsonschema  # noqa: F401
        schema = json.loads(read_text(os.path.join(HERE, "report.schema.json")))
        for r in rows:
            jsonschema.validate(r, schema)
    n_pass = sum(r["status"] == "pass" for r in rows)
    n_xf = sum(r["status"] == "xfail" for r in rows)
    n_fail = sum(r["status"] == "fail" for r in rows)
    verdict = "PASS" if n_fail == 0 else "FAIL"
    total = round(sum(r.get("duration_s", 0) for r in rows), 1)
    out = [f"## {a.title}: {verdict} — {n_pass} passed, {n_xf} expected failures, {n_fail} failed, {total}s", "",
           "| # | step | status | exit | s | detail |", "|---|---|---|---|---|---|"]
    for r in rows:
        detail = r.get("why") or r.get("note") or ""
        if r["status"] == "xfail":
            detail = f"expected: {r.get('xfail')}" + (f" — {r.get('why')}" if r.get("why") else "")
        out.append(f"| {r['n']} | `{r['name']}` | {r['status']} | {r['exit']} | {r.get('duration_s', 0)} | {detail.replace('|', '/')} |")
    ledgers = os.path.join(a.record_dir, "ledgers.jsonl")
    if os.path.exists(ledgers):
        out += ["", "### Ledger rows read back from the hosts", "", "| host | component | newest row |", "|---|---|---|"]
        for l in read_text(ledgers).splitlines(True):
            if not l.strip():
                continue
            d = json.loads(l)
            out.append(f"| {d['host']} | {d['component']} | `{d['row'][:160].replace('|', '/')}` |")
    for name in ("gh-calls.jsonl", "crane-calls.jsonl", "aws-vault-calls.jsonl"):
        p = os.path.join(a.record_dir, name)
        if os.path.exists(p):
            k = sum(1 for l in read_text(p).splitlines(True) if l.strip())
            out.append(f"\n{name.split('-')[0]} stub: {k} call(s) recorded in `{name}`")
    md = "\n".join(out) + "\n"
    if a.markdown:
        with open(a.markdown, "w", encoding="utf-8") as fh:
            fh.write(md)
    else:
        sys.stdout.write(md)
    gs = os.environ.get("GITHUB_STEP_SUMMARY")
    if gs:
        with open(gs, "a", encoding="utf-8") as fh:
            fh.write(md)
    return 0 if n_fail == 0 else 1


if __name__ == "__main__":
    sys.exit(main())

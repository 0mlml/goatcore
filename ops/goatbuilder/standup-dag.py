#!/usr/bin/env python3
"""standup-dag.py — ops/goatbuilder/standup-dag.yaml for the scripts avenue.

  standup-dag.py --shell   print bash 3.2 array assignments goat-standup.sh evals:
                           DAG_N, DAG_LABEL[i], DAG_ID[i], DAG_NEEDS[i] (space-separated)
  standup-dag.py --check   validate the graph: unique ids, needs name known ids, no cycle
"""
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.exit("standup-dag: needs PyYAML (pip3 install pyyaml)")

DAG = Path(__file__).resolve().parent / "standup-dag.yaml"


def load():
    doc = yaml.safe_load(DAG.read_text()) or {}
    return doc.get("steps") or []


def check(steps):
    ids = [s["id"] for s in steps]
    errs = []
    if len(ids) != len(set(ids)):
        errs.append("duplicate step ids")
    known = set(ids)
    for s in steps:
        for n in s.get("needs") or []:
            if n not in known:
                errs.append(f"{s['id']} needs unknown step {n}")
        if not s.get("why"):
            errs.append(f"{s['id']} has no why: every edge cites the dependency it encodes")
    # cycle check by repeated removal of steps whose needs are all removed
    left = {s["id"]: set(s.get("needs") or []) for s in steps}
    while left:
        ready = [k for k, v in left.items() if not (v & set(left))]
        if not ready:
            errs.append(f"cycle among: {', '.join(sorted(left))}")
            break
        for k in ready:
            del left[k]
    return errs


def q(s):
    return "'" + str(s).replace("'", "'\\''") + "'"


def main(argv):
    steps = load()
    if "--check" in argv:
        errs = check(steps)
        for e in errs:
            print(f"standup-dag: {e}", file=sys.stderr)
        print(f"standup-dag: {'ok' if not errs else 'FAIL'} ({len(steps)} steps)")
        return 1 if errs else 0
    if "--shell" in argv:
        print(f"DAG_N={len(steps)}")
        for i, s in enumerate(steps):
            print(f"DAG_LABEL[{i}]={q(s['label'])}; DAG_ID[{i}]={q(s['id'])}; DAG_NEEDS[{i}]={q(' '.join(s.get('needs') or []))}")
        return 0
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

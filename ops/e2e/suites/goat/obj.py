#!/usr/bin/env python3
"""obj — assert on the object a service stored, not on what the CLI printed.

    obj.py <url> <python expression over d>         # GET url (operator token), d = parsed JSON
    obj.py --stdin <python expression over d>       # d = JSON read from stdin
    obj.py --ndjson <python expression over d>      # d = list of JSON lines read from stdin

Exit 0 when the expression is true, 1 when false (printing the object), 2 on a
fetch or parse error. The token comes from E2E_GOAT_TOKEN.
"""
import json
import os
import sys
import urllib.request


def main():
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    src, expr = sys.argv[1], sys.argv[2]
    try:
        if src == "--stdin":
            d = json.load(sys.stdin)
        elif src == "--ndjson":
            d = [json.loads(line) for line in sys.stdin if line.strip()]
        else:
            req = urllib.request.Request(src, headers={"Authorization": "Bearer " + os.environ.get("E2E_GOAT_TOKEN", "")})
            with urllib.request.urlopen(req, timeout=10) as resp:
                d = json.load(resp)
    except Exception as exc:  # noqa: BLE001 - every failure is reported, never swallowed
        print(f"obj: could not read {src}: {exc}", file=sys.stderr)
        return 2
    ok = bool(eval(expr, {"json": json}, {"d": d}))  # noqa: S307 - expressions are the suite's own literals
    if not ok:
        print(json.dumps(d, indent=1)[:2000])
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())

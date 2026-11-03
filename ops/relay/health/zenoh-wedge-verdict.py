#!/usr/bin/env python3
"""zenoh-wedge-verdict — turn the symcap captures into per-wedge hypothesis verdicts.

The investigation kept stalling because a capture needed hand-analysis and the
analyst (me) asserted a cause before the evidence forced it. This reads the
VERDICT SUMMARY that zenoh-symcap.sh now appends to each capture and prints one
row per wedge marking each hypothesis supported / killed / inconclusive, so the
next structural fix is chosen from evidence, not narrative.

Design + hypothesis matrix: docs/design/zenoh-wedge-instrumentation.md

Usage: zenoh-wedge-verdict.py /var/log/zenoh-symcap/symbt-*.txt
"""
import sys, re, glob


def parse(path):
    with open(path, errors="replace") as source_file:
        t = source_file.read()

    def n(pat):
        m = re.search(pat, t)
        return int(m.group(1)) if m else None

    def cnt(pat):
        return len(re.findall(pat, t))

    # Prefer the VERDICT SUMMARY line (new captures); fall back to counting raw
    # frames so this ALSO works on the 12 pre-instrument captures.
    def field(summary_pat, raw_pat):
        v = n(summary_pat)
        return v if v is not None else cnt(raw_pat)

    # HOLDER HUNT: a running (non-futex) thread named in a routing/query/admin frame
    running = "not-futex" in t and "<all threads in futex_wait>" not in t
    return {
        "capture": path.split("/")[-1],
        "read_wait":  field(r"read_contended\(waiters\)=(\d+)", r"rwlock::futex::RwLock::read_contended"),
        "write_wait": field(r"write_contended\(waiters\)=(\d+)", r"rwlock::futex::RwLock::write_contended"),
        "query":      field(r"Session::query=(\d+)", r"api::session::Session::query"),
        "admin":      field(r"adminspace::resources_data=(\d+)", r"adminspace::resources_data"),
        "declare":    field(r"send_declare=(\d+)", r"::send_declare"),
        "ag":         field(r"adminspace_getter/StorageRuntimeInner=(\d+)", r"adminspace_getter|StorageRuntimeInner"),
        "uq":         field(r"undeclare_querier=(\d+)", r"undeclare_querier"),
        "holder_seen": running,
    }


def verdict(r):
    # each hypothesis -> SUPPORTED / killed / inconclusive from the frame counts
    def sup(x):  return (x or 0) > 0
    v = {}
    v["H1 admin-read"]  = "SUPPORTED" if sup(r["admin"]) else "killed"
    v["H2 query-read"]  = "SUPPORTED" if sup(r["query"]) else "killed"
    v["H3 declare-write"] = "SUPPORTED" if sup(r["declare"]) else "killed"
    v["H5 adminspace_getter"] = "SUPPORTED(!)" if sup(r["ag"]) else "killed"
    v["H6 undeclare_querier"] = "SUPPORTED(!)" if sup(r["uq"]) else "killed"
    # holder attribution: which read path dominates the contention
    if not r["holder_seen"]:
        v["holder"] = "inconclusive (all waiters — need faster onset sampling)"
    else:
        dom = max((("admin-read", r["admin"] or 0), ("query-read", r["query"] or 0),
                   ("declare-write", r["declare"] or 0)), key=lambda x: x[1])
        v["holder"] = f"running thread present; dominant frame ~ {dom[0]}"
    return v


def main():
    files = []
    for a in sys.argv[1:]:
        files += sorted(glob.glob(a))
    if not files:
        print("usage: zenoh-wedge-verdict.py /var/log/zenoh-symcap/symbt-*.txt")
        return 2
    agg = {}
    for f in files:
        r = parse(f)
        v = verdict(r)
        print(f"\n=== {r['capture']} ===")
        print(f"  waiters: read={r['read_wait']} write={r['write_wait']}  "
              f"frames: query={r['query']} admin={r['admin']} declare={r['declare']} "
              f"ag={r['ag']} uq={r['uq']}")
        for h, res in v.items():
            print(f"  {h:26s} {res}")
            agg.setdefault(h, []).append(res)
    print("\n=== AGGREGATE across", len(files), "captures ===")
    for h, rs in agg.items():
        s = sum(1 for x in rs if str(x).startswith("SUPPORTED"))
        print(f"  {h:26s} supported in {s}/{len(rs)} captures")
    print("\nRead: a hypothesis SUPPORTED in ~all captures is the driver; one killed in "
          "all is ruled out (H5/H6 expected killed). 'holder inconclusive' across the "
          "board means the onset sampling still isn't catching the lock holder.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

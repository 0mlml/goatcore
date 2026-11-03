#!/usr/bin/env python3
"""
Does what a registered topic actually carries match what it says it carries?

The portal registry says a topic is `xml:event` or `protobuf:Wildcard`. Nothing
until now checked whether the bytes on the key can be read that way. That gap
is not theoretical: on zagros-prod, slot 6cf17a18 (GATEWAY_ONE SVN-001) published
CoT, ASTERIX and ISA topics carrying the BASE64 TEXT of their real payloads —
for weeks — and every surface reported health. The publisher got HTTP 2xx, the
keys existed, samples flowed, panoscope listed them. It was found by eye on
2026-08-14.

Why nothing caught it. Zenoh `put` is fire-and-forget and denial is silent
, so a publisher learns nothing from publishing. `topics.publish_count`
and `topics.last_seen_at` in the portal registry are never written by anything
(registry/schema.sql:70-73 says so), so the topic list shows 0 for every topic
on every goatnet. `vendor-decode-rca` can diagnose a decode failure but is
scheduled nowhere, so it only ever runs after a human already suspects one.
The result is a fabric that will carry unusable bytes indefinitely while
reporting green.

WHAT THIS GRADES, per registered topic:

  OK            a sample exists and decodes against the declared format
  UNDECODABLE   a sample exists and does NOT decode  <- the condition to catch
  DOUBLE-ENCODED  a sample decodes only after stripping a base64 layer. Called
                out separately from UNDECODABLE because it names the bug and
                the fix rather than leaving an operator to bisect. This is the
                hypothesis vendor-decode-rca's H1-H5 does not cover.
  STALE         a sample exists but is older than the staleness budget
  NO-SAMPLE     registered, nothing on the key at all
  UNGRADED      the declared format carries no machine-checkable shape (raw,
                text, cbor, msgpack). Reported, never counted as clean —
                "cannot tell" is not "fine".

And one fabric-side axis the registry cannot see:

  UNREGISTERED  a key under a slot anchor with no registry row. Catches a
                publisher that quietly added a stream nobody declared.

EXIT CODES (docs/design/live-state-guard-scheduling.md §4):
  0  read succeeded, nothing to report
  1  the condition this guard exists to catch
  2  a source could not be read — says NOTHING, must never be read as clean

Read-only. Never writes to the portal or the router.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from base64 import b64decode

EXIT_CLEAN = 0
EXIT_FOUND = 1
EXIT_UNREADABLE = 2

# Formats whose payload shape can be checked without a schema body.
SELF_DESCRIBING = ("xml", "json")
# Formats that carry no checkable shape. Reported as UNGRADED, never as OK.
OPAQUE = ("raw", "text", "cbor", "msgpack")


def http_json(url, token=None, timeout=15):
    req = urllib.request.Request(url)
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode())


def die_unreadable(what, err):
    """A source we could not read. Exit 2 — this is not a clean result."""
    print(f"UNREADABLE: {what}: {err}", file=sys.stderr)
    print("  exit 2 — this run says nothing about decode health, and must not "
          "be recorded as a pass.", file=sys.stderr)
    sys.exit(EXIT_UNREADABLE)


def sample_bytes(entry):
    """Real payload bytes from one zenoh REST GET entry.

    The REST projection returns non-JSON payloads as a base64 string and JSON
    payloads as a native JSON value. Mirrors gateway-one's zenoh_rest.raw_of; the
    two must agree or this guard grades a different thing than the publisher
    produced.
    """
    v = entry.get("value")
    if isinstance(v, str):
        try:
            return b64decode(v, validate=True)
        except Exception:
            return v.encode()
    return json.dumps(v, separators=(",", ":")).encode()


def strip_one_base64_layer(payload):
    """If `payload` is entirely base64 text that decodes, return the decoding.

    This is the double-encode signature: the publisher forwarded the REST
    projection's base64 string instead of decoding it first, so what reached
    the fabric is the base64 TEXT of the real payload.
    """
    if len(payload) < 16:
        return None
    if not re.fullmatch(rb"[A-Za-z0-9+/\r\n]+={0,2}", payload):
        return None
    try:
        inner = b64decode(payload, validate=True)
    except Exception:
        return None
    return inner or None


def decodes_as(payload, fmt, descriptor_ok):
    """Can `payload` be read as `fmt`? Returns (verdict, detail).

    verdict is one of "ok", "no", "ungraded".
    """
    if fmt.startswith("xml"):
        head = payload.lstrip()
        if head.startswith(b"\xef\xbb\xbf"):
            head = head[3:].lstrip()
        if head[:1] != b"<":
            return "no", "not an XML document (does not start with '<')"
        root = re.search(rb"<\s*([A-Za-z_][\w.:-]*)", head)
        declared = fmt.split(":", 1)[1] if ":" in fmt else ""
        if declared and root and root.group(1).decode() != declared:
            return "no", (f"root element is <{root.group(1).decode()}> but "
                          f"topic declares xml:{declared}")
        return "ok", ""
    if fmt == "json":
        try:
            json.loads(payload.decode("utf-8"))
        except Exception as ex:
            return "no", f"not valid JSON: {ex}"
        return "ok", ""
    if fmt in OPAQUE:
        return "ungraded", f"format '{fmt}' carries no checkable shape"
    return "ungraded", f"unrecognised format '{fmt}'"


def portal_decode(portal, token, key):
    """Ask the PORTAL to decode the topic's latest samples.

    Deliberately not a local re-implementation: for protobuf the portal holds
    the uploaded descriptor and is the authority, and grading against the same
    surface the operator reads means this guard cannot disagree with what they
    see in the inspector. Returns (verdict, detail) as decodes_as does.
    """
    url = (f"{portal}/api/topics/decoded?key={urllib.parse.quote(key, safe='')}"
           f"&n=1")
    try:
        resp = http_json(url, token)
    except urllib.error.HTTPError as e:
        body = e.read().decode(errors="replace")
        # 409 no_schema / 400 format are statements about the REGISTRATION,
        # not about the bytes — the operator's decoded pane is dead either
        # way, so they are reportable, not clean.
        if "no_schema" in body:
            return "ungraded", ("declared protobuf but no schema body uploaded "
                                "— the decoded pane 409s for every consumer")
        if e.code == 400 and "format" in body:
            return "ungraded", "decoded endpoint does not handle this format"
        raise
    samples = resp.get("samples") or []
    if not samples:
        return "ungraded", "portal returned no samples to decode"
    s = samples[0]
    if s.get("error"):
        return "no", f"portal decoder: {s['error']}"
    return "ok", ""


def grade(portal, token, router, stale_after_s):
    """Returns (rows, unregistered_keys). Raises on an unreadable source."""
    orgs = http_json(f"{portal}/api/orgs", token)
    org_list = orgs.get("orgs") or orgs.get("vendors") or []

    rows, registered_keys = [], set()
    now = time.time()

    for org in org_list:
        prefix = org["prefix"]
        topics = http_json(
            f"{portal}/api/orgs/{urllib.parse.quote(prefix)}/topics", token
        ).get("topics", [])
        for t in topics:
            key, fmt = t["key_pattern"], t.get("format", "")
            registered_keys.add(key)
            row = {"org": prefix, "display": org.get("display_name", ""),
                   "key": key, "format": fmt}

            # Samples come from the PORTAL, not straight off the router.
            #
            # The router's REST port is reachable from inside the docker
            # network and, on at least one goatnet, from nowhere at all: it is
            # published on whatever LAN address the host had at deploy time and
            # goes stale when the host moves. The portal already proxies
            # the same GET, is mesh-internal, and this check already holds a
            # portal credential — so routing everything that can FAIL this guard
            # through one endpoint removes a dependency that was silently dead
            # rather than restarting a production fabric to repair it.
            samples = (http_json(
                f"{portal}/api/topics/latest?key={urllib.parse.quote(key, safe='')}",
                token) or {}).get("samples") or []
            if not samples:
                row.update(verdict="NO-SAMPLE",
                           detail="registered, but nothing published on this key")
                rows.append(row)
                continue

            entry = samples[0]
            payload = sample_bytes(entry)
            age = _age_seconds(entry, now)
            row["age_s"] = age

            # protobuf has no shape we can judge without the descriptor, so
            # the portal (which holds it) is asked. Everything else is checked
            # here so the guard still works if the portal is degraded.
            if fmt.startswith("protobuf:"):
                verdict, detail = portal_decode(portal, token, key)
            else:
                verdict, detail = decodes_as(payload, fmt, True)

            if verdict == "no":
                inner = strip_one_base64_layer(payload)
                if inner is not None:
                    if fmt.startswith("protobuf:"):
                        # The portal decoded the OUTER bytes and failed. If the
                        # inner layer is a plausible message the outer is a
                        # base64 shadow; the portal cannot be asked about bytes
                        # that are not on the key, so this arm is structural.
                        inner_verdict = "ok" if len(inner) > 1 else "no"
                    else:
                        inner_verdict, _ = decodes_as(inner, fmt, True)
                    if inner_verdict == "ok":
                        row.update(
                            verdict="DOUBLE-ENCODED",
                            detail=("payload is the BASE64 TEXT of a valid "
                                    f"{fmt} payload — the publisher forwarded a "
                                    "zenoh REST projection without decoding it "
                                    "first"))
                        rows.append(row)
                        continue
                row.update(verdict="UNDECODABLE", detail=detail)
            elif verdict == "ungraded":
                # An opaque format has no shape to check, which would make
                # `raw` an escape hatch from this guard. The base64-layer test
                # needs no schema, so it still applies: a payload that is
                # entirely base64 text AND decodes is the same defect whatever
                # the topic declares.
                inner = strip_one_base64_layer(payload)
                if inner is not None:
                    row.update(
                        verdict="DOUBLE-ENCODED",
                        detail=("payload is pure base64 text that decodes to "
                                f"{len(inner)} bytes — the declared format "
                                f"('{fmt}') cannot confirm it, but a publisher "
                                "that forwarded a REST projection undecoded is "
                                "the usual cause"))
                else:
                    row.update(verdict="UNGRADED", detail=detail)
            elif age is not None and stale_after_s and age > stale_after_s:
                row.update(verdict="STALE",
                           detail=f"decodes fine, but last sample is {int(age)}s old")
            else:
                row.update(verdict="OK", detail="")
            rows.append(row)

    # The UNREGISTERED axis is the one thing the portal cannot answer: it needs
    # a wildcard enumeration of the key space, and the portal proxy resolves a
    # single key rather than expanding `**` (verified live — a wildcard returns
    # 200 with an empty sample list, which would read as "nothing undeclared"
    # and is the wrong answer to give).
    #
    # So it needs the router, it is ADVISORY only (ADR 1028 — topic visibility
    # is not access, publishing without a registry row is legal), and it never
    # affects the exit code. When no router is configured the axis is SKIPPED
    # and says so, rather than reporting an empty list that looks like a clean
    # sweep.
    unregistered = []
    if not router:
        return rows, None

    for org in org_list:
        prefix = org["prefix"]
        for entry in http_json(f"{router}/{urllib.parse.quote(prefix, safe='')}/**"):
            if entry["key"] not in registered_keys:
                unregistered.append({"org": prefix, "key": entry["key"]})

    return rows, unregistered


def _age_seconds(entry, now):
    """Zenoh HLC timestamp -> age in seconds. None when absent/unparseable."""
    ts = (entry.get("timestamp") or "").split("/")[0]
    if not ts.isdigit():
        return None
    return max(0.0, now - (int(ts) >> 32))


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--portal", default=os.environ.get("PORTAL_BASE_URL", ""),
                    help="portal base URL (mesh-internal)")
    ap.add_argument("--router", default=os.environ.get("ZENOH_REST_URL", ""),
                    # optional; only the advisory UNREGISTERED axis uses it
                    help="zenoh REST plugin base URL")
    ap.add_argument("--token", default=os.environ.get("PORTAL_OPERATOR_API_KEY", ""),
                    help="portal operator API key")
    ap.add_argument("--stale-after", type=int, default=3600,
                    help="seconds before a decodable topic is graded STALE (0 disables)")
    ap.add_argument("--json", action="store_true", help="emit JSON instead of a table")
    args = ap.parse_args()

    # --router is deliberately NOT required: it serves only the advisory
    # unregistered axis. Requiring it would make a guard that can fully grade
    # the fabric refuse to run because an optional extra is unreachable.
    missing = [n for n, v in (("--portal", args.portal),
                              ("--token", args.token)) if not v]
    if missing:
        print(f"refusing to run unconfigured: missing {', '.join(missing)}",
              file=sys.stderr)
        print("  A guard that runs half-configured reports a clean tree it "
              "never looked at.", file=sys.stderr)
        sys.exit(EXIT_UNREADABLE)

    try:
        rows, unregistered = grade(args.portal.rstrip("/"), args.token,
                                   args.router.rstrip("/"), args.stale_after)
    except urllib.error.HTTPError as e:
        die_unreadable(f"HTTP {e.code} from {e.url}", e.reason)
    except Exception as e:
        die_unreadable("reading portal/router", f"{type(e).__name__}: {e}")

    if args.json:
        # `null` rather than [] when the axis was skipped — a consumer must be
        # able to tell "nothing undeclared" from "nobody looked".
        print(json.dumps({"topics": rows, "unregistered": unregistered}, indent=2))
    else:
        _print_table(rows, unregistered)

    bad = [r for r in rows
           if r["verdict"] in ("UNDECODABLE", "DOUBLE-ENCODED", "NO-SAMPLE", "STALE")]
    return EXIT_FOUND if bad else EXIT_CLEAN


def _print_table(rows, unregistered):
    order = {"DOUBLE-ENCODED": 0, "UNDECODABLE": 1, "NO-SAMPLE": 2,
             "STALE": 3, "UNGRADED": 4, "OK": 5}
    print(f"{'VERDICT':<15} {'FORMAT':<28} {'AGE':>8}  KEY")
    print("-" * 110)
    for r in sorted(rows, key=lambda r: (order.get(r["verdict"], 9), r["key"])):
        age = f"{int(r['age_s'])}s" if r.get("age_s") is not None else "-"
        print(f"{r['verdict']:<15} {r['format']:<28} {age:>8}  {r['key']}")
        if r["detail"]:
            print(f"{'':<15} {'':<28} {'':>8}  -> {r['detail']}")
    if unregistered is None:
        print("\nUNREGISTERED — NOT CHECKED (no router configured). This is not "
              "\"none found\":\nthe axis needs a wildcard enumeration the portal "
              "cannot serve.")
    elif unregistered:
        # Informational, and deliberately NOT part of the exit-1 condition:
        # topic visibility is advisory, not access (ADR 1028), so publishing
        # without a registry row is legal. It is still worth printing — a
        # stream nobody declared is a stream nobody is grading.
        print(f"\nUNREGISTERED ({len(unregistered)}) — on the fabric with no "
              f"registry row. Advisory (ADR 1028), does not fail this run:")
        for u in unregistered[:20]:
            print(f"  {u['key']}")
        if len(unregistered) > 20:
            print(f"  ... and {len(unregistered) - 20} more")
    counts = {}
    for r in rows:
        counts[r["verdict"]] = counts.get(r["verdict"], 0) + 1
    print("\n" + "  ".join(f"{k}={v}" for k, v in sorted(counts.items())))


if __name__ == "__main__":
    sys.exit(main())

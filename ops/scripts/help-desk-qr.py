#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# help-desk-qr.py — renders the two QR codes and the one printable card
# that hand a goatnet's help desk out: A, the inside address (mesh name,
# behind the identity door) and B, the outside desk (public, for people who
# cannot get on). docs/design/help-desk-service-pattern.md §4 (ADR 1176);
# phase 5 of docs/project/mobile-and-help-desk-plan.md.
#
# The addresses come from the site's registry, not from a retyped string:
#   - inside  = the `isitup_services` row with id `help` → its `fqdn`,
#               rendered the way ops/dns/generate-mesh-hosts.py renders it
#   - outside = the top-level group_vars scalar `help_desk_public_url`
# Both are read from ops/ansible/group_vars/<dir>/ where <dir> is the
# group_vars directory whose `site_id` matches --site (the same discovery
# generate-mesh-hosts.py uses; there is no dir-name map to drift).
#
# Modes:
#   --site <site_id> [--out DIR]       render inside.svg, outside.svg, card.html
#   --site <site_id> --check           resolve both addresses, render to a
#                                      temp dir, keep nothing; exit 0 = ok
#   --inside-url/--outside-url         override either address (preview
#                                      before the registry rows exist)
#   --product-name                     card wording; defaults to the site's
#                                      portal_lex_product_name, else "goatnet"
#
# Exit codes: 2 = an address is missing; 3 = no QR encoder available.
#
# QR encoding: `segno` when importable (pip install segno); otherwise the
# small byte-mode encoder below (versions 1–10, error level M), which is
# enough for any URL a card carries and keeps the script dependency-free
# on an operator laptop.

import argparse
import glob
import html
import os
import re
import sys
import tempfile

try:
    import yaml
except ImportError:
    sys.stderr.write("ERROR: PyYAML required (pip install pyyaml)\n")
    sys.exit(2)

REPO_ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
GROUP_VARS_GLOB = os.path.join(REPO_ROOT, "ops/ansible/group_vars/*/")
VAR_RE = re.compile(r"\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}")


# --- registry -------------------------------------------------------------

def load_site(gv_dir):
    scalars, lists = {}, {"isitup_services": []}
    for path in sorted(glob.glob(os.path.join(gv_dir, "*.yml"))):
        with open(path) as f:
            data = yaml.safe_load(f) or {}
        if not isinstance(data, dict):
            continue
        for k, v in data.items():
            if isinstance(v, str):
                scalars[k] = v
        if isinstance(data.get("isitup_services"), list):
            lists["isitup_services"].extend(data["isitup_services"])
    return scalars, lists


def render(value, scalars, depth=0):
    if depth > 5:
        return value

    def sub(m):
        v = scalars.get(m.group(1))
        return v if isinstance(v, str) else m.group(0)

    out = VAR_RE.sub(sub, value)
    if out != value and VAR_RE.search(out):
        return render(out, scalars, depth + 1)
    return out


def find_site(site_id):
    """The group_vars dir declaring `site_id` that carries the service registry.

    More than one dir can declare the same site_id (zagros_hosts and
    zagros_cloud_hosts both say `zagros`); the registry lives in the one with a
    non-empty isitup_services, which is the qualifying rule
    generate-mesh-hosts.py uses too.
    """
    fallback = None
    for gv_dir in sorted(glob.glob(GROUP_VARS_GLOB)):
        scalars, lists = load_site(gv_dir)
        if scalars.get("site_id") != site_id:
            continue
        if lists["isitup_services"]:
            return gv_dir, scalars, lists
        fallback = fallback or (gv_dir, scalars, lists)
    return fallback or (None, {}, {"isitup_services": []})


def registry_addresses(site_id):
    """(inside_url, outside_url, product_name, problems[])"""
    gv_dir, scalars, lists = find_site(site_id)
    problems = []
    if gv_dir is None:
        return None, None, None, [f"no group_vars directory declares site_id '{site_id}'"]
    inside = None
    for svc in lists["isitup_services"]:
        if isinstance(svc, dict) and svc.get("id") == "help" and svc.get("fqdn"):
            fqdn = render(str(svc["fqdn"]), scalars).lower()
            if VAR_RE.search(fqdn):
                problems.append(f"cannot render the help row's fqdn '{svc['fqdn']}'")
            else:
                inside = "https://" + fqdn
            break
    if inside is None and not problems:
        problems.append(f"{os.path.relpath(gv_dir, REPO_ROOT)} has no isitup_services row with id 'help' "
                        "(the inside door; help-desk-service-pattern.md §2)")
    outside = scalars.get("help_desk_public_url")
    if not outside:
        problems.append(f"{os.path.relpath(gv_dir, REPO_ROOT)} sets no help_desk_public_url "
                        "(the outside desk; help-desk-service-pattern.md §3)")
    return inside, outside, scalars.get("portal_lex_product_name"), problems


# --- QR encoding ----------------------------------------------------------
#
# Byte mode, error level M, versions 1-10. Tables are the standard ones
# (ISO/IEC 18004): per version, EC codewords per block and block layout.

_EC_M = {  # version: (ec_per_block, [(blocks, data_per_block), ...])
    1: (10, [(1, 16)]), 2: (16, [(1, 28)]), 3: (26, [(1, 44)]), 4: (18, [(2, 32)]),
    5: (24, [(2, 43)]), 6: (16, [(4, 27)]), 7: (18, [(4, 31)]), 8: (22, [(2, 38), (2, 39)]),
    9: (22, [(3, 36), (2, 37)]), 10: (26, [(4, 43), (1, 44)]),
}
_ALIGN = {1: [], 2: [6, 18], 3: [6, 22], 4: [6, 26], 5: [6, 30], 6: [6, 34],
          7: [6, 22, 38], 8: [6, 24, 42], 9: [6, 26, 46], 10: [6, 28, 50]}

_EXP, _LOG = [0] * 512, [0] * 256
_x = 1
for _i in range(255):
    _EXP[_i] = _x
    _LOG[_x] = _i
    _x <<= 1
    if _x & 0x100:
        _x ^= 0x11D
for _i in range(255, 512):
    _EXP[_i] = _EXP[_i - 255]


def _gf_mul(a, b):
    return 0 if a == 0 or b == 0 else _EXP[_LOG[a] + _LOG[b]]


def _rs_generator(n):
    g = [1]  # product of (x - a^i) for i in 0..n-1
    for i in range(n):
        ng = [0] * (len(g) + 1)
        for j, c in enumerate(g):
            ng[j] ^= c
            ng[j + 1] ^= _gf_mul(c, _EXP[i])
        g = ng
    return g


def _rs_encode(data, n_ec):
    gen = _rs_generator(n_ec)
    rem = [0] * n_ec
    for d in data:
        f = d ^ rem[0]
        rem = rem[1:] + [0]
        if f:
            for j in range(n_ec):
                rem[j] ^= _gf_mul(gen[j + 1], f)
    return rem


def _bits(value, n):
    return [(value >> (n - 1 - i)) & 1 for i in range(n)]


def _encode_codewords(payload):
    data = payload.encode("utf-8")
    for version in range(1, 11):
        ec_per, layout = _EC_M[version]
        total_data = sum(b * d for b, d in layout)
        cci = 8 if version <= 9 else 16
        if len(data) * 8 + 4 + cci <= total_data * 8:
            break
    else:
        raise ValueError("payload too long for a version-10 QR code at level M")
    bits = _bits(0b0100, 4) + _bits(len(data), cci)
    for b in data:
        bits += _bits(b, 8)
    bits += [0] * min(4, total_data * 8 - len(bits))
    bits += [0] * ((8 - len(bits) % 8) % 8)
    cw = [int("".join(map(str, bits[i:i + 8])), 2) for i in range(0, len(bits), 8)]
    pad = (0xEC, 0x11)
    for i in range(total_data - len(cw)):
        cw.append(pad[i % 2])
    blocks, pos = [], 0
    for count, size in layout:
        for _ in range(count):
            blocks.append(cw[pos:pos + size])
            pos += size
    ecs = [_rs_encode(b, ec_per) for b in blocks]
    out = []
    for i in range(max(len(b) for b in blocks)):
        out += [b[i] for b in blocks if i < len(b)]
    for i in range(ec_per):
        out += [e[i] for e in ecs]
    return version, out


def _matrix(version, codewords):
    n = version * 4 + 17
    grid = [[None] * n for _ in range(n)]  # None = data area

    def put(r, c, v):
        if 0 <= r < n and 0 <= c < n:
            grid[r][c] = v

    def finder(r0, c0):
        for r in range(-1, 8):
            for c in range(-1, 8):
                inside = 0 <= r <= 6 and 0 <= c <= 6
                dark = inside and (r in (0, 6) or c in (0, 6) or (2 <= r <= 4 and 2 <= c <= 4))
                put(r0 + r, c0 + c, 1 if dark else 0)

    finder(0, 0), finder(0, n - 7), finder(n - 7, 0)
    centres = _ALIGN[version]
    for r in centres:
        for c in centres:
            # the three that would sit on a finder pattern are omitted
            if (r == centres[0] and c in (centres[0], centres[-1])) or (r == centres[-1] and c == centres[0]):
                continue
            for dr in range(-2, 3):
                for dc in range(-2, 3):
                    put(r + dr, c + dc, 1 if max(abs(dr), abs(dc)) != 1 else 0)
    for i in range(8, n - 8):
        v = 1 if i % 2 == 0 else 0
        if grid[6][i] is None:
            put(6, i, v)
        if grid[i][6] is None:
            put(i, 6, v)
    put(n - 8, 8, 1)  # dark module
    if version >= 7:
        for i in range(18):
            a, c = n - 11 + i % 3, i // 3
            put(a, c, 0), put(c, a, 0)
    reserved = []
    for i in range(9):
        for pos in ((8, i), (i, 8)):
            if grid[pos[0]][pos[1]] is None:
                grid[pos[0]][pos[1]] = 0
                reserved.append(pos)
    for i in range(8):
        for pos in ((8, n - 1 - i), (n - 1 - i, 8)):
            if grid[pos[0]][pos[1]] is None:
                grid[pos[0]][pos[1]] = 0
                reserved.append(pos)
    # placement (version < 7: no version info blocks)
    bits = [b for cw in codewords for b in _bits(cw, 8)]
    idx, col, upward = 0, n - 1, True
    data_cells = []
    while col > 0:
        if col == 6:
            col -= 1
        rows = range(n - 1, -1, -1) if upward else range(n)
        for r in rows:
            for c in (col, col - 1):
                if grid[r][c] is None:
                    grid[r][c] = bits[idx] if idx < len(bits) else 0
                    data_cells.append((r, c))
                    idx += 1
        upward = not upward
        col -= 2
    return grid, data_cells, reserved


_MASKS = [
    lambda r, c: (r + c) % 2 == 0, lambda r, c: r % 2 == 0, lambda r, c: c % 3 == 0,
    lambda r, c: (r + c) % 3 == 0, lambda r, c: (r // 2 + c // 3) % 2 == 0,
    lambda r, c: (r * c) % 2 + (r * c) % 3 == 0,
    lambda r, c: ((r * c) % 2 + (r * c) % 3) % 2 == 0,
    lambda r, c: ((r + c) % 2 + (r * c) % 3) % 2 == 0,
]


def _format_bits(mask):
    data = (0b00 << 3) | mask  # error level M = 00
    rem = data
    for _ in range(10):
        rem = (rem << 1) ^ ((rem >> 9) * 0x537)
    return ((data << 10) | rem) ^ 0x5412


def _penalty(g):
    n = len(g)
    score = 0
    for lines in (g, [list(col) for col in zip(*g)]):
        for line in lines:
            run, prev = 0, None
            for v in line + [None]:
                if v == prev:
                    run += 1
                else:
                    if run >= 5:
                        score += run - 2
                    run, prev = 1, v
            s = "".join(map(str, line))
            score += 40 * (s.count("10111010000") + s.count("00001011101"))
    for r in range(n - 1):
        for c in range(n - 1):
            if g[r][c] == g[r][c + 1] == g[r + 1][c] == g[r + 1][c + 1]:
                score += 3
    dark = sum(map(sum, g))
    total = n * n
    k = (abs(dark * 20 - total * 10) + total - 1) // total - 1
    score += 10 * k
    return score


def _apply_format(grid, mask):
    n = len(grid)
    fmt = _format_bits(mask)
    bit = lambda i: (fmt >> i) & 1  # noqa: E731  bit 0 is the least significant
    # first copy: down column 8 beside the top-left finder, then along row 8
    for i in range(6):
        grid[i][8] = bit(i)
    grid[7][8], grid[8][8], grid[8][7] = bit(6), bit(7), bit(8)
    for i in range(9, 15):
        grid[8][14 - i] = bit(i)
    # second copy: along row 8 at the right (bits 0-7), then down column 8
    # at the bottom (bits 8-14)
    for i in range(8):
        grid[8][n - 1 - i] = bit(i)
    for i in range(8, 15):
        grid[n - 15 + i][8] = bit(i)
    grid[n - 8][8] = 1  # the module that is always dark


def _apply_version(grid, version):
    if version < 7:
        return
    n = len(grid)
    rem = version
    for _ in range(12):
        rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
    info = (version << 12) | rem
    for i in range(18):
        b = (info >> i) & 1
        a, c = n - 11 + i % 3, i // 3
        grid[a][c] = b
        grid[c][a] = b


def qr_matrix(payload):
    version, cw = _encode_codewords(payload)
    base, cells, _ = _matrix(version, cw)
    best, best_score = None, None
    for m, fn in enumerate(_MASKS):
        g = [row[:] for row in base]
        for r, c in cells:
            if fn(r, c):
                g[r][c] ^= 1
        _apply_format(g, m)
        _apply_version(g, version)
        s = _penalty(g)
        if best_score is None or s < best_score:
            best, best_score = g, s
    return best


def qr_svg(payload, size_mm=60):
    """SVG text for `payload`; segno when present, the built-in encoder otherwise."""
    try:
        import segno  # noqa: F401
        import io
        buf = io.BytesIO()
        segno.make(payload, error="m", micro=False).save(buf, kind="svg", scale=4, border=4,
                                                         dark="#000", light="#fff", xmldecl=False)
        return buf.getvalue().decode()
    except ImportError:
        pass  # segno unavailable: use the built-in QR encoder below
    grid = qr_matrix(payload)
    n, q = len(grid), 4
    side = n + 2 * q
    path = []
    for r, row in enumerate(grid):
        c = 0
        while c < n:
            if row[c]:
                start = c
                while c < n and row[c]:
                    c += 1
                path.append(f"M{start + q} {r + q}h{c - start}v1h-{c - start}z")
            else:
                c += 1
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {side} {side}" '
            f'width="{size_mm}mm" height="{size_mm}mm" shape-rendering="crispEdges">'
            f'<rect width="{side}" height="{side}" fill="#fff"/>'
            f'<path d="{"".join(path)}" fill="#000"/></svg>\n')


# --- card -----------------------------------------------------------------

CARD = """\
<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<title>{product} help desk</title>
<style>
  @page {{ size: A4 landscape; margin: 14mm; }}
  body {{ font-family: -apple-system, "Segoe UI", Helvetica, Arial, sans-serif; color: #000;
         background: #fff; margin: 0; padding: 12mm; }}
  h1 {{ font-size: 22pt; margin: 0 0 4mm 0; }}
  p.rule {{ font-size: 13pt; margin: 0 0 8mm 0; }}
  .cols {{ display: flex; gap: 12mm; }}
  .col {{ flex: 1; border: 1.2pt solid #000; padding: 6mm; }}
  .col h2 {{ font-size: 18pt; margin: 0 0 3mm 0; }}
  .col p {{ font-size: 12.5pt; line-height: 1.35; margin: 3mm 0; }}
  .col svg {{ display: block; width: 60mm; height: 60mm; margin: 4mm 0; }}
  .addr {{ font-family: Menlo, Consolas, monospace; font-size: 11pt; word-break: break-all; }}
  footer {{ margin-top: 8mm; font-size: 10.5pt; border-top: 0.8pt solid #000; padding-top: 3mm; }}
  @media print {{ body {{ padding: 0; }} }}
</style></head>
<body>
<h1>{product} help desk</h1>
<p class="rule">Two codes. Connected to the {product_lc}? Scan A. Not connected, or cannot log in? Scan B.</p>
<div class="cols">
  <section class="col">
    <h2>A — on the {product_lc}</h2>
    {inside_svg}
    <p>Connected? Scan this. You will be asked to log in, and the desk will know who you are.</p>
    <p class="addr">{inside}</p>
  </section>
  <section class="col">
    <h2>B — cannot get on</h2>
    {outside_svg}
    <p>Not connected, or cannot log in? Scan this from any internet. Works on a phone on mobile data.</p>
    <p class="addr">{outside}</p>
  </section>
</div>
<footer>No camera? Type the address. A: <span class="addr">{inside}</span> &nbsp;·&nbsp; B: <span class="addr">{outside}</span></footer>
</body></html>
"""


def write_card(out_dir, inside, outside, product):
    os.makedirs(out_dir, exist_ok=True)
    inside_svg, outside_svg = qr_svg(inside), qr_svg(outside)
    with open(os.path.join(out_dir, "inside.svg"), "w") as f:
        f.write(inside_svg)
    with open(os.path.join(out_dir, "outside.svg"), "w") as f:
        f.write(outside_svg)
    page = CARD.format(product=html.escape(product), product_lc=html.escape(product),
                       inside=html.escape(inside), outside=html.escape(outside),
                       inside_svg=inside_svg, outside_svg=outside_svg)
    with open(os.path.join(out_dir, "card.html"), "w") as f:
        f.write(page)


def main():
    ap = argparse.ArgumentParser(description="Render the two help-desk QR codes and the printable card.")
    ap.add_argument("--site", required=True, help="site_id as declared in group_vars (zagros, zagros-prod, ...)")
    ap.add_argument("--out", help="output directory (default ./help-desk-card-<site>/)")
    ap.add_argument("--inside-url", help="override the inside address (registry: isitup_services id=help)")
    ap.add_argument("--outside-url", help="override the outside address (registry: help_desk_public_url)")
    ap.add_argument("--product-name", help="card wording (default: the site's portal_lex_product_name)")
    ap.add_argument("--check", action="store_true", help="resolve and render to a temp dir; keep nothing")
    args = ap.parse_args()

    inside, outside, lex, problems = registry_addresses(args.site)
    if args.inside_url:
        inside, problems = args.inside_url, [p for p in problems if "help" not in p or "row" not in p]
    if args.outside_url:
        outside, problems = args.outside_url, [p for p in problems if "help_desk_public_url" not in p]
    if not inside or not outside:
        for p in problems or ["addresses missing"]:
            sys.stderr.write(f"ERROR: {args.site}: {p}\n")
        sys.stderr.write("Refusing to render a card with a missing address.\n")
        sys.exit(2)
    product = args.product_name or lex or "goatnet"
    for url in (inside, outside):
        if not re.match(r"^https?://[^\s]+$", url):
            sys.stderr.write(f"ERROR: {args.site}: '{url}' is not an http(s) URL\n")
            sys.exit(2)

    if args.check:
        with tempfile.TemporaryDirectory() as tmp:
            write_card(tmp, inside, outside, product)
        print(f"ok: {args.site}: inside {inside}  outside {outside}")
        return
    out_dir = args.out or f"./help-desk-card-{args.site}"
    write_card(out_dir, inside, outside, product)
    print(f"wrote {out_dir}/inside.svg {out_dir}/outside.svg {out_dir}/card.html")
    print(f"  A inside : {inside}")
    print(f"  B outside: {outside}")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Stamp page numbers onto a PDF that was produced without them.

Every PDF we circulate needs page numbers: without them a printed brief cannot be
put back in order, and a reader cannot cite a passage to anyone else.

Why stamp rather than fix the source
------------------------------------
We produce PDFs through three different engines (headless Chrome `--print-to-pdf`,
pandoc/xelatex, weasyprint) and none of them gives us a usable footer for free:

  - Chrome's CLI has no flag for a *custom* header/footer. Dropping
    `--no-pdf-header-footer` restores Chrome's default furniture, which stamps the
    `file://` path and the print date onto every page — worse than no numbers.
    A custom footer needs `Page.printToPDF` over the DevTools Protocol, i.e. a
    Node/Puppeteer dependency we do not otherwise carry.
  - Chrome does not implement the CSS paged-media margin boxes
    (`@page { @bottom-center { content: counter(page) } }`), so the obvious
    stylesheet fix silently does nothing.

Stamping afterwards sidesteps all of it, works the same on every engine's output,
and — the reason it is safe on an already-approved document — it is an overlay.
It adds a text object to each page's existing content stream. Nothing reflows, so
pagination, layout and the rest of the type are byte-for-byte what they were.

Fonts
-----
The number is set in Helvetica declared as a base-14 standard font, so nothing is
embedded and nothing lands as a Type 3 glyph. This matters: headless Chrome on
macOS embeds `system-ui` / `-apple-system` as Type 3, and text in those copies out
of a PDF reader as unreadable private-use codepoints. Stamping never adds to that
count. Run `pdffonts <pdf> | awk '$2=="Type"&&$3=="3"'` to check a file.

Usage
-----
    ops/scripts/stamp-pdf-page-numbers.py FILE.pdf [FILE.pdf ...]
    ops/scripts/stamp-pdf-page-numbers.py FILE.pdf --format 'Page {n} of {total}'
    ops/scripts/stamp-pdf-page-numbers.py FILE.pdf --position bottom-right
    ops/scripts/stamp-pdf-page-numbers.py FILE.pdf --skip-first    # cover page unnumbered
    ops/scripts/stamp-pdf-page-numbers.py FILE.pdf --check         # report only, exit 1 if unstamped

Re-running is safe: a stamped file records `GoatPageNumbers` in its metadata and is
skipped unless `--force` is passed.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

try:
    from pypdf import PdfReader, PdfWriter
    from pypdf.generic import (
        ArrayObject,
        DecodedStreamObject,
        DictionaryObject,
        NameObject,
        TextStringObject,
    )
except ImportError:  # pragma: no cover
    sys.exit("pypdf is required: pip install pypdf")

MARKER = "GoatPageNumbers"

# Helvetica advance widths (1/1000 em) for the glyphs a page number can contain.
_WIDTHS = {
    **{str(d): 556 for d in range(10)},
    " ": 278, "/": 278, "-": 333, "–": 556, ".": 278,
    "P": 667, "a": 556, "g": 556, "e": 556, "o": 556, "f": 278,
}


def _text_width(text: str, size: float) -> float:
    return sum(_WIDTHS.get(ch, 556) for ch in text) * size / 1000.0


def _escape(text: str) -> str:
    return text.replace("\\", r"\\").replace("(", r"\(").replace(")", r"\)")


def _ensure_font(page, writer: PdfWriter) -> str:
    """Add a base-14 Helvetica to the page resources; return its resource name."""
    resources = page.get(NameObject("/Resources"))
    if resources is None:
        resources = DictionaryObject()
        page[NameObject("/Resources")] = writer._add_object(resources)
    resources = resources.get_object()

    fonts = resources.get(NameObject("/Font"))
    if fonts is None:
        fonts = DictionaryObject()
        resources[NameObject("/Font")] = fonts
    fonts = fonts.get_object()

    name = "/GoatPageNum"
    if NameObject(name) not in fonts:
        font = DictionaryObject()
        font[NameObject("/Type")] = NameObject("/Font")
        font[NameObject("/Subtype")] = NameObject("/Type1")
        font[NameObject("/BaseFont")] = NameObject("/Helvetica")
        font[NameObject("/Encoding")] = NameObject("/WinAnsiEncoding")
        fonts[NameObject(name)] = writer._add_object(font)
    return name


def _stamp_page(page, writer: PdfWriter, label: str, position: str,
                size: float, margin: float, grey: float) -> None:
    box = page.mediabox
    width = float(box.width)
    left, bottom = float(box.left), float(box.bottom)

    text_w = _text_width(label, size)
    if position == "bottom-right":
        x = left + width - margin - text_w
    elif position == "bottom-left":
        x = left + margin
    else:  # bottom-center
        x = left + (width - text_w) / 2.0
    y = bottom + margin

    font_name = _ensure_font(page, writer)
    stream = DecodedStreamObject()
    stream.set_data(
        (
            f"q\nBT\n{font_name} {size:g} Tf\n"
            f"{grey:g} {grey:g} {grey:g} rg\n"
            f"1 0 0 1 {x:.2f} {y:.2f} Tm\n"
            f"({_escape(label)}) Tj\nET\nQ\n"
        ).encode("latin-1")
    )
    stream_ref = writer._add_object(stream)

    contents = page.get(NameObject("/Contents"))
    if contents is None:
        page[NameObject("/Contents")] = stream_ref
        return
    # A page's content stream may be split across an array; appending our own
    # keeps the original bytes untouched instead of rewriting them.
    if isinstance(contents.get_object(), ArrayObject):
        contents.get_object().append(stream_ref)
    else:
        page[NameObject("/Contents")] = ArrayObject([contents, stream_ref])


def already_stamped(path: Path) -> bool:
    meta = PdfReader(str(path)).metadata or {}
    return any(str(k).lstrip("/") == MARKER for k in meta)


def stamp(path: Path, fmt: str, position: str, size: float, margin: float,
          grey: float, skip_first: bool, start: int, force: bool) -> str:
    if already_stamped(path) and not force:
        return f"skipped (already stamped): {path}"

    reader = PdfReader(str(path))
    writer = PdfWriter()
    writer.append_pages_from_reader(reader)

    total = len(writer.pages)
    numbered = total - 1 if skip_first else total
    for i, page in enumerate(writer.pages):
        if skip_first and i == 0:
            continue
        n = start + (i - 1 if skip_first else i)
        label = fmt.format(n=n, total=numbered + start - 1)
        _stamp_page(page, writer, label, position, size, margin, grey)

    meta = {k: v for k, v in (reader.metadata or {}).items()}
    meta[NameObject(f"/{MARKER}")] = TextStringObject(
        f"{position} {size:g}pt Helvetica base-14"
    )
    writer.add_metadata(meta)

    tmp = path.with_suffix(".pdf.tmp")
    with tmp.open("wb") as fh:
        writer.write(fh)
    tmp.replace(path)
    return f"stamped {total} page(s) ({numbered} numbered): {path}"


def main() -> int:
    ap = argparse.ArgumentParser(description="Stamp page numbers onto PDFs.")
    ap.add_argument("pdfs", nargs="+", type=Path)
    ap.add_argument("--format", default="{n}",
                    help="label template; {n} and {total} (default: '{n}')")
    ap.add_argument("--position", default="bottom-center",
                    choices=["bottom-center", "bottom-right", "bottom-left"])
    ap.add_argument("--size", type=float, default=9.0, help="points (default 9)")
    ap.add_argument("--margin", type=float, default=26.0,
                    help="points from the trimmed edge (default 26)")
    ap.add_argument("--grey", type=float, default=0.42,
                    help="0=black, 1=white (default 0.42)")
    ap.add_argument("--skip-first", action="store_true",
                    help="leave page 1 unnumbered (cover pages)")
    ap.add_argument("--start", type=int, default=1,
                    help="number the first numbered page as this (default 1)")
    ap.add_argument("--force", action="store_true", help="restamp an already-stamped file")
    ap.add_argument("--check", action="store_true",
                    help="report only; exit 1 if any file lacks OUR stamp")
    args = ap.parse_args()

    missing = [p for p in args.pdfs if not p.is_file()]
    if missing:
        for p in missing:
            print(f"not found: {p}", file=sys.stderr)
        return 2

    if args.check:
        unstamped = [p for p in args.pdfs if not already_stamped(p)]
        for p in args.pdfs:
            print(f"  {'OK  ' if p not in unstamped else 'MISS'}  {p}")
        if unstamped:
            print(f"\n{len(unstamped)} PDF(s) carry no stamped page numbers. Note this\n"
                  "checks for this script's stamp only — a document that numbers\n"
                  "itself in its own footer (see data-backbone-two-pager) reports MISS.",
                  file=sys.stderr)
            return 1
        return 0

    for p in args.pdfs:
        print(stamp(p, args.format, args.position, args.size, args.margin,
                    args.grey, args.skip_first, args.start, args.force))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

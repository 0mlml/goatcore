#!/usr/bin/env python3
"""write-tar.py — deterministic ustar + gzip writer for the federation bundle.

Called by make-bundle.sh. It exists as its own file, in Python rather than as
`tar` flags, for one reason: **byte-identical output must not depend on which
tar is installed.**

GNU tar and bsdtar disagree on defaults that land in the archive — pax vs ustar
headers, whether a directory entry is written, how an empty user name is
encoded — and their flag spellings for pinning those differ too. A bundle built
on a Linux CI runner and one built on an operator's macOS laptop would then
differ in bytes while carrying identical files, which is the false mismatch
docs/design/federation-package-spec.md §9 says destroys confidence in a digest
comparison permanently. Writing the header fields ourselves removes the
question.

Every field that could carry the build environment is pinned:

  mtime   0        no build time
  uid/gid 0        no build account
  uname   ""       no build account
  gname   ""       no build account
  mode    0644     0755 for files staged executable, nothing else
  type    regular  no directory entries at all; extraction creates them
  format  USTAR    no pax extended headers, no GNU extensions
  order   byte-wise ascending on the archive path

The gzip member is written with mtime 0 and no embedded filename. gzip output
can still differ between zlib versions, so make-bundle.sh prints the digest of
the uncompressed tar as well — if two builds' .tar.gz differ and their .tar
agree, the difference is the compressor, not the content.
"""

import argparse
import gzip
import io
import os
import sys
import tarfile

FIXED_MTIME = 0


def staged_files(root):
    """Every regular file under root, as (absolute path, archive-relative path),
    ordered byte-wise ascending on the relative path."""
    found = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        for name in filenames:
            abs_path = os.path.join(dirpath, name)
            if os.path.islink(abs_path):
                sys.exit("write-tar: symlink in staging, refusing: %s" % abs_path)
            rel = os.path.relpath(abs_path, root)
            found.append((abs_path, rel))
    # Byte-wise on the encoded path: the same ordering LC_ALL=C sort gives the
    # manifest, so the archive and the manifest cannot disagree about order.
    found.sort(key=lambda pair: pair[1].encode("utf-8"))
    return found


def build_tar(root, prefix):
    """Return the uncompressed tar as bytes."""
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w", format=tarfile.USTAR_FORMAT) as tar:
        for abs_path, rel in staged_files(root):
            info = tarfile.TarInfo(name="%s/%s" % (prefix, rel))
            st = os.stat(abs_path)
            info.size = st.st_size
            info.mtime = FIXED_MTIME
            info.mode = 0o755 if st.st_mode & 0o100 else 0o644
            info.type = tarfile.REGTYPE
            info.uid = 0
            info.gid = 0
            info.uname = ""
            info.gname = ""
            with open(abs_path, "rb") as fh:
                tar.addfile(info, fh)
    return buf.getvalue()


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--root", required=True, help="staging directory to archive")
    ap.add_argument("--prefix", required=True, help="top-level directory name inside the archive")
    ap.add_argument("--tgz-out", required=True, help="path to write the .tar.gz to")
    ap.add_argument("--tar-digest-out", help="write the uncompressed tar to this path too")
    args = ap.parse_args()

    if not os.path.isdir(args.root):
        sys.exit("write-tar: not a directory: %s" % args.root)

    raw = build_tar(args.root, args.prefix)

    if args.tar_digest_out:
        with open(args.tar_digest_out, "wb") as fh:
            fh.write(raw)

    with open(args.tgz_out, "wb") as fh:
        # filename="" so the archive does not carry the output path; mtime=0 so
        # it does not carry the build time. Both are gzip header fields, and
        # both default to values that change between builds.
        with gzip.GzipFile(filename="", mode="wb", compresslevel=9, fileobj=fh, mtime=0) as gz:
            gz.write(raw)


if __name__ == "__main__":
    main()

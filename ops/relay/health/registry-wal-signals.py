#!/usr/bin/env python3
"""registry-wal-signals — gauges on the portal registry's SQLite WAL itself.

Run by registry-wal-signals.timer (rendered by ops/ansible/roles/relay-health)
on the host that carries the goat-zenoh-portal registry. Writes one
node-exporter textfile so rules/registry-wal.yml can alert on the FILE.

WHY THIS EXISTS. On 2026-08-15 registry.db-wal on mgmt-zagros-01-thor was
20.6 GiB over a 3.9 MiB database: a long-lived reader pinned the checkpoint,
and a checkpoint can never fold frames past the oldest reader's snapshot
(docs/operations/disk-runway-and-retention.md). journal_size_limit(64 MiB)
bounded the high-water mark, but the cause stayed open — and the leak
recurred, growing the WAL to 91.4 GB unnoticed. ThorDiskRunwayShort could not
see it: it derives runway from the 24 h AVERAGE burn, and the recurrence wrote
in ~200 GB/day bursts that averaged to a ~19-day runway, above the 14-day
threshold the whole time. An average is the wrong instrument for a bursty
writer; the file itself is the signal, and a gauge on it catches this class in
minutes.

What it emits (all gauges):

  goat_registry_files_ok          1 = the main db exists, 0 = it does not
  goat_registry_db_bytes          size of registry.db
  goat_registry_db_mtime_seconds  mtime of registry.db — the last time a
                                  checkpoint folded WAL frames back into the
                                  main file, i.e. the last SUCCESSFUL
                                  checkpoint the filesystem saw
  goat_registry_wal_bytes         size of registry.db-wal
  goat_registry_shm_bytes         size of registry.db-shm

Honesty: an absent file OMITS its gauges rather than emitting zero — the
null-not-zero convention from zenoh-router-signals.py. A missing db is
files_ok=0 with no size gauges ("not measured"), never a 0-byte reading that
reads as a healthy empty WAL. A missing -wal beside a present db is also
omitted: SQLite deletes the WAL on a clean close, and "no WAL right now" is
not a measurement of its size.

Staleness: textfile metrics never expire, so a dead timer freezes these at
their last value. node-exporter already ages the file itself
(node_textfile_mtime_seconds{file="registry-wal.prom"}); no second clock is
emitted here.

Usage:
  registry-wal-signals.py [--registry /opt/goat-zenoh-portal/registry.db] \
      [--out /path/registry-wal.prom]      # no --out: exposition on stdout
"""
import argparse
import os
import sys

DEFAULT_REGISTRY = "/opt/goat-zenoh-portal/registry.db"

# name -> HELP text, in emission order.
HELP = {
    "goat_registry_files_ok":
        "1 when the portal registry main db exists, 0 when it does not",
    "goat_registry_db_bytes":
        "Size of the portal registry main db file in bytes",
    "goat_registry_db_mtime_seconds":
        "Unix mtime of the main db: the last checkpoint that folded WAL "
        "frames back",
    "goat_registry_wal_bytes":
        "Size of the portal registry WAL file in bytes",
    "goat_registry_shm_bytes":
        "Size of the portal registry shared-memory index file in bytes",
}


def stat_or_none(path):
    """os.stat, or None when the file is absent/unreadable. Never a zero."""
    try:
        return os.stat(path)
    except OSError:
        return None


def collect(registry_path):
    """Measure the three files. Absent file -> its keys are simply not there."""
    out = {}
    db = stat_or_none(registry_path)
    out["goat_registry_files_ok"] = 1 if db is not None else 0
    if db is None:
        return out
    out["goat_registry_db_bytes"] = db.st_size
    out["goat_registry_db_mtime_seconds"] = int(db.st_mtime)
    wal = stat_or_none(registry_path + "-wal")
    if wal is not None:
        out["goat_registry_wal_bytes"] = wal.st_size
    shm = stat_or_none(registry_path + "-shm")
    if shm is not None:
        out["goat_registry_shm_bytes"] = shm.st_size
    return out


def render(samples):
    """Exposition text: HELP + TYPE + sample per present gauge, in HELP order."""
    lines = []
    for name in HELP:
        if name not in samples:
            continue
        lines.append("# HELP %s %s" % (name, HELP[name]))
        lines.append("# TYPE %s gauge" % name)
        lines.append("%s %d" % (name, samples[name]))
    return "\n".join(lines) + "\n"


def write_atomic(path, text):
    """tmp + rename, so node-exporter never scrapes a half-written file."""
    tmp = path + ".tmp"
    with open(tmp, "w") as f:
        f.write(text)
    os.replace(tmp, path)


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--registry", default=DEFAULT_REGISTRY,
                    help="path to the portal registry main db")
    ap.add_argument("--out", default="",
                    help="textfile to write; empty writes exposition to stdout")
    a = ap.parse_args(argv)

    text = render(collect(a.registry))
    if a.out:
        write_atomic(a.out, text)
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())

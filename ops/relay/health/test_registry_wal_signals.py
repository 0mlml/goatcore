"""Unit tests for registry-wal-signals.py (stdlib only; run with unittest discover)."""
import importlib.util
import io
import os
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location("walsignals", HERE / "registry-wal-signals.py")
walsignals = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(walsignals)


def parse_prom(text):
    """{metric: value} from exposition text, ignoring HELP/TYPE lines."""
    out = {}
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        name, value = line.split()
        out[name] = float(value)
    return out


class CollectTests(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.db = os.path.join(self.dir.name, "registry.db")

    def tearDown(self):
        self.dir.cleanup()

    def write(self, suffix, size):
        with open(self.db + suffix, "wb") as f:
            f.write(b"\0" * size)

    def test_absent_db_is_ok_zero_with_no_size_gauges(self):
        # The null-not-zero convention: a missing db must not read as a
        # healthy 0-byte database with a 1970 checkpoint.
        samples = walsignals.collect(self.db)
        self.assertEqual(samples, {"goat_registry_files_ok": 0})

    def test_all_three_files_measured(self):
        self.write("", 4096)
        self.write("-wal", 128 * 1024)
        self.write("-shm", 32768)
        samples = walsignals.collect(self.db)
        self.assertEqual(samples["goat_registry_files_ok"], 1)
        self.assertEqual(samples["goat_registry_db_bytes"], 4096)
        self.assertEqual(samples["goat_registry_wal_bytes"], 128 * 1024)
        self.assertEqual(samples["goat_registry_shm_bytes"], 32768)
        self.assertEqual(samples["goat_registry_db_mtime_seconds"],
                         int(os.stat(self.db).st_mtime))

    def test_db_mtime_is_the_main_file_not_the_wal(self):
        # The stalled-checkpoint alert ages THIS clock: frames landing in the
        # WAL move the WAL's mtime, not the main db's, until a checkpoint
        # folds them back.
        self.write("", 4096)
        os.utime(self.db, (1000, 2000))
        self.write("-wal", 512)
        samples = walsignals.collect(self.db)
        self.assertEqual(samples["goat_registry_db_mtime_seconds"], 2000)

    def test_absent_wal_beside_present_db_is_omitted_not_zero(self):
        # SQLite deletes the WAL on a clean close; "no WAL" is not a 0-byte
        # measurement of one.
        self.write("", 4096)
        samples = walsignals.collect(self.db)
        self.assertEqual(samples["goat_registry_files_ok"], 1)
        self.assertNotIn("goat_registry_wal_bytes", samples)
        self.assertNotIn("goat_registry_shm_bytes", samples)

    def test_empty_wal_file_is_a_real_zero(self):
        # A present 0-byte WAL IS a measurement — only absence is omitted.
        self.write("", 4096)
        self.write("-wal", 0)
        self.assertEqual(walsignals.collect(self.db)["goat_registry_wal_bytes"], 0)


class RenderTests(unittest.TestCase):
    def test_every_sample_carries_help_and_type(self):
        text = walsignals.render({
            "goat_registry_files_ok": 1,
            "goat_registry_db_bytes": 4096,
            "goat_registry_db_mtime_seconds": 1700000000,
            "goat_registry_wal_bytes": 268435456,
            "goat_registry_shm_bytes": 32768,
        })
        for name in walsignals.HELP:
            self.assertIn("# HELP %s " % name, text)
            self.assertIn("# TYPE %s gauge\n" % name, text)
        self.assertEqual(parse_prom(text)["goat_registry_wal_bytes"], 268435456)

    def test_omitted_sample_emits_no_help_type_or_value(self):
        # node_exporter parses the whole file; a TYPE line with no sample is
        # noise, and a sample for an absent file would be the fabricated zero
        # this collector exists to not emit.
        text = walsignals.render({"goat_registry_files_ok": 0})
        self.assertEqual(parse_prom(text), {"goat_registry_files_ok": 0.0})
        self.assertNotIn("goat_registry_wal_bytes", text)
        self.assertNotIn("goat_registry_db_bytes", text)

    def test_ends_with_exactly_one_newline(self):
        # The textfile collector refuses a file whose last line is unterminated
        # (the F-449-adjacent exposition lint exists for the template variant
        # of this same bug — ops/ci/check-textfile-exposition.sh).
        text = walsignals.render({"goat_registry_files_ok": 1})
        self.assertTrue(text.endswith("\n"))
        self.assertFalse(text.endswith("\n\n"))


class MainTests(unittest.TestCase):
    def test_writes_textfile_and_leaves_no_tmp(self):
        with tempfile.TemporaryDirectory() as d:
            db = os.path.join(d, "registry.db")
            Path(db).write_bytes(b"\0" * 4096)
            Path(db + "-wal").write_bytes(b"\0" * 8192)
            out = os.path.join(d, "registry-wal.prom")
            walsignals.main(["--registry", db, "--out", out])
            samples = parse_prom(Path(out).read_text())
            self.assertEqual(samples["goat_registry_files_ok"], 1.0)
            self.assertEqual(samples["goat_registry_wal_bytes"], 8192.0)
            self.assertFalse(os.path.exists(out + ".tmp"))

    def test_absent_db_still_writes_ok_zero(self):
        # The alerting side needs the 0 to exist: files_ok=0 is a statement,
        # an unwritten file is indistinguishable from a dead timer.
        with tempfile.TemporaryDirectory() as d:
            out = os.path.join(d, "registry-wal.prom")
            walsignals.main(["--registry", os.path.join(d, "absent.db"),
                             "--out", out])
            self.assertEqual(parse_prom(Path(out).read_text()),
                             {"goat_registry_files_ok": 0.0})

    def test_stdout_when_no_out(self):
        with tempfile.TemporaryDirectory() as d:
            db = os.path.join(d, "registry.db")
            Path(db).write_bytes(b"\0" * 1024)
            buf = io.StringIO()
            with redirect_stdout(buf):
                walsignals.main(["--registry", db])
            self.assertEqual(parse_prom(buf.getvalue())["goat_registry_db_bytes"], 1024.0)


if __name__ == "__main__":
    unittest.main()

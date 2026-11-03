"""Read-only slot census closes its connection on successful and failed reads."""
import importlib.util
from pathlib import Path
import sqlite3
import tempfile
import unittest
import unittest.mock as mock


class SlotCensus(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location("slot_sync", Path(__file__).with_name("sync-slot-anchors.py"))
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.db = Path(self.directory.name) / "registry.db"

    def read(self):
        connections = []
        connect = sqlite3.connect

        class Connection(sqlite3.Connection):
            closed = False

            def close(self):
                self.closed = True
                super().close()

        def tracked(*args, **kwargs):
            connection = connect(*args, factory=Connection, **kwargs)
            connections.append(connection)
            return connection

        with mock.patch.object(self.module.sqlite3, "connect", side_effect=tracked):
            try:
                return self.module.live_anchors(str(self.db))
            finally:
                for connection in connections:
                    self.assertTrue(connection.closed, "slot reader leaked its SQLite connection")

    def test_live_slots_are_sorted_and_closed(self):
        connection = sqlite3.connect(self.db)
        try:
            connection.execute("CREATE TABLE slots(slot_id TEXT, assigned_to TEXT, revoked_at INTEGER)")
            connection.executemany("INSERT INTO slots VALUES(?,?,?)", [
                ("b", "person", None), ("a", "person", None),
                ("revoked", "person", 1), ("empty", "", None), ("null", None, None),
            ])
            connection.commit()
        finally:
            connection.close()
        self.assertEqual(self.read(), ["a", "b"])

    def test_failed_query_is_unreadable_and_closed(self):
        sqlite3.connect(self.db).close()
        with self.assertRaises(SystemExit) as error:
            self.read()
        self.assertEqual(error.exception.code, 2)

    def test_missing_source_is_not_created(self):
        with self.assertRaises(SystemExit) as error:
            self.read()
        self.assertEqual(error.exception.code, 2)
        self.assertFalse(self.db.exists())


if __name__ == "__main__":
    unittest.main()

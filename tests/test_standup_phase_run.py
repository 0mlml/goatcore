#!/usr/bin/env python3
"""ops/scripts/standup-phase-run: a phase whose $(printenv NAME) is empty refuses and
names NAME; a value store's values reach the phase without being exported by hand."""
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

RUN = Path(__file__).resolve().parents[1] / "ops/scripts/standup-phase-run"
CMD = 'test -n "$(printenv GALAXY_PG_PASS)" && echo ran'


def run(cmd, store, extra=None, check=False):
    env = {"PATH": os.environ["PATH"], "HOME": tempfile.mkdtemp(), "GOAT_SECRET_STORE": store}
    env.update(extra or {})
    args = [str(RUN)] + (["--check"] if check else []) + [cmd]
    return subprocess.run(args, capture_output=True, text=True, env=env)


class Refusal(unittest.TestCase):
    def test_an_empty_name_refuses_and_is_named(self):
        r = run(CMD, "env")
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertIn("GALAXY_PG_PASS", r.stderr)
        self.assertNotIn("ran", r.stdout, "the phase never ran")

    def test_every_empty_name_is_named_at_once(self):
        r = run('x="$(printenv A_ONE)" y="$(printenv A_TWO)"; true', "env", {"A_ONE": ""})
        self.assertEqual(r.returncode, 3)
        self.assertIn("A_ONE A_TWO", r.stderr)

    def test_a_reference_store_refuses_too(self):
        self.assertEqual(run(CMD, "1password").returncode, 3)

    def test_a_set_name_runs_the_phase(self):
        r = run(CMD, "env", {"GALAXY_PG_PASS": "s3cret-value"})
        self.assertEqual((r.returncode, r.stdout.strip()), (0, "ran"), r.stderr)
        self.assertNotIn("s3cret-value", r.stdout + r.stderr)

    def test_check_only_never_runs_the_command(self):
        r = run("echo ran", "env", check=True)
        self.assertEqual((r.returncode, r.stdout), (0, ""))

    def test_the_phase_status_is_kept(self):
        self.assertEqual(run("exit 7", "env").returncode, 7)


class ValueStore(unittest.TestCase):
    def test_phase_3_values_reach_a_later_phase_from_the_env_file(self):
        d = Path(tempfile.mkdtemp())
        f = d / "secrets.env"
        f.write_text("GALAXY_PG_PASS=from-the-file\n")
        f.chmod(0o600)
        r = run(CMD, "env-file", {"GOAT_SECRET_ENV_FILE": str(f)})
        self.assertEqual((r.returncode, r.stdout.strip()), (0, "ran"), r.stderr)
        self.assertNotIn("from-the-file", r.stdout + r.stderr, "no value is printed")

    def test_an_env_file_without_the_name_still_refuses(self):
        d = Path(tempfile.mkdtemp())
        f = d / "secrets.env"
        f.write_text("OTHER=x\n")
        r = run(CMD, "env-file", {"GOAT_SECRET_ENV_FILE": str(f)})
        self.assertEqual(r.returncode, 3)
        self.assertIn("GALAXY_PG_PASS", r.stderr)


KEEP = Path(__file__).resolve().parents[1] / "ops/scripts/standup-keep-secret"


class KeepSecret(unittest.TestCase):
    """A value a step mints (the Forgejo mirror token) reaches the phases after it: under a
    value store it is written to the store; under a reference store nothing changes."""

    def keep(self, store, extra):
        env = {"PATH": os.environ["PATH"], "HOME": tempfile.mkdtemp(), "GOAT_SECRET_STORE": store}
        env.update(extra)
        return subprocess.run([str(KEEP), "FORGEJO_PUBSUB_TOKEN"], capture_output=True, text=True, env=env)

    def test_a_value_store_keeps_it_and_the_next_phase_reads_it(self):
        f = Path(tempfile.mkdtemp()) / "secrets.env"
        f.write_text("OTHER=x\n")
        f.chmod(0o600)
        r = self.keep("env-file", {"GOAT_SECRET_ENV_FILE": str(f), "FORGEJO_PUBSUB_TOKEN": "minted-token"})
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("minted-token", r.stdout + r.stderr, "never printed")
        nxt = run('test -n "$(printenv FORGEJO_PUBSUB_TOKEN)" && echo ran', "env-file", {"GOAT_SECRET_ENV_FILE": str(f)})
        self.assertEqual(nxt.stdout.strip(), "ran", nxt.stderr)
        again = self.keep("env-file", {"GOAT_SECRET_ENV_FILE": str(f), "FORGEJO_PUBSUB_TOKEN": "a-second-token"})
        self.assertIn("kept", again.stdout, "an existing value is never rotated")

    def test_a_reference_store_is_left_as_it_was(self):
        r = self.keep("1password", {"FORGEJO_PUBSUB_TOKEN": "minted-token"})
        self.assertEqual(r.returncode, 0)
        self.assertIn("not a value store", r.stdout)

    def test_an_empty_token_is_refused(self):
        f = Path(tempfile.mkdtemp()) / "secrets.env"
        self.assertEqual(self.keep("env-file", {"GOAT_SECRET_ENV_FILE": str(f)}).returncode, 1)
        self.assertFalse(f.exists(), "nothing written")


if __name__ == "__main__":
    unittest.main()

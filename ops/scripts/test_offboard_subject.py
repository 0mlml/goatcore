#!/usr/bin/env python3
"""Manifest resolution and real offboard.sh dispatch, with no live commands."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

import yaml
import offboard_subject as subject

HERE = Path(__file__).resolve().parent


class SubjectTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.operators = self.root / "ops/operators"
        self.manifest()

    def manifest(self, goatnet="test", handle="example-user", email="person@example.test",
                 github="example"):
        directory = self.operators / goatnet
        directory.mkdir(parents=True, exist_ok=True)
        path = directory / f"{handle}.yml"
        path.write_text(yaml.safe_dump({
            "identity": {"handle": handle, "email": email, "status": "active",
                         **({"github": github} if github is not None else {})},
            "org": {"short_tag": "example-org"},
        }))
        return path

    def resolve(self, value="example-user", goatnets=None):
        return subject.resolve(self.operators, value, goatnets or ["test"])

    def test_both_inputs_and_email_case(self):
        for value in ["example-user", "person@example.test", "PERSON@EXAMPLE.TEST"]:
            with self.subTest(value=value):
                self.assertEqual(self.resolve(value),
                                 [("test", "example-user", "person@example.test", "example")])

    def test_unicode_email_is_not_an_ascii_alias(self):
        self.manifest(email="k@example.test")
        with self.assertRaises(subject.Refusal):
            self.resolve("K@example.test")
        self.manifest(handle="unicode-user", email="K@example.test")
        self.assertEqual(self.resolve("K@EXAMPLE.TEST")[0][1], "example-user")
        self.assertEqual(self.resolve("K@EXAMPLE.TEST")[0][1], "unicode-user")

    def test_unicode_conflict_prevents_all_driver_calls(self):
        self.manifest(email="k@example.test")
        self.manifest("test-two", email="K@example.test")
        for apply in [False, True]:
            with self.subTest(apply=apply):
                result, calls = self.driver(goatnets=["test", "test-two"], apply=apply)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])
                self.assertEqual((self.home / ".goat/offboard").exists(), apply)

    def test_multiple_goatnets_and_duplicate_selection(self):
        self.manifest("test-two", email="PERSON@example.test")
        self.assertEqual(len(self.resolve(goatnets=["test", "test-two", "test"])), 2)

    def test_email_can_have_different_handles_between_goatnets(self):
        self.manifest("test-two", handle="another-handle")
        self.assertEqual(self.resolve("person@example.test", ["test", "test-two"])[1][1], "another-handle")

    def test_missing_subject_and_goatnet(self):
        for value, envs in [("absent", ["test"]), ("absent@example.test", ["test"]),
                            ("example-user", ["test", "missing"])]:
            with self.subTest(value=value, envs=envs), self.assertRaises(subject.Refusal):
                self.resolve(value, envs)

    def test_invalid_subject_or_goatnet(self):
        for value in ["../example-user", "-flag", "user\tname@example.test", "x@y@z", ""]:
            with self.subTest(value=value), self.assertRaises(subject.Refusal):
                self.resolve(value)
        with self.assertRaises(subject.Refusal):
            self.resolve(goatnets=["../test"])
        with self.assertRaises(subject.Refusal):
            subject.resolve(self.operators, "example-user", [])

    def test_duplicate_email_refuses_both_input_forms(self):
        self.manifest(handle="other", email="PERSON@example.test")
        for value in ["example-user", "person@example.test"]:
            with self.subTest(value=value), self.assertRaises(subject.Refusal):
                self.resolve(value)

    def test_conflicting_identity_between_goatnets(self):
        self.manifest("test-two", email="different@example.test")
        with self.assertRaises(subject.Refusal):
            self.resolve(goatnets=["test", "test-two"])

    def test_conflicting_github_logins_name_the_goatnets(self):
        self.manifest("test-two", github="different")
        rows = self.resolve(goatnets=["test", "test-two"])
        with self.assertRaises(subject.Refusal) as caught:
            subject.resolve_github(rows, "", False, self.operators)
        self.assertIn("test=example", str(caught.exception))
        self.assertIn("test-two=different", str(caught.exception))

    def test_invalid_github_login_refuses_without_echoing_manifest(self):
        path = self.operators / "test/example-user.yml"
        for github in ("@secret-login", "-secret-login", "bad_name", "x" * 40, 123):
            value = {"identity": {"handle": "example-user",
                                  "email": "person@example.test",
                                  "github": github,
                                  "note": "do-not-echo-this"}}
            path.write_text(yaml.safe_dump(value))
            with self.subTest(github=github), self.assertRaises(subject.Refusal) as caught:
                self.resolve()
            self.assertEqual(str(caught.exception),
                             "example-user.yml: invalid identity.github")
            self.assertNotIn(str(github), str(caught.exception))
            self.assertNotIn("do-not-echo-this", str(caught.exception))

    def test_invalid_github_login_in_nonmatching_manifest_is_ignored(self):
        self.manifest(handle="teammate", email="teammate@example.test",
                      github="@secret-login")
        self.assertEqual(self.resolve(),
                         [("test", "example-user", "person@example.test", "example")])

    def test_github_binding_uses_manifest_and_matches_case_insensitively(self):
        rows = self.resolve()
        self.assertEqual(subject.resolve_github(rows, "", False, self.operators), ("example", "manifest"))
        self.assertEqual(subject.resolve_github(rows, "ExAmPlE", False, self.operators),
                         ("example", "manifest"))

    def test_github_binding_requires_acknowledgement_when_manifest_omits_login(self):
        self.manifest(github=None)
        rows = self.resolve()
        with self.assertRaises(subject.Refusal):
            subject.resolve_github(rows, "example", False, self.operators)
        self.assertEqual(subject.resolve_github(rows, "example", True, self.operators),
                         ("example", "operator-unverified"))

    def test_unverified_login_owned_by_another_person_is_refused(self):
        self.manifest(github=None)
        self.manifest(goatnet="other", handle="teammate", email="teammate@example.test",
                      github="Teammate-Login")
        rows = self.resolve()
        with self.assertRaises(subject.Refusal) as caught:
            subject.resolve_github(rows, "teammate-login", True, self.operators)
        self.assertIn("teammate", str(caught.exception))
        self.assertEqual(subject.resolve_github(rows, "unclaimed-login", True, self.operators),
                         ("unclaimed-login", "operator-unverified"))

    def test_unverified_login_named_by_the_subject_elsewhere_is_accepted(self):
        self.manifest(github=None)
        self.manifest(goatnet="other", handle="example-user", email="PERSON@example.test",
                      github="example")
        rows = self.resolve()
        self.assertEqual(subject.resolve_github(rows, "example", True, self.operators),
                         ("example", "operator-unverified"))

    def test_unverified_login_refused_when_another_manifest_is_unreadable(self):
        self.manifest(github=None)
        (self.operators / "other").mkdir()
        (self.operators / "other/broken.yml").write_text("identity: [")
        rows = self.resolve()
        with self.assertRaises(subject.Refusal):
            subject.resolve_github(rows, "unclaimed-login", True, self.operators)

    def test_invalid_manifests_fail_without_echoing_content(self):
        path = self.operators / "test/example-user.yml"
        for body in ["[", "null", "[]", "identity: []", "identity: {}",
                     'identity: {handle: wrong, email: person@example.test}',
                     'identity: {handle: example-user, email: 123}',
                     'identity: {handle: example-user, email: "-flag@example.test"}',
                     'identity: {handle: example-user, email: "p\\nerson@example.test"}',
                     'identity: {handle: example-user, email: first@example.test, email: secret@example.test}',
                     'identity: {handle: example-user, email: secret@example.test}\nidentity: {}']:
            path.write_text(body)
            with self.subTest(body=body), self.assertRaises(subject.Refusal) as caught:
                self.resolve()
            self.assertNotIn("secret@example.test", str(caught.exception))

    def test_symlink_manifest_and_directory_refused(self):
        path = self.operators / "test/example-user.yml"
        original = self.root / "original.yml"
        path.rename(original)
        path.symlink_to(original)
        with self.assertRaises(subject.Refusal):
            self.resolve()
        (self.operators / "linked").symlink_to(self.operators / "test", target_is_directory=True)
        with self.assertRaises(subject.Refusal):
            self.resolve(goatnets=["linked"])

    def test_cli_emits_no_partial_mapping(self):
        result = subprocess.run([sys.executable, str(HERE / "offboard_subject.py"),
                                 "--operators-dir", str(self.operators), "--subject", "example-user",
                                 "--goatnet", "test", "--goatnet", "missing"],
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("REFUSE", result.stderr)

    def driver(self, value="example-user", goatnets=None, apply=False, all_goatnets=False):
        scripts = self.root / "ops/scripts"
        scripts.mkdir(parents=True, exist_ok=True)
        shutil.copy(HERE / "offboard_subject.py", scripts)
        shutil.copy(HERE / "offboard_driver.py", scripts)
        bindir = self.root / "bin"
        bindir.mkdir(exist_ok=True)
        log = self.root / "calls.jsonl"
        log.write_text("")
        stub = f'''#!{sys.executable}
import json, os, pathlib, sys
name = pathlib.Path(sys.argv[0]).name
if name == "git":
    print(os.environ["FIXTURE_ROOT"])
else:
    with open(os.environ["FIXTURE_LOG"], "a") as stream:
        stream.write(json.dumps([name] + sys.argv[1:]) + "\\n")
    if name == "goat" and "status" in sys.argv:
        print('{{"mesh_status":{{"read_ok":true,"peer_ids":[],"errors":[]}}}}')
'''
        for name in ["git", "goat", "gh", "op"]:
            executable = bindir / name
            executable.write_text(stub)
            executable.chmod(0o755)
        args = ["bash", str(HERE / "offboard.sh"), value]
        for goatnet in goatnets or ["test"]:
            args += ["--goatnet", goatnet]
        if all_goatnets:
            args.append("--all-goatnets")
        if apply:
            args.append("--apply")
        env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ["PATH"],
                   HOME=str(self.home), FIXTURE_ROOT=str(self.root), FIXTURE_LOG=str(log))
        env.pop("GOAT_PROFILE_DIR", None)
        result = subprocess.run(args, cwd=self.root, env=env, capture_output=True, text=True)
        calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
        return result, calls

    def test_driver_dispatches_email_and_handle_for_both_inputs(self):
        for value in ["example-user", "PERSON@example.test"]:
            with self.subTest(value=value):
                result, calls = self.driver(value)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("test: example-user <person@example.test>", result.stdout)
                self.assertEqual(calls, [])

    def test_driver_plan_has_no_mutations_or_receipt_directory(self):
        result, calls = self.driver()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("test: example-user <person@example.test>", result.stdout)
        self.assertEqual(calls, [])
        self.assertFalse((self.home / ".goat/offboard").exists())

    def test_driver_invalid_second_environment_prevents_all_calls(self):
        for apply in [False, True]:
            with self.subTest(apply=apply):
                result, calls = self.driver(goatnets=["test", "missing"], apply=apply)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])
                self.assertEqual((self.home / ".goat/offboard").exists(), apply)

    def test_driver_conflicting_second_environment_prevents_all_calls(self):
        self.manifest("test-two", email="someone-else@example.test")
        result, calls = self.driver(goatnets=["test", "test-two"], apply=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

    def test_plain_yaml_on_and_no_handles(self):
        for handle in ["on", "no"]:
            path = self.manifest("plain", handle=handle, email=f"{handle}@example.test")
            path.write_text(f"identity: {{handle: {handle}, email: {handle}@example.test}}\n")
            self.assertEqual(self.resolve(handle, ["plain"])[0][1], handle)

    def test_driver_rejects_glob_or_whitespace_goatnet(self):
        self.manifest("test-two")
        (self.root / "test").mkdir()
        (self.root / "test-two").mkdir()
        for value in ["test*", "test test-two"]:
            with self.subTest(value=value):
                result, calls = self.driver(goatnets=[value], apply=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])
                self.assertTrue((self.home / ".goat/offboard").exists())

    def test_driver_all_goatnets_preflight_before_mutations(self):
        declared = self.root / "ops/goatnets/cross-env.yaml"
        declared.parent.mkdir(parents=True, exist_ok=True)
        declared.write_text("goatnets:\n"
                            "  zagros_prod: {id: zagros-prod, role: production}\n"
                            "  zagros: {id: zagros, role: pre-prod}\n"
                            "  zagros_dev: {id: zagros-dev, role: dev}\n")
        for goatnet in ["zagros-dev", "zagros"]:
            self.manifest(goatnet)
        result, calls = self.driver(apply=True, all_goatnets=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])
        self.assertTrue((self.home / ".goat/offboard").exists())
        self.manifest("zagros-prod")
        result, calls = self.driver(all_goatnets=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, [])
        for goatnet in ["zagros-dev", "zagros", "zagros-prod"]:
            self.assertIn(f"{goatnet}: example-user <person@example.test>", result.stdout)

    def test_driver_bad_mapping_refuses_before_effects(self):
        path = self.operators / "test/example-user.yml"
        for body in ["[", "identity: {handle: wrong, email: person@example.test}",
                     "identity: {handle: example-user, email: a@b, email: c@d}"]:
            path.write_text(body)
            result, calls = self.driver(apply=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(calls, [])
            self.assertTrue((self.home / ".goat/offboard").exists())

    def test_driver_multi_environment_dispatch(self):
        self.manifest("test-two", handle="other-handle")
        result, calls = self.driver("person@example.test", ["test", "test-two"])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, [])
        self.assertIn("test: example-user <person@example.test>", result.stdout)
        self.assertIn("test-two: other-handle <person@example.test>", result.stdout)


if __name__ == "__main__":
    unittest.main()

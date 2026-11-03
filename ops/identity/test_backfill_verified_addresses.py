"""The planner behind backfill-verified-addresses.py: who gets marked, who is left alone."""

import contextlib
import copy
import importlib.util
import io
import json
import os
import sys
import unittest
from urllib.parse import unquote
from unittest.mock import patch

HERE = os.path.dirname(os.path.abspath(__file__))
SPEC = importlib.util.spec_from_file_location(
    "backfill_verified_addresses", os.path.join(HERE, "backfill-verified-addresses.py")
)
backfill = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = backfill
SPEC.loader.exec_module(backfill)


def identity(email, verified=False, credentials=(), addresses=None):
    return {
        "id": f"id-{email}",
        "traits": {"email": email},
        "credentials": list(credentials),
        "verifiable_addresses": (
            addresses
            if addresses is not None
            else [{"value": email, "via": "email", "verified": verified}]
        ),
    }


class PlanTest(unittest.TestCase):
    def test_a_redeemed_unverified_identity_is_marked(self):
        actions, skipped = backfill.plan([identity("a@x.test", credentials=("password",))])
        self.assertEqual([a["email"] for a in actions], ["a@x.test"])
        self.assertEqual(actions[0]["index"], 0)
        self.assertEqual(skipped, [])

    def test_a_webauthn_only_identity_counts_as_redeemed(self):
        actions, _ = backfill.plan([identity("a@x.test", credentials=("webauthn",))])
        self.assertEqual(len(actions), 1)

    def test_an_already_verified_identity_is_left_alone(self):
        actions, skipped = backfill.plan([identity("a@x.test", verified=True, credentials=("password",))])
        self.assertEqual(actions, [])
        self.assertIn("already verified", skipped[0]["why"])

    def test_an_unredeemed_identity_is_skipped_unless_all(self):
        rows = [identity("a@x.test")]
        actions, skipped = backfill.plan(rows)
        self.assertEqual(actions, [])
        self.assertIn("not redeemed", skipped[0]["why"])
        actions, skipped = backfill.plan(rows, mark_all=True)
        self.assertEqual(len(actions), 1)

    def test_only_limits_the_run_to_one_email_case_insensitively(self):
        rows = [identity("a@x.test", credentials=("password",)), identity("b@x.test", credentials=("password",))]
        actions, skipped = backfill.plan(rows, only="A@X.TEST")
        self.assertEqual([a["email"] for a in actions], ["a@x.test"])
        self.assertEqual(skipped, [])

    def test_skip_rules_out_a_redeemed_identity_by_address(self):
        rows = [
            identity("Invited@x.test", credentials=("password",)),
            identity("b@x.test", credentials=("password",)),
        ]
        actions, skipped = backfill.plan(rows, skip=["invited@X.TEST"])
        self.assertEqual([a["email"] for a in actions], ["b@x.test"])
        self.assertEqual([s["email"] for s in skipped], ["invited@x.test"])
        self.assertIn("--skip", skipped[0]["why"])

    def test_skip_does_not_widen_the_default_selection(self):
        rows = [identity("a@x.test"), identity("b@x.test", credentials=("password",))]
        actions, skipped = backfill.plan(rows, skip=["a@x.test"])
        self.assertEqual([a["email"] for a in actions], ["b@x.test"])
        self.assertEqual(len(skipped), 1)

    def test_an_identity_without_an_email_address_is_skipped(self):
        actions, skipped = backfill.plan([identity("a@x.test", credentials=("password",), addresses=[])])
        self.assertEqual(actions, [])
        self.assertIn("no email verifiable address", skipped[0]["why"])

    def test_the_email_address_is_picked_among_several(self):
        addresses = [
            {"value": "other@x.test", "via": "email", "verified": True},
            {"value": "a@x.test", "via": "email", "verified": False},
        ]
        actions, _ = backfill.plan([identity("a@x.test", credentials=("password",), addresses=addresses)])
        self.assertEqual(actions[0]["index"], 1)


def api_identity(name, credentials):
    row = identity(name + "@example.invalid")
    row["credentials"] = credentials
    return row


def credential(kind, config, identifiers=("candidate@example.invalid",)):
    return {kind: {"type": kind, "identifiers": list(identifiers), "config": config}}


class CredentialProjectionTest(unittest.TestCase):
    def test_list_and_plan_require_an_enrolled_first_factor(self):
        cases = [
            ("missing", None, []),
            ("type-list", ["password", "webauthn"], []),
            ("placeholder", credential("webauthn", {"user_handle": "fixture"}), []),
            ("empty-keys", credential("webauthn", {"credentials": []}), []),
            ("second-factor", credential("webauthn", {"credentials": [{"is_passwordless": False}]}), []),
            ("missing-key-mode", credential("webauthn", {"credentials": [{"id": "fixture"}]}), []),
            ("key-without-identifier", credential("webauthn", {"credentials": [{"is_passwordless": True}]}, ()), []),
            ("empty-password", credential("password", {"hashed_password": ""}), []),
            ("password-without-identifier", credential("password", {"hashed_password": "private-hash"}, ()), []),
            ("migration-hook-only", credential("password", {"use_password_migration_hook": True}), []),
            ("password", credential("password", {"hashed_password": "private-hash"}), ["password"]),
            ("passwordless-key", credential("webauthn", {"credentials": [{"id": "private-key-id", "is_passwordless": True}]}), ["webauthn"]),
            ("malformed-config", credential("password", "private-config"), []),
        ]
        for name, credentials, want in cases:
            with self.subTest(name=name):
                row = api_identity(name, credentials)
                with patch.object(backfill, "_req", return_value=([copy.deepcopy(row)], {})):
                    reduced = backfill.list_identities("https://fixture.invalid")
                self.assertEqual(reduced[0]["credentials"], want)
                actions, _ = backfill.plan(reduced)
                self.assertEqual(bool(actions), bool(want))
                self.assertNotIn("private-", json.dumps(reduced))
                self.assertNotIn("private-", json.dumps(actions))

    def test_pagination_reduces_every_page_without_mutating_other_fields(self):
        rows = [
            api_identity("first", credential("webauthn", {"user_handle": "fixture"})),
            api_identity("second", credential("password", {"hashed_password": "private-hash"})),
        ]
        rows[1]["metadata_public"] = {"groups": ["OBSERVERS"]}
        with patch.object(backfill, "PAGE_SIZE", 1), patch.object(backfill, "_req", side_effect=[
            ([copy.deepcopy(rows[0])], {"Link": '</admin/identities?page_token=next>; rel="next"'}),
            ([copy.deepcopy(rows[1])], {}),
        ]) as request:
            reduced = backfill.list_identities("https://fixture.invalid")
        self.assertEqual(request.call_count, 2)
        self.assertEqual(reduced[1]["metadata_public"], rows[1]["metadata_public"])
        self.assertEqual(reduced[0]["credentials"], [])
        self.assertEqual(reduced[1]["credentials"], ["password"])
        self.assertIn("include_credential=password", request.call_args_list[0].args[1])
        self.assertIn("include_credential=webauthn", request.call_args_list[0].args[1])


class ApplySelectionTest(unittest.TestCase):
    def run_script(self, args):
        rows = [
            api_identity("fresh", credential("webauthn", {"user_handle": "fixture"})),
            api_identity("absent", {}),
            api_identity("password", credential("password", {"hashed_password": "private-hash"})),
            api_identity("passkey", credential("webauthn", {"credentials": [{"is_passwordless": True}]})),
        ]
        state = {row["id"]: copy.deepcopy(row) for row in rows}
        writes = []

        def request(method, url, body=None):
            if method == "GET" and "?" in url:
                return copy.deepcopy(list(state.values())), {}
            name = unquote(url.rsplit("/", 1)[-1])
            if method == "PATCH":
                writes.append(name)
                for operation in body:
                    state[name]["verifiable_addresses"][0][operation["path"].rsplit("/", 1)[-1]] = operation["value"]
            return copy.deepcopy(state[name]), {}

        output = io.StringIO()
        with patch.object(backfill, "_req", side_effect=request), contextlib.redirect_stdout(output):
            result = backfill.main(["--admin", "https://fixture.invalid"] + args)
        self.assertEqual(result, 0)
        self.assertNotIn("private-hash", output.getvalue())
        return writes, state

    def test_apply_leaves_untouched_identities_unverified(self):
        writes, state = self.run_script(["--apply"])
        self.assertEqual(writes, ["id-password@example.invalid", "id-passkey@example.invalid"])
        for name in ("fresh", "absent"):
            address = state["id-" + name + "@example.invalid"]["verifiable_addresses"][0]
            self.assertFalse(address["verified"])
        for name in ("password", "passkey"):
            address = state["id-" + name + "@example.invalid"]["verifiable_addresses"][0]
            self.assertTrue(address["verified"])
            self.assertEqual(address["status"], "completed")

    def test_dry_run_makes_no_writes(self):
        self.assertEqual(self.run_script([])[0], [])

    def test_skip_excludes_an_enrolled_invite_accept_identity(self):
        writes, _ = self.run_script(["--apply", "--skip", "PASSWORD@example.invalid"])
        self.assertEqual(writes, ["id-passkey@example.invalid"])

    def test_only_does_not_override_the_enrollment_requirement(self):
        self.assertEqual(self.run_script(["--apply", "--only", "fresh@example.invalid"])[0], [])

    def test_all_is_explicit_and_still_honors_skip(self):
        writes, _ = self.run_script(["--apply", "--all", "--skip", "fresh@example.invalid"])
        self.assertEqual(writes, ["id-absent@example.invalid", "id-password@example.invalid", "id-passkey@example.invalid"])


class VerifyTest(unittest.TestCase):
    def test_verify_names_every_unverified_address(self):
        rows = [identity("a@x.test", verified=True), identity("b@x.test"), identity("c@x.test", addresses=[])]
        self.assertEqual(backfill.verify(rows), ["b@x.test", "c@x.test"])


class PatchShapeTest(unittest.TestCase):
    def test_the_patch_targets_the_chosen_address_only(self):
        sent = {}

        def fake_req(method, url, body=None):
            sent.update(method=method, url=url, body=body)
            return {}, {}

        original = backfill._req
        backfill._req = fake_req
        try:
            backfill.patch_verified("http://admin", {"id": "id-1", "index": 2, "email": "a@x.test"})
        finally:
            backfill._req = original
        self.assertEqual(sent["method"], "PATCH")
        self.assertTrue(sent["url"].endswith("/admin/identities/id-1"))
        self.assertEqual(
            sent["body"],
            [
                {"op": "replace", "path": "/verifiable_addresses/2/verified", "value": True},
                {"op": "replace", "path": "/verifiable_addresses/2/status", "value": "completed"},
            ],
        )


if __name__ == "__main__":
    unittest.main()

# SPDX-License-Identifier: Apache-2.0
#
# paths.py — where things are, computed once.
#
# The package lives at ops/consistency/python/goat_consistency/, so the repo
# root is four levels up. Every other module imports from here rather than
# recomputing a relative walk, because a second copy of "how do I find the
# tree" is how a tool ends up reading a different checkout than the one it was
# run against — the stale-trunk failure ansible-preflight.sh exists to catch.

import os

PACKAGE_DIR = os.path.dirname(os.path.abspath(__file__))
CONSISTENCY_DIR = os.path.normpath(os.path.join(PACKAGE_DIR, "..", ".."))
REPO_ROOT = os.path.normpath(os.path.join(CONSISTENCY_DIR, "..", ".."))

GRAMMAR_FILE = os.path.join(CONSISTENCY_DIR, "grammar.yaml")
INTENT_SCHEMA = os.path.join(CONSISTENCY_DIR, "schema", "intent-v1.schema.json")
INTENT_DIR = os.path.join(CONSISTENCY_DIR, "intent")
TESTDATA_DIR = os.path.join(CONSISTENCY_DIR, "testdata")

# The committed per-goatnet offline-CA SPKI set — THE trust root for the CF-5
# intent signature, exactly as it is for name snapshots and enrollment bundles
# (ADR 1082): an artifact's ca_id resolves here or verification fails.
PUBKEY_DIR = os.path.join(REPO_ROOT, "ops", "enrollment", "public-keys")

TABLE = os.path.join(REPO_ROOT, "ops", "pins", "fleet-pins.yaml")
# The sidecar table a lite goatnet (ADR 1213, docs/design/goatlite.md) lives in:
# goatnet rows plus deviation blocks ONLY, merged into the fleet table at load.
# It is a separate file so a learner's render never changes fleet-pins.yaml's
# bytes — the hash every committed intent carries in `sources`.
LITE_TABLE = os.path.join(REPO_ROOT, "ops", "pins", "lite-pins.yaml")
CAPS_FILE = os.path.join(REPO_ROOT, "ops", "pins", "parity-capabilities.yaml")
ALLOWLIST = os.path.join(REPO_ROOT, "ops", "pins", "unmigrated-pins.txt")
GROUP_VARS = os.path.join(REPO_ROOT, "ops", "ansible", "group_vars")
ROLES = os.path.join(REPO_ROOT, "ops", "ansible", "roles")


def rel(path):
    return os.path.relpath(path, REPO_ROOT)


def intent_path(goatnet):
    return os.path.join(INTENT_DIR, goatnet, "intent.json")

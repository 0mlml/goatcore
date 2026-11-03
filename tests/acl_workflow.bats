#!/usr/bin/env bats

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"

setup() {
  export RUNNER_TEMP="$BATS_TEST_TMPDIR"
  export GITHUB_OUTPUT="$BATS_TEST_TMPDIR/job-output"
  export ACL_FIXTURE_PATHS="$BATS_TEST_TMPDIR/changed-paths"
  export ACL_GIT_FAILURE=0
  mkdir -p "$BATS_TEST_TMPDIR/bin"

  python3 - "$REPO_ROOT/.github/workflows/acl-assertions.yml" \
    "$BATS_TEST_TMPDIR/select-inputs.sh" <<'PY'
import sys
from pathlib import Path
import yaml

doc = yaml.safe_load(Path(sys.argv[1]).read_text())
Path(sys.argv[2]).write_text(doc["jobs"]["assertions"]["steps"][1]["run"])
PY

  cat > "$BATS_TEST_TMPDIR/bin/git" <<'SH'
#!/usr/bin/env bash
if [ "$ACL_GIT_FAILURE" -ne 0 ]; then
  exit "$ACL_GIT_FAILURE"
fi
cat "$ACL_FIXTURE_PATHS"
SH
  chmod +x "$BATS_TEST_TMPDIR/bin/git"
  export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
}

@test "ACL workflow reports on every PR and limits Rust to its inputs" {
  python3 - "$REPO_ROOT/.github/workflows/acl-assertions.yml" <<'PY'
import sys
from pathlib import Path
import yaml

doc = yaml.safe_load(Path(sys.argv[1]).read_text())
events = doc.get("on", doc.get(True))
assert events["pull_request"] is None
job = doc["jobs"]["assertions"]
assert "if" not in job
assert job["steps"][0]["with"]["fetch-depth"] >= 2
assert job["steps"][1]["shell"] == "bash"
for step in job["steps"][2:]:
    assert step["if"] == "steps.changes.outputs.run_tests == 'true'"
assert job["steps"][-1]["run"] == "cargo test -p panoscope-core"
PY

  local path
  for path in ops/ansible/netbird_acl_matrix.yml ops/ansible/netbird_acl_taxonomy.yml \
    crates/panoscope-core/src/aclcheck.rs crates/Cargo.toml crates/Cargo.lock \
    .github/workflows/acl-assertions.yml; do
    printf '%s\0' "$path" > "$ACL_FIXTURE_PATHS"
    : > "$GITHUB_OUTPUT"
    run bash -e -o pipefail "$BATS_TEST_TMPDIR/select-inputs.sh"
    [ "$status" -eq 0 ]
    grep -qx 'run_tests=true' "$GITHUB_OUTPUT"
  done
}

@test "unrelated files leave the ACL check successful without a Rust build" {
  printf '%s\0' docs/example.md crates/goat-cli/src/cmd/topics.rs \
    $'docs/quoted-\n"name.md' > "$ACL_FIXTURE_PATHS"
  run bash -e -o pipefail "$BATS_TEST_TMPDIR/select-inputs.sh"
  [ "$status" -eq 0 ]
  grep -qx 'run_tests=false' "$GITHUB_OUTPUT"
}

@test "a failed Git diff fails the ACL check before it can skip Rust" {
  : > "$ACL_FIXTURE_PATHS"
  export ACL_GIT_FAILURE=128
  run bash -e -o pipefail "$BATS_TEST_TMPDIR/select-inputs.sh"
  [ "$status" -eq 128 ]
  [ ! -s "$GITHUB_OUTPUT" ]
}

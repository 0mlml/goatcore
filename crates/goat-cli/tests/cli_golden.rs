// End-to-end golden tests for the `goat` binary. Uses `trycmd` to
// drive the built binary against `.toml` cases in `tests/cmd/`. Per
// docs/design/goat-cli.md §Conventions item 7 ("set this up before
// there's much to snapshot — backfilling after 30 commands never
// happens").
//
// Add a case: drop `tests/cmd/<name>.toml` with the shape:
//
//   [env]
//   GOAT_PROFILE_DIR = "..."         # if needed
//   ["..."]
//   args = "subcommand --flag"
//   stdout = """
//   expected output
//   """
//   status.code = 0
//
// Run `TRYCMD=overwrite cargo test --test cli_golden` to regenerate.

#[test]
fn cli_golden() {
    trycmd::TestCases::new().case("tests/cmd/*.toml");
}

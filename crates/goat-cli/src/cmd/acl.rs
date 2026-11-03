// Evaluate the embedded ACL declaration with the same evaluator as netvis and CI.
// This command needs no network connection, profile, or credentials. Local source
// differences are reported on stderr; cargo run checks the current checkout.

use std::path::Path;

use clap::{Args, Subcommand};
use panoscope_core::aclcheck::{Declaration, Report, Verdict};

// Keep these includes in the CLI. Other production consumers of panoscope-core
// do not package the ACL files and must be able to build without them.
const TAXONOMY_YAML: &str = include_str!("../../../../ops/ansible/netbird_acl_taxonomy.yml");
const MATRIX_YAML: &str = include_str!("../../../../ops/ansible/netbird_acl_matrix.yml");

/// The compiled-in declaration, keyed by its path in a goat checkout.
const DECLARATION: [(&str, &str); 2] = [
    ("ops/ansible/netbird_acl_taxonomy.yml", TAXONOMY_YAML),
    ("ops/ansible/netbird_acl_matrix.yml", MATRIX_YAML),
];

#[derive(Subcommand, Debug)]
pub enum AclCmd {
    /// Check the embedded declaration. Violations and undetermined results fail.
    Test(AclTestArgs),
}

pub async fn run(cmd: AclCmd) -> anyhow::Result<()> {
    match cmd {
        AclCmd::Test(args) => run_test(args).await,
    }
}

#[derive(Args, Debug)]
pub struct AclTestArgs {
    /// Scope to check. Omit to check every declared scope.
    pub scope: Option<String>,
    /// Emit the full report as JSON.
    #[arg(long)]
    pub json: bool,
    /// Include satisfied assertions in the text report.
    #[arg(long)]
    pub verbose: bool,
}

pub async fn run_test(args: AclTestArgs) -> anyhow::Result<()> {
    // Inside a checkout, the declaration on disk is the one that governs: a
    // goatnet rendered after this binary was built (a learner's goatlite) is in
    // the files and not in the embedded copy, which answered "no site named"
    // for every such goatnet. Outside a checkout, the embedded copy.
    // The note stays on stderr, apart from JSON and from the verdict.
    let on_disk = std::env::current_dir().ok().and_then(|cwd| checkout_declaration(&cwd));
    let decl = match &on_disk {
        Some((tax, mat)) => {
            eprintln!("acl test: checking the declaration in this checkout");
            Declaration::parse(tax, mat)?
        }
        None => Declaration::parse(TAXONOMY_YAML, MATRIX_YAML)?,
    };
    let scopes = match &args.scope {
        Some(s) => vec![s.clone()],
        None => decl.scopes(),
    };

    let mut reports = Vec::new();
    let mut silent = Vec::new();
    for site in &scopes {
        // Reject an unknown scope before checking whether it has assertions.
        let model = decl.model(site)?;
        let tests = decl.tests(site);
        if tests.is_empty() {
            silent.push(site.clone());
            continue;
        }
        reports.push(panoscope_core::aclcheck::evaluate(&model, &tests));
    }

    if args.json {
        println!("{}", serde_json::to_string_pretty(&reports)?);
    } else {
        for r in &reports {
            print_report(r, args.verbose);
        }
        // Include unchecked scopes so the report shows its coverage.
        if !silent.is_empty() {
            println!(
                "\nnot checked — no assertions declared: {}",
                silent.join(", ")
            );
        }
        if reports.is_empty() {
            println!("No scope in the declaration carries an assertion.");
        }
    }

    // Undetermined results fail as well as explicit violations.
    if reports.iter().all(|r| r.is_clean()) {
        Ok(())
    } else {
        anyhow::bail!("ACL assertions did not hold against the declaration")
    }
}

/// Both declaration files read from the checkout `start` is inside, if any.
fn checkout_declaration(start: &Path) -> Option<(String, String)> {
    let root = start.ancestors().find(|dir| dir.join("crates/goat-cli/Cargo.toml").is_file())?;
    let tax = std::fs::read_to_string(root.join(DECLARATION[0].0)).ok()?;
    let mat = std::fs::read_to_string(root.join(DECLARATION[1].0)).ok()?;
    Some((tax, mat))
}

// Missing or unreadable declarations differ from the embedded copy too. Use the
// crate manifest to find the source root even when both YAML files are missing.
#[allow(dead_code)]
fn changed_declaration_files(start: &Path) -> Vec<&'static str> {
    let Some(root) = start
        .ancestors()
        .find(|dir| dir.join("crates/goat-cli/Cargo.toml").is_file())
    else {
        return Vec::new();
    };
    DECLARATION
        .iter()
        .filter(|(path, built)| {
            std::fs::read_to_string(root.join(path))
                .map_or(true, |on_disk| !declarations_match(&on_disk, built))
        })
        .map(|(path, _)| *path)
        .collect()
}

fn declarations_match(on_disk: &str, built: &str) -> bool {
    on_disk.replace("\r\n", "\n") == built.replace("\r\n", "\n")
}

fn print_report(r: &Report, verbose: bool) {
    let head = match r.verdict {
        Verdict::Satisfied => format!("{}: all {} assertions hold", r.site, r.satisfied),
        Verdict::Baselined => format!(
            "{}: {} hold, {} are recorded known violations — nothing new is broken",
            r.site, r.satisfied, r.baselined
        ),
        Verdict::Violated => format!("{}: {} VIOLATED", r.site, r.violated),
        Verdict::Undetermined => format!(
            "{}: {} UNDETERMINED — an unknown is not a pass, so the whole scope is capped",
            r.site, r.undetermined
        ),
    };
    println!("\n{head}");
    println!("  checked against the DECLARATION, {}", r.quantifier);

    for name in &r.baseline_now_passing {
        println!(
            "  ratchet: `{name}` was baselined and now passes — delete the baseline, \
             or it becomes permission"
        );
    }

    for o in &r.outcomes {
        let show = verbose || !matches!(o.verdict, Verdict::Satisfied);
        if !show {
            continue;
        }
        let tag = match o.verdict {
            Verdict::Satisfied => "ok        ",
            Verdict::Baselined => "recorded  ",
            Verdict::Violated => "VIOLATED  ",
            Verdict::Undetermined => "UNDECIDED ",
        };
        println!("  {tag}[{}] {}", o.check, o.name);
        if let Some(reason) = o.reason {
            println!(
                "              {} ({})",
                reason.explain(),
                if reason.is_declaration_fault() {
                    "your declaration"
                } else {
                    "an unread source"
                }
            );
        }
        if let Some(b) = &o.baseline {
            println!(
                "              recorded since {} — {}",
                b.violated_since,
                b.why.trim()
            );
        }
        for c in &o.counterexamples {
            println!(
                "              {} -> {}:{} ({})",
                c.src, c.dst, c.port, c.proto
            );
            if let Some(g) = &c.granted_by {
                println!("                granted by rule `{g}`");
            }
            // Include a nearby rule to help diagnose the missing permission.
            if let Some(n) = &c.nearest {
                println!("                nearest:  {n}");
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn it_runs_with_no_context_no_network_and_no_credentials() {
        run_test(AclTestArgs {
            scope: None,
            json: true,
            verbose: false,
        })
        .await
        .expect("the declared assertions must hold");
    }

    #[tokio::test]
    async fn an_unknown_scope_says_so_rather_than_passing_silently() {
        let e = run_test(AclTestArgs {
            scope: Some("nosuchsite".into()),
            json: false,
            verbose: false,
        })
        .await;
        assert!(e.is_err(), "an unknown scope must not exit 0");
    }

    // The fixture needs the crate manifest used to locate a source checkout.
    fn checkout(taxonomy: &str, matrix: &str) -> tempfile::TempDir {
        let dir = tempfile::tempdir().expect("temp dir");
        let crate_dir = dir.path().join("crates/goat-cli");
        std::fs::create_dir_all(&crate_dir).expect("crate directory");
        std::fs::write(
            crate_dir.join("Cargo.toml"),
            "[package]\nname = \"goat-cli\"\n",
        )
        .expect("crate manifest");
        let ansible = dir.path().join("ops/ansible");
        std::fs::create_dir_all(&ansible).expect("ops/ansible");
        std::fs::write(ansible.join("netbird_acl_taxonomy.yml"), taxonomy).expect("taxonomy");
        std::fs::write(ansible.join("netbird_acl_matrix.yml"), matrix).expect("matrix");
        dir
    }

    #[test]
    fn an_edited_declaration_is_named_from_anywhere_in_the_checkout() {
        let dir = checkout(TAXONOMY_YAML, &format!("{MATRIX_YAML}\n# edited\n"));
        let nested = dir.path().join("crates/goat-cli");
        std::fs::create_dir_all(&nested).expect("nested");
        assert_eq!(
            changed_declaration_files(&nested),
            vec!["ops/ansible/netbird_acl_matrix.yml"]
        );
    }

    #[test]
    fn a_matching_checkout_and_a_plain_folder_are_quiet() {
        let same = checkout(TAXONOMY_YAML, MATRIX_YAML);
        assert!(
            changed_declaration_files(same.path()).is_empty(),
            "matching checkout"
        );
        let plain = tempfile::tempdir().expect("temp dir");
        assert!(
            changed_declaration_files(plain.path()).is_empty(),
            "not a checkout"
        );
    }

    #[test]
    fn line_endings_do_not_change_the_declaration() {
        let crlf = checkout(
            &TAXONOMY_YAML.replace("\r\n", "\n").replace("\n", "\r\n"),
            &MATRIX_YAML.replace("\r\n", "\n").replace("\n", "\r\n"),
        );
        assert!(changed_declaration_files(crlf.path()).is_empty());
        assert!(declarations_match("key: value\n", "key: value\r\n"));
        assert!(declarations_match("key: value\r\n", "key: value\n"));
        assert!(!declarations_match("key: edited\r\n", "key: value\n"));
    }

    #[test]
    fn missing_or_unreadable_declarations_are_reported() {
        let dir = checkout(TAXONOMY_YAML, MATRIX_YAML);
        let taxonomy = dir.path().join(DECLARATION[0].0);
        let matrix = dir.path().join(DECLARATION[1].0);
        std::fs::remove_file(&matrix).expect("remove matrix");
        assert_eq!(
            changed_declaration_files(dir.path()),
            vec![DECLARATION[1].0]
        );
        std::fs::remove_file(&taxonomy).expect("remove taxonomy");
        assert_eq!(
            changed_declaration_files(dir.path()),
            vec![DECLARATION[0].0, DECLARATION[1].0]
        );
        std::fs::create_dir(&matrix).expect("unreadable matrix directory");
        assert_eq!(
            changed_declaration_files(dir.path()),
            vec![DECLARATION[0].0, DECLARATION[1].0]
        );
    }
}

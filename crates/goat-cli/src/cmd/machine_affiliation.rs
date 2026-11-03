use crate::output::{with_spinner, OutputContext};
use clap::{Args, Subcommand};
use panoscope_core::portal::{
    AffiliationRepairResult, AffiliationRepairSpec, PortalClient, PortalError,
};

#[derive(Subcommand, Debug)]
pub enum AffiliationCmd {
    /// Preview exact legacy records, then apply the reviewed revision atomically.
    /// Only use after verifying that all selected spellings describe one organization.
    Repair(RepairArgs),
}

#[derive(Args, Debug)]
pub struct RepairArgs {
    /// Exact stored spelling to repair. Repeat for each approved legacy spelling.
    #[arg(long, required = true)]
    from: Vec<String>,
    /// Approved spelling to store on the selected grants and slots.
    #[arg(long)]
    to: String,
    /// Why these records are known to represent one organization (recorded in audit).
    #[arg(long)]
    reason: String,
    /// Apply instead of previewing. Requires the revision from a reviewed preview.
    #[arg(long, requires = "expected_revision")]
    apply: bool,
    /// Revision printed by preview. A changed group is refused without updates.
    #[arg(long, requires = "apply")]
    expected_revision: Option<String>,
    /// Emit the server's structured preview or application receipt.
    #[arg(long)]
    json: bool,
}

impl RepairArgs {
    fn spec(&self) -> AffiliationRepairSpec {
        AffiliationRepairSpec {
            from: self.from.clone(),
            to: self.to.clone(),
            reason: self.reason.clone(),
            apply: self.apply,
            expected_revision: self.expected_revision.clone(),
        }
    }
}

pub async fn run(
    out: &OutputContext,
    portal: &PortalClient,
    op: AffiliationCmd,
) -> anyhow::Result<()> {
    let AffiliationCmd::Repair(args) = op;
    let spec = args.spec();
    let result = with_spinner(
        out,
        "checking affiliation repair",
        portal.repair_machine_affiliation(&spec),
    )
    .await
    .map_err(map_repair_error)?;
    validate_receipt(&result, spec.apply)?;
    println!("{}", render(&result, args.json)?);
    Ok(())
}

fn validate_receipt(result: &AffiliationRepairResult, apply: bool) -> anyhow::Result<()> {
    // A successful HTTP response must actually describe the requested operation.
    if result.applied != apply || (apply && result.audit_id.filter(|id| *id > 0).is_none()) {
        anyhow::bail!("portal returned an inconsistent repair receipt; inspect the audit record before retrying");
    }
    Ok(())
}

fn render(result: &AffiliationRepairResult, json: bool) -> anyhow::Result<String> {
    if json {
        return Ok(serde_json::to_string(result)?);
    }
    let mut text = format!(
        "{}: {} grant(s), {} slot(s) to {:?}\nRevision: {}",
        if result.applied { "Applied" } else { "Preview" },
        result.grants,
        result.slots,
        result.to,
        result.revision
    );
    for row in &result.rows {
        text.push_str(&format!(
            "\n{} {}: {:?} -> {:?}",
            row.kind, row.id, row.affiliation, result.to
        ));
    }
    if let Some(audit_id) = result.audit_id {
        text.push_str(&format!("\nAudit ID: {audit_id}"));
    } else {
        text.push_str("\nVerify the selected records represent one organization. Re-run these arguments with --apply --expected-revision and the revision above.");
    }
    Ok(text)
}

fn map_repair_error(error: PortalError) -> anyhow::Error {
    let server = super::vendor::server_line(&error);
    match error.http_status().map(|status| status.as_u16()) {
        Some(401 | 403) => anyhow::anyhow!("affiliation repair requires a valid operator context.{server}"),
        Some(404) => anyhow::anyhow!("this portal does not provide affiliation repair; deploy the portal change before using this command.{server}"),
        Some(409) => anyhow::anyhow!("repair preview is stale; preview again and review the affected records.{server}"),
        Some(400) => anyhow::anyhow!("affiliation repair request refused.{server}"),
        _ => super::vendor::map_portal_err(error),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use clap::Parser;
    use panoscope_core::portal::AffiliationRepairRow;

    #[derive(Parser)]
    struct TestCli {
        #[command(subcommand)]
        op: super::super::machine::MachineCmd,
    }

    fn args() -> Vec<&'static str> {
        vec![
            "goat",
            "affiliation",
            "repair",
            "--from",
            "Unit Alpha",
            "--from",
            "UNIT_ALPHA",
            "--to",
            "unit-alpha",
            "--reason",
            "Owner verified",
        ]
    }

    #[test]
    fn preview_is_default_and_preserves_exact_source_spelling() {
        let cli = TestCli::try_parse_from(args()).unwrap();
        let super::super::machine::MachineCmd::Affiliation {
            op: AffiliationCmd::Repair(args),
        } = cli.op
        else {
            panic!("wrong command")
        };
        let spec = args.spec();
        assert!(!spec.apply);
        assert!(spec.expected_revision.is_none());
        assert_eq!(spec.from, ["Unit Alpha", "UNIT_ALPHA"]);
    }

    #[test]
    fn apply_and_revision_require_each_other() {
        let mut apply_only = args();
        apply_only.push("--apply");
        assert!(TestCli::try_parse_from(&apply_only).is_err());
        let mut revision_only = args();
        revision_only.extend(["--expected-revision", "revision"]);
        assert!(TestCli::try_parse_from(revision_only).is_err());
        apply_only.extend(["--expected-revision", "revision", "--json"]);
        let cli = TestCli::try_parse_from(apply_only).unwrap();
        let super::super::machine::MachineCmd::Affiliation {
            op: AffiliationCmd::Repair(args),
        } = cli.op
        else {
            panic!("wrong command")
        };
        assert!(args.apply && args.json);
        assert_eq!(args.expected_revision.as_deref(), Some("revision"));
    }

    #[test]
    fn renders_exact_preview_and_application_receipt() {
        let mut result = AffiliationRepairResult {
            from: vec!["Unit Alpha".into()],
            to: "unit-alpha".into(),
            revision: "abc".into(),
            rows: vec![AffiliationRepairRow {
                kind: "grant".into(),
                id: "fixture".into(),
                affiliation: "Unit Alpha".into(),
            }],
            grants: 1,
            slots: 0,
            applied: false,
            audit_id: None,
        };
        assert!(validate_receipt(&result, false).is_ok());
        assert!(validate_receipt(&result, true).is_err());
        result.applied = true;
        for audit_id in [None, Some(0), Some(-1)] {
            result.audit_id = audit_id;
            assert!(validate_receipt(&result, true).is_err());
        }
        result.applied = false;
        result.audit_id = None;
        let preview = render(&result, false).unwrap();
        assert!(preview.starts_with("Preview:"));
        assert!(preview.contains("grant fixture: \"Unit Alpha\" -> \"unit-alpha\""));
        assert!(preview.contains("--apply --expected-revision"));
        let json: serde_json::Value =
            serde_json::from_str(&render(&result, true).unwrap()).unwrap();
        assert_eq!(json["applied"], false);
        result.applied = true;
        result.audit_id = Some(7);
        assert!(validate_receipt(&result, true).is_ok());
        assert!(validate_receipt(&result, false).is_err());
        let applied = render(&result, false).unwrap();
        assert!(applied.starts_with("Applied:"));
        assert!(applied.contains("Audit ID: 7"));
        assert!(!applied.contains("Re-run"));
    }

    #[test]
    fn old_portal_and_conflicts_fail_loudly() {
        for (status, expected) in [
            (404, "deploy the portal"),
            (409, "preview again"),
            (403, "operator context"),
            (400, "request refused"),
        ] {
            let error = PortalError::HttpWithBody {
                status: reqwest::StatusCode::from_u16(status).unwrap(),
                code: None,
                message: "fixture reason".into(),
            path: String::new(),
            retry_after: None,
        };
            let message = map_repair_error(error).to_string();
            assert!(message.contains(expected), "{message}");
            assert!(message.contains("fixture reason"), "{message}");
        }
    }
}

# ADR 1201 — Two avenues stay functional: the scripts are documented, the CLI is supported

| Field | Value |
|---|---|
| Status | **Accepted** |
| Date | 2026-10-28 |
| Deciders | operator |
| Supersedes | — |
| Amends | [ADR 1198](1198-goatbuilder-config-driven-deploy-release-cli.md) §"Migration" (subsume and delete) |
| Related | [`goatbuilder.md`](../design/goatbuilder.md) §"Migration", [`goatbuilder-conformance.md`](../design/goatbuilder-conformance.md) (the parity gates), `docs/design/deployment-e2e.md` (ADR 1200, landing with the e2e harness: both avenues on every tier) |

## TL;DR (for a layperson)

Every job that builds or ships a goat environment can be done two ways: with the shell and
Python scripts that have always done it, or with the `goatbuilder` command that now does the
same thing verb for verb. Both keep working. The command is the one the team supports and
teaches; the scripts stay in the repository, documented, exercised by the same tests, and
unsupported. Nothing is deleted.

## Decision

The operator's words, 2026-10-28, are the rule:

> make sure that we keep both avenues functional for all three things golden path founder,
> golden path federating node, and new deployments across envs -- both the scripts method
> and the cli method and eventually we will document how scripts can work, but support only
> the cli

Applied:

| Golden path | Scripts avenue (documented) | CLI avenue (supported) |
|---|---|---|
| Founder standup | `ops/scripts/goat-standup.sh` | `goatbuilder standup <env> --site <s>` |
| Federating node | node-addition.md §5.4 by hand; `ops/scripts/peering-standup.sh` | `goatbuilder join-node`, `goatbuilder federate` |
| Deployments across envs | `ops/scripts/rc.sh`, `deploy-component.sh`, `rollback.sh`, `fastlane.sh`, `ops/release/graduate.py`, `ops/pins/generate-pins.py` | `goatbuilder release`, `deploy`, `rollback`, `fastlane`, `promote`, `pins` |

1. **Neither avenue is deleted.** ADR 1198's third migration step ("cut over and delete") is
   withdrawn. A script is never removed because its verb exists.
2. **Both avenues are gated.** Every parity gate (`ops/goatbuilder/parity-*.sh`) and every
   tier of the deployment e2e harness runs both avenues; a failure on either fails the check.
   A script must run on its own: it never depends on the `goatbuilder` binary.
3. **Support is the CLI's.** Runbooks, the one-pagers, the hosted book and the error catalog
   teach `goatbuilder`. A script's page says what it does and that it is unsupported; a defect
   found only in a script is fixed when the parity gate catches it, not on request.
4. **One fact, one home, still.** Both avenues read the same pins, ladder, records and
   intents; the scripts' embedded edits and the CLI's typed edits produce byte-identical files
   (the `pins` and `promote` parity gates say so).

## Consequences

- The repository carries two implementations of each path on purpose; the parity gates are
  what keep them one behaviour. The complexity budget (ADR 1016) counts the scripts as
  documented surface, not supported surface.
- "Delete the four shell drivers" leaves every plan, handout and cold-start note. The
  one-hour goatnet target is measured on the CLI avenue.
- When a script and the binary disagree, the gate fails and the fix lands on both sides in the
  same change, as the first seven parity defects did.

## Alternatives

- **Delete the scripts after one real release** (ADR 1198 as written): fewer moving parts,
  but a team that reaches for the scripts, or reads an older runbook, is left with nothing.
  Rejected by the operator.
- **Support both:** doubles the support surface for no gain; the gates already keep the
  scripts correct. Rejected.

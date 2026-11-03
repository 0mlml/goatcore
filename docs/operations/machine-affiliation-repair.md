# Repair Legacy Machine Affiliations

Legacy records can contain two spellings of one organization. The enrollment guard refuses both spellings until an operator repairs the records.

Use this procedure for a current collision after the organization owner confirms that the spellings identify one organization. The ordinary enrollment guard remains active.

Do not repeat a completed repair because an earlier incident remains open. Confirm the current records before you create a preview.

This procedure changes only the `affiliation` column in `machine_grants` and `slots`. The repair includes historical slots because they also participate in the guard. Quotas, custody, credentials, timestamps and mesh membership stay unchanged.

## Prerequisites

- A portal and CLI release that support `machine affiliation repair`.
- An existing operator context for the intended goatnet.
- Owner confirmation that every selected spelling represents the same organization.
- An encrypted registry backup with a successful restore in an isolated environment.

Deploy the portal change before you use the CLI command. An older portal returns an error; the CLI does not use another repair path. The change requires no schema migration.

## Preview the Records

1. Confirm the intended goatnet and operator context.
2. Obtain owner approval for the target spelling and exact source spellings.
3. Create the preview with synthetic names replaced by the approved values:

```sh
goat --context <operator-context> machine affiliation repair \
  --from 'Unit Alpha' --to 'unit-alpha' \
  --reason 'Organization owner confirmed the legacy spelling'
```

4. Review every grant and slot identifier in the preview.
5. Check that the counts match the approved records.
6. Record the preview revision.

Repeat `--from` for each additional approved spelling. The source values match stored text exactly, including case and spaces. The target uses trimmed NFC text. Similar spelling does not prove common ownership.

Preview writes no records. An omitted spelling in the same comparison group causes refusal. The portal refuses a spelling outside the target's comparison group.

## Apply the Reviewed Repair

1. Keep the source spellings, target spelling and reason from the reviewed preview.
2. Apply the repair with that preview revision:

```sh
goat --context <operator-context> machine affiliation repair \
  --from 'Unit Alpha' --to 'unit-alpha' \
  --reason 'Organization owner confirmed the legacy spelling' \
  --apply --expected-revision <preview-revision>
```

3. Record the audit ID and affected counts.
4. Verify the approved affiliation on the selected grants and slots.
5. Verify that quotas, custody and credentials match the records before repair.
6. Retry ordinary machine enrollment only after these checks pass.

Use `--json` for a structured preview or receipt. The API is `POST /api/machines/affiliation/repair`; both modes require an operator bearer.

## Failure and Recovery

A changed comparison group causes HTTP 409 with no repair writes. Create and review a new preview. A repeated application with the old revision also returns HTTP 409.

Both table updates and the audit entry commit in one transaction. A database or audit failure rolls back that transaction. Database and write-conflict errors return HTTP 500 `affiliation_repair_failed`. A detected stale revision returns HTTP 409 instead. The audit records the authenticated principal, delegated actor, reason, exact mapping, affected identifiers and counts.

A timeout or lost response does not establish whether the commit succeeded. Inspect the audit before any retry. The CLI never retries application automatically.

If the mapping was wrong, stop further repair attempts. Have the registry owner review the backup and audit receipt. Prepare a correction for the recorded rows that preserves later changes. Never restore an older database over a live database with later writes.

The previous portal binary can read repaired records because the change preserves the schema. Rolling back the binary does not undo a completed repair.

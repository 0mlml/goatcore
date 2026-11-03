# goat-logship

Vector-based cyber-logging collector. Normalizes host/OS + fabric security
events to **OCSF** at the edge and ships them **off the host** to an
Object-Lock S3 bucket, retaining near-zero locally. Absence-alarmed so a
silent stream pages instead of vanishing.

Anchor: ADR 1078 ·
design `cyber-logging-and-conmon-export.md`.

## What it does

- **Sources** (§4): `journald` (always), `auditd`, the existing Zenoh-audit
  NDJSON, container stdout, egress-proxy logs — each opt-in via a toggle
  because on-disk paths are host-specific.
- **Normalize** (VRL): every event → an OCSF envelope stamped with a
  `metadata.dictionary_id` from [`dictionary.yaml`](../../../../ops/logging/goat-logship/dictionary.yaml).
  Edge redaction strips secret-shaped tokens.
- **Sink 1 — the durable landing (pluggable, `logship_durable_sink_mode`):**
  - `mock` (default) — a local newline-delimited OCSF/JSON tail
    (`{{ logship_mock_sink_dir }}/ocsf-*.json`). Stores nothing off-box;
    proves collect→normalize→ship with zero cloud dependency.
  - `s3` — `aws_s3` to a dedicated Object-Lock bucket, OCSF/JSON
    partitioned `date/host/class`, disk-buffered. A one-line flip (the sink
    component is always `durable`, so metrics + absence-alarm are unchanged).
- **Sink 2 — external-collector egress (default-off):** CEF-over-syslog-5424-TLS,
  rendered only at the boundary, enabled only against a named collector.
- **Absence-alarm:** `internal_metrics` → `prometheus_exporter`; the rules in
  `prom-rules-goat-logship.yml.j2` fire on stream silence (closes F-169).

## Deploy

Default (`mock`) needs no credentials — deploy straight to a host:
`ansible-playbook goat-logship-deploy.yml -e target=zagros_hosts`.

For `s3` mode: set `logship_durable_sink_mode=s3`, apply the bucket's Terraform,
and export `LOGSHIP_S3_ACCESS_KEY_ID` / `LOGSHIP_S3_SECRET_ACCESS_KEY`
(ingest-only key; never committed).

Wire per goatnet by adding the role to the
site play with the source toggles the host actually has. Removable by
pin-revert with no effect on the audit/metrics planes (ADR 1078 §10).

## Confirm at standup (per ADR 1078 §9)

- Exact S3 bucket name (see the role defaults).
- The Zenoh-audit NDJSON on-disk glob for this host (`logship_zenoh_audit_glob`,
  off until confirmed).
- external collector endpoint (gates enabling Sink 2 beyond default-off).

# goat-logship role tests

Parse-only smoke, mirroring the sibling `otel-collector` role. End-to-end
verification (objects land off-box in S3, the host keeps only the buffer, and
the absence-alarm fires when the collector is stopped) is the Phase-1 live
re-probe on ZAGROS per ADR 1078 §6 — not a CI fixture.

The event dictionary ↔ Vector-transform contract is enforced separately and
in CI by [`ops/ci/check-logship-dictionary.sh`](../../../../ci/check-logship-dictionary.sh):
every `metadata.dictionary_id` a transform emits must map to a row in
[`ops/logging/goat-logship/dictionary.yaml`](../../../../logging/goat-logship/dictionary.yaml).

Run: `./syntax-check.sh`.

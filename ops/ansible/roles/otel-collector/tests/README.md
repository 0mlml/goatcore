# otel-collector role tests

Parse-only smoke. Reaches parity with sibling Block 27 obs-host roles
(`minio` + `openobserve`) which carry full fixtures. End-to-end OTLP
ingest + forwarding is exercised by the Block 27A.3 spike harness at
[`ops/observability/wide-events/spike/run-spike.sh`](../../../../observability/wide-events/spike/run-spike.sh)
on Orin-C.

Run: `./syntax-check.sh`.

# ca-signing-host role tests

Parse-only smoke. Catches YAML + Jinja2 errors at PR-time. Does not
exercise actual age-decrypt + bundle-create signing — those are covered
by the operator-fired ceremony runbook at
`docs/operations/dev-ca-multi-operator-ceremony.md`
and the ca-init / cert-sign integration tests in
[`ops/enrollment/x509ca/`](../../../../enrollment/x509ca/).

Run: `./syntax-check.sh`.

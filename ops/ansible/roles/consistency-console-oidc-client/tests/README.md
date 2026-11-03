# consistency-console-oidc-client role tests

Parse-only smoke. End-to-end Hydra OAuth2 client provisioning is covered by
running `consistency-console-deploy.yml` against a goatnet, which runs this role
before the `consistency-console` role so the client exists before the fence that
presents it.

The failure this role prevents is not caught by any syntax check: a fence
presenting a client nothing registered is green on every container probe and
visible only by loading the page and reading Ory's `invalid_client`. The end-to-end assertion is the assay row
`fenced-surface-oidc-client-exists` (probe `oidc-flow`), which reads the client
ids from the running fences and asks Hydra for each.

Run: `./syntax-check.sh`.

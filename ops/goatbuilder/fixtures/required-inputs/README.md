# required-inputs fixture

A deliberately incomplete site (`zagros`) for `ops/scripts/standup-required-inputs.py`:
three required inputs and one env-bound secret are missing (`alpha_zone`, empty in the role defaults;
`alpha_token`, asserted; `alpha_owner`, `| mandatory`), and two requirements sit
behind conditions that are false for this site (`beta_key` under
`beta_enabled: false`, `alpha_optional_key` behind an `include_tasks` gated by
`alpha_optional`). One scan must name exactly the three, at once.

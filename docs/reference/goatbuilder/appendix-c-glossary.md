# Appendix C. Glossary

| Term | Meaning |
|---|---|
| **acceptance** | the rows `ops/validation/run-acceptance.py` runs to prove a goatnet's provisioned aspects live; standup's phase 13 |
| **assay** | the acceptance rows that apply to a site, run and graded layer by layer; standup's phase 13 and its own verb |
| **artifact type** | what a component is, which decides its rollback: `image`, `config`, `acl`, `data`, `cert` or `forward-only` |
| **avenue** | one of the two ways to do the work: the goatbuilder binary, or the scripts |
| **candidate** | a release-candidate tag `<component>/vX.Y.Z-rc.N` on `main`, pinned on the preprod rung by a deviation |
| **class** | a machine class within a site: `core`, `obs`, `breakglass`, `network-jump` |
| **component** | one deployable unit, a key in `ops/pins/fleet-pins.yaml` |
| **contract** | the ten guarantees C1 to C10 every declarative verb keeps (chapter 6) |
| **converged** | a host whose newest ledger row names the declared digest with `action: deploy` |
| **converger** | what makes hosts match a declaration; today `push-ansible` |
| **declaration** | the digest a component should run on an environment, in git |
| **declarative verb** | a verb that changes state; it asks for confirmation and enforces the contract |
| **deviation** | an entry in `fleet-pins.yaml` that pins a component differently on one goatnet or member group, with a reason |
| **digest** | the content hash `sha256:…` that identifies an image exactly |
| **door** | a way to reach a goatnet's management host without joining its mesh; today an AWS SSM port-forward |
| **dry run** | `--dry-run`: decide everything and print it, touch nothing |
| **environment** | a goatnet, named in the config by its pins key |
| **fixture** | a directory standing in for an environment, used with `--mock` |
| **fleet value** | a component's `fleet:` pin, which every goatnet runs unless a deviation says otherwise |
| **founder** | the first site of a goatnet; it originates the account, the CA and the declared ACL |
| **goatnet** | one goat environment: its mesh, management hosts and services |
| **graduation** | folding a soaked candidate's ref into `fleet:` and deleting its deviation |
| **graduation set** | components that must graduate together |
| **hotfix** | a prod deviation with a hotfix record; it owes a backfill through the ladder |
| **intent** | the signed, per-goatnet rendering of the pins (IntentV1) |
| **joiner** | any site of a goatnet after the founder |
| **ladder** | the ordered rungs a release climbs, in `ops/release/ladder.yaml` |
| **ledger** | `/var/lib/goat/<component>/deploy-history.jsonl` on each host, one row per change; the only proof of what runs |
| **live rows** | the validation rows that run against a real goatnet during a release |
| **member group** | an Ansible inventory group belonging to an environment |
| **parity gate** | a CI check that runs a verb and its script in dry run and compares them |
| **pending** | a host that has not yet changed to the declared digest |
| **posture** | the team's declared secrets store |
| **read-only verb** | a verb that changes nothing and is safe anywhere |
| **record** | the YAML file for one promotion in `ops/release/records/` |
| **rung** | one step of the ladder, bound to one environment |
| **site** | one host site within a goatnet |
| **soak** | the time a candidate runs on preprod before it may go to prod |
| **unknown** | a host whose ledger cannot be read; never counted as done |
| **wave** | a group of phase-12 steps whose needs are met and which run together |

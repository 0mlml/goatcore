# ADR 1204 — A declared secrets posture per team, and a zero-SaaS quickstart

| Field | Value |
|---|---|
| Status | **Accepted** |
| Date | 2026-10-28 |
| Deciders | operator |
| Amends | [ADR 1186](1186-credentials-at-rest-are-references-only.md) — for teams other than goat's own |
| Related | [`pluggable-dependencies.md`](../design/pluggable-dependencies.md) (design, the audit), ADR 1203 (OpenBao beside 1Password), ADR 1201 (both avenues stay functional), [`secret-store-requirements.md`](../design/secret-store-requirements.md) |

## TL;DR (for a layperson)

goat keeps its own rule that no password or key is ever written to a laptop's disk. A team that takes goat and runs it itself may want something simpler, or may want to try goat on a laptop before signing up for any paid service. That team now declares where its secrets live — a password manager, an encrypted file in git, or a plain file kept out of git — and goat's tools work with whichever it chose. One command fills that plain file with strong random passwords for everything an environment needs.

## Decision

The operator's words are the rule:

> "ADR 1186 says no credential value ever sits on disk" we hold this true for ourselves, but we can't enforce this for others and devs will want an ability to rapidly get a goat env up without provisioning and configuring SaaS accounts and things like that

and, on the same day:

> we need to enable fallback to a simpler less sophisticated posture like using secrets in plaintext that are gitignored or using git secrets because each team that takes this and does their own thing with it will need to make their own choices and own their own balance between sophistication and risk. we need to allow simplistic fallbacks for almost everything like that

Applied:

1. **A team declares its secrets posture** as `secrets: {store: …}` in `goatbuilder.yaml` — `1password`, `openbao`, `custom`, `sops`, `git-crypt`, `env-file` or `env` — and `GOAT_SECRET_STORE` overrides it per command.
2. **A checkout that declares nothing gets `env-file`** (amended 2026-11-02, #2341; it was `1password`). That is the store that needs no account of anyone's, for a team that has not chosen one. A checkout declares its posture in `ops/goatbuilder/posture.yml`, generated into `goatbuilder.yaml`. goat declares `1password` there, so ADR 1186 still holds for goat's team.
3. **goat's own posture is `1password`, references only.** ADR 1186 holds for goat's team without change; its guards apply whenever that posture is declared.
4. **Both avenues read the posture through one module**, `ops/secrets/posture.py`: the store in force, which variables are available, how to supply a missing one, and whether the store can serve. `ops/scripts/with-secrets` remains the one injector.
5. **A plaintext store is named as plaintext.** `env-file` requires the file to be gitignored and mode 0600, and its doctor and its generator refuse otherwise. The design states what each simpler store gives up.
5. **A zero-SaaS quickstart exists.** `goatbuilder secrets init --env <goatnet>` writes strong random values for every variable the goatnet's plays template, never rotating a value already there; values only an outside service can issue are listed with their bypass.
6. **1Password and OpenBao are symmetric where they are alike** — one three-part reference grammar (`op://…`, `bao://…`, both in one `.refs` file), one run verb resolving each scheme with its own store, one doctor shape and one sign-in fix — and different only where the stores differ: authentication, where the store lives, OpenBao's CA certificate and its offline cache. The operator's words: "if possible and appropriate I'd like to hold symmetry in design of cred management and cli usage for op and openbao … but if that is not appropriate or it would make it contrived then don't force it." The table is in the design.
7. **Other dependencies get simple fallbacks where the path allows** (`with-aws` for aws-vault, `image-digest` for crane). Where a dependency is load-bearing, the design says so and why.

## Consequences

- An adopting team can run goat with less protection than goat's own team, by its own declared choice; the tools say which posture is in force on every `doctor` run.
- The OpenBao store (ADR 1203) plugs in as one more store through `ops/secrets/<name>/run`.
- goat's own laptops change nothing: the declared posture is `1password` and every existing guard still runs.

## Alternatives

- **Hold every team to references only:** goat cannot enforce it outside its own team, and it blocks a developer who wants to try goat without SaaS accounts. Rejected by the operator.
- **A plaintext mode with no declaration:** a team could end up on plaintext without knowing it. Rejected: the posture is declared and printed.

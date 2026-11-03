# GHCR private-pull pattern

Reusable shape for letting an Ansible-deployed role pull from a private
`ghcr.io/<org>/*` package without making the package public.

First adopter: the `brief` role (PR #106). Any future private GHCR
image — observability dashboards, partner-specific artifacts, in-house
operator tools — adopts the same shape by flipping one gate.

## Why private at all

Most goat-cluster images are public (panoscope, zenoh, etc.) because
the source is in the same public repo and there's no content-confidentiality
benefit to private packages. A package is kept private when one of:

- The image bundles content not in the source tree (e.g. `brief` bundles
  a briefing PDF + slide deck that are not in the tree).
- The image is built from a sibling private repo whose contents shouldn't
  leak by registry side-channel.
- The image leaks site topology / IPs / hostnames via baked-in config.

If none of those apply, prefer public — it sidesteps this whole pattern
and matches the panoscope precedent.

## Pattern shape

Every role that pulls a private GHCR image declares four defaults:

```yaml
# ops/ansible/roles/<role>/defaults/main.yml
<role>_pull_auth_enabled: false             # opt-in; default off so the
                                            # pattern doesn't fire when
                                            # the image is public
<role>_pull_registry: "ghcr.io"             # rarely needs override
<role>_pull_username: "<org>"             # GitHub org/user that owns
                                            # the package
<role>_pull_password: ""                    # set via group_vars or -e
```

In tasks/main.yml, add a login step **before** the image pull:

```yaml
- name: <role> | log target docker daemon in to private registry (when enabled)
  community.docker.docker_login:
    registry_url: "{{ <role>_pull_registry }}"
    username: "{{ <role>_pull_username }}"
    password: "{{ <role>_pull_password }}"
  become: true
  when:
    - <role>_pull_auth_enabled | bool
    - <role>_pull_password | length > 0
  no_log: true   # keep PAT out of ansible-playbook stdout
```

In the site's group_vars, opt in and source the PAT from a shared env var:

```yaml
# ops/ansible/group_vars/<site>_hosts/vars.yml
<role>_pull_auth_enabled: true
<role>_pull_password: "{{ lookup('env', 'GHCR_READ_TOKEN') }}"
```

## Shared `GHCR_READ_TOKEN`

A **single** PAT, scoped `read:packages` only, covers every private
`ghcr.io/<org>/*` package the operator workstation needs to deploy.
One token → one rotation event → one 1Password entry, regardless of how
many private packages exist.

- **Location:** `.envrc.local.d/10-cloud-and-thirdparty.envrc` →
  `export GHCR_READ_TOKEN='ghp_...'`
- **GitHub UI:** https://github.com/settings/tokens
- **Scope:** `read:packages` and nothing else. (NOT `repo`, NOT `admin`,
  NOT `write:packages`. Least-privilege is load-bearing — if the PAT
  leaks, the attacker can pull private images but can't push, can't
  read source, can't touch GitHub config.)
- **Expiration:** 1 year, calendared rotation. The PAT is the only
  attacker-usable artifact in the chain (the image is the destination).
- **Persistence on target:** `community.docker.docker_login` writes
  the credential to `/root/.docker/config.json` on the target host
  after first deploy. Subsequent pulls (CI rebuild → redeploy, future
  `:sha-<...>` pins) work without re-running the login task. PAT
  rotation triggers a re-run.

## When to add the pattern to a new role

1. The image you want to pull is in a **private** `ghcr.io/<org>/*`
   package (verify visibility: `gh api -X GET /users/<org>/packages/container/<name> --jq .visibility`).
2. Adopt the four defaults shape in `defaults/main.yml`.
3. Add the `community.docker.docker_login` task before the image-pull
   task.
4. Flip `<role>_pull_auth_enabled: true` + source `_pull_password`
   from `GHCR_READ_TOKEN` in the site's `group_vars/<site>_hosts/vars.yml`.
5. Add a pre-flight bullet to the role's runbook pointing operators at
   the PAT-setup procedure (see `brief-runbook.md`
   §Pre-flight item 4 for the template).

## When **not** to use this

- **Public image:** if the package is public, the `docker pull` works
  without auth. Don't add the login step — it's unnecessary moving
  parts.
- **oci.goat substrate (Block 75 / ADR 0838):**
  once per-site `oci.goat/<service>` mirroring lands, pull-side auth
  shifts to the per-site mirror's posture (likely cosign + bundle-rooted,
  not GHCR PAT). This pattern is a v0 expedient — adequate for
  nova's trial-fabric scope, due to be superseded by the
  per-site-mirror substrate.

## Threat model

- **In-scope:** content-confidentiality of the image (e.g. brief's
  slides + briefing PDF). An attacker on the public internet cannot
  pull the image without the PAT.
- **Out-of-scope:** target-host disk forensics (the credential persists
  unencrypted in `/root/.docker/config.json`). The nova fabric is
  a trial-fabric posture; for higher-confidentiality deployments,
  rotate the PAT after each deploy and consider `docker logout` as a
  post-task.
- **PAT leak vectors:** chat transcripts, terminal history, `ps` output
  of a misbehaving ansible run that doesn't honor `no_log`, CI logs.
  The `no_log: true` on the login task is the single in-band mitigation;
  out-of-band mitigations are 1Password, fine scope, and rotation.

## Cross-refs

- Role first adopter: `ops/ansible/roles/brief/`
- Runbook PAT setup: `docs/operations/brief-runbook.md` §Pre-flight
- Future per-site mirror: ADR 0838 +
  `docs/design/oci-goat-substrate.md`
- Local secrets pattern: `docs/operations/local-secrets-pattern.md`

# Guard bypass switches

Every environment sentinel that disarms a guard or preflight in this repository, in one place. A bypass that a script sets for you is a guard that has been removed without saying so, so each switch below is set by a person, per invocation where the guard allows it.

| Switch | Disarms | Scope | Set where |
|---|---|---|---|
| `GOAT_ALLOW_DESTRUCTIVE_GIT=1` | `.claude/hooks/pretooluse-destructive-git-guard.sh`: hard reset, force-push without lease, forced branch delete, `clean -f`, stash mutations, forced worktree removal | one command | in the command string |
| `GOAT_ALLOW_MASTER_GIT_MUTATE=1` | `.claude/hooks/pretooluse-master-worktree-git-guard.sh`: a mutating git op without `git -C <linked worktree>` | one command | in the command string; the preflight's stale-trunk refusal names it because a master fast-forward is exactly this case |
| `GOAT_ALLOW_DOCKER_DESKTOP=1` | `.claude/hooks/pretooluse-docker-desktop-guard.sh` | one command | in the command string |
| `GOAT_DOCKER_DESKTOP_OK=1` or `~/.goat/state/docker-desktop-allowed` | the same guard | session or machine | environment at launch, or the state file |
| `GOAT_ALLOW_MASTER_WRITE=1` | `.claude/hooks/pretooluse-master-worktree-write-guard.sh`: Edit/Write into the master worktree | session | environment at launch (tool calls carry no per-command env) |
| `GOAT_SKIP_ANSIBLE_PREFLIGHT=1` | `ops/scripts/ansible-preflight.sh` stale-trunk refusal | one run | environment |
| `GOAT_SKIP_ARG_GUARDS=1` | the preflight's zero-task `--tags` and mesh-inventory-collision guards | one run | environment |
| `GOAT_SKIP_GHCR_PARITY=1` | the preflight's registry-namespace parity call | one run | environment |
| `GOAT_ANSIBLE_TRUNK_REMOTE=<name>` | not a bypass: which remote the preflight treats as trunk (default `usareur-af`) | one run | environment |
| `GOAT_CLOSEOUT_PROTOCOL=off` | the default-on close-out skill | session | environment |
| `--no-verify` | both halves of `ops/git-hooks/pre-commit`, including `ops/ci/check-no-live-secret.sh`, the only guard for shapeless live secrets | one commit | CONTRIBUTING forbids it |
| `work release --allow-unmerged "<reason>"` | the merge-check in `ops/work/work release`; the reason is recorded | one release | command line |
| `# docker-bridge-ip-ok: <reason>` as a trailing comment | `ops/ci/check-no-docker-bridge-ip-pins.sh` for that one line only; meant for a network's own ipam gateway, which is a definition rather than a pin; the reason is required and a bare marker is refused (PR #958) | one line | in the artifact, next to the address |

Not overridable by design: `.claude/hooks/pretooluse-agent-reference-guard.sh`.

Known hardcoded uses, which the switch's own rule says should not exist: `ops/federation-partnerenv/drill/lib/tunnel.sh:80` and `fabric.sh:119` set `GOAT_SKIP_ANSIBLE_PREFLIGHT=1` for the drill's generated plays, so those runs never see the stale-tree check. Open item in the guard-cull review.

# ADR 1196 — Equipment and site identifiers are near-secrets: gitignored or referenced, never tracked

- **Status:** **Proposed.** Nothing changes on any goatnet on merge. It adds a CI/pre-commit guard for shaped identifiers (MAC addresses, mesh/CGNAT IPs in portable surfaces), a gitignored local-file convention for the rest, and guidance for humans and AI. Enforced going forward; the pre-existing tracked hits are swept under F-585.
- **Date:** 2026-10-27
- **Deciders:** operator
- **Evidence:** F-585 — equipment identifiers in tracked documentation.
- **Related:** [ADR 1186](1186-credentials-at-rest-are-references-only.md) (credentials at rest are references) · **[ADR 0222](0222-oss-public-private-overlay-fork-relationship.md) (the layering counterpart — public core + private overlay; this ADR is "what is sensitive and how it is stored", 0222 is "how the two repos relate")** · `local-secrets-pattern.md` · `ops/ci/check-no-identity-spillage.sh` · F-585

## TL;DR (for a layperson)

The notes that help us run the gear — the hardware addresses of each machine, their network
addresses, their logins, and how their disks are locked — are the kind of thing an adversary wants
and we were keeping in files that go to GitHub and will one day be public. From now on those details
live only in files that git ignores, or as pointers to a password vault, exactly like passwords
already do. A machine check refuses the ones it can recognise (hardware addresses, mesh network
addresses) from being committed; the ones a machine cannot reliably recognise (logins, OS and disk
details) are handled by putting them in the right place from the start, with written guidance for
both people and AI assistants.

## Context

goat already treats credential **values** as references, not plaintext (ADR 1186): operator files
hold `op://vault/item/field` pointers and the value is resolved per command. But a second class of
sensitive material was never covered: the **identifiers of our own equipment and sites** —

- **MAC addresses** of specific machines;
- **mesh / CGNAT IPs** (NetBird + Tailscale, `100.64.0.0/10`) that are durable reachability for a box;
- **LAN IPs**, **hostnames**, **host logins** (usernames);
- **OS build and disk-encryption posture** (e.g. "Ubuntu 24.04, full-disk LUKS") tied to a named host.

None of these is a credential, so the credential guards ignored them. Individually they are low
value; assembled into an inventory — "these named machines at this named site have these
MACs, these addresses, this login, this disk encryption" — they are a target package, and the
material is unwanted even in a private repository.

## Decision

**Equipment and site identifiers are near-secrets. They may exist in plaintext only in a gitignored
local file, or as `op://` references; never in a tracked file.** Two enforcement tiers, because the
material splits by whether a scanner can recognise it:

### Tier 1 — reactive gate, for shaped identifiers

A value with a distinctive shape is refused at commit and in CI by
`ops/ci/check-no-identity-spillage.sh` (already the home for age recipients and SSH pubkeys):

- **MAC addresses** — gated repo-wide. Boundary-anchored to exactly six octets, so an eight-octet key
  fingerprint is not a match. Broadcast (`ff:ff:ff:ff:ff:ff`) and all-zero are allowed.
- **Mesh / CGNAT IPs (`100.64.0.0/10`)** — gated in the portable, shareable surfaces `.claude/skills/**`
  and `docs/reference/**`, which are written to be read out of context and will be published. Allowed
  in the declared infrastructure-as-code homes `terraform/**` and `ops/ansible/inventory/**`.

**RFC1918 LAN IPs are deliberately not gated.** They are indistinguishable by regex from structural
subnet references (a venue scan range, a gateway) and are ephemeral DHCP; the durable identifiers
(MAC, mesh IP) are what the gate catches, and the real LAN IPs live in the local file. The gate is
diff-scoped in CI (it scans the files a change touches), matching the existing identity-spillage
workflow, because the tree still carries pre-existing hits swept under F-585.

**Inline exception.** A legitimate, non-live value — a synthetic MAC in a parser test, say — carries
an inline `opsec-allow: <reason>` annotation and passes. Use it sparingly and state the reason.

### Tier 2 — positive structure, for unshaped identifiers

Host **logins, hostnames, OS build and disk-encryption posture** have no distinctive shape, so a
scanner cannot reliably recognise them without unworkable false positives. These are not gated
reactively. Instead they are kept out of tracked files **by structure and guidance**: they go in the
gitignored local inventory from the start, and the tracked file holds only node type and role.

### Where the values live

- **A structured inventory** (several fields per host): a gitignored local file. Convention:
  `*.local.md` / `*.local.yaml` / `*.local.txt` (gitignored repo-wide). Example:
  `docs/reference/<site>-inventory.local.md`. The tracked sibling keeps only structure and
  points to the local file and to 1Password.
- **A single value** referenced by config: an `op://` reference in `.envrc.local.d/*.refs`, resolved
  per command via `ops/scripts/with-secrets` (ADR 1186). No requirement to call the secret store on
  every action — a gitignored plaintext local file is the sanctioned low-friction form.
- **Regenerated on demand:** on-site, `goat lan scan` rebuilds the inventory from the live network.

### Guidance for humans and AI

When you write or review any tracked file (doc, skill, script, config, ADR, finding):

- Do **not** put a specific machine's MAC, mesh/LAN IP, login, hostname, or OS/disk-encryption posture
  in it. Put node type and role; put the real values in the gitignored local file or an `op://` ref.
- A skill or reference doc that needs the values says *where to get them* (the `.local.md` file or the
  1Password item), with placeholders like `<host-mesh-ip>` / `<login>` in examples.
- Quoting a real value as forensic evidence in a finding, or a synthetic value in a test, is allowed
  with an `opsec-allow:` annotation and a reason.
- This is `~/.claude/CLAUDE.md`-level guidance for AI assistants: treat equipment/site identifiers
  like credentials — reference, never embed.

## Alternatives considered

- **Gate all IPs repo-wide.** Rejected: the repo legitimately carries thousands of IPs in IaC and
  operational docs; a diff-scoped-whole-file scan would block ordinary work. Scope the IP gate to the
  surfaces that get published.
- **Try to regex usernames / OS / encryption.** Rejected: no distinctive shape, unworkable false
  positives. Handle positively (Tier 2) instead of pretending a scanner can.

## Consequences

- New equipment MACs and mesh IPs cannot be committed to portable surfaces; the pre-existing ones are
  swept under F-585 before the open-source release.
- The site inventory stays usable on-site (local file / 1Password / `goat lan scan`).
- Usernames, OS and encryption posture are kept out of tracked files by where they are written, not by
  a gate that would cry wolf.

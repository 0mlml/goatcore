# hello-galaxy — a warrant policy that belongs to nobody

**Status: WORK IN PROGRESS.** Built ahead of need, deliberately. It runs today
and it is not finished; the unfinished parts are named at the bottom rather
than left to be discovered.

```
ops/warrant/demo/run.sh
```

No network, no credentials, no goatnet. It reads this directory and prints an
access-control list generated from it.

## What it is for

warrant ([ADR 1129](../../../docs/adr/1129-warrant-scoped-expiring-network-authorizations.md))
splits into a **mechanism** that can be public and a **policy** that cannot:
`git log -p` on a rule-intents file is simultaneously a map of what reaches
what and a list of who authorized it. The seam is
`generate.py --policy-dir`, and this directory is a complete policy on the far
side of it — so the seam is exercised by something real rather than asserted in
a design document.

It is also the fastest way to understand the shape. It is nine rules and five port-sets,
and it makes four points:

| what `run.sh` shows | why it matters |
|---|---|
| An ACL generated from named sets | The ports live in one place |
| **Zero** literal `ports:` lines in the source | The thing being reviewed is the intent, not the expansion |
| One edit to a set moving every rule that uses it | The Zenoh port was eight identical literals before this |
| The byte-identical check | The same gate CI runs on the real declaration |

## The cast

| group | what it is |
|---|---|
| `HUB` | the substrate — runs the goatnet's own services |
| `CREW` | people with accounts |
| `PROBE` | machines that publish data |
| `OBSERVER` | read-only visitors; the narrowest tier that still exists |
| `BREAKGLASS` | the way back in when everything else is broken |
| `QUARANTINED` | a peer under suspicion — kept, not cut off |

Two of these are worth reading the rules for. **`QUARANTINED` keeps web and
DNS**: the containment is the *absence* of the fabric ports, not the absence of
everything, so a quarantined peer can still resolve names and read the page
explaining its own state. And **`BREAKGLASS` is its own rule** rather than a
widening of an existing one, so "who can open a shell" is one grep instead of
an inference across four rules.

## What is deliberately boring

Port-set names describe the **mechanism**, never the purpose: `shell` is
tcp/22, not "administration". A reference vocabulary has to survive being read
by someone in a different organisation who has different words for the same
ports — and a set named for a purpose quietly accumulates ports that serve that
purpose, which is how a vocabulary stops meaning anything.

## Not finished

- **`goat warrant why-blocked` cannot be pointed at this policy.** The CLI
  compiles the real declaration in at build time, so the demo has a generator
  but no query tool. For the public repo that has to change — either the
  command grows the same `--policy-dir` seam the generator has, or the public
  binary compiles this directory in. Neither is done.
- **No `make demo`.** `warrant-plan.md` §3A calls for one; this is a shell
  script, which is enough to run and not enough to be the published entry point.
- **The narrowing prover and the expiry check are not wired here.** Both work
  on any policy — `check-warrant-narrowing.py` takes two matrix files and
  nothing else — but `run.sh` does not demonstrate them.

## Where the real thing lives

- Operator instructions: [`docs/operations/warrant-runbook.md`](../../../docs/operations/warrant-runbook.md)
- Design: [`docs/design/warrant.md`](../../../docs/design/warrant.md)
- Scope and what was deliberately not built: `docs/project/warrant-plan.md`

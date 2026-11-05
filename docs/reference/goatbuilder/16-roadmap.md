# 16. Roadmap

Everything in chapters 1 to 15 is on `main`. This chapter lists what is
designed and dated but not built, so a reader can tell the two apart.

| Date | Item | What it adds | Design |
|---|---|---|---|
| 2026-11-13 | Tutorial 2 on hardware | the founder-and-joiner pair run once on real machines | [goatnet-tutorials.md](../../design/goatnet-tutorials.md) |
| 2026-11-20 | A `boundary` machine class | `join-node --class boundary` installs a partner egress node through its released installer, with a parity gate | [goatnet-tutorials.md](../../design/goatnet-tutorials.md) |
| 2026-11-27 | Tutorial 3 | install a boundary node, grant one topic to one partner, prove the read and its revocation | [goatnet-tutorials.md](../../design/goatnet-tutorials.md) |
| 2026-11-30 | Zero-SaaS recipes | a store-generic new-site secret generator, a local registry, a local mail sink, alert channels off when empty | [pluggable-dependencies.md](../../design/pluggable-dependencies.md) |
| 2026-11-30 | Zero-SaaS standup, measured | a goatnet stood up end to end on the `env-file` posture with no hosted account | [pluggable-dependencies.md](../../design/pluggable-dependencies.md) |
| 2027-Q1 | Forge adapter | the release path's pull requests, CI waits and admin nomination on a forge other than GitHub | [pluggable-dependencies.md](../../design/pluggable-dependencies.md) |

## Designed, with no date yet

| Item | What it adds | Design |
|---|---|---|
| OpenBao injector on `main` | `ops/secrets/openbao/run`, so the `openbao` store resolves `bao://` references | [pluggable-dependencies.md](../../design/pluggable-dependencies.md) |
| `status` on real hosts | `status` reads real hosts through the push converger, as `verify` and `diff` do today | [goatbuilder-conformance.md](../../design/goatbuilder-conformance.md) |
| A pull converger | `converger: pull-agent`: an agent on each machine applies its class's signed release and `deploy` observes it; push and pull mix per class | [goatbuilder.md](../../design/goatbuilder.md) |
| goatlite | a `lite` preset and a smaller verb set over the same binary and contract | [goatbuilder.md](../../design/goatbuilder.md) |
| A semantic error matcher | a fourth matcher after the lexical one | [error-interpretation.md](../../design/error-interpretation.md) |

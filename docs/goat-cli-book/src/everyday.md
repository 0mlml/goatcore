# Everyday tasks

The commands you'll actually reach for, as quick recipes. Add `--help` to any of them for the full set of flags, or see the **[Command reference](reference.md)**.

## Check you're connected

```text
goat doctor      # stepped: mesh → router → certificate → portal, with a fix-it hint per failure
goat status      # one-glance health: router port + a real mTLS session + portal + your cert's expiry
```

`goat doctor` is your first stop whenever something feels off — each step that fails prints a specific remediation. When everything's green, both commands give you a clear all-clear.

## Publish and subscribe

```text
goat sub 'acme/radar1/tracks'             # watch a topic live (Ctrl-C to stop)
goat sub 'acme/**'                        # a whole subtree
goat pub acme/radar1/tracks '{"t":1}'     # publish a payload
```

Want to see the ripple a publish sets off? Add `--narrate`:

```text
goat pub acme/radar1/tracks '{"t":1}' --narrate
```

## Work with topics

```text
goat topic ls                              # list topics
goat topic ls acme                         # drill into an org/prefix
goat topic show acme/radar1/tracks         # one topic's full record
goat topic create acme/radar1/tracks --format protobuf:Track --description "live tracks"
```

After you create a topic, goat nudges you toward the natural next step (watch it, or publish to it).

## Diagnose a problem

```text
goat inspect topic=acme/radar1/tracks      # a diagnostic verdict for a topic
goat inspect vendor=acme                   # ...or a whole org (`vendor=` is the literal target syntax)
goat inspect system=all                    # ...or a system
```

`inspect` returns findings and the remedy for each — what's wrong and what to do about it.

## Scripting

Every command that prints a human view also offers `--json` for a stable, machine-readable one. The JSON form never carries banners, hints, or flourishes, and its shape and your exit code stay constant across releases and languages:

```text
goat hello --json | jq .round_trip
goat status --json | jq '.session.ok'
```

This is the contract: **friendly on a terminal, clean in a pipe.**

One caution: `goat status` opens a real session at the router each time it runs, so it is a check you run when you want an answer, not something to poll from a timer. For a trend over time use `goat health` or `goat top`, which read what the goatnet already publishes.

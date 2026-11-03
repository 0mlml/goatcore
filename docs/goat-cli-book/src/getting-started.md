# Getting started

You don't need a profile, a bundle, or a network to start. goat ships with a sandbox so you can learn by doing, safely.

## 1. Say hello

```text
goat hello
```

```text
◇ SANDBOX — simulated fabric, no real mesh. Run `goat init` to attach a real env.
goat hello — saying `lo` to the fabric as `default` on topic `hello/default`
  [t+  42ms] portal     accepted topic `hello/default` (simulated)
  [t+  74ms] fabric     routed `lo` to `hello/default` over the WireGuard P2P path (simulated)
  [t+ 101ms] subscriber your subscriber received `lo` — round-trip complete (simulated)
  └─ `lo`: the first message ever sent over a network (ARPANET, 29 Oct 1969) — they typed "LOGIN" and the link crashed after two letters.
round-trip complete.
```

`goat hello` publishes a message, subscribes to it, and watches it come back — narrating every hop the data took. In the sandbox those hops are simulated; against a real env they're real. Either way, when it says *round-trip complete*, you know the path works end to end.

> The message is `lo` on purpose — the first thing ever sent over a network.

## 2. Take the tour

```text
goat tutor
```

`goat tutor` is a self-paced, hands-on tour — think `vimtutor` for the fabric. Eight short lessons take you from "what is this" to publishing, subscribing, diagnosing, and reading the substrate cascade. It runs in the sandbox by default, saves your place, and you can stop and resume any time:

```text
goat tutor --list     # see the lessons
goat tutor            # start (or resume)
goat tutor --reset    # start over
```

## 3. Attach a real environment

When you've been issued an onboarding **bundle** (a `.cbor` file with your identity), the wizard takes it from here:

```text
goat ca add <bundle-ca.pem>        # trust the CA that signed it — once per goatnet
goat profile init <bundle.cbor>    # install your identity
goat init                          # the wizard picks it up
goat doctor                        # confirm you can reach the fabric
```

The first line is not optional. `goat` ships trusting **no** goatnet, so
`profile init` refuses until you add the root that signed your bundle — the
`-ca.pem` ships beside the `.cbor` it verifies. `goat ca add` prints a
fingerprint; check it against one your operator gives you before relying on it,
because a bundle that reached you over a channel proves only that the channel
delivered it. `goat ca ls` shows what you trust; one binary works against every
goatnet whose root you have added.

`goat init` is the friendly front door — it detects whether you're set up, lets you choose the sandbox or a real env, proves it works, and points you at the tutor. `goat doctor` then runs a stepped reachability check (mesh → router → certificate → portal) and, if anything's wrong, tells you exactly what to fix.

When all of doctor's steps pass, you'll see a small 🐐 — you're on the fabric.

## Where to next

- New to the ideas? Read **[How the fabric works](concepts.md)**.
- Ready to do things? Jump to **[Everyday tasks](everyday.md)**.
- Something not working? **[Troubleshooting](troubleshooting.md)**.

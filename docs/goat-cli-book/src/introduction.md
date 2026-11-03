# Welcome

**goat is the operator's command-line for the data fabric** — one tool to find the fabric, prove you can reach it, publish and subscribe to data, and see what's happening underneath. It runs on macOS, Linux, and Windows, and it works the same on a connected laptop or an air-gapped island.

If you have five minutes, here's the whole thing:

```text
# 1. install it (one line — see "Install & verify" for the details)
curl -fsSL https://get.<your-goatnet>/goat-$(uname -s | tr A-Z a-z)-amd64 -o ~/.local/bin/goat && chmod +x ~/.local/bin/goat

# 2. say hello — no profile, no mesh, no setup required
goat hello

# 3. learn it hands-on, at your own pace
goat tutor
```

That `goat hello` runs against an **in-process sandbox** — a fully simulated fabric — so you can try the tool, learn the commands, and run the whole tutorial before you've been handed any credentials, on a plane if you like. When you're ready for the real thing, one bundle and `goat init` switches you over.

> **The sandbox is always marked.** Anything simulated prints a `◇ SANDBOX` banner. You will never mistake a practice run for the real fabric.

## What's in this guide

- **[Getting started](getting-started.md)** — from zero to your first round-trip, then the guided tour.
- **[How the fabric works](concepts.md)** — the mental model: mesh, identity, topics, pub/sub, and why there's no broker.
- **[Everyday tasks](everyday.md)** — the commands you'll reach for: publish, subscribe, doctor, status, inspect, narrate.
- **[Install & verify](install.md)** — every platform, with checksum verification.
- **[Languages](languages.md)** — goat speaks 28 NATO-member languages plus Ukrainian (and one for fun).
- **[Troubleshooting](troubleshooting.md)** — when something's red, start here.
- **[Command reference](reference.md)** — every command and flag, generated from the binary itself.

## A note on output

goat is built to be both a pleasure to use *and* safe to script. Friendly touches — a 🐐 when everything's green, a hint about your next step — only ever appear on an interactive terminal. Pipe goat into a script or ask for `--json` and you get clean, stable, machine-readable output with nothing decorative in the way. Set `GOAT_NO_FUN=1` and even the interactive flourishes go quiet.

# Bringing vendors up on goat-cli

Runbook for getting people who already hold an onboarding bundle onto the
`goat` CLI, now that the mesh download surface exists
([`get.<site>.*`](../design/goat-cli-mesh-download.md) — per-goatnet:
`get.nova.*` on nova-a, `get.zagros.*` on ZAGROS). Bundles minted before
2026-06-04 carry a `<handle>-goat-cli.cbor` profile but predate the
in-bundle install line, so the binary step has to be sent separately.

## Who needs it

Anyone with a `*-goat-cli.cbor` in `ops/operators/out/` whose bundle was
delivered without a working CLI install path. Check:

```bash
ls ops/operators/out/*-goat-cli.cbor
```

## Precondition (operator, one-time)

Deploy the download surface per
[`goat-cli-mesh-download.md`](../design/goat-cli-mesh-download.md) §Deploy.
Verify from an on-mesh host:

```bash
# Use your goatnet's host, get.<site>.<zone>
curl -fsSL -o /dev/null -w '%{http_code}\n' https://get.<site>.<zone>/goat-linux-amd64
```

## Message to send each vendor (macOS / Linux)

> You already have your onboarding bundle: a `…-goat-cli.cbor` file and the
> `…-goat-cli-ca.pem` that sits beside it.
> Once you're on the NetBird mesh, install the CLI and load your profile:
>
> ```bash
> GOAT_GET=get.<site>.<zone>   # your goatnet's host (we'll tell you)
> OS=$(uname -s | tr 'A-Z' 'a-z'); ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
> DL=$(mktemp -d)                                # private download dir, not a shared /tmp name
> curl -fsSL https://${GOAT_GET}/goat-${OS}-${ARCH} -o "$DL/goat"
> [ "$OS" = darwin ] && codesign --force --sign - "$DL/goat"   # macOS: clear the launch hang, before the move
> sudo mkdir -p /usr/local/bin
> sudo install -m 0755 "$DL/goat" /usr/local/bin/goat
> goat --version
> goat ca add <your-handle>-goat-cli-ca.pem       # trust our CA, once per goatnet — before profile init
> goat profile init <your-handle>-goat-cli.cbor   # the .cbor we sent you
> goat doctor                                      # verifies reachability
> ```
>
> `/usr/local/bin` is on your `PATH` by default, so `goat` is found immediately. If you have an older `goat` in `~/.local/bin`, remove it with `rm -f ~/.local/bin/goat` so it does not shadow the new one.
>
> `goat` ships trusting no goatnet, so `goat profile init` refuses (`E_BUNDLE_INVALID` — wrong CA) until `goat ca add` has run. It prints a fingerprint — check it against the one we give you before you rely on it.

The download host is mesh-only, so they must have NetBird up first
(`goat doctor`'s first step checks exactly that and gives a remediation
hint if not).

## Windows vendors

goat-cli now builds for Windows/amd64 (ADR 1019 2026-06-07 revisit). Once a
release built with the Windows leg is cut and the mesh download surface is
bumped to it, Windows vendors get `goat-windows-amd64.exe` from
`get.<site>.*` — every subcommand works except `goat client` (tunnel-daemon
IPC, not built for Windows yet). Until that release lands, or as an
always-available fallback, Windows vendors use the browser surfaces —
portal (`portal.nova.*`), panoscope (`panoscope.nova.*`), brief
(`brief.nova.*`) — plus NetBird and the Python `eclipse-zenoh`
publish/subscribe path, all of which work on Windows.

## Re-issuing bundles (optional)

Bundles minted from 2026-06-04 onward already embed the install line — no
separate message needed. If you'd rather re-hand a self-contained bundle
than send the snippet above, re-run `goat admin onboard <handle>` (NetBird
+ Ory idempotent; a fresh `zenoh.trial.vendor` prefix re-mints the cert if
the prior one is unrecoverable — see
`operator-onboard-utilities.md` §83E).

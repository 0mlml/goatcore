# Tutorial 0 — goatlite: your first goatnet, on one box, from one file

You will stand up a working goatnet on one Ubuntu host or VM on your LAN and
use it: enrol your laptop, sign in, read its health, publish a message and
receive it. You write one file of four facts and run one command. What comes up
is a real goatnet with its own mesh, identity service, certificate authority
and data fabric, not a demo. You rehearse the run on a worked example first, on
your laptop, in under a minute.

| | |
|---|---|
| Audience | Someone who has never run a goatnet and knows none of the software under it |
| Time | The rehearsal: five minutes. The real run: about an hour the first time |
| You need | One Ubuntu 24.04 host or VM with 4 cores and 16 GB that you can SSH into with a key, a laptop on the same LAN (Linux or Apple Silicon macOS; any other laptop needs [Rust](https://rustup.rs/) to build from source) with the [NetBird client](https://docs.netbird.io/how-to/installation), and a checkout of this repository |
| You finish with | A goatnet whose acceptance gate is green and on which you have published a message and received it |
| Status | Written 2026-11-02; unmeasured — the cold-read drill of [goatlite.md §9](../design/goatlite.md#9-acceptance) has not run |
| Next | [Tutorial 1 — a new goatnet, from nothing](01-new-goatnet.md) |

goatlite is `preset: lite` in the goatbuilder configuration. It runs the same
fourteen-phase standup every goatnet runs, in its LAN profile, with every
decision already made ([goatlite.md](../design/goatlite.md),
[ADR 1213](../adr/1213-goatlite-preset-one-box-one-file.md)). The procedure
behind every phase is the standup runbook,
[goatnet-standup.md](../operations/goatnet-standup.md); this tutorial is one
instance of it, not a second procedure. The script that stands beside the
builder, `ops/scripts/goat-standup.sh`, runs the same phases and stays
documented.

Until ADR 1197's public images land, pulling the goatnet's container
images needs one account: a GitHub token with `read:packages`, exported as
`GHCR_READ_TOKEN`. Nothing else in this tutorial needs an account with any
outside service.

## The words used here

- **goatnet** — one environment: its own mesh, identity, certificate authority and data fabric.
- **the box** — the one host or VM the goatnet runs on.
- **zone** — the DNS name under which the goatnet's services live. goatlite's is `<name>.lan`, served by the box itself.
- **phase** — one numbered, re-runnable step of the standup. A failed phase is fixed and resumed by number.
- **assay** — the acceptance gate: every provisioned part proven live and working, layer by layer.

## 1. Get the checkout and the binaries

Clone this repository and work from its root. Every command below runs there.

```text
git clone https://github.com/usareur-af/goatcore.git
cd goatcore
```

Then put `goatbuilder` and `goat` on your `PATH`. Download them from the
repository's [Releases](https://github.com/usareur-af/goatcore/releases), or
build them from source.

### Download a release

Each binary has its own release tag: `goatbuilder/v<version>` for
`goatbuilder` and `goat-cli-v<version>` for `goat`. Take the newest of each
from the Releases page and set them below. The asset for your laptop is
`linux-amd64`, `linux-arm64` or `darwin-arm64` (Apple Silicon).

```bash
GB_TAG=goatbuilder/v<version>      # e.g. goatbuilder/v0.4.1
GOAT_TAG=goat-cli-v<version>       # e.g. goat-cli-v0.5.14
PLATFORM=linux-amd64               # or linux-arm64, darwin-arm64
REL=https://github.com/usareur-af/goatcore/releases/download
mkdir -p ~/.local/bin
fetch() {   # fetch <binary> <tag>: download, check the checksum, install
  ( cd "$(mktemp -d)" &&
    curl -fL --remote-name-all "$REL/$2/$1-$PLATFORM" "$REL/$2/SHA256SUMS" &&
    { sha256sum -c --ignore-missing SHA256SUMS 2>/dev/null || shasum -a 256 -c --ignore-missing SHA256SUMS; } &&
    install -m 0755 "$1-$PLATFORM" ~/.local/bin/"$1" )
}
fetch goatbuilder "$GB_TAG"
fetch goat "$GOAT_TAG"
goatbuilder --version
goat --version
```

Each `-c` line must print `OK`. Each release also carries
`SHA256SUMS.cosign-bundle`, a signature over `SHA256SUMS` made by the
repository's release workflow; its release notes give the `cosign verify-blob`
line to check it.

On macOS, a binary downloaded with a browser instead of `curl` is quarantined
and macOS refuses to open it. The binaries are not notarized; clear the
attribute once: `xattr -d com.apple.quarantine ~/.local/bin/goatbuilder ~/.local/bin/goat`.

### Or build from source

On any other laptop, or to run the code in your checkout, build both with
[Rust](https://rustup.rs/). The first build takes several minutes.

```text
(cd crates && cargo build --release --locked -p goatbuilder -p goat-cli)
mkdir -p ~/.local/bin
install -m 0755 crates/target/release/goatbuilder crates/target/release/goat ~/.local/bin/
goatbuilder --version
goat --version
```

If `--version` says "command not found", add `~/.local/bin` to your `PATH`.

## 2. Write the one file

Create `goatlite.yaml` at the root of the checkout. Four facts are required;
two more are optional. The repository and the services a goatlite runs are read
from the checkout you cloned.

```yaml
schema: goatbuilder/v1
preset: lite
lite:
  name: pasture            # the goatnet's name; its zone is pasture.lan
  host: 192.0.2.50         # the box's LAN address or hostname
  ssh_user: ubuntu         # the user your SSH key logs in as
  admin: <your-email>      # the first and only administrator: you
  zone: pasture.lan        # optional; defaults to <name>.lan
  disk_encryption: false   # optional; defaults to true. false for a VM
  data_disk: /dev/nvme0n1  # optional; the disk to encrypt, when disk_encryption is true
  ssh_pubkey: ~/.ssh/id_ed25519.pub   # optional; defaults to the first ~/.ssh/id_*.pub found
```

`disk_encryption: true` encrypts the box's data disk and asks for a passphrase
at every boot; `data_disk` names the disk it encrypts, so check it with `lsblk`
on the box before a real run. On a VM you will throw away, set
`disk_encryption` to `false`. Your SSH public key becomes the administrator key
on the box. Everything else
the full configuration asks for is supplied by the preset; the table of what it
decides is [goatlite.md §3](../design/goatlite.md#3-what-the-preset-decides).

## 3. Rehearse it on the fixture

The worked example is a goatlite called `pasture`. A scripted stand-in for the
box makes every phase run for real against files, nothing touches a network,
and the output is the same every time.

The commands below run from the root of the checkout. The copy keeps the
worked example clean, so you can run it as often as you like.

```bash
WORK="$(mktemp -d)/pasture"
cp -R ops/goatbuilder/fixtures/goatnet-from-scratch/00-goatlite "$WORK"
goatbuilder standup pasture --mock "$WORK" --ack "A Learner" --yes
goatbuilder status --env pasture --mock "$WORK"
goatbuilder assay pasture --values ops/goatbuilder/fixtures/goatnet-from-scratch/00-goatlite/assay-values.yaml --plan
```

**Checkpoint 1.** Your output matches
`ops/goatbuilder/fixtures/goatnet-from-scratch/checkpoints/00-goatlite.txt` line
for line, apart from the milliseconds. Read it before moving on: the phases run
in the standup's order, the internet-only phases are skipped and say so, the
last line of `standup` says every declared component is verified on the one
machine, and `assay --plan` lists every acceptance check that applies to a
one-box LAN site. Run it twice: a second standup is always safe.

## 4. Check the laptop and the box

```text
goatbuilder doctor
```

The doctor reads your `goatlite.yaml` and prints one `lite` row naming every
decision it derived from your four facts: the box, the SSH user, the
administrator, the zone, the environment and its one site, disk encryption,
the secrets store. Then it names every tool it needs on the laptop and checks
`GHCR_READ_TOKEN` is set. Under the lite preset the cloud rows read as skipped,
declared. Fix what it names, then run it again until every row passes. The
doctor does not reach the box; phase 1 of the standup does.

## 5. Stand it up

```text
goatbuilder standup pasture
```

The driver first writes the goatnet's configuration files into your checkout
from your four facts, then runs the phases. What each prints, in plain words:

| Phase | What happens | What you see when it worked |
|---|---|---|
| 1 | Gates: the configuration is checked, the access policy is declared, the box is measured | `preflight OK`, then `READY` |
| 2 | The box is baselined; the data disk is encrypted if you asked for it | the container engine's data lives on the declared disk |
| 3 | Every secret the goatnet needs is generated into one local file | the file exists, mode 0600, gitignored |
| 4 | The goatnet's certificate authority is minted on the box and every service certificate is cut from it | every certificate present |
| 5 | The management stack comes up | the play stops at a schema wait on a first run, which is expected; phase 6 finishes it |
| 6 | The identity service comes up, then the management stack finishes and prints a single-use setup key | `failed=0` and the setup key, which you save |
| 7, 8 | Skipped: internet-only | each says so |
| 9 | The data-fabric router and the workload-identity server come up | both healthy; the router listens on the box's LAN address |
| 10 | The LAN services come up: the dashboard, the name server for your zone, the login pages | the dashboard answers on the LAN |
| 11 | **The one browser step.** The run pauses and prints what to do, below | you resume with `--from 12` |
| 12 | The access policy is applied, the service identities are minted, the portal and the remaining services come up | `OK: no All-group rule; every enabled policy is declared.` then `OK: every required identity is present and verified.` |
| 13 | The assay | every applicable row `PASS` |
| 14 | Skipped: no rescue node and no relay tier are declared | it says so |

A phase that fails says which phase, what state the box is in, and the resume
command. Every phase is idempotent: fix the cause and resume from that number.

### The browser step

Phase 11 needs a person, once: the goatnet's first login creates your account,
and the one service token every later identity is minted from can only be made
after someone has signed in. The driver prints these steps with your names
filled in, and prints nothing for this phase once the token is in your secrets
file.

1. **Make your laptop resolve the zone.** Either set the laptop's DNS server to
   the box's address, or pin the dashboard, login and enrolment names of your
   zone to the box's address in the laptop's hosts file.
2. **Read your one-time password** from the box, from the file
   `/opt/mgmt-host/.bootstrap-passwords/<your-email>.txt`.
3. **Sign in** at `https://dashboard.<zone>` with your email and that password.
   Your browser will warn about the certificate until section 8 of this
   tutorial; proceed for now.
4. **Re-run phase 10 once** to make your new account an administrator. The
   driver prints the exact line. Until you do, the dashboard refuses you.
5. **Create the service token.** In the dashboard open Team, then Service Users,
   and create a service user named `svc-<name>-netbird-infra` with the admin
   role. Create a token on it.
6. **Store the token** where the driver says. Under the lite preset that is one
   line in the goatnet's secrets file; `goatbuilder secrets hint <NAME>_NB_API_TOKEN`
   prints the exact line to add.

Then resume:

```text
goatbuilder standup pasture --from 12
```

## 6. Enrol your laptop

Phase 6 printed a single-use setup key. Enrol the laptop with it, against the
enrolment name of your zone. `netbird` is the NetBird client from "You need"
above, installed the way [its documentation](https://docs.netbird.io/how-to/installation)
says for your operating system:

```text
sudo netbird up --management-url https://netbird.<zone>:443 --setup-key <the key from phase 6>
```

Run it on the laptop, then confirm:

```text
netbird status
```

You want `Management: Connected` and an address in the mesh range. From here
the laptop resolves every name in your zone through the goatnet's own name
server, and you can remove any hosts-file pin from the browser step.

## 7. Done means green, proven

Phase 13 ran the assay. Run it again any time:

```text
goatbuilder assay pasture
```

It checks the goatnet layer by layer: the box, the management stack, TLS and
DNS, the mesh, identity, the fabric, time, the service surface. Every
applicable row is `PASS`. A row that could not run is `UNKNOWN`, which is never
a pass, and the command exits 3 until there are none. Read the table, not the
exit code.

## 8. Use it

**Your welcome package.** Your fabric identity comes from the goatnet's own
onboarding, the same way every later person's will: the identity console
issues you a package holding the goatnet's root certificate, your fabric
bundle and a notes file with the two commands to run, in order. Trust the root
once per laptop, then establish your profile:

```text
goat ca add <the .pem in the package>
goat profile init <the .cbor in the package> --context pasture
```

Install the same root certificate in your laptop's system trust store, and the
browser warning from the sign-in step goes away.

**Read its health.**

```text
goat status
goat health
goat health --domain mesh
```

`goat status` opens one real session to the router and gives one verdict per
layer. `goat health` reads the fabric's own health beat; `--domain mesh`
reads the mesh's.

**Round-trip a message.** `goat hello` is the built-in smoke: it publishes
through the fabric and receives its own message back.

```text
goat hello
```

**Publish and subscribe for real.** Your package names the prefix you may write
under. Subscribe in one terminal, publish in another, and watch the message
arrive.

```text
goat sub '<your-prefix>/**' --take 1 --for 60s
```

```text
goat pub <your-prefix>/hello/v1 --payload 'first message on pasture' --confirm
```

`--confirm` reads the value back from the router's storage, so a denied write
is reported instead of silently dropped. `goat topic ls` lists what the
goatnet has declared.

## When something fails

Every error `goat` and `goatbuilder` print says what happened, the likely
cause, what to do and where to read. Paste any error, or its code, back in:

```text
goatbuilder explain GB-029
goatbuilder explain "Permission denied (publickey)"
goat explain E_OFF_MESH
```

For the goatnet as a whole, run the assay and read the first row that is not
`PASS`:

```text
goatbuilder assay pasture
```

When the catalog does not know your error, `goatbuilder explain --report -`
writes the text and its interpretation to a file to share, so the catalog
grows.

## Undo

Re-running is the repair: fix the cause and resume from the failed phase.
`goatbuilder teardown pasture --site pasture --dry-run` prints the burn-down
order, the standup in reverse; `--services` stops the services and keeps every
volume.

## What you have

A real goatnet on one box: its own mesh with your laptop on it, its own
identity service with you as its administrator, its own certificate authority,
its own data-fabric router carrying your messages, every secret in one local
file, and no account with any outside service beyond the image-pull token.
Everything you learn on it carries forward. The growth path to an
internet-enrollable goatnet is an `internet:` block added to the `lite:` entry
and a re-run, never a second standup; the block parses today and nothing acts
on it yet, and what growing costs is measured in
[goatlite.md §6](../design/goatlite.md#6-growing-out-of-lite).

The next rung is [Tutorial 1](01-new-goatnet.md), the full shape: the same
fourteen phases with every decision this preset made for you written out, a
founder site that can take an observability host, a rescue node and, later,
federated nodes.

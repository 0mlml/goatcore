# goat-cli — macOS code-signing & notarization runbook

Why this exists: the macOS `goat` binary ships from the Rust linker with an
`adhoc,linker-signed` signature that intermittently **hangs at launch** in the
kernel's Gatekeeper trust evaluation (pre-`main()`, un-killable) — see
F-208 and the
copied-binary `Killed: 9` sibling F-209.

There are two tiers of fix. The release pipeline does whichever it has the
credentials for; both are implemented in the **one** signing step in
`.github/workflows/goat-cli-release.yml`.

## Tier 1 — proper ad-hoc re-sign (active today, no credentials)

When no Developer ID secrets are present, the pipeline runs
`codesign --force --sign -` on the macOS binaries before upload. This replaces
the fragile `linker-signed` ad-hoc signature (codesign flags `0x20002`) with a
**proper** ad-hoc one (flags `0x2`, fresh cdhash) that registers cleanly in the
trust cache. This is the lever that clears the F-209/F-208 launch failures.

Limitation: ad-hoc is **not notarized**, so `spctl -a -t execute` still
*rejects* it and macOS still does a (now non-fragile) trust evaluation on first
run. Good enough to stop the hang; Tier 2 is the real end-state.

The same `codesign --force --sign -` is also appended to every macOS install
block in [`goat-cli-install.md`](goat-cli-install.md) and the `get.<site>.*` landing
page, so even older binaries and hand-copies get a proper signature.

## Tier 2 — Developer ID + notarization (durable end-state)

When the Developer ID secrets below are present, the same pipeline step instead
does distribution-grade signing: hardened runtime + secure timestamp +
`Developer ID Application` signature + `xcrun notarytool` notarization. After
that, `spctl -a -t execute` **accepts** the binary and Gatekeeper takes the
cached, ticketed path — no synchronous flaky evaluation, no launch hang.

### What this environment already has

- An **App Store Connect API key**: `~/.appstoreconnect/AuthKey_<KEYID>.p8`
  (used by `notarytool`).
- An **"Apple Development"** signing cert in the login keychain → Developer
  Program membership is active.

### What's still missing (the provisioning gap)

1. **A `Developer ID Application` certificate.** The "Apple Development" cert
   **cannot** notarize for distribution — distribution signing requires a
   `Developer ID Application` cert specifically. Mint one (Xcode → Settings →
   Accounts → Manage Certificates → `+` → *Developer ID Application*, or via the
   ASC API), then export it from Keychain Access as a `.p12` with a password.
2. **The ASC issuer-id** — the UUID at App Store Connect → Users and Access →
   Integrations → App Store Connect API. Not derivable from the local files.

### Provision the GitHub Actions secrets

Set these as repository (or org) secrets on `usareur-af/goatcore`:

| Secret | Value |
|---|---|
| `APPLE_DEV_ID_APP_CERT_P12_BASE64` | `base64 -i DeveloperIDApplication.p12` |
| `APPLE_DEV_ID_APP_CERT_PASSWORD` | the `.p12` export password |
| `APPLE_DEV_ID_APP_IDENTITY` | the cert common name, e.g. `Developer ID Application: <Name> (<TEAMID>)` |
| `APPLE_NOTARY_KEY_P8_BASE64` | `base64 -i ~/.appstoreconnect/AuthKey_<KEYID>.p8` |
| `APPLE_NOTARY_KEY_ID` | the `<KEYID>` from the `.p8` filename |
| `APPLE_NOTARY_ISSUER_ID` | the ASC issuer-id UUID (step 2) |

```bash
# Example (run locally; pipe through gh):
gh secret set APPLE_DEV_ID_APP_CERT_P12_BASE64 < <(base64 -i DeveloperIDApplication.p12)
gh secret set APPLE_NOTARY_KEY_P8_BASE64 < <(base64 -i ~/.appstoreconnect/AuthKey_<KEYID>.p8)
gh secret set APPLE_NOTARY_KEY_ID --body '<KEYID>'
gh secret set APPLE_NOTARY_ISSUER_ID --body '<issuer-uuid>'
gh secret set APPLE_DEV_ID_APP_IDENTITY --body 'Developer ID Application: <Name> (<TEAMID>)'
gh secret set APPLE_DEV_ID_APP_CERT_PASSWORD --body '<p12-password>'
```

Once all six are set, the next `goat-cli-v*` release tag produces
Developer-ID-signed + notarized macOS binaries automatically. Verify on a clean
Mac: `spctl -a -t execute -vv goat` should print `accepted` and `source=Notarized
Developer ID`, and `goat --version` returns in well under a second with no
pre-`main` stall.

### Known limitation — no stapling for a bare CLI

`xcrun stapler staple` attaches a notarization ticket only to a container
(`.app`/`.dmg`/`.pkg`), **not** a loose Mach-O. The notarized `goat` binary
therefore relies on the online (then cached) Gatekeeper ticket lookup. On a
machine that has *never* been online, the first run still can't fetch the ticket.
For fully-offline first-run, a follow-on would distribute `goat` inside a stapled
container; the mesh download surface is online by construction, so this is a
low-priority edge case.

## goat-clientd

`goat-clientd` (the goat-client tunnel daemon) is **not** a goat-cli release
artifact — it is built/distributed in the goat-client track (ADR 0840), which has
its own code-signing requirement noted there. The install-path re-sign in
[`goat-cli-install.md`](goat-cli-install.md) re-signs `goat-clientd` too when
present; the pipeline signing here covers only `goat`.

# ADR 1212 — Team secrets are never decryptable on the server; the server only ever holds encrypted values

| Field | Value |
|---|---|
| Status | **Accepted** (2026-10-31, operator): R0 is the rule; both shapes in §Decision are built completely and separately, on synthetic values, and the operator chooses between them when both are measured. The choice amends this ADR. |
| Date | 2026-10-31 |
| Deciders | operator |
| Repo scope | goat core (`ops/scripts/with-secrets`, `ops/secrets/`); the team's instance lives in a private infra repo |
| Supersedes | [ADR 1203](1203-self-hosted-credential-store-openbao.md) (a store that decrypts on its server) |
| Related | [ADR 1186](1186-credentials-at-rest-are-references-only.md) (references only on disk), [ADR 1209](1209-card-sign-in-is-per-sovereign-pki-profile.md) and ADR 1210 (sign-in, which stand), [`secret-store-requirements.md`](../design/secret-store-requirements.md) R0 |

## Context

The team keeps its credentials in 1Password and wants a self-hosted store beside it. 1Password's server never holds a key that can read a vault: values are encrypted on the device and sharing re-wraps keys on the device. The store of ADR 1203, now superseded, did not have that property: it unsealed a storage key on the server and answered plaintext to any authenticated client, so it needed a VPN in front and anyone with the host's disk could read stored values. The written requirements had no line stating the property; it is now R0.

## Decision

1. **R0 is a must.** The server of the team's credential store never holds a key that can decrypt a stored value. An attacker with the server's disk and root obtains ciphertext and nothing that opens it. Backups are ciphertext. Because the server holds nothing readable, it may stand on the public internet with no VPN; the certificate-gated edge of ADR 1209 is its door.
2. **1Password remains the store** until a replacement has carried a full deploy with byte-identical values on three admins' laptops.
3. **The replacement is one of two shapes.** Both are built completely and separately, each to its measured gates on synthetic values and never on a real team secret, and the operator chooses with both in hand; this ADR is amended with the choice:
   - **OpenBao with client-side encryption.** The server built under ADR 1203, with its card, YubiKey and team-Kanidm sign-in, kept as the component that stores, serves, versions and audits. Each partition has a data key wrapped to every member's and every machine's decryption key; `with-secrets` and `people.py` encrypt a value on the device before writing and decrypt after reading. The server's audit log names the identity and the value for every fetch. The encryption layer is goat's, built on the grant-crypto SDK, and reviewed by someone who did not write it before the first real value is written.
   - **Vaultwarden with the Bitwarden CLI.** A self-hosted Bitwarden-compatible server whose audited client model does the encryption; master password plus a FIDO2 key, the team Kanidm in front through OIDC; `with-secrets` resolves references through an adapter over the CLI. Machine secrets then live in SOPS-encrypted files with age, each host decrypting with its own key.

## What decides between the two shapes

OpenBao with client-side encryption if the store is a tool for admins and machines through goat's own tooling, where a server-side read log naming the person and the value, one-step revocation and government-card sign-in matter, and the team will own a reviewed encryption layer. Vaultwarden if the store must also be the team's everyday password manager with a browser extension and a phone app, and nobody is to own cryptography. The comparison with sources is the decision document of 2026-10-31 and the fact sheet beside it.

## Consequences

- No value held by the team is ever decryptable by a cloud host or its operator; a disk image yields nothing. The VPN is no longer a precondition for using the store.
- Read attribution on a zero-knowledge server is what the server can see. With the OpenBao shape it is a server log of who fetched which ciphertext; with Vaultwarden it is client-reported events.
- Revocation anywhere is removal from the key set plus rotation of what the person may have cached; this was already the rule under 1Password.
- Government-card sign-in to the store survives only in the OpenBao shape; the YubiKey is the hardware factor in both.
- The first measured gate for either shape: plant one value, image the server's disk with root, confirm the value is unrecoverable.

## Alternatives set aside

Bitwarden Enterprise self-hosted (the Vaultwarden model with a vendor-validated module and documented event logs, about $864 a year); Passbolt (item names readable by the server by default); Psono (loses R0 when single sign-on is enabled); SOPS with age alone (no read log, no revocation); Infisical and the cloud secret managers (decrypt on the server, as ADR 1203's store did).

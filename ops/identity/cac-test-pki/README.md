# cac-test-pki — test-only PKIs shaped like the sovereign PKI profiles

`./gen-test-card.sh <pki id> [identifier] [email]` generates a self-signed CA whose
subject is the profile's example issuer DN and a card certificate built from the
profile's `example` in `../sovereign-pkis.yaml` — subject DN, SAN UPN / e-mail /
otherName — with the identifier substituted. Output in `out/<pki id>/` (gitignored):
`ca.crt`, `card.crt/key/p12` (p12 pass `cardtest`), `IDENTIFIER`.

| Profile | Card shape | Identifier |
|---|---|---|
| `us-dod` | CN `MAINTAINER.TEST.MI.<EDIPI>`, SAN UPN `<EDIPI>121002@mil`, SAN e-mail | the 10-digit EDIPI |
| `est-id` (sketch) | CN `JÕEORG,JAAK-KRISTJAN,<code>`, serialNumber `PNOEE-<code>`, SAN e-mail | the 11-digit personal identity code |

`./gen-test-cac.sh [EDIPI]` is the `us-dod` profile under the old file names
(`out/cac.crt`, `out/EDIPI`) for `../cac-live-test/` and `../keycloak-cac-proof/`.

**Not a real PKI.** A stand-in that mimics each profile's shape so the card sign-in
paths (`../mtls-proxy-proof/`, `../keycloak-cac-proof/`) are provable with no card
and no real CA. The real trust anchors are the ones each profile's `trust_anchor`
names. Needs a python3 with PyYAML (`PYTHON=... ./gen-test-card.sh ...` to pick one).

# cac-live-test — the FULL CAC → goat-identity chain, with your real CAC

Proves the whole chain end-to-end with your **physical CAC** in a browser:

```
CAC card  ->  CAC front-end (#2 nginx mTLS+Dex  |  #3 Keycloak x509)  ->  Kratos federation  ->  native goat identity
```

trusting the **real DoD PKI**. You end on a page showing the **native goat identity
minted from your card** — the Kratos UUID anchor + `metadata_public.external_subjects`
carrying your EDIPI + assurance (via the real `../claim-maps/eams-a.jsonnet`).

## Prerequisites

1. Docker + Python 3 + a browser with working CAC middleware.
2. The **DoD PKI CA bundle** (PEM) at `~/dod-bundle.pem` — full acquisition + verify steps
   in [`../../../docs/operations/dod-pki-cac-federation-setup.md`](../../../docs/operations/dod-pki-cac-federation-setup.md)
   Part A (DISA "PKCS#7 for DoD PKI Only" → unzip → `openssl pkcs7 -print_certs` → ~49 certs
   incl. DoD Root CA 3 + DOD ID CA-NN).
3. One-time hosts entry (browser + docker must agree on the names):
   `echo '127.0.0.1 cac-idp kratos-idp' | sudo tee -a /etc/hosts`

## Run

**Path #2 — sovereign mTLS proxy (any CAC, no per-user setup):**
```
./run.sh mtls /path/to/dod-bundle.pem
```
**Path #3 — Keycloak x509 (needs your EDIPI so Keycloak binds the token to a user):**
```
./run.sh keycloak /path/to/dod-bundle.pem <YOUR-EDIPI>
```
(Don't know your EDIPI? `./run.sh mtls …` shows it — path #2 needs no user setup.)

Then, **CAC inserted**, open **http://localhost:9090** → start the flow → choose **CAC** on
the Kratos page → pick your cert + enter your PIN (accept the self-signed *server* cert
warning first). You land on the minted goat identity.

## What's proven vs. what your card proves

- **Path #2 full chain is proven headlessly** with a software test cert:
  `./verify-full.sh` (CAC cert → nginx mTLS → Dex → Kratos → goat identity). Your real
  CAC + the real DoD PKI is what this kit adds on top.
- **Path #3**: both legs are proven independently (CAC→token in `../keycloak-cac-proof/`,
  token→Kratos→goat in `verify-full.sh`); its full chain isn't headless-automated because
  Keycloak's x509 *browser* flow shows an interactive "confirm certificate" page — which
  you click in the real-CAC browser test.
- Revocation (CRL/OCSP) is OFF here for a quick test; the production roles
  (`roles/icam-{mtls-proxy,keycloak-cac}`) turn it on. Server TLS is self-signed
  (localhost) — that's the browser↔service leg, not the CAC trust (validated against the
  real DoD bundle at the front-end). Nothing here is deployed to a goatnet.

## If it needs a nudge with your real card

Real-CAC CN formats vary (usually `LAST.FIRST.MI.EDIPI`); the EDIPI regex is
`CN=[^,]*\.(\d+)`. If your EDIPI isn't extracted, adjust
the nginx `map` (#2) / Keycloak `x509-cert-auth.regular-expression` (#3).

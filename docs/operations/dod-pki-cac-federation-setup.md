# DoD PKI / CAC federation — setup runbook

How to stand up CAC/PIV federation into goat (the three toggle-switch paths), for both a
**local real-CAC test** and a **real goatnet deployment**. Design:
[`../design/external-icam-federation.md`](../design/external-icam-federation.md).

The one artifact every path needs is the **DoD PKI CA bundle** — the trust anchor a CAC
chains to. Part A gets it once; Parts B/C use it. The bundle's source, verification and
the CAs to load are also recorded as the `us-dod` profile of `ops/identity/sovereign-pkis.yaml`
(ADR 1209); another nation's card is another profile in that file, not another runbook.

---

## Part A — get the DoD PKI CA bundle (once)

The bundle is public (DISA DoD Cyber Exchange, no CAC needed to download):

1. Go to **`https://public.cyber.mil/pki-pke/`** → **Tools** (Tools & Configuration Files).
2. Under **Configuration Files**, download
   **"PKI CA Certificate Bundles: PKCS#7 for DoD PKI Only"** (latest version, e.g. v5.14).
   Downloads as `unclass-certificates_pkcs7_DoD.zip`.
   - NOT "ECA" (contractor PKI), "WCF B&I", or "JITC" (test PKI) — those are different PKIs.
3. Unzip and convert the combined PKCS#7 to a flat PEM bundle:
   ```
   unzip unclass-certificates_pkcs7_DoD.zip -d dod-bundle
   openssl pkcs7 -print_certs \
     -in dod-bundle/Certificates_PKCS7_v5_14_DoD/Certificates_PKCS7_v5_14_DoD.pem.p7b \
     -out ~/dod-bundle.pem
   # (if that errors "header too long", the .p7b is DER: use the .der.p7b + `-inform DER`)
   ```
4. Verify it carries DoD Root CA 3 + the DOD ID CA intermediates that issue CACs:
   ```
   grep -c "BEGIN CERTIFICATE" ~/dod-bundle.pem        # ~49
   openssl crl2pkcs7 -nocrl -certfile ~/dod-bundle.pem | openssl pkcs7 -print_certs -noout \
     | grep -iE "DoD Root CA 3|DOD ID CA"
   ```
   Your CAC's signing cert is a `DOD ID CA-NN`; all current ones (59–81 as of v5.14) are in
   the bundle, so any current CAC validates. (The zip also ships `DoD_PKE_CA_chain.pem`, a
   ready PEM — but the converted combined bundle above is the complete set.)

`~/dod-bundle.pem` is the trust anchor for Parts B and C.

---

## Part B — local real-CAC test (prove it with your card, no goatnet)

Kit: [`../../ops/identity/cac-live-test/`](../../ops/identity/cac-live-test/). Proves the
full chain **CAC → CAC front-end → Kratos → native goat identity** in a browser.

1. One-time hosts entry (browser + docker must agree on the names):
   ```
   echo '127.0.0.1 cac-idp kratos-idp' | sudo tee -a /etc/hosts
   ```
2. Run a path (from `ops/identity/cac-live-test/`):
   ```
   ./run.sh mtls     ~/dod-bundle.pem            # path #2 — any CAC, no per-user setup
   ./run.sh keycloak ~/dod-bundle.pem <EDIPI>    # path #3 — needs your EDIPI
   ```
3. **CAC inserted**, open `http://localhost:9090` → start → choose **CAC** → select your
   cert + enter PIN (accept the self-signed *server* cert warning first). You land on the
   native goat identity minted from your card (Kratos UUID anchor + `external_subjects`
   with your EDIPI). `Ctrl-C` stops everything.

Reduced-fidelity here: CRL/OCSP off, self-signed server cert (browser↔service leg only).

---

## Part C — real goatnet deployment (the ansible roles)

The two sovereign paths ship as real default-off roles; one flag enables each.

1. **Stage the bundle** on the mgmt-host, e.g. `/opt/mgmt-host/pki/dod-roots.pem`
   (`~/dod-bundle.pem` from Part A).
2. **Path #2 — `icam-mtls-proxy`** (`roles/icam-mtls-proxy/`), card sign-in from any
   sovereign PKI profile in `ops/identity/sovereign-pkis.yaml` (ADR 1209). Stage the
   trust anchors in the per-profile layout the store also reads, then enable:
   ```
   ops/identity/sovereign-pkis-render.py openbao-dir --out ./pki --bundle us-dod=~/dod-bundle.pem
   scp -r ./pki <mgmt-host>:/opt/mgmt-host/      # -> /opt/mgmt-host/pki/us-dod/ca-bundle.pem
   ```
   ```yaml
   icam_mtls_proxy_enabled: true
   icam_mtls_proxy_pkis: ["us-dod"]              # live profile ids; the role refuses a sketch
   icam_mtls_proxy_pki_dir: /opt/mgmt-host/pki
   icam_mtls_proxy_bind_ip: "{{ mesh_ip_a }}"    # Traefik dials the proxy here
   icam_mtls_proxy_tls: traefik-acme             # the site's Let's Encrypt cert, SNI passthrough (role README)
   # production: load each profile's CRL (revocation in the registry) before relying on it
   ```
   Add a `card-proxy.<site>.<zone>` row to the site's `isitup_services` (mesh DNS + status
   page), regenerate `ops/dns/<site>/mesh-hosts`, then `<site>-refresh.yml --tags
   icam-mtls-proxy`.
   The proxy reads the EDIPI from the SAN UPN (`<EDIPI><persona code>@mil`) and hands
   Dex `X-Remote-User: us-dod:<EDIPI>`.
3. **Path #3 — `icam-keycloak-cac`** (`roles/icam-keycloak-cac/`):
   ```yaml
   icam_keycloak_cac_enabled: true
   icam_keycloak_cac_ca_bundle: /opt/mgmt-host/pki/dod-roots.pem
   icam_keycloak_cac_crl_enabled: true
   ```
   Users are mapped from the cert to Keycloak users (LDAP/directory federation or JIT) —
   deployment-specific.
4. **Wire Kratos to it** — the CAC front-end is an upstream OIDC provider (the toggle
   switch), in the site IdP's `icam_federation` (`group_vars/_examples/icam-federation.example.yml`):
   ```yaml
   icam_federation:
     enabled: true
     providers:
       - id: card-local
         mode: mtls_proxy            # or keycloak_frontend (then id cac-keycloak, claim_map eams-a)
         scheme: sovereign-pki       # documentary; sovereign-pki.jsonnet records dod-piv for us-dod
         authority: doctrinal
         issuer_url: "https://card-proxy.<site>.<zone>"    # = icam_mtls_proxy_issuer_url, no trailing slash
         client_id: kratos                                 # the Dex static client (dex.yaml.j2)
         claim_map: sovereign-pki    # GENERATED from the registry; carries pki + pki_subject (+ edipi for us-dod)
         asserts_min: { ial: ial3, aal: aal3, fal: fal2 }
         boundary_il: 5
   ```
   Client secrets via env (`ICAM_MTLS_KRATOS_CLIENT_SECRET` / `ICAM_KC_KRATOS_CLIENT_SECRET`);
   never inlined. Mesh-only bind; no public plane. For `mtls_proxy` the proxy and Kratos
   share ONE secret under two names (`ICAM_MTLS_KRATOS_CLIENT_SECRET`,
   `ICAM_OIDC_CARD_LOCAL_CLIENT_SECRET`), both `op://` references to one 1Password item;
   deploy Kratos with `ory-deploy.yml --limit <site>_hosts`; the role points the proxy's
   mesh name at the Docker host gateway for Kratos, whose outgoing-HTTP client refuses
   the mesh range.
5. **Path #1 (external enterprise IdP)** needs no DoD bundle — Kratos federates directly to
   an IdP that already did the CAC. See Part D.

---

## Part D — getting #1 (upstream OIDC) online

#1 makes Kratos an **OIDC relying party** to an external IdP that already authenticated
the CAC. No smartcard code — the blocker is **relying-party registration**, not
engineering. Two real targets, in order of ease:

### D.1 — DoD Entra / Azure Gov (RECOMMENDED — Kratos-native)

Kratos's generic OIDC provider authenticates with a **`client_secret`**, which Entra
supports directly — so this is a pure config wire-up, no shim.

1. Get an **app registration** in the DoD Entra (Azure Gov) tenant (your org's Azure-Gov
   admin).
2. Give them Kratos's redirect URI:
   `https://auth.<site>.<zone>/self-service/methods/oidc/callback/entra`. Get back:
   `client_id`, a `client_secret`, and the tenant issuer
   `https://login.microsoftonline.us/<tenant-id>/v2.0`. Configure conditional access to
   require phishing-resistant MFA / CAC (CBA).
3. Wire the provider (site IdP `icam_federation`), reusing the built `entra` claim-map:
   ```yaml
   icam_federation:
     enabled: true
     providers:
       - id: entra
         mode: upstream_oidc
         scheme: entra
         authority: industry
         issuer_url: "https://login.microsoftonline.us/<tenant-id>/v2.0"
         client_id: "<app-registration-client-id>"     # secret via ICAM_OIDC_ENTRA_CLIENT_SECRET env
         scopes: ["openid", "email", "profile"]
         claim_map: entra
         asserts_min: { ial: ial2, aal: aal2, fal: fal2 }
         boundary_il: 5
   ```
   Reachability is easy — `login.microsoftonline.us` is internet-reachable, so Kratos
   federates without enterprise-network plumbing. The only gate is the app registration.

### D.2 — login.gov (civil / partner IAL2)

login.gov requires **`private_key_jwt`** client authentication (a signed-JWT keypair,
**not** a `client_secret`). Kratos's generic OIDC provider authenticates with a
`client_secret`, so **login.gov cannot be wired to Kratos directly** — it needs either
Kratos `private_key_jwt` support (verify per Kratos version) or a small OIDC bridge (the
same Dex pattern #2 uses). Steps once bridged:

1. Register a test SP at the **login.gov identity sandbox** (`dashboard.int.identitysandbox.gov`)
   — openly registerable, no DoD sponsorship. Generate the RP keypair
   (`openssl req -newkey rsa:2048 -nodes -keyout lg.key -x509 -days 365 -out lg.crt`),
   upload `lg.crt`, set the redirect URI, choose the IAL2 / `hspd12` (PIV/CAC) ACRs.
2. The dashboard gives you the `issuer` + `client_id`. Wire via the bridge (or native
   private_key_jwt), reusing the built `login-gov` claim-map (`scheme: login-gov`,
   `boundary_il: 2`, `ial2` gated).

**Provider stanzas** for all of the above are pre-staged (commented) in
`ops/ansible/group_vars/_examples/icam-federation.example.yml`.

---

## EDIPI extraction

- The EDIPI lives in the CAC cert's **Subject CN** (`LAST.FIRST.MI.EDIPI`) and the
  **SAN `otherName` msUPN** (`EDIPI@mil`, OID 1.3.6.1.4.1.311.20.2.3).
- The roles extract from the CN (regex `CN=[^,]*\.(\d+)` / Keycloak SubjectDN-regex). The
  SAN otherName is the authoritative source; parsing it (nginx njs) is the production
  hardening step. If a card's CN shape differs, adjust the regex.

## Status

- **#2 (mtls-proxy) + #3 (keycloak) — LIVE-FIRED with a real CAC + real DoD PKI**:
  a physical CAC validated against DoD Root CA 3 +
  the DOD ID CA-NN intermediates minted a native goat identity carrying the real EDIPI.
  Both ship as **real, default-off roles behind one flag each** — proven and in the
  pocket, activated on demand per Part C.
- **#1 (upstream OIDC)** — config-built (`upstream_oidc` mode + `entra`/`login-gov`
  claim-maps); pending relying-party registration at an upstream IdP such as DoD
  Entra-Gov (app registration, internet-reachable). Wire-up is one provider stanza once
  RP credentials exist.
- **Production hardening (when a role is activated):** enable revocation (CRL/OCSP), parse
  the authoritative EDIPI from the SAN otherName msUPN (njs / Keycloak SAN source), swap
  the self-signed server cert for the mesh CA cert. Not yet deployed to a live goatnet.

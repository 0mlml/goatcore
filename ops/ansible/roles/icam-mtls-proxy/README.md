# icam-mtls-proxy — card sign-in from any sovereign PKI

**Status: built, default-off.** One flag
(`icam_mtls_proxy_enabled: true`) stands the path up for the PKI profiles a goatnet
names. Decision: ADR 1209 (the `mtls_proxy` mode of ADR 1069, generalised). Design:
`docs/design/external-icam-federation.md`.

## What it does

The card path that works with **no reachable enterprise IdP**. **nginx** terminates
x509 client-certificate mTLS against the CA bundles of the enabled PKI profiles, works
out which profile issued the card (issuer DN), reads the identifier that profile says
the card carries (SAN UPN, SAN e-mail, a SAN otherName, the subject CN or the serial;
`pki.js` parses the SAN), and hands **Dex**'s `authproxy` connector:

| Header | Value |
|---|---|
| `X-Remote-User` | `<pki>:<identifier>`, for example `us-dod:1234567890` |
| `X-Remote-Pki` | `<pki>` |
| `X-Remote-Email` | the card's first SAN e-mail, or empty |

Dex issues an OIDC token with `preferred_username = <pki>:<identifier>`; Kratos
federates to Dex (`icam_federation` provider in `mtls_proxy` mode) and
`ops/identity/claim-maps/sovereign-pki.jsonnet` records the binding as
`metadata_public.external_subjects[{pki, pki_subject, ...}]` on a native goat
identity. For `us-dod` the same entry also carries `edipi`, so readers of the old
name keep working. Nothing in this path sets a person's nationality.

Only `/auth` and `/callback` require a card; the RP back-channel (discovery, token,
keys) does not. A card whose issuer matches no enabled profile, or that yields no
identifier under its profile's rule, is refused with 403 before Dex sees it.

## The registry is the only place a PKI is described

`ops/identity/sovereign-pkis.yaml` holds every profile: issuer match, identity rule,
subject CN format, trust anchor, revocation, a synthetic example card. The role reads
it at deploy time through `ops/ansible/filter_plugins/sovereign_pki.py` and renders
one nginx `map` chain per enabled profile. There is no per-PKI variable in this role.

## Enable

```yaml
# group_vars/<site>_hosts/icam-mtls-proxy.yml
icam_mtls_proxy_enabled: true
icam_mtls_proxy_pkis: ["us-dod"]              # live profile ids from the registry
icam_mtls_proxy_pki_dir: /opt/mgmt-host/pki   # <dir>/<id>/ca-bundle.pem per profile
icam_mtls_proxy_bind_ip: "{{ mesh_ip_a }}"    # the mgmt host's mesh address
icam_mtls_proxy_tls: traefik-acme             # the site certificate (below)
```

Register the hostname in the site's `isitup_services` (it is how mesh DNS and the status
page learn a surface exists) and regenerate `ops/dns/<site>/mesh-hosts` with
`ops/dns/generate-mesh-hosts.py`. The role runs from `site-refresh.yml` (so
`<site>-refresh.yml`), tag `icam-mtls-proxy`; it is a no-op where the flag is off.

Stage the trust anchors on the mgmt-host in the same layout the store uses:

```
ops/identity/sovereign-pkis-render.py openbao-dir --out ./pki --bundle us-dod=~/dod-bundle.pem
scp -r ./pki <mgmt-host>:/opt/mgmt-host/
```

Each profile's `trust_anchor` in the registry says where its PEM comes from and how
to verify the download. The role refuses to deploy when an enabled profile's bundle
is missing, when a named id is not a `live` profile, or when the list is empty.

Then the site IdP's `icam_federation` provider (see
`group_vars/_examples/icam-federation.example.yml`, the `mtls_proxy` block) points
at `icam_mtls_proxy_issuer_url` with `client_id: kratos` (the Dex static client) and
`claim_map: sovereign-pki`; deploy it with `ory-deploy.yml --limit <site>_hosts`. One
secret is shared by the two sides and read from the deploy environment under two
names: `ICAM_MTLS_KRATOS_CLIENT_SECRET` (this role, into the proxy's `.env`) and
`ICAM_OIDC_CARD_LOCAL_CLIENT_SECRET` (the ory role, into Kratos's `.env`). Both are
`op://` references to ONE 1Password item (ADR 1186); the role refuses to deploy
without the value.

## Server certificate

The proxy serves the Let's Encrypt certificate Traefik already holds for the site (the
mesh-only wildcard covers `card-proxy.<site>.<zone>`), copied out of `acme.json` by
`acme-cert-sync.py` at deploy and daily by a systemd timer, and Traefik passes this
hostname's TLS through on `:443` untouched (a TCP router matched on SNI) so nginx still
sees the client certificate. Chosen over a second ACME client or a mesh-CA leaf because
it adds no new issuer, no DNS-01 token on the proxy, and no port in the issuer URL;
`icam_mtls_proxy_tls: self-signed` (the default) keeps the role portable for proofs.

Kratos reaches the proxy through the Docker host gateway, not the mesh address: its
outgoing-HTTP client refuses 100.64.0.0/10 in every configuration (the range is not
private to it, and its exception list only covers the RFC 1918 ranges), so the `ory`
role adds `card-proxy.<site>.<zone>:host-gateway` to the kratos service's
`extra_hosts`; Traefik answers there with the same certificate.

## Binding to a person

A card signs in as the goat identity Kratos links to `(issuer, client_id,
"<pki>:<identifier>")`. The first sign-in mints a native identity with no groups
(rule 2 of the claim maps); an admin assigns role and affiliation afterwards, or
links the external subject to an existing person. Nothing is inferred from the card
beyond the identifier.

Linking a card to an EXISTING person is Kratos's own account-linking, no goat tool:
the person signs in with their password at `auth.<site>.<zone>`, opens Settings, and
under "Link" chooses "Certificate (smart card)"; the card is presented once and
Kratos attaches the OIDC credential to that identity. From then on the card signs in
as that person, with that person's groups. A card presented BEFORE linking mints the
groupless identity instead; an admin can then delete it from the id console and the
person links from Settings.

## Proven

- Headless, per profile: `ops/identity/mtls-proxy-proof/run.sh [<pki id>]` — a
  synthetic card from the test PKI (`ops/identity/cac-test-pki/gen-test-card.sh
  <pki id>`) → nginx mTLS + identifier extraction → Dex → OIDC token carrying
  `<pki>:<identifier>`.

## Deployment notes

- Revocation: each profile's `revocation` names its CRL/OCSP. Enable nginx `ssl_crl`
  from a refreshed CRL file.
- `deploy-history.yml` tracks the nginx image pin (ADR 1060). A site adds its
  `{host, container_name, compose_path}` entry when it flips the flag.
- Verify a deploy: `curl https://card-proxy.<site>.<zone>/.well-known/openid-configuration`
  from a mesh peer returns Dex's discovery document with `issuer` equal to
  `icam_mtls_proxy_issuer_url`; `docker ps` on the host shows `icam-mtls-proxy-nginx-1`
  and `icam-mtls-proxy-dex-1`; the sign-in page at `auth.<site>.<zone>` offers
  "Certificate (smart card)".

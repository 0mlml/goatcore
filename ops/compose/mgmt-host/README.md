# mgmt-host compose

Docker Compose for the per-site Netbird management plane (Spock PG +
federated signal server + Netbird management + Traefik + dnsmasq + rathole).
Runs on any `mgmt-host`.

Design context: `docs/design/ha-multi-site.md`
§4 (per-site stack), §13 (phases).

## Status

Scaffold (2026-04-19). This compose file currently brings up just
`postgres-a` and `signal-a` — enough to validate the Spock image boots with
the baked config and to have a signal server listening. Netbird management,
Traefik, dnsmasq, and rathole land in separate PRs as Phase 1 proceeds.
Phase 2 adds a second instance of each via a `docker-compose.phase2.yml`
override so a single host exercises multi-master Spock + signal federation.

## Layout

```
ops/compose/mgmt-host/
├── docker-compose.yml   base stack (single instance of each service)
├── .env.example         template — Ansible renders the real .env from env vars
└── README.md
```

## Bring-up (on a provisioned mgmt-host)

```
cd /opt/mgmt-host
cp .env.example .env      # Ansible does this from env vars in production
# edit .env with the site-specific values
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f signal-a
```

Smoke-test the Spock image is running correctly:

```
docker compose exec postgres-a psql -U postgres -tAXc "SHOW wal_level;"
# expected: logical

docker compose exec postgres-a psql -U postgres -d netbird -tAXc \
    "SELECT extname FROM pg_extension WHERE extname='spock';"
# expected: spock
```

## Phase 2 (two instances on one pilot)

Once the single-instance stack is proven, Phase 2 adds a second instance of
each service on the same host via a compose override and sets up the Spock
subscription between PG-A and PG-B.

```
docker compose -f docker-compose.yml -f docker-compose.phase2.yml up -d
```

(That override file is TBD — tracked in
`docs/design/ha-multi-site.md` §13
Phase 2.)

## Notes

- Ports bind to `127.0.0.1` only. External exposure happens via Traefik
  (TLS, Let's Encrypt DNS-01 via Cloudflare) in a later PR.
- All container state lives under `/opt/docker-data/` (NVMe on production
  Orins — mount provisioned by Ansible / cloud-init per
  [`../../../docs/design/orin-os-decision.md`](../../../docs/design/orin-os-decision.md)).
  Docker's data root is pointed at that path; volumes land under it.
- Secrets (PG passwords, Netbird setup key, replicator credentials) are
  NOT in this compose file or .env beyond placeholders. Ansible renders
  them into `/opt/secrets/` at deploy time; LUKS (key custody via OP-TEE
  on Orin / TPM on x86) protects that mount.

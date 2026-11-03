# syntax=docker/dockerfile:1.7
#
# Spock-enabled PostgreSQL 16 for the goat multi-site mesh.
#
# Upstream base: ghcr.io/pgedge/pgedge-postgres:16-spock5-minimal
#   - multi-arch (linux/amd64 + linux/arm64)
#   - ships PG 16.15 + Spock 5.0.11
#   - PostgreSQL License, free for self-hosted use
#
# PINNED BY DIGEST, and it was not before. `16-spock5-minimal` is a FLOATING tag
# that pgEdge re-points on every release — it moved to 16.14/spock-5.0.11 on
# and to 16.15/spock-5.0.11 . A floating base means the
# image, and every test of it, changes with no commit here: this workflow was
# green and red with nothing in between touching
# ops/postgres/spock or the workflow. Same class as a vulnerability database
# turning a green check red overnight — the fix is to make the input a fact we
# committed. Digest pins are the F-245 discipline used for every other image in
# this repo; the base had escaped it.
#
# Bump procedure: list the published versions and take the one currently tagged
# 16-spock5-minimal, then update BOTH the digest and the version comment above.
#   gh api --paginate /orgs/pgedge/packages/container/pgedge-postgres/versions \
#     -q '.[]|select((.metadata.container.tags//[])|join(",")|test("16.*spock5.*minimal"))
#         |"\(.updated_at)\t\(.name)\t\((.metadata.container.tags//[])|join(","))"'
#
# We overlay:
#   - postgresql.conf tuned for Spock logical replication
#   - pg_hba.conf template allowing mesh-overlay IPs (site-specific entries
#     are rendered in by Ansible at deploy time; the image ships a safe default)
#   - SQL init scripts that create the Netbird databases and load the Spock
#     extension in each replicated DB. Role creation + Spock subscriptions
#     are driven by Ansible at runtime, not baked into the image.

FROM ghcr.io/pgedge/pgedge-postgres:16-spock5-minimal@sha256:676baa1c1b2234eced313d3bb9a4cb768bf217d90885b03eaf3d27fc283abac6

# Baked config lives at /etc/postgresql/. The upstream entrypoint doesn't use
# this path by default — docker-compose passes `-c config_file=...` so this
# file is picked up instead of the one created by initdb inside PGDATA.
COPY conf/postgresql.conf /etc/postgresql/postgresql.conf
COPY conf/pg_hba.conf     /etc/postgresql/pg_hba.conf

# Runs once on first boot via the upstream postgres entrypoint.
COPY init/ /docker-entrypoint-initdb.d/

# --- pgBackRest ------------------------------------------------------------
# Baked in so the binary survives container recreates (e.g. image pin bumps)
# and so postgres-a does not need PGDG network reachability at runtime — a
# hard requirement for DDIL/disconnected deployments (Orin sites).
#
# pgBackRest is NOT in EPEL 9; PGDG is the canonical source. The role at
# ops/ansible/roles/pgbackrest still ships the dnf-install tasks as a
# safety-net — they are gated on `command -v pgbackrest` and become no-ops
# when the binary is already present.
USER root
# Direct RPM install from PGDG — pinned version, HTTPS-authenticated transport,
# no repo config needed. Dependencies (libyaml, libxml2 etc.) resolve from the
# already-enabled Rocky baseos/appstream repos which have their own GPG chain.
# Multi-arch: TARGETARCH is set by buildx (amd64/arm64) — map to dnf arch name.
#
# Bump procedure: list https://download.postgresql.org/pub/repos/yum/common/redhat/rhel-9-x86_64/
# and pick the latest `pgbackrest-X.Y.Z-1PGDG.rhel9.<minor>.<arch>.rpm` entry.
# Two moving parts: PGBACKREST_VERSION (semver) AND PGBACKREST_RHEL_SUFFIX
# (PGDG's downstream rhel-minor tag — they used to publish under bare
# `rhel9` and as of mid-2026 switched to `rhel9.8` once Rocky 9.8 landed
# upstream; older RPMs aged off the active stream, so a bare `rhel9` URL
# now 404s for every version. If a future PGDG bump moves to `rhel9.10`
# or similar, change the suffix here).
ARG TARGETARCH
ARG PGBACKREST_VERSION=2.58.0
ARG PGBACKREST_RHEL_SUFFIX=rhel9.8
RUN set -eu \
 && case "${TARGETARCH:-amd64}" in \
      amd64) RPM_ARCH=x86_64 ;; \
      arm64) RPM_ARCH=aarch64 ;; \
      *) echo "Unsupported TARGETARCH: $TARGETARCH"; exit 1 ;; \
    esac \
 && dnf install -y "https://download.postgresql.org/pub/repos/yum/common/redhat/rhel-9-${RPM_ARCH}/pgbackrest-${PGBACKREST_VERSION}-1PGDG.${PGBACKREST_RHEL_SUFFIX}.${RPM_ARCH}.rpm" \
 && mkdir -p /etc/pgbackrest /var/log/pgbackrest /var/spool/pgbackrest \
 && chown -R postgres:postgres /var/log/pgbackrest /var/spool/pgbackrest /etc/pgbackrest \
 && dnf clean all \
 && rm -rf /var/cache/dnf

# --- Postgres contrib extensions (Block 21C — added) -----------
# Ory Kratos's migrations require pg_trgm + btree_gin (full-text indexes on
# identity_credential_identifiers). The pgedge-postgres-minimal base ships
# with PG16 binaries but does NOT bundle the contrib extensions. Adding the
# pgEdge-built contrib RPM here ensures Kratos + Hydra can run on the same
# postgres-spock cluster as netbird-mgmt — preserving the architectural
# pattern (Spock multi-master, pgBackRest paired-stanza, active-active over
# Router-A/B) for identity state without forking a separate Postgres image.
#
# pgEdge ships postgresql16-contrib via their own yum repo when the base
# is installed; if not present in the minimal image we fall back to the
# stock pgEdge community repo. Resolution order:
#   1. dnf install postgresql16-contrib (already in pgEdge package set)
#   2. fall back to PGDG community RPM if pgEdge package missing
ARG POSTGRES_CONTRIB_VERSION=16.15   # keep in sync with the base minor (see header); PGDG fallback path only
RUN set -eu \
 && case "${TARGETARCH:-amd64}" in \
      amd64) RPM_ARCH=x86_64 ;; \
      arm64) RPM_ARCH=aarch64 ;; \
    esac \
 && (dnf install -y postgresql16-contrib 2>/dev/null \
     || dnf install -y "https://download.postgresql.org/pub/repos/yum/common/redhat/rhel-9-${RPM_ARCH}/postgresql16-contrib-${POSTGRES_CONTRIB_VERSION}-2PGDG.rhel9.${RPM_ARCH}.rpm") \
 && dnf clean all \
 && rm -rf /var/cache/dnf \
 && ls /usr/pgsql-16/share/extension/ | grep -E "^(pg_trgm|btree_gin)\.control$" \
   || (echo "FATAL: contrib extensions not present after install" && exit 1)

# --- F-044 — scrub baked PGDATA ------------------------------------------
# The pgedge-postgres base image ships /var/lib/pgsql/16/data already
# populated from build-time setup. Docker named-volume first-mount
# auto-copies image contents into the new volume; the upstream postgres
# entrypoint then sees a non-empty PGDATA, prints "Database directory
# appears to contain a database; Skipping initialization", skips initdb
# entirely, and POSTGRES_PASSWORD env is silently ignored. Every consumer
# of postgres-spock then crash-loops in management-a / kratos / hydra
# with `password authentication failed for user "postgres"` until the
# operator manually ALTER USERs the password.
#
# Scrubbing the data dir at build time forces upstream initdb to run on
# first volume mount, which applies POSTGRES_PASSWORD properly and
# executes /docker-entrypoint-initdb.d/ scripts (our init/ overlay).
#
# See docs/findings/F-044-pgedge-postgres-baked-data-dir.md for the full
# mechanism + reproducer + the F-006 distinction (post-init restart timing
# vs initdb being skipped entirely).
RUN set -eu \
 && rm -rf /var/lib/pgsql/16/data \
 && mkdir -p /var/lib/pgsql/16/data \
 && chown postgres:postgres /var/lib/pgsql/16/data \
 && chmod 0700 /var/lib/pgsql/16/data

USER postgres

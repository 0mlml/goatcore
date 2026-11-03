# syntax=docker/dockerfile:1.7
# SPDX-License-Identifier: Apache-2.0
#
# goat-fed-republisher container image.
#
# Same two-stage shape as ops/zenoh-trial-peers/audit-subscriber/Dockerfile:
#   1. rust-builder: cargo build --release --locked
#   2. Runtime: Chainguard glibc-dynamic per ADR 0700.
#
# No embedded identity material. FOUR files are bind-mounted at deploy time —
# a leaf cert + key for the LOCAL session and another pair for the PEERING EDGE
# session, plus each side's CA root. They are deliberately different identities:
# the local one is an ordinary fabric client, the edge one is what the edge's
# access rules grant `from/<sovereign>/**` and nothing wider.
#
# The composed agreement is mounted read-only too. It is the configuration.

FROM cgr.dev/chainguard/rust:latest-dev AS rust-builder
USER root
WORKDIR /build
COPY Cargo.toml Cargo.lock* ./
RUN mkdir -p src && echo "fn main() {}" > src/main.rs && cargo fetch && rm -rf src
COPY src/ ./src/
RUN cargo build --release --locked

FROM cgr.dev/chainguard/glibc-dynamic:latest
WORKDIR /
COPY --from=rust-builder /build/target/release/goat-fed-republisher /usr/local/bin/goat-fed-republisher
EXPOSE 9310
ENTRYPOINT ["/usr/local/bin/goat-fed-republisher"]

# syntax=docker/dockerfile:1.7
# SPDX-License-Identifier: Apache-2.0
#
# onboard-runner container image — the pull-model onboarding auto-executor
# (docs/design/operator-onboard-intake.md §"Fast-follow"), run as a CONTAINER
# with its OWN scoped portal identity rather than a host process reusing the
# operator's login (the earlier shape).
#
# Two-stage build:
#   1. build   — static Go binary (CGO disabled), no runtime deps of its own.
#   2. runtime — Chainguard wolfi-base (glibc). The runner SHELLS OUT to `goat
#      admin onboard --native`, so the runtime carries:
#        - glibc         — the site `goat` binary is glibc-linked
#                          (aarch64-unknown-linux-gnu); wolfi-base's rolling
#                          glibc is >= the Ubuntu-24.04 build target (verified
#                          on the ZAGROS Thor: wolfi-base runs the host goat).
#        - sh + curl     — the native onboard's Ory leg runs `sh -c 'curl …'`
#                          in-container against the sibling Kratos admin API
#                          (GOAT_ONBOARD_KRATOS_LOCAL=1).
#        - python 3.13   — lifecycle-sweep.py is the single hold release engine.
#      The `goat` binary itself is NOT baked in — it is bind-mounted from the
#      host at deploy time (/usr/local/bin/goat, role-managed, one version
#      source), so the container tracks the host's goat with no second copy to
#      drift. The image carries NO identity material; the minimal goat profile
#      (portal endpoint + the runner's own portal token) is materialized at
#      startup by docker-entrypoint.sh from the deploy env-file.

FROM golang:1.26-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb AS build
WORKDIR /src
# Cache module downloads first for layer reuse.
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /onboard-runner .

FROM cgr.dev/chainguard/wolfi-base:latest
# curl (+ ca-certificates) for the native onboard's local Kratos-admin leg.
RUN apk add --no-cache curl ca-certificates python-3.13
# A writable home for the materialized goat profile — non-root uid, no passwd
# entry needed (goat reads $HOME, not /etc/passwd).
RUN mkdir -p /home/runner && chown 65532:65532 /home/runner
COPY --from=build /onboard-runner /usr/local/bin/onboard-runner
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 0755 /usr/local/bin/docker-entrypoint.sh
USER 65532:65532
ENV HOME=/home/runner
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

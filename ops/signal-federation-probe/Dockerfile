# Build + run image for signal-federation-probe.
# Multi-arch via cgr.dev/chainguard/go (FIPS-friendly base) + chainguard/static
# runtime image (no shell, no package manager — minimal attack surface).
#
# The registry host is cgr.DEV. This file said cgr.io previously which
# does not resolve at all — so the image could never have been built, which is
# consistent with there having been no build workflow for it either.
# Every other Dockerfile in this repo already used cgr.dev; these were the only
# three references that did not.
FROM cgr.dev/chainguard/go:latest AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /signal-federation-probe .

FROM cgr.dev/chainguard/static:latest
COPY --from=build /signal-federation-probe /signal-federation-probe
EXPOSE 9091
ENTRYPOINT ["/signal-federation-probe"]

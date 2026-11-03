# Multi-stage build for the mgmt-host-bootstrap CLI.
# Per ADR 0410. Chainguard-first per Block 13C supply chain.
#
# Multi-arch via docker buildx: TARGETARCH/TARGETOS are populated by the
# buildx driver per `--platform=linux/amd64` or `--platform=linux/arm64`.
# Default to amd64 when neither is set (plain `docker build` on host arch).

FROM --platform=$BUILDPLATFORM cgr.dev/chainguard/go:latest AS build
ARG TARGETARCH
ARG TARGETOS=linux
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" \
    -o /out/mgmt-host-bootstrap .

FROM cgr.dev/chainguard/static:latest
COPY --from=build /out/mgmt-host-bootstrap /usr/local/bin/mgmt-host-bootstrap
ENTRYPOINT ["/usr/local/bin/mgmt-host-bootstrap"]

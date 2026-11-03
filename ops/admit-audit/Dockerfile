# admit-audit — read-only sidecar for the mgmt-host stack.
#
# Build arg `GOPROXY` lets CI cache module fetches; defaults to the
# public proxy.

FROM golang:1.26.10-alpine@sha256:a2012b4a1d1f9f7e9f44e0d39b805a901e856b1dd80b4e6ee4338db547050308 AS builder
ARG GOPROXY=https://proxy.golang.org,direct
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/admit-audit .

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/admit-audit /admit-audit
USER nonroot:nonroot
EXPOSE 9124
ENTRYPOINT ["/admit-audit"]

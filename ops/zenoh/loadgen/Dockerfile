FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /loadgen .

FROM alpine:3.19
COPY --from=builder /loadgen /loadgen
ENTRYPOINT ["/loadgen"]

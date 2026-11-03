# onboard-runner — build the daemon for host deploy.
#
# The ansible role installs bin/linux-<arch>/onboard-runner. The site Thor is an
# AGX Orin (linux/arm64); `make build` cross-compiles both arches.

BIN := onboard-runner
OUT := bin

.PHONY: build build-arm64 build-amd64 test clean

build: build-arm64 build-amd64

build-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o $(OUT)/linux-arm64/$(BIN) .

build-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o $(OUT)/linux-amd64/$(BIN) .

test:
	go test ./...

clean:
	rm -rf $(OUT)

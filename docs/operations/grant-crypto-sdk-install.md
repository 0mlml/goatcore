# grant-crypto SDK — mesh install runbook

| Field | Value |
|---|---|
| Status | Authored 2026-08-02 with the SDK mesh-distribution surface (role + workflow). Deploy operator-fired. |
| Audience | On-mesh developers building against the grant-crypto SDK ([ADR 1106](../adr/1106-grant-crypto-sdk-and-endpoint-forward-secrecy.md)); operators bumping the served version. |
| Surface | `get.<site>.<zone>/sdk/` — a subdir of the existing goat-cli download surface ([goat-cli-mesh-download.md](../design/goat-cli-mesh-download.md)), same host/cert/DNS/mesh-only gate. Live rollout target: `get.zagros.netbird.zagros.example/sdk/`. |
| Related | [`grant-crypto-sdk.md`](../design/grant-crypto-sdk.md) (frozen API), [`ops/grant-crypto/sdk/README.md`](../../ops/grant-crypto/sdk/README.md), [`goat-cli-mesh-download.md`](../design/goat-cli-mesh-download.md) §"SDK assets". |

## What's served

A mesh peer enrolls NetBird from its bundle first (so it's on the mesh), then
fetches from `https://get.<site>.<zone>/sdk/`. The surface serves exactly the
pinned `grant-crypto-sdk-v*` release:

- **Prebuilt C ABI libraries** — `grant-crypto-capi-<os>-<arch>.tar.gz` for
  linux/darwin × amd64/arm64. Each holds the shared library (`.so`/`.dylib`), the
  static library (`libgrant_crypto_capi.a`), the C header (`include/grant_crypto.h`),
  and an `INSTALL.txt`. This is the FFI seam the Go/Python/TS bindings link.
- **Source tarball** — `grant-crypto-sdk-<ver>-src.tar.gz`: the whole SDK
  workspace (Rust core + C ABI + bindings) plus the shared conformance vectors
  and the wire spec, re-rooted to a top-level `grant-crypto/` so it builds
  standalone. Build the library yourself on an unlisted platform, or run the
  suites.
- **Checksums** — a sibling `<asset>.sha256` for every artifact plus the
  aggregate `SHA256SUMS`. Always verify before use.

The `/sdk/index.html` landing page renders the artifact table and the
per-language quickstarts; `/sdk/version.json` (`{version, tag, install_url}`) is
the machine-readable "what version are we handing out" marker.

- **Documentation site** — the full grant-crypto SDK docs (concepts, quickstart
  in all four languages, API + C-ABI reference, wire format, security,
  troubleshooting) at `https://get.<site>.<zone>/sdk/docs/`, linked from the
  `/sdk/` landing page. The SDK **ships its own documentation**: the site is
  hand-authored static HTML at `ops/grant-crypto/sdk/docs/site/`, so it rides
  inside the source tarball at `grant-crypto/sdk/docs/site/`, and the release
  attaches a standalone `grant-crypto-sdk-docs-<ver>.tar.gz` that the goat-get
  role unpacks into `www/sdk/docs/`. Same bytes, pinned to the served release.

## Fetch + verify + build

Prebuilt library (fastest path — you just want to link a binding):

```bash
base=https://get.zagros.netbird.zagros.example/sdk
lib=grant-crypto-capi-linux-amd64.tar.gz     # pick your platform
curl -fsSL "$base/$lib"        -o "$lib"
curl -fsSL "$base/$lib.sha256" -o "$lib.sha256"
sha256sum -c "$lib.sha256"     # macOS: shasum -a 256 -c "$lib.sha256"
tar xzf "$lib"                 # -> .so/.dylib, libgrant_crypto_capi.a, include/, INSTALL.txt
```

From source (build the library, or run the tests):

```bash
base=https://get.zagros.netbird.zagros.example/sdk
src=grant-crypto-sdk-0.1.0-src.tar.gz        # name carries the version
curl -fsSL "$base/$src"        -o "$src"
curl -fsSL "$base/$src.sha256" -o "$src.sha256"
sha256sum -c "$src.sha256"
tar xzf "$src"
cd grant-crypto/sdk
cargo build -p grant-crypto-capi   # shared + static library under target/
cargo test                          # optional: conformance + roundtrip suites
```

## Per-language setup

Each binding's own `README.md` (shipped in the source tarball at
`grant-crypto/sdk/bindings/<lang>/`) is the authoritative, CI-tested quickstart.
The essentials:

- **Rust** (the core directly) — depend on `grant-crypto/sdk/core` (`cargo add
  --path core`). Toolchain floor Rust ≥ 1.77.
- **Go** (≥ 1.22) — module
  `github.com/usareur-af/goatcore/ops/grant-crypto/sdk/bindings/go`, cgo over
  the static library, zero Go deps. `go mod edit -replace` to the unpacked
  binding and point the cgo `LDFLAGS` search path at the `libgrant_crypto_capi.a`
  from the prebuilt tarball (or your `cargo build`). `CGO_ENABLED=1` (the default).
- **Python** (≥ 3.8) — `pip install ./grant-crypto/sdk/bindings/python` (stdlib
  `ctypes`, zero pip deps; import name `grant_crypto`). Set
  `GRANT_CRYPTO_CAPI_LIB` to the directory (or file) of the shared library.
- **TypeScript/Node** (≥ 20) — in `grant-crypto/sdk/bindings/ts`, `npm install &&
  npm run build`; koffi loads the shared library at runtime from
  `GRANT_CRYPTO_CAPI_LIB`. Consume from your project via the built package or
  `npm link`.

The core + C ABI ship in lockstep from one tag; `gc_abi_version()` is a monotone
integer with **no cross-version ABI promise** — pull the library and the source
from the same release.

## Operator: bump the served version

The surface pins one release tag; serving a newer SDK is one line plus a redeploy
(easy-rollback ledger per [ADR 0859](../adr/0859-easy-rollback-engineering-principle.md)):

1. Cut the release: push a `grant-crypto-sdk-v<semver>` tag (or dispatch
   `.github/workflows/grant-crypto-sdk-release.yml`). It builds the source tarball
   (and proves it self-sufficient by `cargo test`-ing from inside it), the four
   C ABI library tarballs, the checksums, and attaches them to the GitHub Release.
2. Bump `goat_get_sdk_release_tag` in `ops/ansible/roles/goat-get/defaults/main.yml`
   (or pass `-e goat_get_sdk_release_tag=...`). The asset names and
   `version.json` derive from this one var — nothing else to change.
3. Re-run the deploy (SDK enabled per-site via `goat_get_sdk_enabled`):

   ```bash
   cd ops/ansible
   OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES \
     GOAT_CLI_RELEASE_TOKEN=$GOAT_CLI_RELEASE_TOKEN \
     ../scripts/ansible-preflight.sh -i inventory/hosts.ini -i inventory/mgmt-hosts-mesh.ini \
     -l <mgmt-host> -e target=<site>_hosts goat-get-deploy.yml
   ```

   The role re-fetches the pinned assets into `www/sdk/`, re-renders the landing
   page + `version.json`, and (fail-closed) refuses if any listed asset is absent
   from the release or the token lacks `contents:read`.
4. Live-probe `https://get.<site>.<zone>/sdk/version.json` and update the host's
   `served_sdk_release_tag` in `ops/ansible/roles/goat-get/deploy-history.yml`.

Enabling the surface on a **new** site: the SDK is a subdir of the existing
`get.<site>` surface, so no new DNS record or LE SAN is needed — set
`goat_get_sdk_enabled: true` in that site's group_vars and re-run the deploy.

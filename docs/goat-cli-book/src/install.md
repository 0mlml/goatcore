# Install & verify

goat is a single self-contained binary — no runtime, no dependencies. It's served mesh-internally from `get.<your-goatnet>` (you're already on the mesh if you can read this), and the version here always matches the binary that surface serves.

## Quick install

**macOS / Linux**

```bash
OS=$(uname -s | tr '[:upper:]' '[:lower:]')              # darwin | linux
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')  # amd64 | arm64
curl -fsSL "https://get.<your-goatnet>/goat-${OS}-${ARCH}" -o ~/.local/bin/goat
chmod +x ~/.local/bin/goat
goat --version
```

Make sure `~/.local/bin` is on your `PATH`.

**Windows (PowerShell)**

```powershell
iwr "https://get.<your-goatnet>/goat-windows-amd64.exe" -OutFile "$env:USERPROFILE\goat.exe"
& "$env:USERPROFILE\goat.exe" --version
```

> Every subcommand works on Windows except `goat client` (the local tunnel daemon), which is Linux/macOS-only.

## Verify the download (recommended)

The download surface publishes a per-binary `*.sha256` and an aggregate `SHA256SUMS`. For a high-assurance install, verify before you run:

```bash
BASE="https://get.<your-goatnet>"
curl -fsSL "$BASE/SHA256SUMS" -o SHA256SUMS
curl -fsSL "$BASE/goat-${OS}-${ARCH}" -o goat
# macOS: shasum -a 256 -c   ·   Linux: sha256sum -c
sha256sum -c SHA256SUMS --ignore-missing
install -m 0755 goat ~/.local/bin/goat
```

## Platforms

| OS | Architectures |
|---|---|
| Linux | `amd64`, `arm64` |
| macOS | `amd64` (Intel), `arm64` (Apple Silicon) |
| Windows | `amd64` |

## Updating

Re-run the install one-liner — it always fetches the version this site is serving. To confirm what you have, `goat --version`.

// `goat client <subcmd>` — wrapper around the goat-clientd daemon's
// local JSON-RPC IPC.
//
// Phase F.3 wrapped the foundation four methods (getStatus / connect /
// disconnect / importBundle). Phase F.5 (netbird-CLI parity) widens the
// surface against the methods the RELEASED goat-clientd v0.1.2 already
// exposes — confirmed from goat-maintainer/goat-client@goat-client-v0.1.2
// internal/ipc/ipc.go, NOT the v0.2.0 rc surface:
//   getMode / setMode  → `goat client mode get|set`   (ADR 0840 triad)
//   getStatus (richer) → `goat client status --detail` (mode + inner-mesh)
//   getInnerMeshStatus / getInnerMeshDiagnostics / enableInnerMesh /
//     disableInnerMesh → `goat client mesh status|diag|enable|disable`
//     (the netbird-mesh embed; PR2)
//   getDiagnostics     → `goat client diagnostics` (outer-tunnel/daemon
//     diagnostics: uptime, last connect attempt, last error, log tail;
//     PR3). NB: there is no debugBundle RPC in v0.1.2, so the netbird-
//     style `debug bundle` tarball stays daemon-side future work.
//   (trust-roots show is CLI-only — reads the daemon's trust-roots.pem
//    from disk, no RPC; path mirrors daemon.DefaultTrustRootsPath.)
//
// Daemon socket default: ~/.goat-client/goat-clientd.sock (configurable
// via --socket / $GOAT_CLIENT_SOCKET). Wire protocol: one JSON-RPC
// request per TCP write, response read until EOF/full JSON. The daemon
// closes the conn after one response per call (observed via netcat).
//
// Phase F.3 deliberately wraps the daemon, not the goat-client GUI
// binary (which is a Fyne window app with no CLI surface, only
// -daemon-addr / -window flags). The daemon socket IS the substrate.
//
// Admin device onboarding (goat admin client onboard) is deferred to
// Phase F.4 — it needs a new per-device manifest substrate at
// ops/devices/<handle>.yml that doesn't exist yet.

use clap::{Args, Subcommand};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
// The Unix-domain-socket transport (and its timeout) is unix-only; the
// daemon (goat-clientd) ships for Linux + macOS only. See `transport`.
#[cfg(unix)]
use std::time::Duration;

#[cfg(unix)]
const IPC_TIMEOUT: Duration = Duration::from_secs(10);
const DEFAULT_SOCKET_REL: &str = ".goat-client/goat-clientd.sock";

#[derive(Subcommand, Debug)]
pub enum ClientCmd {
    /// Print the daemon's tunnel status (state, bundle metadata, last
    /// handshake, configured endpoints, byte counters).
    Status(StatusArgs),
    /// Ask the daemon to bring the tunnel up. The daemon needs root
    /// (TUN creation) — if the daemon was started unprivileged, this
    /// returns the daemon's own permission-denied message verbatim.
    Up(IpcArgs),
    /// Ask the daemon to bring the tunnel down.
    Down(IpcArgs),
    /// Bundle lifecycle (import a CBOR bundle into the daemon).
    Bundle {
        #[command(subcommand)]
        op: BundleCmd,
    },
    /// Get or set the daemon's active mode
    /// (wg-cp0-only | netbird-only | combined).
    Mode {
        #[command(subcommand)]
        op: ModeCmd,
    },
    /// Inspect the offline-CA trust roots the daemon verifies bundles
    /// against. Reads the trust-roots.pem from disk (no daemon RPC).
    TrustRoots {
        #[command(subcommand)]
        op: TrustRootsCmd,
    },
    /// Inspect and control the inner mesh (the netbird embed, active in
    /// netbird-only / combined mode).
    Mesh {
        #[command(subcommand)]
        op: MeshCmd,
    },
    /// Daemon / outer-tunnel diagnostics: uptime, last connect attempt,
    /// last error, and the recent log tail. Wraps getDiagnostics. (The
    /// netbird-style `debug bundle` tarball needs a daemon debugBundle
    /// RPC that v0.1.2 doesn't expose — tracked as daemon-side work.)
    Diagnostics(DiagArgs),
    /// Toggle post-quantum protection on THIS host at BOTH layers —
    /// self-service, no admin token (you're just reconfiguring your own
    /// host). Writes the Zenoh kx-mode state file (F-259 Tier 1 kill-switch:
    /// new goat/zenoh processes latch it; restart long-running surfaces),
    /// then flips Rosenpass (ADR 0602): `on` runs `netbird down` then re-ups
    /// with the Rosenpass flags; `off` re-ups with `--enable-rosenpass=false`
    /// (a bare re-up persists the prior setting, so the disable must be
    /// explicit). Briefly bounces the tunnel; reads back the verified `netbird
    /// status` line. To toggle OTHER peers, use `goat admin pqc --peer|--fleet`.
    Pqc(PqcArgs),
}

#[derive(Args, Debug)]
pub struct PqcArgs {
    /// `on` (enable Rosenpass) or `off` (revert to classical WireGuard).
    pub state: super::admin::PqcState,
    /// netbird binary for the local toggle (PATH lookup or absolute).
    #[arg(long, env = "GOAT_NETBIRD_BIN", default_value = "netbird")]
    pub netbird_bin: String,
    /// Skip the confirmation prompt (the toggle briefly bounces the tunnel).
    #[arg(long, short = 'y')]
    pub yes: bool,
    /// Resolve + print what WOULD run, change nothing, exit 0.
    #[arg(long)]
    pub dry_run: bool,
    /// Emit JSON instead of the human summary.
    #[arg(long)]
    pub json: bool,
}

#[derive(Args, Debug)]
pub struct DiagArgs {
    /// Emit the raw JSON reply instead of pretty lines.
    #[arg(long)]
    pub json: bool,
    /// Override the daemon socket path.
    #[arg(long, env = "GOAT_CLIENT_SOCKET")]
    pub socket: Option<PathBuf>,
}

#[derive(Subcommand, Debug)]
pub enum MeshCmd {
    /// Print the inner mesh's state, peer count, and byte counters.
    Status(IpcArgs),
    /// Print inner-mesh diagnostics: per-peer handshake/transfer stats
    /// plus the daemon's recent inner-mesh log tail.
    Diag(IpcArgs),
    /// Bring the inner mesh up (no-op if the active mode excludes it or
    /// it's already up; the daemon is authoritative).
    Enable(IpcArgs),
    /// Bring the inner mesh down.
    Disable(IpcArgs),
}

#[derive(Subcommand, Debug)]
pub enum ModeCmd {
    /// Print the daemon's current active mode.
    Get(IpcArgs),
    /// Set the daemon's active mode. Accepts wg-cp0-only | netbird-only
    /// | combined; the daemon is authoritative and may reject a mode the
    /// loaded bundle can't satisfy.
    Set(ModeSetArgs),
}

#[derive(Subcommand, Debug)]
pub enum TrustRootsCmd {
    /// Render each offline-CA key in the daemon's trust-roots.pem with
    /// its PEM block type and SHA-256 fingerprint, so an operator can
    /// verify trust-anchor pinning.
    Show(TrustRootsArgs),
}

#[derive(Args, Debug)]
pub struct ModeSetArgs {
    /// Target mode: wg-cp0-only | netbird-only | combined.
    pub mode: String,
    /// Override the daemon socket path.
    #[arg(long, env = "GOAT_CLIENT_SOCKET")]
    pub socket: Option<PathBuf>,
}

#[derive(Args, Debug)]
pub struct TrustRootsArgs {
    /// Path to the trust-roots PEM. Defaults to the daemon's
    /// DefaultTrustRootsPath (`<config-dir>/goat-client/trust-roots.pem`,
    /// falling back to `~/.goat-client/trust-roots.pem`). Pass this if
    /// the daemon was started with a non-default `--trust-roots`.
    #[arg(long)]
    pub path: Option<PathBuf>,
}

#[derive(Subcommand, Debug)]
pub enum BundleCmd {
    /// Send a CBOR bundle file to the daemon via importBundle. The
    /// daemon parses + verifies the offline-CA signature itself; this
    /// CLI is just the transport.
    Import {
        /// Path to the signed CBOR bundle file.
        path: PathBuf,
        /// Override the daemon socket path.
        #[arg(long, env = "GOAT_CLIENT_SOCKET")]
        socket: Option<PathBuf>,
    },
}

#[derive(Args, Debug)]
pub struct StatusArgs {
    /// Emit one JSON object instead of pretty lines.
    #[arg(long)]
    pub json: bool,
    /// Render the full daemon snapshot: active mode, error detail, and
    /// the inner-mesh (netbird embed) sub-block when present. Without
    /// it, status stays terse (state + bundle + handshake/bytes).
    #[arg(long)]
    pub detail: bool,
    /// Override the daemon socket path (default:
    /// `$HOME/.goat-client/goat-clientd.sock`, also honors
    /// `$GOAT_CLIENT_SOCKET`).
    #[arg(long, env = "GOAT_CLIENT_SOCKET")]
    pub socket: Option<PathBuf>,
}

#[derive(Args, Debug)]
pub struct IpcArgs {
    /// Override the daemon socket path.
    #[arg(long, env = "GOAT_CLIENT_SOCKET")]
    pub socket: Option<PathBuf>,
}

#[derive(Debug, Serialize)]
struct RpcRequest<'a> {
    jsonrpc: &'a str,
    method: &'a str,
    params: serde_json::Value,
    id: u64,
}

#[derive(Debug, Deserialize)]
struct RpcResponse {
    #[serde(default)]
    result: Option<serde_json::Value>,
    #[serde(default)]
    error: Option<RpcError>,
}

#[derive(Debug, Deserialize)]
struct RpcError {
    code: i64,
    message: String,
}

#[derive(Debug, Deserialize)]
struct DaemonStatus {
    state: String,
    #[serde(default)]
    mode: String,
    #[serde(default, rename = "bundleLoaded")]
    bundle_loaded: bool,
    #[serde(default, rename = "deviceID")]
    device_id: String,
    #[serde(default)]
    site: String,
    #[serde(default, rename = "bundleExpiresAt")]
    bundle_expires_at: String,
    #[serde(default, rename = "peerPubkey")]
    peer_pubkey: String,
    #[serde(default, rename = "lastHandshake")]
    last_handshake: String,
    #[serde(default, rename = "bytesIn")]
    bytes_in: u64,
    #[serde(default, rename = "bytesOut")]
    bytes_out: u64,
    #[serde(default, rename = "configuredEndpoints")]
    configured_endpoints: Vec<String>,
    #[serde(default, rename = "errorMessage")]
    error_message: String,
    #[serde(default, rename = "innerMesh")]
    inner_mesh: Option<InnerMeshSnapshot>,
}

/// Mirrors goat-clientd's `InnerMeshSnapshot` (the netbird-embed
/// sub-status carried inside getStatus when a mesh-eligible bundle is
/// loaded in netbird-only / combined mode).
#[derive(Debug, Deserialize)]
struct InnerMeshSnapshot {
    #[serde(default)]
    state: String,
    #[serde(default, rename = "peerCount")]
    peer_count: i64,
    #[serde(default, rename = "bytesIn")]
    bytes_in: u64,
    #[serde(default, rename = "bytesOut")]
    bytes_out: u64,
    #[serde(default, rename = "lastHandshake")]
    last_handshake: String,
    /// How many of `peer_count` links have Rosenpass (post-quantum) key
    /// exchange active. 0 = no PQC on any peer link.
    #[serde(default, rename = "rosenpassPeers")]
    rosenpass_peers: i64,
    /// Configured Rosenpass *intent* for this profile's inner mesh (the
    /// override set via setRosenpass, else the bundle policy) — distinct
    /// from `rosenpass_peers`, the live active count. `enabled` with
    /// `rosenpass_peers == 0` means "on, but no peer has negotiated PQC
    /// yet" — honest, not a contradiction.
    #[serde(default, rename = "rosenpassEnabled")]
    rosenpass_enabled: bool,
    #[serde(default, rename = "rosenpassPermissive")]
    rosenpass_permissive: bool,
}

/// getInnerMeshDiagnostics reply: per-peer stats + the daemon's recent
/// inner-mesh log lines.
#[derive(Debug, Deserialize)]
struct InnerMeshDiagnosticsReply {
    #[serde(default, rename = "logTail")]
    log_tail: Vec<String>,
    #[serde(default, rename = "peerStats")]
    peer_stats: Vec<InnerMeshPeerStats>,
}

/// One inner-mesh peer's transfer + handshake stats.
#[derive(Debug, Deserialize)]
struct InnerMeshPeerStats {
    #[serde(default, rename = "peerPubKey")]
    peer_pub_key: String,
    #[serde(default, rename = "bytesIn")]
    bytes_in: u64,
    #[serde(default, rename = "bytesOut")]
    bytes_out: u64,
    #[serde(default, rename = "lastHandshake")]
    last_handshake: String,
}

/// getDiagnostics reply (v0.1.2): outer-tunnel / daemon diagnostics.
#[derive(Debug, Deserialize)]
struct DiagnosticsReply {
    #[serde(default, rename = "logTail")]
    log_tail: Vec<String>,
    #[serde(default, rename = "lastConnectAttempt")]
    last_connect_attempt: String,
    #[serde(default, rename = "lastError")]
    last_error: String,
    #[serde(default, rename = "uptimeSeconds")]
    uptime_seconds: i64,
}

/// getMode reply: `{ "mode": "<mode>" }`.
#[derive(Debug, Deserialize)]
struct GetModeReply {
    #[serde(default)]
    mode: String,
}

/// setMode reply: `{ "previousMode": "...", "mode": "..." }`.
#[derive(Debug, Deserialize)]
struct SetModeReply {
    #[serde(default, rename = "previousMode")]
    previous_mode: String,
    #[serde(default)]
    mode: String,
}

/// setRosenpass reply: the effective Rosenpass intent before/after the toggle.
#[derive(Debug, Deserialize)]
struct SetRosenpassReply {
    #[serde(default, rename = "previousEnabled")]
    previous_enabled: bool,
    #[serde(default, rename = "previousPermissive")]
    previous_permissive: bool,
    #[serde(default)]
    enabled: bool,
    #[serde(default)]
    permissive: bool,
}

/// The three modes ADR 0840 defines and goat-clientd's `internal/mode`
/// validates. We validate client-side for a clean error; the daemon is
/// authoritative (it may still reject a mode the loaded bundle can't
/// satisfy).
const VALID_MODES: [&str; 3] = ["wg-cp0-only", "netbird-only", "combined"];

pub async fn run(_ctx: &super::Ctx, op: ClientCmd) -> anyhow::Result<()> {
    match op {
        ClientCmd::Status(args) => {
            let raw = call(
                &resolve_socket(args.socket.as_deref())?,
                "getStatus",
                serde_json::json!({}),
            )
            .await?;
            if args.json {
                println!("{}", raw);
                return Ok(());
            }
            let s: DaemonStatus = serde_json::from_str(&raw)
                .map_err(|e| anyhow::anyhow!("decode getStatus response: {e}; raw: {raw}"))?;
            print!("{}", render_status(&s, args.detail));
            Ok(())
        }
        ClientCmd::Up(args) => {
            let _ = call(
                &resolve_socket(args.socket.as_deref())?,
                "connect",
                serde_json::json!({}),
            )
            .await?;
            println!("goat client up: connect requested");
            Ok(())
        }
        ClientCmd::Down(args) => {
            let _ = call(
                &resolve_socket(args.socket.as_deref())?,
                "disconnect",
                serde_json::json!({}),
            )
            .await?;
            println!("goat client down: disconnect requested");
            Ok(())
        }
        ClientCmd::Bundle { op } => match op {
            BundleCmd::Import { path, socket } => {
                let bytes = std::fs::read(&path)
                    .map_err(|e| anyhow::anyhow!("read bundle {}: {e}", path.display()))?;
                // Encode CBOR bytes as base64 in the params for JSON
                // transport; the daemon decodes back to bytes server-
                // side. The empirical importBundle EOF probe with {}
                // confirms the daemon expects a bundle body — base64
                // is the natural JSON-safe encoding.
                use base64::Engine;
                let b64 = base64::engine::general_purpose::STANDARD.encode(&bytes);
                let params = serde_json::json!({ "bundle": b64 });
                let _ = call(&resolve_socket(socket.as_deref())?, "importBundle", params).await?;
                println!(
                    "goat client bundle import: {} bytes accepted by daemon",
                    bytes.len()
                );
                Ok(())
            }
        },
        ClientCmd::Mode { op } => match op {
            ModeCmd::Get(args) => {
                let raw = call(
                    &resolve_socket(args.socket.as_deref())?,
                    "getMode",
                    serde_json::json!({}),
                )
                .await?;
                let r: GetModeReply = serde_json::from_str(&raw)
                    .map_err(|e| anyhow::anyhow!("decode getMode response: {e}; raw: {raw}"))?;
                println!("{}", r.mode);
                Ok(())
            }
            ModeCmd::Set(args) => {
                let mode = validate_mode(&args.mode)?;
                let params = serde_json::json!({ "mode": mode });
                let raw = call(&resolve_socket(args.socket.as_deref())?, "setMode", params).await?;
                let r: SetModeReply = serde_json::from_str(&raw)
                    .map_err(|e| anyhow::anyhow!("decode setMode response: {e}; raw: {raw}"))?;
                if r.previous_mode == r.mode {
                    println!("mode unchanged: {}", r.mode);
                } else {
                    println!("mode: {} -> {}", r.previous_mode, r.mode);
                }
                Ok(())
            }
        },
        ClientCmd::TrustRoots { op } => match op {
            TrustRootsCmd::Show(args) => {
                let path = resolve_trust_roots_path(args.path.as_deref())?;
                let pem_bytes = std::fs::read(&path).map_err(|e| {
                    anyhow::anyhow!(
                        "read trust roots {}: {e} — is goat-clientd installed, or pass --path?",
                        path.display()
                    )
                })?;
                print!("{}", render_trust_roots(&path, &pem_bytes)?);
                Ok(())
            }
        },
        ClientCmd::Mesh { op } => match op {
            MeshCmd::Status(args) => {
                let raw = call(
                    &resolve_socket(args.socket.as_deref())?,
                    "getInnerMeshStatus",
                    serde_json::json!({}),
                )
                .await?;
                let m: InnerMeshSnapshot = serde_json::from_str(&raw).map_err(|e| {
                    anyhow::anyhow!("decode getInnerMeshStatus response: {e}; raw: {raw}")
                })?;
                print!("{}", render_inner_mesh_status(&m));
                Ok(())
            }
            MeshCmd::Diag(args) => {
                let raw = call(
                    &resolve_socket(args.socket.as_deref())?,
                    "getInnerMeshDiagnostics",
                    serde_json::json!({}),
                )
                .await?;
                let d: InnerMeshDiagnosticsReply = serde_json::from_str(&raw).map_err(|e| {
                    anyhow::anyhow!("decode getInnerMeshDiagnostics response: {e}; raw: {raw}")
                })?;
                print!("{}", render_inner_mesh_diag(&d));
                Ok(())
            }
            MeshCmd::Enable(args) => {
                let _ = call(
                    &resolve_socket(args.socket.as_deref())?,
                    "enableInnerMesh",
                    serde_json::json!({}),
                )
                .await?;
                println!("inner mesh: enable requested");
                Ok(())
            }
            MeshCmd::Disable(args) => {
                let _ = call(
                    &resolve_socket(args.socket.as_deref())?,
                    "disableInnerMesh",
                    serde_json::json!({}),
                )
                .await?;
                println!("inner mesh: disable requested");
                Ok(())
            }
        },
        ClientCmd::Diagnostics(args) => {
            let raw = call(
                &resolve_socket(args.socket.as_deref())?,
                "getDiagnostics",
                serde_json::json!({}),
            )
            .await?;
            if args.json {
                println!("{}", raw);
                return Ok(());
            }
            let d: DiagnosticsReply = serde_json::from_str(&raw)
                .map_err(|e| anyhow::anyhow!("decode getDiagnostics response: {e}; raw: {raw}"))?;
            print!("{}", render_diagnostics(&d));
            Ok(())
        }
        // Self-service local PQC toggle. Prefers the goat-clientd daemon
        // (it owns the embedded inner mesh); falls back to the standalone
        // netbird CLI when no daemon is running.
        ClientCmd::Pqc(a) => pqc(a).await,
    }
}

/// `goat client pqc <on|off>` — toggle Rosenpass (post-quantum) key
/// exchange for THIS host.
///
/// Dual-actuator, daemon-first: goat-client devices run the inner mesh
/// *embedded* inside goat-clientd, which the standalone `netbird` CLI
/// cannot reach — so when a goat-clientd socket is present that is the
/// mesh the operator means, and we drive it via the `setRosenpass` RPC
/// (which persists the intent + reconnects the inner mesh daemon-side).
/// With no daemon socket the host is a plain standalone-netbird peer, and
/// we fall back to the netbird-CLI down->up dance in `admin::pqc_local`.
async fn pqc(a: PqcArgs) -> anyhow::Result<()> {
    if let Ok(sock) = resolve_socket(None) {
        if sock.exists() {
            let enabled = matches!(a.state, super::admin::PqcState::On);
            // Enable permissively (a peer never strands itself against
            // not-yet-enabled peers during rollout); disabling clears both.
            let permissive = enabled;
            if a.dry_run {
                if a.json {
                    println!(
                        "{}",
                        serde_json::json!({
                            "actuator": "daemon",
                            "socket": sock.display().to_string(),
                            "method": "setRosenpass",
                            "enabled": enabled,
                            "permissive": permissive,
                            "dry_run": true,
                        })
                    );
                } else {
                    println!(
                        "DRY RUN: would call goat-clientd setRosenpass enabled={enabled} permissive={permissive} on {}",
                        sock.display()
                    );
                }
                return Ok(());
            }
            let params = serde_json::json!({ "enabled": enabled, "permissive": permissive });
            let raw = call(&sock, "setRosenpass", params).await?;
            if a.json {
                println!("{}", raw);
                return Ok(());
            }
            let r: SetRosenpassReply = serde_json::from_str(&raw)
                .map_err(|e| anyhow::anyhow!("decode setRosenpass response: {e}; raw: {raw}"))?;
            println!(
                "pqc (rosenpass): enabled {} -> {}, permissive {} -> {} (via goat-clientd)",
                r.previous_enabled, r.enabled, r.previous_permissive, r.permissive
            );
            return Ok(());
        }
    }
    // No daemon socket — standalone-netbird host. Toggle via the CLI.
    super::admin::pqc_local(a.state, &a.netbird_bin, a.yes, a.dry_run, a.json).await
}

/// Client-side mode validation — returns the canonical mode string or a
/// clean error listing the valid set. The daemon re-validates.
fn validate_mode(m: &str) -> anyhow::Result<&'static str> {
    VALID_MODES
        .iter()
        .copied()
        .find(|valid| *valid == m)
        .ok_or_else(|| {
            anyhow::anyhow!(
                "invalid mode {m:?}; expected one of {}",
                VALID_MODES.join(" | ")
            )
        })
}

/// Default daemon socket path with no explicit override — `goat heal`
/// probes this to decide whether goat-clientd is a usable actuator.
pub(crate) fn default_socket() -> anyhow::Result<PathBuf> {
    resolve_socket(None)
}

fn resolve_socket(arg: Option<&std::path::Path>) -> anyhow::Result<PathBuf> {
    if let Some(p) = arg {
        return Ok(p.to_path_buf());
    }
    if let Some(p) = std::env::var_os("GOAT_CLIENT_SOCKET") {
        return Ok(PathBuf::from(p));
    }
    let home = crate::profile::resolve_home_dir()
        .map_err(|_| anyhow::anyhow!("$HOME not set; pass --socket <path>"))?;
    Ok(home.join(DEFAULT_SOCKET_REL))
}

/// Send a newline-framed JSON-RPC request to the daemon and return the
/// raw response bytes.
///
/// Unix-only transport: goat-clientd listens on a Unix domain socket.
/// The Windows goat-clientd is designed to use a named pipe instead
/// (`\\.\pipe\goat-client`, see docs/design/goat-client.md / ADR 0840),
/// but that daemon + IPC path is not built yet — so on Windows this
/// returns a clear not-yet-available error rather than connecting. When
/// the Windows daemon ships, replace this stub with a named-pipe client.
#[cfg(unix)]
async fn transport(socket: &std::path::Path, body: &[u8]) -> anyhow::Result<Vec<u8>> {
    use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
    use tokio::net::UnixStream;
    use tokio::time::timeout;

    let mut stream = timeout(IPC_TIMEOUT, UnixStream::connect(socket))
        .await
        .map_err(|_| {
            anyhow::anyhow!(
                "daemon socket connect timed out after {}s ({})",
                IPC_TIMEOUT.as_secs(),
                socket.display()
            )
        })?
        .map_err(|e| {
            anyhow::anyhow!(
                "daemon socket {} unreachable: {e} — is goat-clientd running?",
                socket.display(),
            )
        })?;

    stream.write_all(body).await?;
    stream.write_all(b"\n").await?;

    // Read ONE `\n`-framed reply, not to EOF. The daemon replies with a single
    // newline-terminated JSON line and KEEPS THE CONNECTION OPEN (it does not
    // close after each reply) — so `read_to_end` blocks until the timeout
    // waiting for an EOF that never comes, which is exactly the "daemon
    // response read timed out" hang. `read_until('\n')` also handles a
    // one-shot daemon that closes without a trailing newline (returns at EOF),
    // so this is robust to both IPC framings.
    let mut reader = BufReader::new(&mut stream);
    let mut line = Vec::new();
    let n = timeout(IPC_TIMEOUT, reader.read_until(b'\n', &mut line))
        .await
        .map_err(|_| {
            anyhow::anyhow!(
                "daemon response read timed out after {}s",
                IPC_TIMEOUT.as_secs()
            )
        })??;
    if n == 0 {
        anyhow::bail!("daemon closed the connection without a reply");
    }
    Ok(line)
}

#[cfg(not(unix))]
async fn transport(_socket: &std::path::Path, _body: &[u8]) -> anyhow::Result<Vec<u8>> {
    anyhow::bail!(
        "`goat client` controls the goat-clientd tunnel daemon over a local socket. \
         The Windows daemon (named-pipe IPC) is designed but not built yet, so this \
         command is not available on Windows — manage the mesh tunnel with the \
         NetBird client for now. Every other `goat` subcommand runs natively here."
    )
}

pub(crate) async fn call(
    socket: &std::path::Path,
    method: &str,
    params: serde_json::Value,
) -> anyhow::Result<String> {
    let req = RpcRequest {
        jsonrpc: "2.0",
        method,
        params,
        id: 1,
    };
    let body = serde_json::to_vec(&req)?;
    let buf = transport(socket, &body).await?;

    let resp: RpcResponse = serde_json::from_slice(&buf).map_err(|e| {
        anyhow::anyhow!(
            "decode daemon response: {e}; raw: {}",
            String::from_utf8_lossy(&buf)
        )
    })?;
    if let Some(err) = resp.error {
        anyhow::bail!("daemon RPC error {}: {}", err.code, err.message);
    }
    let result = resp.result.unwrap_or(serde_json::Value::Null);
    Ok(serde_json::to_string(&result).unwrap_or_default())
}

fn render_status(s: &DaemonStatus, detail: bool) -> String {
    let mut out = String::new();
    out.push_str(&format!("state             {}\n", s.state));
    if detail && !s.mode.is_empty() {
        out.push_str(&format!("mode              {}\n", s.mode));
    }
    out.push_str(&format!("bundle loaded     {}\n", s.bundle_loaded));
    if s.bundle_loaded {
        out.push_str(&format!("device            {}\n", s.device_id));
        out.push_str(&format!("site              {}\n", s.site));
        out.push_str(&format!("bundle expires    {}\n", s.bundle_expires_at));
        out.push_str(&format!("peer pubkey       {}\n", s.peer_pubkey));
        out.push_str(&format!(
            "endpoints         {}\n",
            if s.configured_endpoints.is_empty() {
                "(none)".to_string()
            } else {
                s.configured_endpoints.join(", ")
            },
        ));
    }
    if s.state == "connected" {
        out.push_str(&format!("last handshake    {}\n", s.last_handshake));
        out.push_str(&format!(
            "bytes in/out      {} / {}\n",
            s.bytes_in, s.bytes_out
        ));
    }
    if detail {
        if !s.error_message.is_empty() {
            out.push_str(&format!("error             {}\n", s.error_message));
        }
        if let Some(m) = &s.inner_mesh {
            out.push_str("inner mesh:\n");
            out.push_str(&format!("  state           {}\n", m.state));
            out.push_str(&format!("  peers           {}\n", m.peer_count));
            out.push_str(&format!(
                "  bytes in/out    {} / {}\n",
                m.bytes_in, m.bytes_out
            ));
            out.push_str(&format!("  post-quantum    {}\n", pqc_line(m)));
            if m.state == "connected" {
                out.push_str(&format!("  last handshake  {}\n", m.last_handshake));
            }
        }
    }
    out
}

/// Resolve the trust-roots PEM path: explicit `--path`, else the
/// daemon's DefaultTrustRootsPath resolution order.
fn resolve_trust_roots_path(arg: Option<&std::path::Path>) -> anyhow::Result<PathBuf> {
    if let Some(p) = arg {
        return Ok(p.to_path_buf());
    }
    default_trust_roots_path().ok_or_else(|| {
        anyhow::anyhow!("could not resolve a default trust-roots path ($HOME unset); pass --path")
    })
}

/// Mirror goat-clientd's `daemon.DefaultTrustRootsPath`:
/// `<os-config-dir>/goat-client/trust-roots.pem`, falling back to
/// `~/.goat-client/trust-roots.pem`. The config-dir resolution matches
/// Go's `os.UserConfigDir` per platform.
fn default_trust_roots_path() -> Option<PathBuf> {
    if let Some(cfg) = user_config_dir() {
        return Some(cfg.join("goat-client").join("trust-roots.pem"));
    }
    crate::profile::resolve_home_dir()
        .ok()
        .map(|h| h.join(".goat-client").join("trust-roots.pem"))
}

/// Platform config-dir base, matching Go's `os.UserConfigDir`.
fn user_config_dir() -> Option<PathBuf> {
    if cfg!(target_os = "macos") {
        std::env::var_os("HOME")
            .map(|h| PathBuf::from(h).join("Library").join("Application Support"))
    } else if cfg!(target_os = "windows") {
        std::env::var_os("APPDATA").map(PathBuf::from)
    } else {
        std::env::var_os("XDG_CONFIG_HOME")
            .map(PathBuf::from)
            .filter(|p| !p.as_os_str().is_empty())
            .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".config")))
    }
}

/// Parse a trust-roots PEM and render each block's type + SHA-256
/// fingerprint. Mirrors the daemon's accepted block set (PUBLIC KEY /
/// CERTIFICATE, ECDSA P-256). We render structurally — we don't verify
/// the key is P-256 (the daemon does that on load); the operator just
/// needs to see which anchors are pinned.
fn render_trust_roots(path: &std::path::Path, pem_bytes: &[u8]) -> anyhow::Result<String> {
    let blocks =
        pem::parse_many(pem_bytes).map_err(|e| anyhow::anyhow!("parse {}: {e}", path.display()))?;
    if blocks.is_empty() {
        anyhow::bail!(
            "{} contains no PEM blocks — expected PUBLIC KEY or CERTIFICATE offline-CA anchors",
            path.display()
        );
    }
    let mut out = String::new();
    out.push_str(&format!("trust roots: {}\n", path.display()));
    out.push_str(&format!("anchors:     {}\n", blocks.len()));
    for (i, b) in blocks.iter().enumerate() {
        let fp = sha256_hex(b.contents());
        out.push_str(&format!("  [{i}] {:<11} sha256:{fp}\n", b.tag()));
    }
    Ok(out)
}

/// Lowercase hex SHA-256 of the given bytes (the DER body of a PEM
/// block) — a stable per-anchor fingerprint operators can compare.
fn sha256_hex(data: &[u8]) -> String {
    use sha2::{Digest, Sha256};
    let digest = Sha256::digest(data);
    let mut s = String::with_capacity(digest.len() * 2);
    for byte in digest {
        s.push_str(&format!("{byte:02x}"));
    }
    s
}

/// Render `goat client mesh status` — the inner-mesh snapshot as
/// standalone (un-indented) lines. The same snapshot also appears, two-
/// space-indented, under `goat client status --detail`.
fn render_inner_mesh_status(m: &InnerMeshSnapshot) -> String {
    let mut out = String::new();
    out.push_str(&format!("state             {}\n", m.state));
    out.push_str(&format!("peers             {}\n", m.peer_count));
    out.push_str(&format!(
        "bytes in/out      {} / {}\n",
        m.bytes_in, m.bytes_out
    ));
    out.push_str(&format!("post-quantum      {}\n", pqc_line(m)));
    if m.state == "connected" {
        out.push_str(&format!("last handshake    {}\n", m.last_handshake));
    }
    out
}

/// Human PQC line: the configured *intent* (from the daemon-held override
/// or the bundle policy) plus the live active-peer count. `intent=on` with
/// `0/N peers` is honest — enabled, but no peer has negotiated PQC yet.
fn pqc_line(m: &InnerMeshSnapshot) -> String {
    let intent = if m.rosenpass_enabled {
        if m.rosenpass_permissive {
            "on (permissive)"
        } else {
            "on (strict)"
        }
    } else {
        "off"
    };
    let active = if m.rosenpass_peers > 0 {
        format!("active ({}/{} peers)", m.rosenpass_peers, m.peer_count)
    } else {
        "inactive".to_string()
    };
    format!("intent={intent}, {active}")
}

/// Render `goat client mesh diag` — per-peer stats then the daemon's
/// recent inner-mesh log tail.
fn render_inner_mesh_diag(d: &InnerMeshDiagnosticsReply) -> String {
    let mut out = String::new();
    out.push_str(&format!("peers             {}\n", d.peer_stats.len()));
    for (i, p) in d.peer_stats.iter().enumerate() {
        out.push_str(&format!("  [{i}] {}\n", p.peer_pub_key));
        out.push_str(&format!(
            "      bytes in/out  {} / {}\n",
            p.bytes_in, p.bytes_out
        ));
        out.push_str(&format!("      last handshake {}\n", p.last_handshake));
    }
    out.push_str(&format!("log tail          {} line(s)\n", d.log_tail.len()));
    for line in &d.log_tail {
        out.push_str(&format!("  {line}\n"));
    }
    out
}

/// Render `goat client diagnostics` — outer-tunnel / daemon health:
/// uptime, last connect attempt, last error, then the recent log tail.
/// A zero/empty `lastConnectAttempt` (the Go zero time marshals to
/// `0001-01-01T00:00:00Z`) and empty `lastError` render as friendly
/// placeholders.
fn render_diagnostics(d: &DiagnosticsReply) -> String {
    let mut out = String::new();
    out.push_str(&format!("uptime            {}s\n", d.uptime_seconds));
    let last_connect =
        if d.last_connect_attempt.is_empty() || d.last_connect_attempt.starts_with("0001-01-01") {
            "(never)".to_string()
        } else {
            d.last_connect_attempt.clone()
        };
    out.push_str(&format!("last connect      {last_connect}\n"));
    out.push_str(&format!(
        "last error        {}\n",
        if d.last_error.is_empty() {
            "(none)"
        } else {
            &d.last_error
        }
    ));
    out.push_str(&format!("log tail          {} line(s)\n", d.log_tail.len()));
    for line in &d.log_tail {
        out.push_str(&format!("  {line}\n"));
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn status_decodes_v0_1_x_shape() {
        // Captured from a real getStatus probe .
        let raw = r#"{"state":"disconnected","bundleLoaded":true,"deviceID":"op-laptop","site":"vega","bundleExpiresAt":"2026-08-04T16:32:16+02:00","peerPubkey":"/QwEzd8N5g8onYvOpQ0ia5OMEOt1cPawrQSqEUis3OU=","lastHandshake":"0001-01-01T00:00:00Z","bytesIn":0,"bytesOut":0,"configuredEndpoints":["203.0.113.23:51821","203.0.113.10:51821","203.0.113.1:51821"]}"#;
        let s: DaemonStatus = serde_json::from_str(raw).unwrap();
        assert_eq!(s.state, "disconnected");
        assert!(s.bundle_loaded);
        assert_eq!(s.device_id, "op-laptop");
        assert_eq!(s.configured_endpoints.len(), 3);
    }

    /// Build a DaemonStatus with the common fields set; tests override
    /// what they care about. Keeps the constructors from churning as the
    /// reply shape grows.
    fn status_fixture() -> DaemonStatus {
        DaemonStatus {
            state: "disconnected".into(),
            mode: String::new(),
            bundle_loaded: true,
            device_id: "op-laptop".into(),
            site: "vega".into(),
            bundle_expires_at: "2026-08-04T16:32:16+02:00".into(),
            peer_pubkey: "abc".into(),
            last_handshake: "0001-01-01T00:00:00Z".into(),
            bytes_in: 0,
            bytes_out: 0,
            configured_endpoints: vec!["1.1.1.1:51821".into()],
            error_message: String::new(),
            inner_mesh: None,
        }
    }

    #[test]
    fn render_status_disconnected_omits_handshake() {
        let s = status_fixture();
        let out = render_status(&s, false);
        assert!(out.contains("state             disconnected"));
        assert!(out.contains("device            op-laptop"));
        assert!(!out.contains("last handshake"));
        assert!(!out.contains("bytes in/out"));
    }

    #[test]
    fn render_status_connected_includes_handshake() {
        let mut s = status_fixture();
        s.state = "connected".into();
        s.device_id = "op".into();
        s.site = "nova".into();
        s.bundle_expires_at = "x".into();
        s.peer_pubkey = "y".into();
        s.last_handshake = "2026-05-31T10:00:00Z".into();
        s.bytes_in = 1024;
        s.bytes_out = 512;
        s.configured_endpoints = vec![];
        let out = render_status(&s, false);
        assert!(out.contains("last handshake    2026-05-31T10:00:00Z"));
        assert!(out.contains("bytes in/out      1024 / 512"));
        assert!(out.contains("endpoints         (none)"));
    }

    #[test]
    fn terse_render_omits_mode_and_inner_mesh() {
        // Even with mode + inner-mesh present, the non-detail render
        // stays terse — those land only under --detail.
        let mut s = status_fixture();
        s.mode = "combined".into();
        s.inner_mesh = Some(InnerMeshSnapshot {
            state: "connected".into(),
            peer_count: 3,
            bytes_in: 10,
            bytes_out: 20,
            last_handshake: "2026-06-03T10:00:00Z".into(),
            rosenpass_peers: 0,
            rosenpass_enabled: false,
            rosenpass_permissive: false,
        });
        let out = render_status(&s, false);
        assert!(!out.contains("mode"), "terse must not show mode");
        assert!(
            !out.contains("inner mesh"),
            "terse must not show inner mesh"
        );
    }

    #[test]
    fn detail_render_includes_mode_error_and_inner_mesh() {
        let mut s = status_fixture();
        s.state = "error".into();
        s.mode = "combined".into();
        s.error_message = "wg-cp0 handshake timeout".into();
        s.inner_mesh = Some(InnerMeshSnapshot {
            state: "connected".into(),
            peer_count: 3,
            bytes_in: 4096,
            bytes_out: 2048,
            last_handshake: "2026-06-03T10:00:00Z".into(),
            rosenpass_peers: 2,
            rosenpass_enabled: true,
            rosenpass_permissive: true,
        });
        let out = render_status(&s, true);
        assert!(out.contains("mode              combined"));
        assert!(out.contains("error             wg-cp0 handshake timeout"));
        assert!(out.contains("inner mesh:"));
        assert!(out.contains("peers           3"));
        assert!(out.contains("bytes in/out    4096 / 2048"));
        assert!(out.contains("post-quantum    intent=on (permissive), active (2/3 peers)"));
        assert!(out.contains("last handshake  2026-06-03T10:00:00Z"));
    }

    #[test]
    fn full_status_reply_decodes_with_mode_and_inner_mesh() {
        // Shape per goat-clientd v0.1.2 StatusReply (mode + nested
        // innerMesh). Confirms the wider deserialize the F.3 struct
        // dropped on the floor.
        let raw = r#"{"mode":"combined","state":"connected","bundleLoaded":true,"deviceID":"op","site":"site-a","peerPubkey":"AAAA","bytesIn":1,"bytesOut":2,"configuredEndpoints":["1.2.3.4:51821"],"innerMesh":{"state":"connected","peerCount":5,"bytesIn":100,"bytesOut":200,"lastHandshake":"2026-06-03T10:00:00Z"}}"#;
        let s: DaemonStatus = serde_json::from_str(raw).unwrap();
        assert_eq!(s.mode, "combined");
        let m = s.inner_mesh.expect("innerMesh present");
        assert_eq!(m.peer_count, 5);
        assert_eq!(m.bytes_out, 200);
    }

    #[test]
    fn validate_mode_accepts_the_triad_and_rejects_others() {
        assert_eq!(validate_mode("wg-cp0-only").unwrap(), "wg-cp0-only");
        assert_eq!(validate_mode("netbird-only").unwrap(), "netbird-only");
        assert_eq!(validate_mode("combined").unwrap(), "combined");
        let err = validate_mode("turbo").unwrap_err();
        let msg = format!("{err}");
        assert!(msg.contains("invalid mode"), "got: {msg}");
        assert!(
            msg.contains("wg-cp0-only | netbird-only | combined"),
            "got: {msg}"
        );
    }

    #[test]
    fn set_mode_reply_decodes_previous_and_new() {
        let raw = r#"{"previousMode":"wg-cp0-only","mode":"combined"}"#;
        let r: SetModeReply = serde_json::from_str(raw).unwrap();
        assert_eq!(r.previous_mode, "wg-cp0-only");
        assert_eq!(r.mode, "combined");
    }

    #[test]
    fn inner_mesh_status_decodes_and_renders_connected() {
        let raw = r#"{"state":"connected","peerCount":4,"bytesIn":4096,"bytesOut":2048,"lastHandshake":"2026-06-03T10:00:00Z"}"#;
        let m: InnerMeshSnapshot = serde_json::from_str(raw).unwrap();
        assert_eq!(m.peer_count, 4);
        let out = render_inner_mesh_status(&m);
        assert!(out.contains("state             connected"));
        assert!(out.contains("peers             4"));
        assert!(out.contains("bytes in/out      4096 / 2048"));
        assert!(out.contains("last handshake    2026-06-03T10:00:00Z"));
    }

    #[test]
    fn inner_mesh_status_omits_handshake_when_not_connected() {
        let m = InnerMeshSnapshot {
            state: "connecting".into(),
            peer_count: 0,
            bytes_in: 0,
            bytes_out: 0,
            last_handshake: "0001-01-01T00:00:00Z".into(),
            rosenpass_peers: 0,
            rosenpass_enabled: false,
            rosenpass_permissive: false,
        };
        let out = render_inner_mesh_status(&m);
        assert!(out.contains("state             connecting"));
        assert!(
            !out.contains("last handshake"),
            "non-connected omits handshake"
        );
    }

    #[test]
    fn pqc_line_reports_intent_distinct_from_active_count() {
        let base = InnerMeshSnapshot {
            state: "connected".into(),
            peer_count: 3,
            bytes_in: 0,
            bytes_out: 0,
            last_handshake: String::new(),
            rosenpass_peers: 0,
            rosenpass_enabled: false,
            rosenpass_permissive: false,
        };
        // Off, no peers.
        assert_eq!(pqc_line(&base), "intent=off, inactive");
        // On (permissive) but no peer has negotiated yet — honest, not a
        // contradiction.
        let on_no_peers = InnerMeshSnapshot {
            rosenpass_enabled: true,
            rosenpass_permissive: true,
            ..clone_snap(&base)
        };
        assert_eq!(pqc_line(&on_no_peers), "intent=on (permissive), inactive");
        // On (strict) with live peers.
        let on_strict = InnerMeshSnapshot {
            rosenpass_enabled: true,
            rosenpass_permissive: false,
            rosenpass_peers: 2,
            ..clone_snap(&base)
        };
        assert_eq!(
            pqc_line(&on_strict),
            "intent=on (strict), active (2/3 peers)"
        );
    }

    // InnerMeshSnapshot is Deserialize-only (no Clone); tiny local copy
    // helper keeps the pqc_line cases terse.
    fn clone_snap(m: &InnerMeshSnapshot) -> InnerMeshSnapshot {
        InnerMeshSnapshot {
            state: m.state.clone(),
            peer_count: m.peer_count,
            bytes_in: m.bytes_in,
            bytes_out: m.bytes_out,
            last_handshake: m.last_handshake.clone(),
            rosenpass_peers: m.rosenpass_peers,
            rosenpass_enabled: m.rosenpass_enabled,
            rosenpass_permissive: m.rosenpass_permissive,
        }
    }

    #[test]
    fn inner_mesh_diag_decodes_and_renders_peers_and_log() {
        let raw = r#"{"logTail":["dialing relay","handshake ok"],"peerStats":[{"peerPubKey":"PUBKEY1","bytesIn":10,"bytesOut":20,"lastHandshake":"2026-06-03T10:00:00Z"}]}"#;
        let d: InnerMeshDiagnosticsReply = serde_json::from_str(raw).unwrap();
        assert_eq!(d.peer_stats.len(), 1);
        assert_eq!(d.log_tail.len(), 2);
        let out = render_inner_mesh_diag(&d);
        assert!(out.contains("peers             1"));
        assert!(out.contains("[0] PUBKEY1"));
        assert!(out.contains("bytes in/out  10 / 20"));
        assert!(out.contains("log tail          2 line(s)"));
        assert!(out.contains("dialing relay"));
        assert!(out.contains("handshake ok"));
    }

    #[test]
    fn inner_mesh_diag_handles_empty() {
        let raw = r#"{"logTail":[],"peerStats":[]}"#;
        let d: InnerMeshDiagnosticsReply = serde_json::from_str(raw).unwrap();
        let out = render_inner_mesh_diag(&d);
        assert!(out.contains("peers             0"));
        assert!(out.contains("log tail          0 line(s)"));
    }

    #[test]
    fn diagnostics_decodes_and_renders_active() {
        let raw = r#"{"logTail":["bringing tunnel up","handshake ok"],"lastConnectAttempt":"2026-06-03T10:00:00Z","lastError":"","uptimeSeconds":3600}"#;
        let d: DiagnosticsReply = serde_json::from_str(raw).unwrap();
        assert_eq!(d.uptime_seconds, 3600);
        let out = render_diagnostics(&d);
        assert!(out.contains("uptime            3600s"));
        assert!(out.contains("last connect      2026-06-03T10:00:00Z"));
        assert!(out.contains("last error        (none)"));
        assert!(out.contains("log tail          2 line(s)"));
        assert!(out.contains("handshake ok"));
    }

    #[test]
    fn diagnostics_renders_placeholders_for_zero_time_and_no_error() {
        // Go's zero time marshals to 0001-01-01...; an unconnected daemon
        // with no error should read cleanly, not dump the sentinel.
        let raw = r#"{"logTail":[],"lastConnectAttempt":"0001-01-01T00:00:00Z","lastError":"","uptimeSeconds":12}"#;
        let d: DiagnosticsReply = serde_json::from_str(raw).unwrap();
        let out = render_diagnostics(&d);
        assert!(out.contains("last connect      (never)"));
        assert!(out.contains("last error        (none)"));
        assert!(!out.contains("0001-01-01"), "zero time must not leak");
    }

    #[test]
    fn diagnostics_surfaces_last_error() {
        let raw = r#"{"logTail":[],"lastConnectAttempt":"2026-06-03T09:00:00Z","lastError":"no route to relay","uptimeSeconds":5}"#;
        let d: DiagnosticsReply = serde_json::from_str(raw).unwrap();
        let out = render_diagnostics(&d);
        assert!(out.contains("last error        no route to relay"));
    }

    #[test]
    fn render_trust_roots_lists_blocks_with_fingerprints() {
        // Two minimal PEM blocks — one PUBLIC KEY, one CERTIFICATE. We
        // render structurally (tag + sha256 of the DER body); we don't
        // assert P-256 validity (the daemon owns that on load).
        let pem = "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZI\n-----END PUBLIC KEY-----\n\
                   -----BEGIN CERTIFICATE-----\nMIIBkTCC\n-----END CERTIFICATE-----\n";
        let path = std::path::Path::new("/tmp/trust-roots.pem");
        let out = render_trust_roots(path, pem.as_bytes()).unwrap();
        assert!(out.contains("anchors:     2"));
        assert!(out.contains("[0] PUBLIC KEY  sha256:"));
        assert!(out.contains("[1] CERTIFICATE sha256:"));
        // Fingerprint is 64 lowercase hex chars.
        let fp = sha256_hex(b"abc");
        assert_eq!(fp.len(), 64);
        assert!(fp
            .chars()
            .all(|c| c.is_ascii_hexdigit() && !c.is_ascii_uppercase()));
    }

    #[test]
    fn render_trust_roots_errors_on_empty_pem() {
        let path = std::path::Path::new("/tmp/empty.pem");
        let err = render_trust_roots(path, b"not a pem at all").unwrap_err();
        assert!(format!("{err}").contains("no PEM blocks"));
    }

    #[test]
    fn default_trust_roots_path_ends_in_goat_client_pem() {
        // Resolution may vary by platform/env, but when HOME is set the
        // path must land under a goat-client dir and end in the pem.
        if std::env::var_os("HOME").is_some() {
            let p = default_trust_roots_path().expect("HOME set");
            assert!(p.ends_with("trust-roots.pem"), "got {}", p.display());
            assert!(
                p.to_string_lossy().contains("goat-client"),
                "got {}",
                p.display()
            );
        }
    }

    #[test]
    fn resolve_trust_roots_explicit_path_wins() {
        let p = std::path::Path::new("/tmp/custom-roots.pem");
        assert_eq!(
            resolve_trust_roots_path(Some(p)).unwrap(),
            PathBuf::from("/tmp/custom-roots.pem")
        );
    }

    #[test]
    fn resolve_socket_explicit_override_wins() {
        let p = std::path::Path::new("/tmp/foo.sock");
        assert_eq!(
            resolve_socket(Some(p)).unwrap(),
            PathBuf::from("/tmp/foo.sock")
        );
    }
}

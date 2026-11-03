//! goat-relay — Netbird-relay-compatible relay server (Block 33B+).
//!
//! # Architecture invariants (ADR 0709 D7)
//!
//! Tasks here run on **one core for their lifetime**. Spawn via
//! `tokio::task::spawn_local` only — never `tokio::spawn` (the global
//! multi-threaded API). The workspace `clippy.toml` blocks `tokio::spawn`
//! and `tokio::task::spawn` at the lint layer.
//!
//! Async runtime: tokio with `tokio::runtime::Builder::new_current_thread()`
//! per pinned thread. **Work-stealing is structurally impossible** because
//! no multi-threaded runtime exists in the binary.
//!
//! Library dependency: this binary uses [`goat_relay_protocol`] for
//! parse/serialize/state-machine logic (clean-room implementation per
//! ADR 0709 D1). The library crate is runtime-agnostic; only this binary
//! crate sees tokio.
//!
//! # Status
//!
//! Block 33B step 3a — CLI flags + cert sourcing + main.rs
//! binds the listener.
//!
//! Subsequent commits (step 3b+) add the handshake-FSM driver, in-memory
//! routing table, kTLS hand-off, QUIC listener, keepalive, and bench-env
//! conformance.
//!
//! # Usage
//!
//! ```text
//! goat-relay [--tls-cert <pem-path>] [--tls-key <pem-path>] [--bind <addr>]
//! ```
//!
//! Without `--tls-cert`/`--tls-key`, the binary boots an idle shard with
//! no listener (useful for dev / regression tests / smoke). With both
//! flags, the listener binds and serves WS-over-TLS until SIGINT.
//!
//! Working state checkpoint: `docs/project/block-33-progress.md`.

#![doc(html_root_url = "https://github.com/usareur-af/goatbird")]

use std::fs::File;
use std::io::BufReader;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::Arc;

use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use rustls::ServerConfig;

use goat_relay_protocol::handshake::Validator;
use goat_relay_protocol::messages::MsgType;
use goat_relay_protocol::VERSION as PROTOCOL_VERSION;

use goat_relay::auth::TimedHmacValidator;
use goat_relay::listener::quic::{serve as serve_quic, QuicListenerConfig};
use goat_relay::listener::ws::{serve as serve_ws, WsListenerConfig};
use goat_relay::listener::ws_ktls;
#[cfg(target_os = "linux")]
use goat_relay::listener::ws::serve_uring as serve_ws_uring;
use goat_relay::routing::RoutingTable;
use goat_relay::shard::Shard;

/// Default bind address for the WS-over-TLS listener (matches the
/// netbird relay port).
const DEFAULT_BIND_ADDR: &str = "0.0.0.0:33073";

/// Send-capable carrier from main → shard init. The shard then creates
/// the [`RoutingTable`] inside its own thread (the routing table holds
/// `Rc`, which is `!Send`, and would otherwise block crossing the
/// `thread::spawn` boundary).
struct ListenerSetup {
    bind_addr: SocketAddr,
    tls_config: Arc<ServerConfig>,
    advertised_addr: String,
    /// Relay-client auth validator, built once from the per-site shared
    /// secret and shared (read-only) across every shard's listeners via
    /// `Arc`. Always present in listener mode — the binary fails closed if
    /// no secret is configured (see `run`).
    validator: Arc<dyn Validator + Send + Sync>,
}

/// Parsed CLI flags.
struct Cli {
    /// Path to the TLS cert chain in PEM format. If `None`, no listener
    /// is bound (idle mode).
    tls_cert: Option<PathBuf>,
    /// Path to the TLS private key in PEM format. If `None`, no listener
    /// is bound (idle mode).
    tls_key: Option<PathBuf>,
    /// Bind address; defaults to [`DEFAULT_BIND_ADDR`].
    bind_addr: SocketAddr,
    /// Address peers reach us at — returned in AuthResponse so peer-pair
    /// rendezvous tells the OTHER peer where to connect. For cloud hosts
    /// behind an EIP / NAT / LB this differs from `bind_addr` (relay binds
    /// 0.0.0.0:N internally; advertises public-IP:N to peers). Defaults to
    /// `bind_addr.to_string()` for symmetry with the prior 1-flag CLI.
    advertised_addr: String,
    /// Number of shards to spawn (Block 33C.1 lever L8). Each shard is a
    /// pinned OS thread running its own `current_thread` tokio runtime
    /// and binds the listener with SO_REUSEPORT so the kernel distributes
    /// connections across shards. Defaults to all available cores; clamps
    /// to the available core count when explicitly higher. `--shards 1`
    /// preserves the single-shard pre-L8 behavior.
    shards: Option<usize>,
    /// Block 33C.1 lever L8.b — opt-in SO_REUSEPORT_CBPF source-IP
    /// affinity steering. When set, shard 0 attaches a 3-instruction
    /// classic-BPF program that returns `src_ip % shards` for IPv4
    /// incoming connections, pinning each peer to one shard and
    /// eliminating cross-shard MPSC sync. Linux-only (silently ignored
    /// elsewhere). Default off.
    cbpf_affinity: bool,
    /// Block 50A.5 production-readiness — fail-fast at boot if the kernel
    /// TLS ULP is not available on Linux. Default `false` (warn-only) —
    /// the relay still accepts traffic via userspace TLS, but operators
    /// see a loud startup warning in logs. Setting `--ktls-required`
    /// trades availability for performance-floor enforcement: it makes
    /// the ~30% throughput regression in the userspace fall-back path
    /// non-silent. On non-Linux (macOS dev, Windows)
    /// the flag is a no-op since kTLS was never compiled in.
    ktls_required: bool,
    /// F-041 mitigation — server-side QUIC-preference bias in milliseconds.
    /// When > 0, the WS accept handler sleeps this many ms after `tcp_accept`
    /// returns and BEFORE the TLS handshake starts. This delays the WS
    /// dialer's return on the netbird daemon side; QUIC's ~1-RTT handshake
    /// finishes well within the bias window, so the daemon's race-dialer
    /// (a true first-to-finish race with no QUIC preference) picks QUIC.
    /// Default `0` (off; no behavior change). Recommended `100` for envs
    /// that want deterministic QUIC selection (benches; production with
    /// strong QUIC preference). Cost: paid exactly once per peer per
    /// daemon process lifetime — daemons hold the relay connection after
    /// race wins, no per-message tax.
    prefer_quic_bias_ms: u64,
    /// ADR 1055 white-box stats endpoint bind address. `None` (default) =
    /// disabled — no stats port is opened. When set (recommended
    /// `127.0.0.1:9098`), shard 0 serves a read-only JSON snapshot of the
    /// drop + per-peer-forwarded-byte counters for the on-host self-
    /// publisher (ADR 1058) or an operator SSM pull. Intended for a
    /// LOCALHOST bind — the relay is not a mesh peer, so this must not
    /// become a new public port.
    stats_addr: Option<SocketAddr>,
}

impl Cli {
    /// Hand-rolled arg parser. Avoids a `clap` dep for the small flag
    /// surface; revisit if the surface grows.
    fn parse_from_args() -> Result<Self, String> {
        let mut tls_cert: Option<PathBuf> = None;
        let mut tls_key: Option<PathBuf> = None;
        let mut bind_addr_str: Option<String> = None;
        let mut advertised_addr_str: Option<String> = None;
        let mut shards_str: Option<String> = None;
        let mut cbpf_affinity: bool = false;
        let mut ktls_required: bool = false;
        let mut prefer_quic_bias_ms: u64 = 0;
        let mut stats_addr_str: Option<String> = None;

        let mut args = std::env::args().skip(1);
        while let Some(arg) = args.next() {
            match arg.as_str() {
                "--tls-cert" => {
                    tls_cert = Some(PathBuf::from(args.next().ok_or_else(|| {
                        "--tls-cert requires a path argument".to_string()
                    })?));
                }
                "--tls-key" => {
                    tls_key = Some(PathBuf::from(args.next().ok_or_else(|| {
                        "--tls-key requires a path argument".to_string()
                    })?));
                }
                "--bind" => {
                    bind_addr_str = Some(args.next().ok_or_else(|| {
                        "--bind requires an address argument (e.g., 0.0.0.0:33073)".to_string()
                    })?);
                }
                "--advertised" => {
                    advertised_addr_str = Some(args.next().ok_or_else(|| {
                        "--advertised requires an address argument (e.g., 1.2.3.4:33073)"
                            .to_string()
                    })?);
                }
                "--shards" => {
                    shards_str = Some(args.next().ok_or_else(|| {
                        "--shards requires a positive integer argument (e.g., --shards 4)"
                            .to_string()
                    })?);
                }
                "--cbpf-affinity" => {
                    // Boolean flag — no value follows. Block 33C.1 lever L8.b.
                    cbpf_affinity = true;
                }
                "--ktls-required" => {
                    // Boolean flag — no value follows. Block 50A.5
                    // production-readiness gate: fail-fast on Linux if the
                    // kernel TLS ULP is not loadable.
                    ktls_required = true;
                }
                "--prefer-quic-bias-ms" => {
                    // F-041 mitigation — value-bearing flag. Server-side
                    // bias toward QUIC by delaying WS dialer return on
                    // the netbird daemon's race-dialer.
                    let val = args.next().ok_or_else(|| {
                        "--prefer-quic-bias-ms requires a non-negative integer (e.g., --prefer-quic-bias-ms 100)".to_string()
                    })?;
                    prefer_quic_bias_ms = val.parse().map_err(|e| {
                        format!("--prefer-quic-bias-ms '{val}' is not a non-negative integer: {e}")
                    })?;
                }
                "--stats-addr" => {
                    // ADR 1055 white-box stats endpoint. Value-bearing;
                    // default off. Recommended a localhost bind.
                    stats_addr_str = Some(args.next().ok_or_else(|| {
                        "--stats-addr requires an address argument (e.g., 127.0.0.1:9098)".to_string()
                    })?);
                }
                "--help" | "-h" => {
                    return Err(usage());
                }
                other => {
                    return Err(format!("unknown flag: '{other}'\n\n{}", usage()));
                }
            }
        }

        match (&tls_cert, &tls_key) {
            (Some(_), None) => return Err("--tls-cert provided without --tls-key".into()),
            (None, Some(_)) => return Err("--tls-key provided without --tls-cert".into()),
            _ => {}
        }

        let bind_addr_str = bind_addr_str.unwrap_or_else(|| DEFAULT_BIND_ADDR.to_string());
        let bind_addr: SocketAddr = bind_addr_str
            .parse()
            .map_err(|e| format!("--bind addr '{bind_addr_str}' is not a valid SocketAddr: {e}"))?;

        // Distinct from --bind: this is the address peers reach us at, which
        // for cloud-fronted hosts (EIP, NAT, LB) differs from the bind addr.
        // Defaults to bind_addr for symmetry with the prior 1-flag CLI.
        let advertised_addr = advertised_addr_str.unwrap_or_else(|| bind_addr.to_string());

        let shards = match shards_str {
            None => None,
            Some(s) => {
                let n: usize = s
                    .parse()
                    .map_err(|e| format!("--shards '{s}' is not a positive integer: {e}"))?;
                if n == 0 {
                    return Err("--shards must be >= 1".into());
                }
                Some(n)
            }
        };

        let stats_addr = match stats_addr_str {
            None => None,
            Some(s) => Some(
                s.parse()
                    .map_err(|e| format!("--stats-addr '{s}' is not a valid SocketAddr: {e}"))?,
            ),
        };

        Ok(Cli {
            tls_cert,
            tls_key,
            bind_addr,
            advertised_addr,
            shards,
            cbpf_affinity,
            ktls_required,
            prefer_quic_bias_ms,
            stats_addr,
        })
    }
}

fn usage() -> String {
    "goat-relay — Netbird-relay-compatible relay server (Block 33B+)\n\
     \n\
     USAGE:\n    \
       goat-relay [--tls-cert <pem>] [--tls-key <pem>] [--bind <addr>] [--advertised <addr>] [--shards <n>]\n\
     \n\
     FLAGS:\n    \
       --tls-cert <path>   Path to TLS cert chain (PEM). Required for listener.\n    \
       --tls-key  <path>   Path to TLS private key (PEM). Required for listener.\n    \
       --bind     <addr>   Bind address (default: 0.0.0.0:33073).\n    \
       --advertised <addr> Address peers reach us at (default: bind addr).\n                       \
       Use the public IP for cloud-fronted deploys behind EIP/NAT/LB.\n    \
       --shards <n>        Number of shards (default: number of CPU cores). Each shard\n                       \
       binds the listener with SO_REUSEPORT; the kernel distributes\n                       \
       connections across them.\n    \
       --cbpf-affinity     Block 33C.1 lever L8.b — opt-in SO_REUSEPORT_CBPF source-IP\n                       \
       affinity steering. Pins each peer to a single shard via\n                       \
       src_ip % shards (Linux only; default off).\n    \
       --ktls-required     Block 50A.5 — fail-fast at boot if the kernel TLS ULP is\n                       \
       not available on Linux. Default is warn-only: relay still starts\n                       \
       on userspace TLS but the ~30%-throughput-loss path is silent.\n                       \
       Set this on production hosts where the userspace fall-back is\n                       \
       a real degradation.\n    \
       --prefer-quic-bias-ms <ms>  F-041 mitigation. Sleep <ms> after each tcp_accept\n                       \
       and BEFORE the TLS handshake, biasing the netbird daemon's\n                       \
       race-dialer toward QUIC (delays WS dialer's return so QUIC's\n                       \
       1-RTT handshake wins). Default 0 (off). Recommended 100 for\n                       \
       envs that want deterministic QUIC selection. Cost paid once\n                       \
       per peer per daemon process lifetime.\n    \
       --stats-addr <addr> ADR 1055 white-box stats endpoint (default off). When set,\n                       \
       shard 0 serves a read-only JSON snapshot of the drop +\n                       \
       per-peer-forwarded-byte counters. Use a localhost bind\n                       \
       (127.0.0.1:9098) for the on-host self-publisher / SSM pull;\n                       \
       the relay is not a mesh peer, so do NOT make it public.\n    \
       --help              Print this help.\n\
     \n\
     ENVIRONMENT:\n    \
       GOAT_RELAY_AUTH_SECRET   Per-site relay shared secret for relay-client auth\n                       \
       (netbird timed-HMAC v2; key = SHA256 of this value). REQUIRED\n                       \
       when a listener is bound — the relay fails closed (refuses to\n                       \
       start) without it, since an un-authenticated public relay is an\n                       \
       open relay. Read from the environment only, never argv. Same\n                       \
       secret the clients + legacy relay tier use (site relay_secret).\n    \
       GOAT_RELAY_REQUIRE_PEER_BOUND  Set to reject unbound (algo-1) tokens and\n                       \
       require peer-ID-bound (algo-2) tokens only — ADR 1021 Phase C.\n                       \
       Default unset = dual-accept (both), the migration window. Flip\n                       \
       only once the whole fleet issues peer-bound tokens.\n    \
       GOAT_RELAY_USE_URING     Set to opt into the io_uring data plane (Linux; ADR 0711).\n\
     \n\
     With both --tls-cert and --tls-key set, binds the WS-over-TLS + QUIC listeners\n\
     (and requires GOAT_RELAY_AUTH_SECRET).\n\
     Without either, boots idle shards (useful for smoke / regression tests).\n"
        .into()
}

fn main() -> ExitCode {
    let cli = match Cli::parse_from_args() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("{e}");
            return ExitCode::from(2);
        }
    };

    eprintln!(
        "goat-relay {} — Block 33B step 5 (kTLS scaffolding cfg-gated to Linux; userspace on others)",
        env!("CARGO_PKG_VERSION")
    );
    eprintln!(
        "wire-protocol library: goat-relay-protocol {} (Transport msg type = {:?})",
        PROTOCOL_VERSION,
        MsgType::Transport
    );

    // Block 50A.5 production-readiness — boot-time kTLS regression guard.
    // Probe the kernel TLS ULP availability NOW, before any listener
    // accepts a connection, so the verdict appears in startup logs and
    // operators have the option to fail-fast on the userspace fall-back
    // path (which is silent in every other respect — accepts traffic but
    // costs ~30% throughput per M-009 / M-019).
    let ktls_verdict = ws_ktls::probe_at_boot();
    if matches!(ktls_verdict, ws_ktls::KtlsBootVerdict::Unavailable) && cli.ktls_required {
        eprintln!(
            "error: --ktls-required was set but the kernel TLS ULP is not available; \
             check /etc/modules-load.d/goat-relay-tls.conf. \
             Refusing to start in performance-degraded mode."
        );
        return ExitCode::from(3);
    }

    let cores = match core_affinity::get_core_ids() {
        Some(c) if !c.is_empty() => c,
        _ => {
            eprintln!("error: no CPU cores reported by core_affinity::get_core_ids");
            return ExitCode::from(1);
        }
    };

    // Block 33C.1 lever L8 — N shards each pinned to its own core, each
    // binding the listener with SO_REUSEPORT. Default N = number of
    // available cores; --shards <n> overrides + clamps to the available
    // count.
    let n_shards = cli.shards.unwrap_or(cores.len()).min(cores.len());
    // Block 33C.1 lever L8.b — captured here so the per-shard closure
    // (move) can reference it.
    let cbpf_affinity = cli.cbpf_affinity;
    // ADR 1055 stats endpoint bind addr (Copy) — same hoist so each shard's
    // move closure copies it; only shard 0 actually binds it.
    let stats_addr = cli.stats_addr;
    if cbpf_affinity {
        eprintln!(
            "L8.b CBPF source-IP affinity steering: ENABLED (shard 0 will attach src_ip % {n_shards} program)"
        );
    }
    eprintln!(
        "booting N={n_shards} shard(s) pinned to cores {:?} (of {} available)",
        cores
            .iter()
            .take(n_shards)
            .map(|c| c.id)
            .collect::<Vec<_>>(),
        cores.len()
    );

    // Block 33C.1 lever L5 (ADR 0711) — operator opt-in via env var.
    // Linux-only; on other platforms the flag is ignored. Both paths
    // coexist in the binary; flipping back is one env-var unset away.
    #[cfg(target_os = "linux")]
    let use_uring = std::env::var_os("GOAT_RELAY_USE_URING").is_some();
    #[cfg(not(target_os = "linux"))]
    let use_uring = false;
    if use_uring {
        eprintln!("ws data plane: io_uring (L5 path; userspace TLS; GOAT_RELAY_USE_URING set)");
    } else {
        eprintln!("ws data plane: mio + epoll (default; kTLS hand-off applies on Linux)");
    }

    // Block 33C.1 lever L8 — single shared routing table across all
    // shards (Arc<RwLock> internals; Send + Sync since the L8 part-1
    // refactor). Each shard's listener tasks Clone this handle, so a
    // peer registered on shard A is reachable from a forward initiated
    // on shard B.
    let routing_table = RoutingTable::new();

    // Per-peer counting gate. It runs on the dispatch hot path, so pay
    // for it only when wanted: DEFAULT = on iff we serve the ADR 1055 stats
    // endpoint (`--stats-addr`; no point counting what nothing reads). The
    // GOAT_RELAY_PEER_STATS env var force-overrides either way (0/off, 1/on) —
    // the runtime knob to turn the cost off (or on for pull-only measurement)
    // without touching the endpoint.
    let peer_counting = match std::env::var("GOAT_RELAY_PEER_STATS").ok().as_deref() {
        Some("1") | Some("true") | Some("on") => true,
        Some("0") | Some("false") | Some("off") => false,
        _ => cli.stats_addr.is_some(),
    };
    routing_table.set_peer_counting(peer_counting);
    eprintln!(
        "per-peer stats counting: {} (ADR 1055 §2b; F-235 hot-path gate)",
        if peer_counting { "ENABLED" } else { "disabled" }
    );

    let listener_setup = match (cli.tls_cert.as_ref(), cli.tls_key.as_ref()) {
        (Some(cert_path), Some(key_path)) => {
            // Fail-closed relay-client authentication. The per-site shared
            // secret is sourced from the environment, NEVER argv (argv is
            // world-readable via /proc/<pid>/cmdline + `ps`). It is the
            // same `relay_secret` handed to clients and the legacy relay
            // tier; the validator is wire-compatible with netbird relay
            // timed-HMAC v2. A listener that cannot authenticate its
            // clients must not bind — otherwise the relay is an open relay
            // (anyone can register/evict peer-ID routing slots). See the
            // Block 33 relay design doc + the relay-client-auth finding.
            let auth_secret = match std::env::var("GOAT_RELAY_AUTH_SECRET") {
                Ok(s) if !s.is_empty() => s,
                _ => {
                    eprintln!(
                        "error: GOAT_RELAY_AUTH_SECRET is unset or empty but a listener was \
                         requested (--tls-cert/--tls-key given). goat-relay refuses to start \
                         without relay-client authentication (fail-closed). Set the per-site \
                         relay shared secret in the environment (it is the same secret the \
                         clients + legacy relay tier use)."
                    );
                    return ExitCode::from(2);
                }
            };
            // ADR 1021 migration policy. Default (dual-accept) takes both
            // unbound (algo 1) and peer-bound (algo 2) tokens so relays can
            // upgrade ahead of the management server. Setting
            // GOAT_RELAY_REQUIRE_PEER_BOUND=1 enforces peer-bound-only
            // (Phase C) — flip it only once the whole fleet issues algo-2
            // tokens, or every peer is locked out.
            let require_peer_bound = std::env::var_os("GOAT_RELAY_REQUIRE_PEER_BOUND").is_some();
            // Optional secret-rotation overlap window: when
            // GOAT_RELAY_AUTH_SECRET_PREVIOUS is set, accept tokens signed
            // by either the current OR the previous secret so a per-site
            // secret rotation does not hard-cut every in-flight token. Drop
            // the env var once the fleet has re-minted on the new secret.
            let previous_secret = std::env::var("GOAT_RELAY_AUTH_SECRET_PREVIOUS")
                .ok()
                .filter(|s| !s.is_empty());
            let validator: Arc<dyn Validator + Send + Sync> = Arc::new(match &previous_secret {
                Some(prev) => TimedHmacValidator::with_rotation(
                    auth_secret.as_bytes(),
                    prev.as_bytes(),
                    !require_peer_bound,
                ),
                None => TimedHmacValidator::with_policy(auth_secret.as_bytes(), !require_peer_bound),
            });
            let mode = if require_peer_bound {
                "peer-bound-only (ADR 1021 Phase C)"
            } else {
                "dual-accept (unbound + peer-bound)"
            };
            let rotation = if previous_secret.is_some() {
                " + rotation window (current+previous secret)"
            } else {
                ""
            };
            eprintln!(
                "relay-client auth: ENABLED (netbird timed-HMAC, key = SHA256(GOAT_RELAY_AUTH_SECRET); policy = {mode}{rotation})"
            );
            match build_server_config(cert_path, key_path) {
                Ok(tls_config) => Some(Arc::new(ListenerSetup {
                    bind_addr: cli.bind_addr,
                    tls_config,
                    advertised_addr: cli.advertised_addr.clone(),
                    validator,
                })),
                Err(e) => {
                    eprintln!("error: failed to build TLS config: {e}");
                    return ExitCode::from(1);
                }
            }
        }
        _ => {
            eprintln!("no --tls-cert/--tls-key provided; running in IDLE mode (no listener)");
            None
        }
    };

    let mut shards: Vec<Shard> = Vec::with_capacity(n_shards);
    for (shard_idx, core) in cores.iter().take(n_shards).enumerate().map(|(i, c)| (i, *c)) {
        let setup_clone = listener_setup.clone();
        let routing_clone = routing_table.clone();

        let shard_result = Shard::spawn(core, move |shutdown_rx| async move {
            match setup_clone {
                Some(setup) => {
                    eprintln!(
                        "shard {shard_idx} (core {}): binding listeners on {} (SO_REUSEPORT)",
                        core.id, setup.bind_addr
                    );

                    // Convert the oneshot shutdown receiver into a
                    // watch-channel "cancel signal" that both listeners can
                    // observe. watch (vs Notify) is safe across late
                    // subscribers — the value persists.
                    let (signal_tx, signal_rx) = tokio::sync::watch::channel(false);
                    let signal_forwarder = tokio::task::spawn_local(async move {
                        let _ = shutdown_rx.await;
                        let _ = signal_tx.send(true);
                    });

                    let make_cancel =
                        |mut rx: tokio::sync::watch::Receiver<bool>| async move {
                            loop {
                                if *rx.borrow() {
                                    return;
                                }
                                if rx.changed().await.is_err() {
                                    return;
                                }
                            }
                        };

                    let ws_cfg = WsListenerConfig {
                        bind_addr: setup.bind_addr,
                        tls_config: Arc::clone(&setup.tls_config),
                        advertised_addr: setup.advertised_addr.clone(),
                        routing_table: routing_clone.clone(),
                        // L8.b CBPF steering — group-wide attach by shard 0
                        // only. Other shards pass `None`. When the operator
                        // hasn't opted in, all shards pass `None`.
                        cbpf_steering_num_sockets: if cbpf_affinity && shard_idx == 0 {
                            Some(n_shards as u32)
                        } else {
                            None
                        },
                        prefer_quic_bias_ms: cli.prefer_quic_bias_ms,
                        validator: Arc::clone(&setup.validator),
                    };
                    // Clone the shared routing handle for the ADR 1055 stats
                    // endpoint BEFORE quic_cfg moves it (shard 0 only uses it).
                    let routing_for_stats = routing_clone.clone();
                    let quic_cfg = QuicListenerConfig {
                        bind_addr: setup.bind_addr,
                        tls_config: Arc::clone(&setup.tls_config),
                        advertised_addr: setup.advertised_addr.clone(),
                        routing_table: routing_clone,
                        validator: Arc::clone(&setup.validator),
                        keepalive_timings: goat_relay::keepalive::KeepaliveTimings::default(),
                    };

                    let (ws_bound_tx, _ws_bound_rx) = tokio::sync::oneshot::channel();
                    let (quic_bound_tx, _quic_bound_rx) = tokio::sync::oneshot::channel();
                    // Extra cancel branch for the shard-0 stats endpoint.
                    let stats_signal_rx = signal_rx.clone();
                    let ws_cancel = make_cancel(signal_rx.clone());
                    let quic_cancel = make_cancel(signal_rx);

                    let ws_task = tokio::task::spawn_local(async move {
                        // L5 path opt-in: io_uring data plane on Linux when
                        // GOAT_RELAY_USE_URING is set; otherwise the default
                        // mio + (kTLS on Linux) path. ADR 0711.
                        let result = {
                            #[cfg(target_os = "linux")]
                            {
                                if use_uring {
                                    serve_ws_uring(ws_cfg, ws_cancel, ws_bound_tx).await
                                } else {
                                    serve_ws(ws_cfg, ws_cancel, ws_bound_tx).await
                                }
                            }
                            #[cfg(not(target_os = "linux"))]
                            {
                                let _ = use_uring; // suppress unused warning
                                serve_ws(ws_cfg, ws_cancel, ws_bound_tx).await
                            }
                        };
                        match result {
                            Ok(()) => {
                                eprintln!("shard {shard_idx}: ws listener exited cleanly")
                            }
                            Err(e) => eprintln!(
                                "shard {shard_idx}: ws listener exited with error: {e}"
                            ),
                        }
                    });
                    let quic_task = tokio::task::spawn_local(async move {
                        match serve_quic(quic_cfg, quic_cancel, quic_bound_tx).await {
                            Ok(()) => {
                                eprintln!("shard {shard_idx}: quic listener exited cleanly")
                            }
                            Err(e) => eprintln!(
                                "shard {shard_idx}: quic listener exited with error: {e}"
                            ),
                        }
                    });

                    // ADR 1055 white-box stats endpoint — a single instance
                    // on shard 0 reading the shared routing table. Default
                    // off (only when --stats-addr is set). Non-fatal: a bind
                    // failure inside serve_stats logs and returns without
                    // touching the data plane.
                    let stats_task = match (shard_idx, stats_addr) {
                        (0, Some(saddr)) => {
                            let stats_cancel = make_cancel(stats_signal_rx);
                            Some(tokio::task::spawn_local(async move {
                                goat_relay::stats::serve_stats(saddr, routing_for_stats, stats_cancel)
                                    .await;
                            }))
                        }
                        _ => {
                            // Not shard 0 (or disabled): release the unused clones.
                            drop(routing_for_stats);
                            drop(stats_signal_rx);
                            None
                        }
                    };

                    let _ = ws_task.await;
                    let _ = quic_task.await;
                    if let Some(t) = stats_task {
                        let _ = t.await;
                    }
                    let _ = signal_forwarder.await;
                }
                None => {
                    let _ = shutdown_rx.await;
                }
            }
        });

        match shard_result {
            Ok(s) => shards.push(s),
            Err(e) => {
                eprintln!("error: Shard::spawn (idx {shard_idx}, core {}) failed: {e}", core.id);
                // Best-effort shutdown of any already-spawned shards.
                for s in shards {
                    s.shutdown();
                }
                return ExitCode::from(1);
            }
        }
    }

    eprintln!("{n_shards} shard(s) up; awaiting ctrl-c");

    let supervisor = match tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
    {
        Ok(rt) => rt,
        Err(e) => {
            eprintln!("error: supervisor runtime build failed: {e}");
            for s in shards {
                s.shutdown();
            }
            return ExitCode::from(1);
        }
    };
    // F-223: shut down on SIGTERM as well as SIGINT. systemd / Docker /
    // Kubernetes stop a process with SIGTERM, NOT SIGINT — without a
    // SIGTERM handler every production stop (incl. a relay rotation, which
    // is a restart) is a hard kill that bypasses the shard-shutdown drain
    // below. Whichever signal fires first proceeds to graceful shutdown.
    supervisor.block_on(async {
        #[cfg(unix)]
        {
            use tokio::signal::unix::{signal, SignalKind};
            match signal(SignalKind::terminate()) {
                Ok(mut term) => {
                    tokio::select! {
                        r = tokio::signal::ctrl_c() => {
                            if let Err(e) = r {
                                eprintln!("warning: ctrl_c handler failed: {e}");
                            }
                            eprintln!("SIGINT received; signaling shard shutdown");
                        }
                        _ = term.recv() => {
                            eprintln!("SIGTERM received; signaling shard shutdown");
                        }
                    }
                }
                Err(e) => {
                    eprintln!("warning: SIGTERM handler install failed: {e} — SIGINT only");
                    let _ = tokio::signal::ctrl_c().await;
                    eprintln!("SIGINT received; signaling shard shutdown");
                }
            }
        }
        #[cfg(not(unix))]
        {
            if let Err(e) = tokio::signal::ctrl_c().await {
                eprintln!("warning: ctrl_c handler failed: {e} — proceeding to shutdown");
            }
            eprintln!("ctrl-c received; signaling shard shutdown");
        }
    });

    for s in shards {
        s.shutdown();
    }
    eprintln!("all shards down; exit 0");
    ExitCode::SUCCESS
}

/// Load the cert chain (PEM) and private key (PEM) from disk and build a
/// rustls [`ServerConfig`] backed by the ring crypto provider (ADR 0709
/// D8). The config is **QUIC-compatible**: TLS 1.3 only and
/// `max_early_data_size = u32::MAX`. The same config drives both the WS
/// listener (which is happy with TLS 1.3 only) and the QUIC listener
/// (which requires TLS 1.3 + the early-data-size constraint).
fn build_server_config(
    cert_path: &std::path::Path,
    key_path: &std::path::Path,
) -> Result<Arc<ServerConfig>, String> {
    let _ = rustls::crypto::ring::default_provider().install_default();

    let certs = load_certs(cert_path)?;
    if certs.is_empty() {
        return Err(format!(
            "no certificates found in '{}' (expected at least one PEM CERTIFICATE block)",
            cert_path.display()
        ));
    }
    let key = load_key(key_path)?;

    // TLS 1.3-only matches both upstream's QUIC requirement and our
    // production posture (1.2 deprecated). max_early_data_size = u32::MAX
    // is required by quinn's QuicServerConfig::try_from.
    let mut config = ServerConfig::builder_with_protocol_versions(&[&rustls::version::TLS13])
        .with_no_client_auth()
        .with_single_cert(certs, key)
        .map_err(|e| format!("rustls server config build: {e}"))?;
    config.max_early_data_size = u32::MAX;
    // F-022: rustls 0.23 defaults `enable_secret_extraction = false`
    // (security-relevant opt-in). `ktls::config_ktls_server` calls
    // `dangerous_extract_secrets` and fails with `ExportSecrets` if
    // the opt-in is missing. Required for the cfg(target_os = "linux")
    // kTLS hand-off in `listener/ws_ktls.rs`. No effect on the macOS
    // userspace path. See docs/findings/F-022-* for the full writeup.
    config.enable_secret_extraction = true;
    // F-042 (filed): netbird daemon's QUIC dialer requires
    // ALPN string `"nb-quic"` (per `shared/relay/tls/alpn.go`). Without
    // this, the daemon's QUIC dial fails immediately with
    // `CRYPTO_ERROR 0x178 — peer doesn't support any known protocol`
    // (TLS alert 120 = no_application_protocol), and the race-dialer
    // falls through to WebSocket. This is the root cause of the
    // "QUIC always loses" symptom that drove the F-041 prefer-quic
    // bias work — the bias was solving a different problem than the
    // one we had. ALPN advertised on BOTH WS and QUIC paths because
    // both use the same `ServerConfig`; netbird's WS dialer doesn't
    // require ALPN, so this is harmless on the WS path.
    config.alpn_protocols = vec![b"nb-quic".to_vec()];
    Ok(Arc::new(config))
}

/// Read all PEM-encoded CERTIFICATE blocks from `path`.
fn load_certs(path: &std::path::Path) -> Result<Vec<CertificateDer<'static>>, String> {
    let file = File::open(path)
        .map_err(|e| format!("open cert file '{}': {e}", path.display()))?;
    let mut reader = BufReader::new(file);
    rustls_pemfile::certs(&mut reader)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| format!("parse cert PEM '{}': {e}", path.display()))
}

/// Read the first PEM-encoded private key block from `path`. Accepts
/// PKCS#8, RSA-only, or SEC1 EC key formats (rustls-pemfile coverage).
fn load_key(path: &std::path::Path) -> Result<PrivateKeyDer<'static>, String> {
    let file = File::open(path)
        .map_err(|e| format!("open key file '{}': {e}", path.display()))?;
    let mut reader = BufReader::new(file);
    rustls_pemfile::private_key(&mut reader)
        .map_err(|e| format!("parse key PEM '{}': {e}", path.display()))?
        .ok_or_else(|| {
            format!(
                "no private key found in '{}' (expected PKCS#8, RSA, or SEC1 PEM block)",
                path.display()
            )
        })
}

#[cfg(test)]
mod tests {
    use super::*;

    use rcgen::CertificateParams;
    use std::io::Write;

    /// Generate a self-signed cert + key and write them as PEM to a
    /// tempdir, then load them back via build_server_config and verify
    /// the rustls ServerConfig builds.
    #[test]
    fn pem_roundtrip_self_signed() {
        let key_pair = rcgen::KeyPair::generate().expect("rcgen key");
        let cert = CertificateParams::new(vec!["localhost".to_string()])
            .expect("rcgen params")
            .self_signed(&key_pair)
            .expect("self-sign");
        let cert_pem = cert.pem();
        let key_pem = key_pair.serialize_pem();

        let tmp = std::env::temp_dir().join(format!(
            "goat-relay-test-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_nanos())
                .unwrap_or(0)
        ));
        std::fs::create_dir_all(&tmp).expect("mkdir");
        let cert_path = tmp.join("cert.pem");
        let key_path = tmp.join("key.pem");
        File::create(&cert_path)
            .expect("create cert pem")
            .write_all(cert_pem.as_bytes())
            .expect("write cert");
        File::create(&key_path)
            .expect("create key pem")
            .write_all(key_pem.as_bytes())
            .expect("write key");

        let _config = build_server_config(&cert_path, &key_path)
            .expect("server config builds from PEM-on-disk");

        // Cleanup.
        let _ = std::fs::remove_dir_all(&tmp);
    }
}

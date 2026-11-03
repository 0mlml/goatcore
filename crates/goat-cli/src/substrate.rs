//! The Substrate seam (docs/design/goat-cli-onboarding-and-delight.md):
//! a single selection point that backs the onboarding/learning commands
//! with either a real fabric or an in-process sandbox simulator, so
//! `goat hello` — and later `init` / `tutor` — run identically against a
//! real env or against nothing at all (a plane, a classroom, before a
//! bundle is even issued).
//!
//! Enum dispatch rather than a trait object keeps it dependency-free (no
//! async-trait) and the surface small; it grows methods as init/tutor
//! land. The sandbox NEVER touches a network and is always marked as
//! simulated by the caller — it must never be mistaken for a real mesh.

use std::time::{Duration, Instant};

/// Which backing the onboarding commands run against.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    Sandbox,
    Real,
}

/// One narrated hop of a `goat hello` round-trip.
#[derive(Debug, Clone, serde::Serialize)]
pub struct Hop {
    pub label: &'static str,
    pub detail: String,
    pub elapsed_ms: u128,
}

/// The outcome of [`Substrate::hello`] — a small structured proof rendered
/// either as friendly narrated hops (pretty) or one JSON object (`--json`,
/// machine-clean: `mode` distinguishes a sandbox proof from a real one so
/// a CI smoke can assert on it).
#[derive(Debug, Clone, serde::Serialize)]
pub struct HelloResult {
    pub mode: &'static str,
    pub topic: String,
    pub payload: String,
    pub round_trip: bool,
    pub hops: Vec<Hop>,
}

/// The selected backing. `Real` borrows the profile at call time, so the
/// enum itself carries no state.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Substrate {
    Sandbox,
    Real,
}

impl Substrate {
    /// Choose the backing: an explicit `--sandbox`, or the *absence* of a
    /// profile, selects the sandbox; otherwise the real fabric.
    ///
    /// `Denied` is an error here rather than a third path into the sandbox.
    /// An unreadable profile is a profile that exists, so silently narrating
    /// a simulated round-trip against it would tell the operator the fabric
    /// is fine while they are nowhere near it — the same wrong-hint bug this
    /// module's `init_state` probe exists to kill, in its quietest form.
    /// An explicit `--sandbox` still wins: asking for the simulator is not
    /// a claim about the profile.
    pub fn select(
        force_sandbox: bool,
        profile: &crate::profile::Profile,
    ) -> Result<Self, crate::errors::GoatError> {
        if force_sandbox {
            return Ok(Substrate::Sandbox);
        }
        match profile.init_state() {
            crate::profile::InitState::Initialized => Ok(Substrate::Real),
            crate::profile::InitState::Absent => Ok(Substrate::Sandbox),
            crate::profile::InitState::Denied => {
                profile.require_initialized().map(|()| Substrate::Real)
            }
        }
    }

    pub fn mode(&self) -> Mode {
        match self {
            Substrate::Sandbox => Mode::Sandbox,
            Substrate::Real => Mode::Real,
        }
    }

    /// Round-trip a greeting through the fabric. `who` is the operator's
    /// short id (used in the topic key). The payload is always `lo` — the
    /// first message ever sent over a network (ARPANET, 29 Oct 1969; the
    /// operators typed "LOGIN" and the link crashed after two letters).
    pub async fn hello(
        &self,
        who: &str,
        profile: &crate::profile::Profile,
    ) -> anyhow::Result<HelloResult> {
        match self {
            Substrate::Sandbox => Ok(sandbox_hello(who).await),
            Substrate::Real => real_hello(who, profile).await,
        }
    }
}

/// Simulated round-trip: no network, no profile. Realistic-feeling timing
/// so the narration reads like the real thing, but every hop is explicitly
/// `(simulated)` and the caller prints a SANDBOX banner.
async fn sandbox_hello(who: &str) -> HelloResult {
    let start = Instant::now();
    let topic = format!("hello/{who}");
    let mut hops = Vec::new();

    tokio::time::sleep(Duration::from_millis(40)).await;
    hops.push(Hop {
        label: "portal",
        detail: format!("accepted topic `{topic}` (simulated)"),
        elapsed_ms: start.elapsed().as_millis(),
    });
    tokio::time::sleep(Duration::from_millis(30)).await;
    hops.push(Hop {
        label: "fabric",
        detail: format!("routed `lo` to `{topic}` over the WireGuard P2P path (simulated)"),
        elapsed_ms: start.elapsed().as_millis(),
    });
    tokio::time::sleep(Duration::from_millis(25)).await;
    hops.push(Hop {
        label: "subscriber",
        detail: "your subscriber received `lo` — round-trip complete (simulated)".to_string(),
        elapsed_ms: start.elapsed().as_millis(),
    });

    HelloResult {
        mode: "sandbox",
        topic,
        payload: "lo".to_string(),
        round_trip: true,
        hops,
    }
}

/// Real round-trip: open the profile's mTLS session (which proves identity
/// acceptance against the router ACL — the hard part), subscribe to
/// `hello/<who>`, publish `lo`, and wait briefly for the echo. An echo
/// proves pub/sub mechanics end-to-end; no echo (e.g. write-ACL/locality)
/// still leaves a live session, reported honestly rather than as success.
async fn real_hello(who: &str, profile: &crate::profile::Profile) -> anyhow::Result<HelloResult> {
    let start = Instant::now();
    let topic = format!("hello/{who}");

    let session = crate::cmd::pubsub::open_session_for(profile).await?;
    let mut hops = Vec::new();
    hops.push(Hop {
        label: "session",
        detail: "mTLS session opened — your identity was accepted by the router ACL".to_string(),
        elapsed_ms: start.elapsed().as_millis(),
    });

    let router = profile.router_endpoint_label();
    let subscriber = session
        .declare_subscriber(topic.clone())
        .await
        .map_err(|e| crate::classify::classify_zenoh_err(e, &router, "declare a subscriber"))?;
    // Give the subscriber a moment to wire up before publishing.
    tokio::time::sleep(Duration::from_millis(60)).await;

    session
        .put(topic.clone(), "lo")
        .await
        .map_err(|e| crate::classify::classify_zenoh_err(e, &router, "publish"))?;
    hops.push(Hop {
        label: "fabric",
        detail: format!("published `lo` to `{topic}`"),
        elapsed_ms: start.elapsed().as_millis(),
    });

    let round_trip = match tokio::time::timeout(Duration::from_secs(3), subscriber.recv_async()).await
    {
        Ok(Ok(sample)) => {
            let n = sample.payload().to_bytes().len();
            hops.push(Hop {
                label: "subscriber",
                detail: format!("received {n} byte(s) back — round-trip confirmed"),
                elapsed_ms: start.elapsed().as_millis(),
            });
            true
        }
        _ => {
            hops.push(Hop {
                label: "subscriber",
                detail: "no echo within 3s — session is live, but the round-trip did not return \
                         (write-ACL or locality)"
                    .to_string(),
                elapsed_ms: start.elapsed().as_millis(),
            });
            false
        }
    };

    let _ = session.close().await;
    Ok(HelloResult {
        mode: "real",
        topic,
        payload: "lo".to_string(),
        round_trip,
        hops,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn sandbox_hello_round_trips_lo() {
        let r = sandbox_hello("default").await;
        assert_eq!(r.mode, "sandbox");
        assert_eq!(r.payload, "lo");
        assert!(r.round_trip);
        assert_eq!(r.topic, "hello/default");
        // Three narrated hops, monotonic elapsed.
        assert_eq!(r.hops.len(), 3);
        assert!(r.hops[0].elapsed_ms <= r.hops[2].elapsed_ms);
        // The JSON proof is machine-clean and carries the mode.
        let j = serde_json::to_string(&r).unwrap();
        assert!(j.contains("\"mode\":\"sandbox\""));
        assert!(j.contains("\"payload\":\"lo\""));
    }

    /// The three-way select, including the case that used to sandbox
    /// silently: a profile that is present but unreadable.
    ///
    /// Unix-only: the Denied leg is set up with POSIX mode bits. `select`
    /// itself is portable — `init_state` probes with `File::open`, and
    /// Windows surfaces its own denial as `PermissionDenied` the same way.
    #[cfg(unix)]
    #[test]
    fn select_errors_on_denied_rather_than_sandboxing() {
        use crate::profile::Profile;
        use std::os::unix::fs::PermissionsExt;

        let tmp = tempfile::tempdir().unwrap();
        let p = Profile {
            dir: tmp.path().to_path_buf(),
            context: "t".to_string(),
        };

        // Absent: no profile at all is the designed sandbox default.
        assert_eq!(Substrate::select(false, &p).unwrap().mode(), Mode::Sandbox);

        std::fs::create_dir_all(p.context_dir()).unwrap();
        std::fs::write(p.profile_toml_path(), "portal_endpoint = \"x\"\n").unwrap();
        assert_eq!(Substrate::select(false, &p).unwrap().mode(), Mode::Real);

        // Denied: readable-by-nobody profile.toml. Hard-fails with the
        // error that names the real condition, instead of narrating a
        // simulated round-trip over a profile that is right there.
        let mode = |path: &std::path::Path, bits| {
            std::fs::set_permissions(path, std::fs::Permissions::from_mode(bits)).unwrap()
        };
        mode(&p.profile_toml_path(), 0o000);
        if std::fs::File::open(p.profile_toml_path()).is_ok() {
            // Running as root: permissions don't bind, Denied is unobservable.
            mode(&p.profile_toml_path(), 0o600);
            return;
        }
        let err = Substrate::select(false, &p).unwrap_err();
        assert_eq!(err.code(), "E_PROFILE_DENIED");
        // …but an explicit --sandbox is still an explicit request.
        assert_eq!(Substrate::select(true, &p).unwrap().mode(), Mode::Sandbox);
        mode(&p.profile_toml_path(), 0o600); // so tempdir cleanup works
    }
}

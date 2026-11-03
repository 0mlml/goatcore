// `init`'s derivations — read the running system, never ask.
//
// THE RULE THIS MODULE EXISTS TO ENFORCE. A capability typed by an operator is
// a claim; a capability read from the running system is a fact. A package is
// reviewed by people who cannot check it against the machine it describes, so
// a claim that is wrong stays wrong until the link fails. Every
// `capabilities.*` member below is therefore MEASURED, and this module refuses
// to read any of them from configuration — a declaration file that sets one is
// an error, not an override.
//
// WHERE A VALUE GENUINELY CANNOT BE DERIVED, THE DERIVATION FAILS AND NAMES
// WHAT IS MISSING. It is never prompted for and never defaulted: a silent
// default is how a link ends up with no rate limit (§3).
//
// The line between the two halves:
//
//   MEASURED, and refused from the file:
//     capabilities.transports        what is installed and configured
//     capabilities.fabric_fips_image the running router's image
//     capabilities.fabric_version    the running router
//     capabilities.pqc_capable       whether Rosenpass is actually running
//     transport_offer.public_key     the federation interface's own key
//
//   DECLARED, in a file that is reviewed the way production configuration is:
//     sovereign_id, the term, the cut contact, and the link parameters that
//     are agreed between two parties rather than observed on one — an
//     endpoint, a link subnet, a measured path MTU, the release set and the
//     accept limits.
//
// `sovereign_id` is in the second list because it is not a capability. It is
// the organisation's name, taken from ISO 3166-1 (§3), and no probe of a
// running system produces it.

use std::path::Path;
use std::process::Command;

/// The declaration file, beside the profile it belongs to.
pub const DECLARATION_FILE: &str = "federation.toml";

/// The measured members. Naming one of these in the declaration file is an
/// error: it would turn a fact into a claim, silently.
pub const MEASURED_ONLY: [&str; 5] = [
    "capabilities.transports",
    "capabilities.fabric_fips_image",
    "capabilities.fabric_version",
    "capabilities.pqc_capable",
    "transport.public_key",
];

/// The container the fabric's router runs as, fleet-wide
/// (ops/ansible/roles/goat-zenoh).
const ROUTER_CONTAINER: &str = "goat-zenoh";

/// One derived value, with the source it came from — or the reason it could
/// not be derived. `source` is printed on success so a reviewer can re-run the
/// same read; `hint` is printed on failure and says what is missing.
#[derive(Debug, Clone)]
pub struct Derived<T> {
    pub field: &'static str,
    pub outcome: Result<T, String>,
    pub source: String,
}

impl<T> Derived<T> {
    fn ok(field: &'static str, value: T, source: impl Into<String>) -> Self {
        Derived {
            field,
            outcome: Ok(value),
            source: source.into(),
        }
    }
    fn fail(field: &'static str, why: impl Into<String>, source: impl Into<String>) -> Self {
        Derived {
            field,
            outcome: Err(why.into()),
            source: source.into(),
        }
    }
}

// ---------------------------------------------------------------------------
// Identity — from the profile's own credentials
// ---------------------------------------------------------------------------

/// `identity.trust_domain` — the SPIFFE trust domain out of the profile leaf
/// certificate's URI subject-alternative name. Derived rather than declared
/// because the certificate is what a counterparty's edge will actually see.
pub fn trust_domain(cert_pem: &Path) -> Derived<String> {
    const FIELD: &str = "identity.trust_domain";
    let src = cert_pem.display().to_string();
    // NotFound/IsADirectory/PermissionDenied were all collapsed into one
    // "run profile init" remedy — correct for the first, wrong for the
    // other two (a permission problem needs a chown/ownership fix, not a
    // re-init, and re-init would just fail the same way again).
    let bytes = match std::fs::read(cert_pem) {
        Ok(b) => b,
        Err(e) if e.kind() == std::io::ErrorKind::PermissionDenied => {
            return Derived::fail(
                FIELD,
                format!(
                    "profile certificate at {src} exists but is not readable by this user \
                     (permission denied) — fix ownership/permissions, not `profile init`"
                ),
                src,
            );
        }
        Err(_) => {
            return Derived::fail(
                FIELD,
                format!(
                    "no profile certificate at {src}. Run `goat profile init <bundle.cbor>` first"
                ),
                src,
            );
        }
    };
    let Ok(pem) = pem::parse(&bytes) else {
        return Derived::fail(FIELD, "the profile certificate is not readable PEM", src);
    };
    let der = pem.contents();
    let Ok((_, cert)) = x509_parser::parse_x509_certificate(der) else {
        return Derived::fail(FIELD, "the profile certificate is not readable X.509", src);
    };
    let mut uris: Vec<String> = Vec::new();
    if let Ok(Some(san)) = cert.tbs_certificate.subject_alternative_name() {
        for name in &san.value.general_names {
            if let x509_parser::extensions::GeneralName::URI(u) = name {
                uris.push((*u).to_string());
            }
        }
    }
    let spiffe = uris.iter().find(|u| u.starts_with("spiffe://"));
    match spiffe {
        Some(u) => {
            // spiffe://<trust-domain>/<path…> — the trust domain is the
            // authority component and nothing below it.
            let rest = &u["spiffe://".len()..];
            let domain = rest.split('/').next().unwrap_or_default();
            if domain.is_empty() {
                return Derived::fail(
                    FIELD,
                    format!("the URI-SAN `{u}` names no trust domain"),
                    src,
                );
            }
            Derived::ok(
                FIELD,
                format!("spiffe://{domain}"),
                format!("{src} URI-SAN"),
            )
        }
        None => Derived::fail(
            FIELD,
            format!(
                "the profile certificate carries no `spiffe://` URI-SAN (it has {uris:?}). The \
                 trust domain is read from the certificate a counterparty's edge will actually \
                 see, so there is nowhere else to take it from"
            ),
            src,
        ),
    }
}

/// `identity.ca_fingerprint` — `sha256:` over the DER of the trust anchor in
/// the profile's root bundle (§5.1): the root whose key signed the profile
/// certificate's chain.
///
/// The bundle is never read by position, and a one-certificate bundle is
/// checked like any other. An zagros operator bundle after the CA separation
/// carries the zagros CA that signs the leaf AND the shared root the router's own
/// server certificate still chains to (#1115), and the wrong anchor here is the
/// wrong trust anchor for every peering certificate the counterparty will
/// issue. The selection is [`super::anchor::issuing_root`].
pub fn ca_fingerprint(ca_roots_pem: &Path, leaf_pem: &Path) -> Derived<String> {
    const FIELD: &str = "identity.ca_fingerprint";
    let src = ca_roots_pem.display().to_string();
    let Ok(bytes) = std::fs::read(ca_roots_pem) else {
        return Derived::fail(FIELD, format!("no CA root bundle at {src}"), src);
    };
    let Ok(items) = pem::parse_many(&bytes) else {
        return Derived::fail(FIELD, "the CA root bundle is not readable PEM", src);
    };
    let certs: Vec<&pem::Pem> = items.iter().filter(|p| p.tag() == "CERTIFICATE").collect();
    if certs.is_empty() {
        return Derived::fail(FIELD, "the CA root bundle contains no certificate", src);
    }
    let leaf_src = leaf_pem.display().to_string();
    let Ok(presented) = std::fs::read(leaf_pem) else {
        return Derived::fail(
            FIELD,
            format!(
                "the profile certificate at {leaf_src}, which says which bundle certificate is \
                 the anchor, could not be read"
            ),
            src,
        );
    };
    let (anchor, subject) = match super::anchor::issuing_root(&certs, &presented, &leaf_src) {
        Ok(found) => found,
        Err(why) => return Derived::fail(FIELD, why, src),
    };
    use sha2::{Digest, Sha256};
    let mut h = Sha256::new();
    h.update(anchor.contents());
    Derived::ok(
        FIELD,
        format!("sha256:{:x}", h.finalize()),
        format!(
            "sha256 over the DER in {src} ({subject}, the root that issued the profile \
             certificate)"
        ),
    )
}

// ---------------------------------------------------------------------------
// Capabilities — measured, every one
// ---------------------------------------------------------------------------

/// `capabilities.transports` — what is actually installed and configured, not
/// what could be.
///
/// An empty result is a FAILURE, not `[]`: an empty set-array is not a
/// declaration (§3), and a package that offered one would refuse at
/// composition anyway with nothing to point at.
pub fn transports() -> Derived<Vec<String>> {
    const FIELD: &str = "capabilities.transports";
    let mut found = Vec::new();
    let mut notes = Vec::new();

    // WireGuard: an interface has to exist. The tool being installed says
    // nothing about whether this host can carry a link.
    match run("wg", &["show", "interfaces"]) {
        Ok(out) if !out.trim().is_empty() => {
            found.push("wireguard".to_string());
            notes.push(format!("`wg show interfaces` → {}", out.trim()));
        }
        Ok(_) => notes.push(
            "`wg show interfaces` is empty — WireGuard is installed with no interface up".into(),
        ),
        Err(e) => notes.push(format!("`wg show interfaces` — {e}")),
    }

    // ipsec-fips is two facts, and both are required: an IPsec implementation,
    // AND the host actually running in FIPS mode. Reporting `ipsec-fips`
    // because strongSwan is installed would be the exact claim-for-a-fact
    // substitution this module exists to prevent.
    let ipsec = run("swanctl", &["--version"]).is_ok() || run("ipsec", &["--version"]).is_ok();
    // Absent (ENOENT — no such sysctl, e.g. not Linux or a non-FIPS kernel)
    // IS a readable "not FIPS". PermissionDenied is a DIFFERENT fact: the
    // file exists and this host may well be in FIPS mode, but this caller
    // cannot see it — collapsing that into `false` is the exact
    // claim-for-a-fact substitution `pqc_capable()` (below) was written to
    // avoid, and this check didn't copy that discipline.
    let fips = match std::fs::read_to_string("/proc/sys/crypto/fips_enabled") {
        Ok(s) => Some(s.trim() == "1"),
        Err(e) if e.kind() == std::io::ErrorKind::PermissionDenied => None,
        Err(_) => Some(false),
    };
    match (ipsec, fips) {
        (true, Some(true)) => {
            found.push("ipsec-fips".to_string());
            notes.push("strongSwan present and /proc/sys/crypto/fips_enabled is 1".into());
        }
        (true, Some(false)) => {
            notes.push("strongSwan present but /proc/sys/crypto/fips_enabled is not 1 — that is `ipsec`, not `ipsec-fips`".into());
        }
        (true, None) => {
            notes.push(
                "strongSwan present but /proc/sys/crypto/fips_enabled could not be read \
                 (permission denied) — FIPS status on this host is UNKNOWN, not false; \
                 `ipsec-fips` is withheld rather than guessed."
                    .into(),
            );
        }
        (false, _) => {
            notes.push("no strongSwan (`swanctl`/`ipsec`) on PATH".into());
        }
    }

    if found.is_empty() {
        Derived::fail(
            FIELD,
            format!(
                "no transport is installed and configured on this host. An empty set-array is not \
                 a declaration, so there is nothing to emit. What was read: {}",
                notes.join("; ")
            ),
            "local system probe",
        )
    } else {
        Derived::ok(FIELD, super::json::normalise_set(&found), notes.join("; "))
    }
}

/// `capabilities.fabric_fips_image` — the RUNNING router's image, not the
/// pinned one. A pin says what should be running; the container says what is.
pub fn fabric_fips_image() -> Derived<bool> {
    const FIELD: &str = "capabilities.fabric_fips_image";
    match router_image() {
        Ok((image, tool)) => {
            let fips = image.contains("zenoh-pqc-fips");
            Derived::ok(
                FIELD,
                fips,
                format!("`{tool} inspect {ROUTER_CONTAINER}` → {image}"),
            )
        }
        Err(e) => Derived::fail(
            FIELD,
            format!(
                "{e}. This is read from the running router because a validated build is a \
                 property of what is executing, not of what is pinned — run this on a router host"
            ),
            format!("{ROUTER_CONTAINER} container"),
        ),
    }
}

/// `capabilities.fabric_version` — from the router itself.
pub fn fabric_version() -> Derived<String> {
    const FIELD: &str = "capabilities.fabric_version";
    // TWO PATHS, AND THE SECOND IS THE ONE THE FLEET USES. The published
    // zenoh-pqc image ships the router at `/zenohd` with no PATH entry for it,
    // so the bare name resolves on a developer's box and on nothing the fleet
    // runs. Probing only the bare name made this field underivable on every
    // host running the pinned image, which is every host.
    // Every attempt's actual error used to be discarded, so "docker isn't
    // installed", "permission denied on the docker socket", and "the
    // container genuinely isn't running" all asserted the identical hard-
    // coded conclusion below — very different problems with very different
    // fixes.
    let mut attempts = Vec::new();
    for tool in ["docker", "podman"] {
        for router in ["zenohd", "/zenohd"] {
            match run(tool, &["exec", ROUTER_CONTAINER, router, "--version"]) {
                Ok(out) => {
                    if let Some(v) = parse_version(&out) {
                        return Derived::ok(
                            FIELD,
                            v,
                            format!("`{tool} exec {ROUTER_CONTAINER} {router} --version`"),
                        );
                    }
                    attempts.push(format!("{tool} exec {router}: ran, but no version in output"));
                }
                Err(e) => attempts.push(format!("{tool} exec {router}: {e}")),
            }
        }
    }
    Derived::fail(
        FIELD,
        format!(
            "could not read a version out of the running `{ROUTER_CONTAINER}` container. \
             Attempts: {}",
            attempts.join(" | ")
        ),
        format!("{ROUTER_CONTAINER} container"),
    )
}

/// `capabilities.pqc_capable` — whether the intra-mesh PQC layer is RUNNING.
///
/// A host with the software installed and the unit stopped is not PQC-capable
/// in any sense a counterparty cares about, and `false` is not defaulted to
/// either: an unreadable probe is an unknown, and an unknown is a failure.
pub fn pqc_capable() -> Derived<bool> {
    const FIELD: &str = "capabilities.pqc_capable";
    // `systemctl is-active` EXITS NON-ZERO FOR EVERY STATE THAT IS NOT ACTIVE
    // and prints the state on stdout regardless — `inactive` with exit 3 is a
    // complete, readable answer, not a probe that failed. Reading only the exit
    // status threw that answer away and made the field underivable on any host
    // where Rosenpass is simply not installed, which is the default: it is
    // opt-in per goatnet (`rosenpass_enabled`). The result was that `init`
    // refused to produce a package on a host whose systemd had just answered
    // the question.
    match run_reading_output("systemctl", &["is-active", "rosenpass"]) {
        Ok(out) => {
            let s = out.trim().to_string();
            Derived::ok(
                FIELD,
                s == "active",
                format!("`systemctl is-active rosenpass` → {s}"),
            )
        }
        Err(_) => match run("systemctl", &["list-unit-files", "rosenpass.service"]) {
            // The unit exists and is not active — that is a readable `false`.
            Ok(out) if out.contains("rosenpass.service") => Derived::ok(
                FIELD,
                false,
                "`systemctl list-unit-files rosenpass.service` — present, not active",
            ),
            _ => Derived::fail(
                FIELD,
                "no `rosenpass` unit and no systemd to ask. `false` is not defaulted here: an \
                 unreadable probe is an unknown, and emitting an unknown as a capability is the \
                 substitution this verb exists to prevent",
                "systemd",
            ),
        },
    }
}

/// `transport_offer.public_key` — the federation interface's own public key,
/// read out of the running interface.
pub fn transport_public_key(interface: &str) -> Derived<String> {
    const FIELD: &str = "transport_offer.public_key";
    match run("wg", &["show", interface, "public-key"]) {
        Ok(out) if !out.trim().is_empty() => {
            Derived::ok(FIELD, out.trim().to_string(), format!("`wg show {interface} public-key`"))
        }
        Ok(_) => Derived::fail(FIELD, format!("interface `{interface}` reported no public key"), "wg"),
        Err(e) => Derived::fail(
            FIELD,
            format!("`wg show {interface} public-key` — {e}. The declared federation interface has to exist before its key can be read"),
            "wg",
        ),
    }
}

/// The interface MTU, used only to CROSS-CHECK a declared `measured_mtu`.
///
/// An interface MTU is not a measured path MTU and is never substituted for
/// one — but a declared path MTU larger than the interface that carries it
/// cannot be right, and that is worth saying before the package is signed.
pub fn interface_mtu(interface: &str) -> Option<i64> {
    std::fs::read_to_string(format!("/sys/class/net/{interface}/mtu"))
        .ok()
        .and_then(|s| s.trim().parse().ok())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn router_image() -> Result<(String, &'static str), String> {
    let mut attempts = Vec::new();
    for tool in ["docker", "podman"] {
        match run(
            tool,
            &["inspect", "--format", "{{.Config.Image}}", ROUTER_CONTAINER],
        ) {
            Ok(out) if !out.trim().is_empty() => return Ok((out.trim().to_string(), tool)),
            Ok(_) => attempts.push(format!("{tool}: ran, but printed nothing")),
            Err(e) => attempts.push(format!("{tool}: {e}")),
        }
    }
    // Every attempt's actual error used to be discarded here too, asserting
    // one hard-coded "not running" conclusion regardless of whether the real
    // cause was a missing docker/podman binary, a permission-denied docker
    // socket, or the container genuinely being absent — three different
    // fixes collapsed into one wrong-sounding diagnosis for two of them.
    Err(format!(
        "the `{ROUTER_CONTAINER}` container is not running under docker or podman here. \
         Attempts: {}",
        attempts.join(" | ")
    ))
}

/// `zenohd 1.9.0 …` → `1.9.0`. First dotted-numeric token, so a build suffix
/// or a git hash on the line does not become part of the version.
fn parse_version(out: &str) -> Option<String> {
    out.split_whitespace()
        .find(|t| {
            let core = t.trim_start_matches('v');
            core.split('.').count() >= 2 && core.starts_with(|c: char| c.is_ascii_digit())
        })
        .map(|t| t.trim_start_matches('v').to_string())
}

/// Run a read-only probe. Anything that is not a clean exit is an error rather
/// than an empty answer — "the command failed" and "the answer is nothing" are
/// different facts and only one of them is a derivation.
/// Like [`run`], but for the commands whose ANSWER is on stdout even when they
/// exit non-zero — `systemctl is-active` is the case that matters here. Kept
/// separate and used deliberately: for every other probe a non-zero exit means
/// the probe could not be made, and treating that as data is exactly the
/// claim-for-a-fact substitution this module exists to prevent.
///
/// Still an error when the binary cannot be executed at all, or when it exits
/// non-zero with nothing on stdout — there is no answer to read in either case.
fn run_reading_output(bin: &str, args: &[&str]) -> Result<String, String> {
    let out = Command::new(bin)
        .args(args)
        .output()
        .map_err(|e| format!("{bin}: {e}"))?;
    let stdout = String::from_utf8_lossy(&out.stdout).trim().to_string();
    if stdout.is_empty() {
        let err = String::from_utf8_lossy(&out.stderr).trim().to_string();
        return Err(if err.is_empty() {
            format!("{bin} exited {} with no output", out.status)
        } else {
            err
        });
    }
    Ok(stdout)
}

fn run(bin: &str, args: &[&str]) -> Result<String, String> {
    let out = Command::new(bin)
        .args(args)
        .output()
        .map_err(|e| format!("{bin}: {e}"))?;
    if !out.status.success() {
        let err = String::from_utf8_lossy(&out.stderr).trim().to_string();
        return Err(if err.is_empty() {
            format!("{bin} exited {}", out.status)
        } else {
            err
        });
    }
    Ok(String::from_utf8_lossy(&out.stdout).into_owned())
}

#[cfg(test)]
mod tests {
    use super::*;

    use crate::fed::anchor::test_pki::{leaf_signed_by, root, Ca};

    /// Writes the bundle and, when given, the profile certificate; returns the
    /// derivation. The selection itself is tested in `anchor`.
    fn derive_with(roots: &[&Ca], leaf_pem: Option<&str>) -> Derived<String> {
        let dir = tempfile::tempdir().unwrap();
        let bundle = dir.path().join("ca-roots.pem");
        let leaf = dir.path().join("cert.pem");
        let joined: String = roots.iter().map(|c| c.cert.pem()).collect();
        std::fs::write(&bundle, joined).unwrap();
        if let Some(p) = leaf_pem {
            std::fs::write(&leaf, p).unwrap();
        }
        ca_fingerprint(&bundle, &leaf)
    }

    #[test]
    fn the_fingerprint_is_over_the_signing_root_and_the_source_names_it() {
        use sha2::{Digest, Sha256};
        let a = root("root-a");
        let b = root("root-b");
        let d = derive_with(&[&b, &a], Some(&leaf_signed_by(&a)));
        let want = format!("sha256:{:x}", Sha256::digest(a.cert.der()));
        assert_eq!(d.outcome.as_deref(), Ok(want.as_str()), "{d:?}");
        assert!(d.source.contains("root-a"), "{}", d.source);
    }

    #[test]
    fn no_readable_profile_certificate_is_refused_and_says_so() {
        // One root or two: the anchor is read from the leaf either way.
        let a = root("root-a");
        let b = root("root-b");
        for roots in [vec![&a], vec![&a, &b]] {
            let why = derive_with(&roots, None).outcome.unwrap_err();
            assert!(
                why.contains("profile certificate") && why.contains("could not be read"),
                "{why}"
            );
        }
    }

    #[test]
    fn version_token_survives_a_noisy_line() {
        assert_eq!(
            parse_version("zenohd v1.9.0 built with rustc"),
            Some("1.9.0".into())
        );
        assert_eq!(parse_version("zenohd 1.9.0"), Some("1.9.0".into()));
        assert_eq!(parse_version("no version here"), None);
    }
}

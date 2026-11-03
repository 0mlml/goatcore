// Bundle verifier — ADR 1019 Phase B Step 2D.
//
// Mirrors the Go side at ops/enrollment/bundle/bundle.go: CBOR map,
// signature = ASN.1 DER ECDSA-Sig-Value over SHA-256(canonical CBOR
// of the bundle with the "signature" field removed). CA root public keys
// are supplied BY THE OPERATOR at runtime — nothing about any goatnet is
// compiled into this binary.
//
// The Rust verifier deliberately ONLY parses the fields goat-cli
// needs (the GoatCLIExtras nested map + lifecycle timestamps). All
// other fields ride through opaque — they participate in the
// signature but the Rust side never inspects them.
//
// Canonical re-encoding: walk the parsed CBOR Value tree, recursively
// sort every map by bytewise-lexicographic order of encoded key
// bytes (matches the Go side's fxamacker `SortBytewiseLexical` per
// bundle.go's `canonicalEnc()`). For short text keys this is
// equivalent to length-first-then-lex (since the CBOR type byte
// 0x60+len carries the length).

use crate::errors::GoatError;
use ciborium::value::Value;
use p256::ecdsa::{signature::Verifier, Signature, VerifyingKey};
use sha2::{Digest, Sha256};
use std::time::{SystemTime, UNIX_EPOCH};

/// CBOR field name carrying the id of the CA that signed the bundle.
/// Read for DIAGNOSTICS ONLY — never for authorization. It is inside the
/// signed payload, so a caller can name the signer of a bundle it cannot
/// verify, which is the difference between "wrong CA" and "you do not have
/// that goatnet's root; add it".
const CA_ID_KEY: &str = "ca_id";

/// Prefix on the raw signature-mismatch hint, so `parse_and_verify` can tell
/// a TRUST failure from a malformed/expired one and answer accordingly.
const SIG_FAIL_PREFIX: &str = "signature did not verify";

/// CBOR field name for the signature entry on EnrollmentBundle.
const SIGNATURE_KEY: &str = "signature";
/// CBOR field name for the nested GoatCLIExtras map.
const GOAT_CLI_EXTRAS_KEY: &str = "goat_cli_extras";

/// The subset of the bundle the Rust verifier needs to inspect.
/// Everything else round-trips opaque inside the canonical bytes the
/// signature covers.
#[derive(Debug, Clone)]
pub struct Bundle {
    pub device_id: String,
    pub issued_at_unix: i64,
    pub activation_deadline_unix: i64,
    pub expires_at_unix: i64,
    pub extras: GoatCliExtras,
}

/// Mirrors `bundle.GoatCLIExtras` on the Go side. All fields are
/// load-bearing for `goat profile init` — partial population is
/// rejected by `HasGoatCLIExtras` on the issuer side.
#[derive(Debug, Clone)]
pub struct GoatCliExtras {
    pub portal_endpoint: String,
    pub portal_token: Vec<u8>,
    pub router_endpoint: String,
    /// OPTIONAL onboarding-engine URL (goat-id-console). Present only on goatnets
    /// that stood up the onboard route; when absent the CLI falls back to
    /// `portal_endpoint` for `/api/onboard/*` (docs/design/onboarding-extraction-plan.md).
    pub onboard_endpoint: Option<String>,
    pub mtls_cert: Vec<u8>,
    pub mtls_key: Vec<u8>,
    pub ca_roots: Vec<u8>,
}

/// Parse + verify a bundle. Returns the subset goat-cli consumes.
///
/// Error mapping:
///   - CBOR parse failure         → E_BUNDLE_INVALID "malformed CBOR"
///   - missing signature          → E_BUNDLE_INVALID "no signature field"
///   - re-encode failure          → E_BUNDLE_INVALID (treated as malformed)
///   - signature verify failure   → E_BUNDLE_INVALID "signature invalid"
///   - missing GoatCLIExtras      → E_BUNDLE_INVALID "no goat-cli extras"
///   - expired/pre-activation     → E_BUNDLE_INVALID (lifecycle)
pub fn parse_and_verify(bytes: &[u8]) -> Result<Bundle, GoatError> {
    // Structure before trust. A corrupt download and an unknown CA are
    // different faults with different fixes, and on a machine with no roots
    // yet — which every machine is, once — checking trust first would report
    // every truncated file as a missing CA root and send the operator chasing
    // a key they already have.
    decodes_as_a_bundle(bytes)?;

    let keys = ca_root_verifying_keys();
    if keys.is_empty() {
        return Err(GoatError::BundleInvalid {
            hint: no_roots_hint(bytes),
        });
    }
    // Try every trusted root in order. A bundle that verifies against
    // any root either succeeds immediately, or fails with the first
    // non-signature error (malformed, expired, missing extras) so those
    // faults are reported as-is and never dressed up as trust problems.
    // Only when every root fails the signature check do we fall back to
    // the "untrusted CA" hint that names the signer and what this
    // machine currently trusts.
    let mut saw_sig_failure = false;
    for (_label, key) in &keys {
        match parse_and_verify_with(bytes, key) {
            Ok(b) => return Ok(b),
            Err(GoatError::BundleInvalid { ref hint }) if hint.starts_with(SIG_FAIL_PREFIX) => {
                saw_sig_failure = true;
                continue;
            }
            Err(e) => return Err(e),
        }
    }
    if saw_sig_failure {
        return Err(GoatError::BundleInvalid {
            hint: untrusted_ca_hint(bytes, &keys),
        });
    }
    Err(GoatError::BundleInvalid {
        hint: no_roots_hint(bytes),
    })
}

/// Reject a file that is not a CBOR-map bundle at all, with the same wording
/// `parse_and_verify_with` uses — the two must not disagree about what a
/// malformed file is. Says nothing about the signature: this runs before any
/// key is loaded, precisely so a structural fault is never dressed as a trust
/// problem.
fn decodes_as_a_bundle(bytes: &[u8]) -> Result<(), GoatError> {
    let val: Value = ciborium::de::from_reader(bytes).map_err(|e| GoatError::BundleInvalid {
        hint: format!("malformed CBOR: {e}"),
    })?;
    match val {
        Value::Map(_) => Ok(()),
        _ => Err(GoatError::BundleInvalid {
            hint: "bundle is not a CBOR map".into(),
        }),
    }
}

/// The `ca_id` a bundle names, if it carries one. Diagnostics only.
fn bundle_ca_id(bytes: &[u8]) -> Option<String> {
    let val: Value = ciborium::de::from_reader(bytes).ok()?;
    let entries = match val {
        Value::Map(m) => m,
        _ => return None,
    };
    match find_text_keyed(&entries, CA_ID_KEY) {
        Some(Value::Text(t)) if !t.trim().is_empty() => Some(t.trim().to_string()),
        _ => None,
    }
}

/// What to say when this machine holds no CA roots at all.
fn no_roots_hint(bytes: &[u8]) -> String {
    let who = bundle_ca_id(bytes)
        .map(|id| format!("This bundle is signed by `{id}`. "))
        .unwrap_or_default();
    format!(
        "{who}This machine trusts no CA roots, so no bundle can be verified.\n\
         \n\
         goat ships with no goatnet built in — a root is something you add, once, \
         per goatnet:\n\
         \x20 goat ca add <root.pem>          # from your goatnet's operators\n\
         \x20 goat ca ls                      # what this machine trusts, with fingerprints\n\
         \n\
         Roots for a goatnet you can already reach are served alongside the CLI \
         download at its get.<site> host."
    )
}

/// What to say when roots exist but none of them signed this bundle.
fn untrusted_ca_hint(bytes: &[u8], have: &[(String, VerifyingKey)]) -> String {
    let mut trusted: Vec<&str> = have.iter().map(|(l, _)| l.as_str()).collect();
    trusted.sort_unstable();
    let listed = if trusted.is_empty() {
        "(none)".to_string()
    } else {
        trusted.join(", ")
    };
    match bundle_ca_id(bytes) {
        Some(id) => format!(
            "This bundle is signed by `{id}`, and this machine does not trust that CA.\n\
             \n\
             Trusted here: {listed}\n\
             \n\
             Add the missing root — one command, no reinstall:\n\
             \x20 goat ca add <path-to-{id}.pem>\n\
             \n\
             Get that file from the goatnet's operators, or from its get.<site> host, \
             and check the fingerprint `goat ca add` prints against one you were given \
             out of band before you rely on it.\n\
             \n\
             If you DO already trust that CA, the payload has been altered since \
             signing — the signature would otherwise match."
        ),
        // No ca_id to name. Still better than the three-causes string: say
        // what was tried, so the reader knows trust is a candidate cause.
        None => format!(
            "Signature did not match any CA root this machine trusts ({listed}), \
             and the bundle names no ca_id.\n\
             \n\
             Either it is signed by a CA you have not added (`goat ca add <root.pem>`), \
             or the payload has been altered since signing."
        ),
    }
}

/// Like `parse_and_verify` but verifies against an explicit key
/// rather than the build-time embedded CA root. Used by unit tests
/// that mint fixture bundles with a per-test keypair; production
/// callers should use `parse_and_verify`.
pub fn parse_and_verify_with(
    bytes: &[u8],
    verifying_key: &VerifyingKey,
) -> Result<Bundle, GoatError> {
    let val: Value = ciborium::de::from_reader(bytes).map_err(|e| GoatError::BundleInvalid {
        hint: format!("malformed CBOR: {e}"),
    })?;
    let mut entries = match val {
        Value::Map(m) => m,
        _ => {
            return Err(GoatError::BundleInvalid {
                hint: "bundle is not a CBOR map".into(),
            })
        }
    };

    let signature_bytes = take_text_keyed_bytes(&mut entries, SIGNATURE_KEY).ok_or_else(|| {
        GoatError::BundleInvalid {
            hint: "bundle has no signature field".into(),
        }
    })?;

    // Recursively sort every map in the remaining tree so the
    // re-encoded bytes match the Go side's canonical CBOR (which is
    // what the CA signed).
    sort_canonical_in_place(&mut entries);
    let mut signable = Vec::new();
    ciborium::ser::into_writer(&Value::Map(entries.clone()), &mut signable).map_err(|e| {
        GoatError::BundleInvalid {
            hint: format!("re-encode for verify failed: {e}"),
        }
    })?;

    let digest = Sha256::digest(&signable);
    let sig = Signature::from_der(&signature_bytes).map_err(|e| GoatError::BundleInvalid {
        hint: format!("signature not ASN.1 DER: {e}"),
    })?;
    verifying_key
        .verify(&signable, &sig)
        // Defensive: verify hashes msg internally; fall back to verify_prehash if needed.
        .or_else(|_| {
            use p256::ecdsa::signature::hazmat::PrehashVerifier;
            verifying_key.verify_prehash(&digest, &sig)
        })
        .map_err(|_| GoatError::BundleInvalid {
            // Deliberately terse and PREFIXED: parse_and_verify replaces this
            // with a message naming the signing CA once every root has been
            // tried. Reaching a user unchanged only happens via
            // parse_and_verify_with, which tests call directly.
            hint: format!("{SIG_FAIL_PREFIX} against this key"),
        })?;

    // Pull the lifecycle timestamps + goat-cli extras out of the
    // signable map. These are the only fields the verifier needs.
    let device_id = string_field(&entries, "device_id").unwrap_or_default();
    let issued_at = int_field(&entries, "issued_at").unwrap_or(0);
    // Lifecycle bounds are MANDATORY and fail-closed. Every legitimately
    // minted bundle carries a positive activation_deadline + expires_at:
    // the issuer's validateArgs enforces ttl-days>0, activation-days>0,
    // activation<=ttl (ops/enrollment/cmd/bundle-create/main.go), and the
    // Go verifier's CheckExpiry/CheckActivationDeadline have no zero
    // sentinel — a zero-value time is simply "in the past" = expired.
    // So there is NO "no expiry" bundle class. A MISSING or non-integer
    // field must therefore be REJECTED, not read as "no bound": the old
    // `unwrap_or(0)` collapsed absent/mistyped into the same 0 that the
    // gates below treated as "unbounded", installing a never-expiring
    // credential.
    // Mirror the ok_or_else fail-closed idiom the extras fields use above.
    let activation_deadline =
        int_field(&entries, "activation_deadline").ok_or_else(|| GoatError::BundleInvalid {
            hint: "bundle has no valid activation_deadline (field absent or non-integer)".into(),
        })?;
    let expires_at = int_field(&entries, "expires_at").ok_or_else(|| GoatError::BundleInvalid {
        hint: "bundle has no valid expiry (expires_at absent or non-integer)".into(),
    })?;

    let extras_map = find_text_keyed(&entries, GOAT_CLI_EXTRAS_KEY).and_then(|v| match v {
        Value::Map(m) => Some(m),
        _ => None,
    });
    let extras_map = extras_map.ok_or_else(|| GoatError::BundleInvalid {
        hint: "no goat-cli extras in bundle — issuer must pass --goat-cli-* flags to bundle-create"
            .into(),
    })?;

    let extras = GoatCliExtras {
        portal_endpoint: string_field(extras_map, "portal_endpoint").ok_or_else(|| {
            GoatError::BundleInvalid {
                hint: "goat-cli extras missing portal_endpoint".into(),
            }
        })?,
        portal_token: bytes_field(extras_map, "portal_token").ok_or_else(|| {
            GoatError::BundleInvalid {
                hint: "goat-cli extras missing portal_token".into(),
            }
        })?,
        router_endpoint: string_field(extras_map, "router_endpoint").ok_or_else(|| {
            GoatError::BundleInvalid {
                hint: "goat-cli extras missing router_endpoint".into(),
            }
        })?,
        // OPTIONAL — absent on goatnets without the onboard route (back-compat).
        onboard_endpoint: string_field(extras_map, "onboard_endpoint"),
        mtls_cert: bytes_field(extras_map, "mtls_cert").ok_or_else(|| {
            GoatError::BundleInvalid {
                hint: "goat-cli extras missing mtls_cert".into(),
            }
        })?,
        mtls_key: bytes_field(extras_map, "mtls_key").ok_or_else(|| GoatError::BundleInvalid {
            hint: "goat-cli extras missing mtls_key".into(),
        })?,
        ca_roots: bytes_field(extras_map, "ca_roots").ok_or_else(|| GoatError::BundleInvalid {
            hint: "goat-cli extras missing ca_roots".into(),
        })?,
    };

    // Lifecycle gates. Issuer-side `validateArgs` enforces
    // activation_days <= ttl_days so the inequalities are total. Both
    // bounds are already known-present (fail-closed parse above), so the
    // comparisons are unconditional — no `!= 0` escape hatch that would
    // let a zero/absent bound read as "never expires".
    let now_unix = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);
    if now_unix > expires_at {
        return Err(GoatError::BundleInvalid {
            hint: format!(
                "bundle expired at unix={expires_at}, now={now_unix} — re-onboard or refresh"
            ),
        });
    }
    // activation_deadline only bounds first-use; an already-installed
    // profile keeps working past it. `goat profile init` is by
    // definition first-use, so enforce here.
    if now_unix > activation_deadline {
        return Err(GoatError::BundleInvalid {
            hint: format!(
                "bundle activation deadline (unix={activation_deadline}) passed at now={now_unix} — operator must reissue"
            ),
        });
    }

    Ok(Bundle {
        device_id,
        issued_at_unix: issued_at,
        activation_deadline_unix: activation_deadline,
        expires_at_unix: expires_at,
        extras,
    })
}

/// Parse the embedded CA root PEM (SPKI) into a VerifyingKey. The PEM
/// may carry RFC 1421 "encapsulated header lines" (Key: Value pairs
/// before the base64 body); an embedded CA pubkey may carry them.
/// The `pem` crate handles them; p256's strict RFC 7468 parser does
/// not.
// pub(crate): the name-fallback resolver (crate::names, ADR 1082) verifies
// snapshot signatures against the same embedded CA root as bundles.
/// Where CA roots come from. THIS BINARY SHIPS NONE.
///
/// goat is goatnet-agnostic: it is built with no knowledge of any environment,
/// and every CA root is something an operator adds. Roots are read from, in
/// order:
///
///   1. `$GOAT_CA_ROOTS` — a file, or a directory of `*.pem`;
///   2. `<profile-dir>/ca-roots/*.pem` (`$GOAT_PROFILE_DIR`, else `~/.goat`).
///
/// There is no third source and no compiled-in floor. Earlier builds pinned
/// one goatnet's root and then four, which only moved the problem: a
/// fifth goatnet still needed a new binary, and "which environments does this
/// build know about" became a property of the release rather than of the
/// machine. Adding a goatnet, or rotating a CA, must not be a client release.
///
/// This does not weaken verification. A signature must still match a root the
/// operator deliberately installed; adding a file cannot make a bad bundle
/// verify, only a good one from a goatnet this machine was told about.
const CA_ROOTS_ENV: &str = "GOAT_CA_ROOTS";

/// The directory `goat ca add` writes to, relative to the profile dir.
pub(crate) const CA_ROOTS_DIRNAME: &str = "ca-roots";

/// Every CA root this machine trusts, as (label, key). Label is the file's
/// `CA-ID:` header when it has one, else the file stem — so errors and
/// `goat ca ls` name a CA the way the rest of the system does.
///
/// Never errors: an unreadable or absent trust-roots location contributes
/// nothing, and a malformed PEM is skipped rather than breaking the roots that
/// are good. An empty result is a legitimate state (a fresh machine) and the
/// caller says so in terms the reader can act on.
pub(crate) fn ca_root_verifying_keys() -> Vec<(String, VerifyingKey)> {
    let mut out: Vec<(String, VerifyingKey)> = Vec::new();
    let mut seen: Vec<Vec<u8>> = Vec::new();
    for (label, bytes) in operator_ca_root_pems() {
        if let Ok(k) = parse_ca_root_pem(&label, &bytes) {
            let enc = k.to_sec1_point(false).as_bytes().to_vec();
            if seen.contains(&enc) {
                continue;
            }
            seen.push(enc);
            out.push((ca_label(&label, &bytes), k));
        }
    }
    out
}

/// Prefer the PEM's own `CA-ID:` header (RFC 1421 encapsulated header, which
/// the issuer writes) over the filename, so renaming a file cannot silently
/// rename a CA in an error message.
pub(crate) fn ca_label(path_or_name: &str, pem_bytes: &[u8]) -> String {
    if let Ok(txt) = std::str::from_utf8(pem_bytes) {
        for line in txt.lines() {
            if let Some(rest) = line.trim().strip_prefix("CA-ID:") {
                let v = rest.trim();
                if !v.is_empty() {
                    return v.to_string();
                }
            }
        }
    }
    std::path::Path::new(path_or_name)
        .file_stem()
        .and_then(|s| s.to_str())
        .unwrap_or(path_or_name)
        .to_string()
}

/// The directory `goat ca` manages: `$GOAT_PROFILE_DIR/ca-roots`, else
/// `~/.goat/ca-roots`.
/// Profile base recorded at startup so the trust store follows `--profile-dir`
/// as well as the env var. Set once from `Ctx::from_cli`; verification is a free
/// function with no handle on the CLI, and a machine's trust store must not
/// depend on which command happens to be running.
static PROFILE_BASE: std::sync::RwLock<Option<std::path::PathBuf>> = std::sync::RwLock::new(None);

/// Record the resolved profile base. Called once per process from
/// `Ctx::from_cli`, so `--profile-dir` moves the trust store for EVERY command
/// and not just `goat ca` — a bundle installed with `--profile-dir X` must
/// verify against the roots in X.
pub(crate) fn set_profile_base(dir: &std::path::Path) {
    if let Ok(mut g) = PROFILE_BASE.write() {
        *g = Some(dir.to_path_buf());
    }
}

pub(crate) fn ca_roots_dir() -> Option<std::path::PathBuf> {
    if let Ok(g) = PROFILE_BASE.read() {
        if let Some(b) = g.as_ref() {
            return Some(b.join(CA_ROOTS_DIRNAME));
        }
    }
    let base = std::env::var("GOAT_PROFILE_DIR")
        .ok()
        .map(std::path::PathBuf::from)
        .or_else(|| {
            crate::profile::resolve_home_dir()
                .ok()
                .map(|h| h.join(".goat"))
        })?;
    Some(base.join(CA_ROOTS_DIRNAME))
}

/// Warn when a CA-root location exists but could not be read. `NotFound` is
/// the one error that genuinely means "nothing configured"; every other
/// kind — permission denied on a locked-down machine or after an
/// install-as-root, EIO, a symlink loop — means a root may well be
/// configured and is NOT being trusted, which the operator has to hear.
/// One shared helper instead of four near-identical inline match arms, so
/// the rule ("unreadable ≠ absent") lives in one place.
fn warn_unreadable(path: &std::path::Path, what: &str, err: &std::io::Error) {
    if err.kind() == std::io::ErrorKind::NotFound {
        return;
    }
    let cause = if err.kind() == std::io::ErrorKind::PermissionDenied {
        "exists but is not readable by this user (permission denied)".to_string()
    } else {
        format!("could not be read ({err})")
    };
    eprintln!(
        "⚠ CA root {what} {} {cause} — its root(s), if any, are NOT being trusted. Fix the \
         file or its ownership/permissions rather than assuming none are configured.",
        path.display()
    );
}

/// Read every operator-supplied CA root PEM as (path, bytes).
pub(crate) fn operator_ca_root_pems() -> Vec<(String, Vec<u8>)> {
    let mut roots: Vec<std::path::PathBuf> = Vec::new();
    if let Ok(v) = std::env::var(CA_ROOTS_ENV) {
        let v = v.trim();
        if !v.is_empty() {
            roots.push(std::path::PathBuf::from(v));
        }
    }
    if let Some(d) = ca_roots_dir() {
        roots.push(d);
    }

    // Before warn_unreadable existed, every read error here was collapsed
    // identically to "absent", so `no_roots_hint()` told a
    // permission-denied operator "goat ships with no goatnet built in — a
    // root is something you add", a categorically wrong diagnosis when a
    // root IS configured but unreadable.
    let mut out = Vec::new();
    for root in roots {
        let meta = match std::fs::metadata(&root) {
            Ok(m) => m,
            Err(e) => {
                warn_unreadable(&root, "path", &e);
                continue;
            }
        };
        if meta.is_file() {
            match std::fs::read(&root) {
                Ok(b) => out.push((root.display().to_string(), b)),
                Err(e) => {
                    warn_unreadable(&root, "file", &e);
                }
            }
            continue;
        }
        let entries = match std::fs::read_dir(&root) {
            Ok(e) => e,
            Err(e) => {
                warn_unreadable(&root, "directory", &e);
                continue;
            }
        };
        let mut files: Vec<std::path::PathBuf> = entries
            .flatten()
            .map(|e| e.path())
            .filter(|p| p.extension().and_then(|s| s.to_str()) == Some("pem"))
            .collect();
        // Deterministic order so two machines with the same directory try the
        // same roots in the same order.
        files.sort();
        for f in files {
            match std::fs::read(&f) {
                Ok(b) => out.push((f.display().to_string(), b)),
                Err(e) => {
                    warn_unreadable(&f, "file", &e);
                }
            }
        }
    }
    out
}

pub(crate) fn parse_ca_root_pem(id: &str, pem_bytes: &[u8]) -> Result<VerifyingKey, GoatError> {
    let blob = pem::parse(pem_bytes).map_err(|e| GoatError::BundleInvalid {
        hint: format!("CA root {id}: PEM parse failed: {e}"),
    })?;
    if blob.tag() != "PUBLIC KEY" {
        return Err(GoatError::BundleInvalid {
            hint: format!(
                "embedded CA root {id} is a {} block, want PUBLIC KEY (build problem)",
                blob.tag()
            ),
        });
    }
    use p256::pkcs8::DecodePublicKey;
    let pub_key = p256::PublicKey::from_public_key_der(blob.contents()).map_err(|e| {
        GoatError::BundleInvalid {
            hint: format!("embedded CA root {id} SPKI parse failed: {e} (build problem)"),
        }
    })?;
    Ok(VerifyingKey::from(&pub_key))
}

/// Find a text-keyed entry and return a reference to its value.
fn find_text_keyed<'a>(map: &'a [(Value, Value)], key: &str) -> Option<&'a Value> {
    for (k, v) in map {
        if matches_text_key(k, key) {
            return Some(v);
        }
    }
    None
}

/// Find + remove a text-keyed byte-string entry. Returns the bytes.
fn take_text_keyed_bytes(map: &mut Vec<(Value, Value)>, key: &str) -> Option<Vec<u8>> {
    let pos = map.iter().position(|(k, _)| matches_text_key(k, key))?;
    let (_, v) = map.remove(pos);
    match v {
        Value::Bytes(b) => Some(b),
        _ => None,
    }
}

fn matches_text_key(k: &Value, want: &str) -> bool {
    matches!(k, Value::Text(s) if s == want)
}

/// Extract a text-string field from a map.
fn string_field(map: &[(Value, Value)], key: &str) -> Option<String> {
    find_text_keyed(map, key).and_then(|v| match v {
        Value::Text(s) => Some(s.clone()),
        _ => None,
    })
}

/// Extract a byte-string field from a map.
fn bytes_field(map: &[(Value, Value)], key: &str) -> Option<Vec<u8>> {
    find_text_keyed(map, key).and_then(|v| match v {
        Value::Bytes(b) => Some(b.clone()),
        _ => None,
    })
}

/// Extract a positive integer field from a map. CBOR integers can
/// arrive as Integer or as Tag(0,_)/Tag(1,_) for time values; the
/// Go side encodes time.Time as bare integer Unix seconds (see
/// wire-probe at ops/enrollment + the comment in bundle.go)
/// so we only need the Integer arm here.
fn int_field(map: &[(Value, Value)], key: &str) -> Option<i64> {
    find_text_keyed(map, key).and_then(|v| match v {
        Value::Integer(i) => i64::try_from(*i).ok(),
        _ => None,
    })
}

/// Recursively sort every CBOR Map in the value tree by bytewise-
/// lexicographic order of encoded key bytes. Matches the Go side's
/// `cbor.CanonicalEncOptions()` (`SortBytewiseLexical`).
fn sort_canonical_in_place(map: &mut [(Value, Value)]) {
    for (_, v) in map.iter_mut() {
        sort_value_in_place(v);
    }
    map.sort_by_cached_key(|(k, _)| encoded_key_bytes(k));
}

fn sort_value_in_place(v: &mut Value) {
    match v {
        Value::Map(m) => sort_canonical_in_place(m),
        Value::Array(arr) => {
            for item in arr.iter_mut() {
                sort_value_in_place(item);
            }
        }
        _ => {}
    }
}

/// Encode a single CBOR key into its byte form for the sort key.
/// `into_writer` is infallible for primitive Value kinds; we panic
/// on serialization failure because that'd be a ciborium bug.
fn encoded_key_bytes(k: &Value) -> Vec<u8> {
    let mut out = Vec::new();
    ciborium::ser::into_writer(k, &mut out).expect("encoding a CBOR key is infallible");
    out
}

// Test-only signing imports for the fixture minter below. `Signature`
// is already imported at module scope for production verify; the signer
// half is test-only.
#[cfg(test)]
use p256::ecdsa::{signature::Signer as _, SigningKey};

/// Mint a fixture bundle signed by `signing_key`, mirroring what
/// bundle-create does on the Go side. Returns the CBOR bytes.
/// `expires_offset_secs` / `activation_offset_secs` are relative to now
/// so callers can exercise expired + pre-activation paths. Promoted to
/// module scope as `pub(crate)` (still `#[cfg(test)]`) so the
/// profile-install tests in cmd/profile.rs can drive a real
/// read -> verify -> install round-trip against a genuinely-signed bundle,
/// not just unit-test the verifier here.
#[cfg(test)]
pub(crate) fn mint_fixture_bundle(
    signing_key: &SigningKey,
    expires_offset_secs: i64,
    activation_offset_secs: i64,
    with_extras: bool,
) -> Vec<u8> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64;
    let mut entries: Vec<(Value, Value)> = vec![
        (Value::Text("version".into()), Value::Integer(1.into())),
        (
            Value::Text("device_id".into()),
            Value::Text("operator-test".into()),
        ),
        (
            Value::Text("peer_pubkey".into()),
            Value::Bytes(vec![0x01; 32]),
        ),
        (
            Value::Text("acl_groups".into()),
            Value::Array(vec![Value::Text("OPERATORS".into())]),
        ),
        (Value::Text("site".into()), Value::Text("zagros".into())),
        (Value::Text("known_endpoints".into()), Value::Null),
        (Value::Text("issued_at".into()), Value::Integer(now.into())),
        (
            Value::Text("activation_deadline".into()),
            Value::Integer((now + activation_offset_secs).into()),
        ),
        (
            Value::Text("expires_at".into()),
            Value::Integer((now + expires_offset_secs).into()),
        ),
        (Value::Text("nonce".into()), Value::Bytes(vec![0xab; 16])),
        (Value::Text("ca_id".into()), Value::Text("test-ca".into())),
    ];
    if with_extras {
        entries.push((
            Value::Text("goat_cli_extras".into()),
            Value::Map(vec![
                (
                    Value::Text("portal_endpoint".into()),
                    Value::Text("https://portal.test".into()),
                ),
                (
                    Value::Text("portal_token".into()),
                    Value::Bytes(b"opaque-token".to_vec()),
                ),
                (
                    Value::Text("router_endpoint".into()),
                    Value::Text("tls/router.test:7447".into()),
                ),
                (
                    Value::Text("onboard_endpoint".into()),
                    Value::Text("https://onboard.test".into()),
                ),
                // Opaque test bytes — the Rust verifier doesn't
                // parse PEM structure (the device-side loader does
                // that), so the fixture just needs round-trip-able
                // bytes. Deliberately NOT PEM-shaped to keep the
                // repo-wide credential-leak grep (tests/build.bats
                // "no credentials in any tracked file") clean —
                // that check skips *_test.go but doesn't know
                // about Rust's #[cfg(test)] gated blocks.
                (
                    Value::Text("mtls_cert".into()),
                    Value::Bytes(b"FIXTURE-MTLS-CERT-BYTES-FOR-TESTING-ONLY".to_vec()),
                ),
                (
                    Value::Text("mtls_key".into()),
                    Value::Bytes(b"FIXTURE-MTLS-KEY-BYTES-FOR-TESTING-ONLY".to_vec()),
                ),
                (
                    Value::Text("ca_roots".into()),
                    Value::Bytes(b"FIXTURE-CA-ROOTS-BYTES-FOR-TESTING-ONLY".to_vec()),
                ),
            ]),
        ));
    }

    // Canonical sort the signable.
    sort_canonical_in_place(&mut entries);
    let mut signable = Vec::new();
    ciborium::ser::into_writer(&Value::Map(entries.clone()), &mut signable).unwrap();

    // Sign.
    let sig: Signature = signing_key.sign(&signable);

    // Insert the signature, re-sort (so the with-signature form is
    // canonical too — matches the Go side's behavior), re-encode.
    entries.push((
        Value::Text("signature".into()),
        Value::Bytes(sig.to_der().as_bytes().to_vec()),
    ));
    sort_canonical_in_place(&mut entries);
    let mut wire = Vec::new();
    ciborium::ser::into_writer(&Value::Map(entries), &mut wire).unwrap();
    wire
}

#[cfg(test)]
mod tests {
    use super::*;
    use p256::ecdsa::SigningKey;
    use p256::elliptic_curve::Generate;
    use std::sync::{Mutex, MutexGuard, PoisonError};

    #[test]
    fn happy_path_round_trip_with_test_key() {
        let signing_key = SigningKey::generate();
        let verifying_key = *signing_key.verifying_key();
        let wire = mint_fixture_bundle(&signing_key, 86_400, 3600, true);
        let bundle = parse_and_verify_with(&wire, &verifying_key).expect("verify should succeed");
        assert_eq!(bundle.device_id, "operator-test");
        assert_eq!(bundle.extras.portal_endpoint, "https://portal.test");
        assert_eq!(bundle.extras.router_endpoint, "tls/router.test:7447");
        assert_eq!(
            bundle.extras.onboard_endpoint.as_deref(),
            Some("https://onboard.test")
        );
        assert_eq!(bundle.extras.portal_token, b"opaque-token");
        assert!(bundle.extras.mtls_cert.starts_with(b"FIXTURE-MTLS-CERT"));
        assert!(bundle.extras.mtls_key.starts_with(b"FIXTURE-MTLS-KEY"));
        assert!(bundle.extras.ca_roots.starts_with(b"FIXTURE-CA-ROOTS"));
    }

    #[test]
    fn tampered_payload_fails_verify() {
        let signing_key = SigningKey::generate();
        let verifying_key = *signing_key.verifying_key();
        let mut wire = mint_fixture_bundle(&signing_key, 86_400, 3600, true);
        // Tamper somewhere in the middle (avoiding the leading map
        // header). The signature now covers different bytes; verify
        // must fail with E_BUNDLE_INVALID.
        let mid = wire.len() / 2;
        wire[mid] ^= 0xff;
        let err = parse_and_verify_with(&wire, &verifying_key).unwrap_err();
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
    }

    #[test]
    fn wrong_signer_fails_verify() {
        let signing_key = SigningKey::generate();
        let other_signing_key = SigningKey::generate();
        let other_verifying_key = *other_signing_key.verifying_key();
        let wire = mint_fixture_bundle(&signing_key, 86_400, 3600, true);
        let err = parse_and_verify_with(&wire, &other_verifying_key).unwrap_err();
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
    }

    #[test]
    fn missing_extras_fails_with_specific_hint() {
        let signing_key = SigningKey::generate();
        let verifying_key = *signing_key.verifying_key();
        let wire = mint_fixture_bundle(&signing_key, 86_400, 3600, /* with_extras = */ false);
        let err = parse_and_verify_with(&wire, &verifying_key).unwrap_err();
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
        // Hint should be the "no goat-cli extras" one specifically.
        let msg = format!("{err:?}");
        assert!(
            msg.contains("no goat-cli extras"),
            "expected no-extras hint, got: {msg}"
        );
    }

    #[test]
    fn expired_bundle_rejected() {
        let signing_key = SigningKey::generate();
        let verifying_key = *signing_key.verifying_key();
        // expires 1 hour ago.
        let wire = mint_fixture_bundle(&signing_key, -3600, -7200, true);
        let err = parse_and_verify_with(&wire, &verifying_key).unwrap_err();
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
        let msg = format!("{err:?}");
        assert!(msg.contains("expired"), "expected expired hint, got: {msg}");
    }

    /// Mint a genuinely-signed bundle whose `expires_at` field is
    /// replaced by `expires` — `None` omits the field entirely, `Some(v)`
    /// substitutes an arbitrary CBOR value (e.g. a non-integer to exercise
    /// the type-mismatch path). Everything else mirrors a valid bundle so
    /// the ONLY thing under test is how the verifier treats a
    /// missing/mistyped expiry. Signature is computed over the mutated
    /// map, so these bundles are correctly signed — the rejection is the
    /// lifecycle-field fail-closed check, not a signature failure.
    fn mint_bundle_with_expires(signing_key: &SigningKey, expires: Option<Value>) -> Vec<u8> {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;
        let mut entries: Vec<(Value, Value)> = vec![
            (Value::Text("version".into()), Value::Integer(1.into())),
            (
                Value::Text("device_id".into()),
                Value::Text("operator-test".into()),
            ),
            (Value::Text("issued_at".into()), Value::Integer(now.into())),
            (
                Value::Text("activation_deadline".into()),
                Value::Integer((now + 3600).into()),
            ),
        ];
        if let Some(v) = expires {
            entries.push((Value::Text("expires_at".into()), v));
        }
        sort_canonical_in_place(&mut entries);
        let mut signable = Vec::new();
        ciborium::ser::into_writer(&Value::Map(entries.clone()), &mut signable).unwrap();
        let sig: Signature = signing_key.sign(&signable);
        entries.push((
            Value::Text("signature".into()),
            Value::Bytes(sig.to_der().as_bytes().to_vec()),
        ));
        sort_canonical_in_place(&mut entries);
        let mut wire = Vec::new();
        ciborium::ser::into_writer(&Value::Map(entries), &mut wire).unwrap();
        wire
    }

    #[test]
    fn absent_expires_at_rejected() {
        // A validly-signed bundle that OMITS expires_at must be rejected,
        // not read as a never-expiring credential.
        let signing_key = SigningKey::generate();
        let verifying_key = *signing_key.verifying_key();
        let wire = mint_bundle_with_expires(&signing_key, None);
        let err = parse_and_verify_with(&wire, &verifying_key).unwrap_err();
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
        let msg = format!("{err:?}");
        assert!(
            msg.contains("no valid expiry"),
            "expected no-valid-expiry hint, got: {msg}"
        );
    }

    #[test]
    fn non_integer_expires_at_rejected() {
        // A validly-signed bundle whose expires_at is the WRONG CBOR type
        // (here a text string) must be rejected — int_field returns None
        // for any non-Integer value and that must fail closed, never
        // collapse to "unbounded".
        let signing_key = SigningKey::generate();
        let verifying_key = *signing_key.verifying_key();
        let wire = mint_bundle_with_expires(&signing_key, Some(Value::Text("not-a-number".into())));
        let err = parse_and_verify_with(&wire, &verifying_key).unwrap_err();
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
        let msg = format!("{err:?}");
        assert!(
            msg.contains("no valid expiry"),
            "expected no-valid-expiry hint, got: {msg}"
        );
    }

    // Positive control for a valid future expires_at is
    // `happy_path_round_trip_with_test_key` above (expires_offset =
    // 86_400) — a full bundle with extras that verifies clean; and the
    // expired-still-rejects case is `expired_bundle_rejected`.

    // ------------------------------------------------------------------
    // Trust-store tests. Everything below reads roots from disk through
    // the real loader, so each test needs a private trust store and a
    // private profile dir, and the two process-wide env vars that name
    // them. `TrustStore` owns all of that for the lifetime of one test.
    // ------------------------------------------------------------------

    const PROFILE_DIR_ENV: &str = "GOAT_PROFILE_DIR";

    // Serialize tests that mutate process-wide env vars so Cargo's
    // parallel runner cannot race GOAT_CA_ROOTS / GOAT_PROFILE_DIR.
    static ENV_MUTEX: Mutex<()> = Mutex::new(());

    /// Poison-tolerant lock. A test that fails while holding the mutex
    /// poisons it; with a plain `unwrap()` every later env-mutating test
    /// then dies with `PoisonError` instead of running, so one genuine
    /// break reads as a dozen failures with the cause visible in one.
    /// The guarded state is the env, which `TrustStore::drop` restores
    /// even on panic, so the poison carries no information worth keeping.
    fn env_lock() -> MutexGuard<'static, ()> {
        ENV_MUTEX.lock().unwrap_or_else(PoisonError::into_inner)
    }

    fn spki_pem(key: &SigningKey) -> String {
        use p256::pkcs8::EncodePublicKey;
        key.verifying_key()
            .to_public_key_pem(Default::default())
            .expect("encode spki")
    }

    /// One test's view of the world: `GOAT_CA_ROOTS` points at a fresh
    /// directory holding exactly `roots` (named `<file>.pem`, tried in
    /// filename order as the loader does), and `GOAT_PROFILE_DIR` points
    /// at an empty sibling so the developer's own `~/.goat/ca-roots`
    /// never joins the trust list. `Drop` restores both variables and
    /// removes the directory, and `Drop` runs on panic too, so a failed
    /// assertion cannot leak env state into the next test.
    struct TrustStore {
        _lock: MutexGuard<'static, ()>,
        dir: std::path::PathBuf,
        prev_roots: Option<std::ffi::OsString>,
        prev_profile: Option<std::ffi::OsString>,
    }

    impl TrustStore {
        fn new(name: &str, roots: &[(&str, &SigningKey)]) -> Self {
            let lock = env_lock();
            let dir = std::env::temp_dir().join(format!("goat-ca-{name}-{}", std::process::id()));
            let _ = std::fs::remove_dir_all(&dir);
            let roots_dir = dir.join("roots");
            let profile_dir = dir.join("profile");
            std::fs::create_dir_all(&roots_dir).expect("mkdir roots");
            std::fs::create_dir_all(&profile_dir).expect("mkdir profile");
            for (file, key) in roots {
                std::fs::write(roots_dir.join(format!("{file}.pem")), spki_pem(key))
                    .expect("write root pem");
            }
            let prev_roots = std::env::var_os(CA_ROOTS_ENV);
            let prev_profile = std::env::var_os(PROFILE_DIR_ENV);
            // SAFETY: serialized by ENV_MUTEX (held in `_lock` for the
            // store's whole lifetime); restored in `drop`.
            unsafe {
                std::env::set_var(CA_ROOTS_ENV, &roots_dir);
                std::env::set_var(PROFILE_DIR_ENV, &profile_dir);
            }
            Self {
                _lock: lock,
                dir,
                prev_roots,
                prev_profile,
            }
        }

        /// No roots anywhere: the trust configuration a fresh machine has.
        fn empty(name: &str) -> Self {
            Self::new(name, &[])
        }

        /// The `<profile-dir>/ca-roots` directory `goat ca add` writes to.
        fn profile_ca_roots(&self) -> std::path::PathBuf {
            self.dir.join("profile").join(CA_ROOTS_DIRNAME)
        }
    }

    impl Drop for TrustStore {
        fn drop(&mut self) {
            // SAFETY: still serialized by `_lock`, which drops after this.
            unsafe {
                match self.prev_roots.take() {
                    Some(v) => std::env::set_var(CA_ROOTS_ENV, v),
                    None => std::env::remove_var(CA_ROOTS_ENV),
                }
                match self.prev_profile.take() {
                    Some(v) => std::env::set_var(PROFILE_DIR_ENV, v),
                    None => std::env::remove_var(PROFILE_DIR_ENV),
                }
            }
            let _ = std::fs::remove_dir_all(&self.dir);
        }
    }

    // A compiled-in CA key ties a binary to the goatnet it was built for: a
    // bundle from any other goatnet fails with an error that reads like a corrupt
    // file. Compiling in more keys makes the symptom rarer and leaves the bug,
    // because the next goatnet still needs a release. So the invariant is not
    // "enough keys": it is NO keys.
    //
    // This test is the regression gate. If someone adds `include_bytes!` back,
    // a process with an empty trust configuration starts trusting something,
    // and this fails.
    #[test]
    fn nothing_is_trusted_without_operator_supplied_roots() {
        let _store = TrustStore::empty("empty");
        let found = operator_ca_root_pems();
        assert!(
            found.is_empty(),
            "goat must compile in no CA root — found {:?}. A key in the binary is \
             the F-406 bug regardless of how many there are.",
            found.iter().map(|(l, _)| l).collect::<Vec<_>>()
        );
    }

    // The companion to the above: with no roots, a well-formed bundle is
    // refused, and the refusal says what to do rather than reading like file
    // corruption (which is exactly how F-406 presented in production).
    #[test]
    fn a_bundle_with_no_trusted_roots_is_refused_with_an_actionable_hint() {
        let _store = TrustStore::empty("noroots");
        let signing_key = SigningKey::generate();
        let wire = mint_bundle_with_expires(&signing_key, Some(Value::Integer(86_400.into())));
        let err = parse_and_verify(&wire).unwrap_err();
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
        let msg = format!("{err:?}");
        assert!(
            msg.contains("goat ca add"),
            "the refusal must name the command that fixes it, got: {msg}"
        );
    }

    // A root the build never heard of must be usable without a rebuild: drop
    // a PEM in the trust-roots directory and it is honoured. This is the half
    // that makes CA rotation an ops action rather than a client release.
    #[test]
    fn operator_supplied_ca_root_is_picked_up() {
        let store = TrustStore::empty("profile-root");
        let roots = store.profile_ca_roots();
        std::fs::create_dir_all(&roots).expect("mkdir");
        // Mint the "operator-supplied" file rather than reusing something the
        // binary carries — the binary carries nothing, which is the point.
        std::fs::write(roots.join("extra.pem"), spki_pem(&SigningKey::generate()))
            .expect("write pem");
        let found = operator_ca_root_pems();
        assert!(
            found.iter().any(|(label, _)| label.ends_with("extra.pem")),
            "a PEM in <profile-dir>/ca-roots must be discovered; got {:?}",
            found.iter().map(|(l, _)| l).collect::<Vec<_>>()
        );
    }

    // ------------------------------------------------------------------
    // Multi-root verification (GH #599). `parse_and_verify` must return
    // the SIGNING root's verdict — success or its lifecycle/extras error —
    // and fall through to the untrusted-CA hint only when no trusted root
    // signed the bundle. Before the fix the loop kept the LAST root's
    // error, so a signer that sorted first had its real error overwritten
    // by the next root's signature failure.
    // ------------------------------------------------------------------

    /// Where the signing root sits in filename order relative to one
    /// unrelated trusted root. Both orders must give the same answer.
    #[derive(Clone, Copy)]
    enum SignerSorts {
        First,
        Second,
    }

    /// Verify `wire` (signed by `signer`) against a two-root store and
    /// return the result. The other root is a fresh random key.
    fn verify_with_two_roots(
        name: &str,
        signer: &SigningKey,
        order: SignerSorts,
        wire: &[u8],
    ) -> Result<Bundle, GoatError> {
        let other = SigningKey::generate();
        let (first, second): (&SigningKey, &SigningKey) = match order {
            SignerSorts::First => (signer, &other),
            SignerSorts::Second => (&other, signer),
        };
        let _store = TrustStore::new(name, &[("00-first", first), ("99-second", second)]);
        parse_and_verify(wire)
    }

    /// The failure shape every lifecycle/extras case must have: the
    /// signing root's own hint, and never the trust wording.
    fn assert_signer_error(result: Result<Bundle, GoatError>, want: &str) {
        let err = result.expect_err("bundle must be refused");
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
        let msg = format!("{err:?}");
        assert!(
            msg.contains(want),
            "expected hint containing {want:?}, got: {msg}"
        );
        assert!(
            !msg.contains("does not trust that CA"),
            "a signed bundle's error must not be dressed as a trust problem: {msg}"
        );
    }

    /// (expires offset, activation offset, with_extras, expected hint) for
    /// the three post-signature failures and the success case.
    const LIFECYCLE_CASES: &[(&str, i64, i64, bool, Option<&str>)] = &[
        // The reported case: activation window lapsed, expiry still ahead.
        (
            "activation",
            86_400,
            -3600,
            true,
            Some("activation deadline"),
        ),
        ("expired", -3600, -7200, true, Some("expired")),
        ("no-extras", 86_400, 3600, false, Some("no goat-cli extras")),
        ("valid", 86_400, 3600, true, None),
    ];

    fn run_lifecycle_cases(order: SignerSorts, tag: &str) {
        for (label, expires, activation, extras, want) in LIFECYCLE_CASES {
            let signer = SigningKey::generate();
            let wire = mint_fixture_bundle(&signer, *expires, *activation, *extras);
            let result = verify_with_two_roots(&format!("{tag}-{label}"), &signer, order, &wire);
            match want {
                Some(hint) => assert_signer_error(result, hint),
                None => {
                    let bundle =
                        result.unwrap_or_else(|e| panic!("[{label}] valid bundle refused: {e:?}"));
                    assert_eq!(bundle.device_id, "operator-test");
                }
            }
        }
    }

    #[test]
    fn signer_sorting_first_reports_its_own_lifecycle_verdict() {
        run_lifecycle_cases(SignerSorts::First, "signer-first");
    }

    #[test]
    fn signer_sorting_second_reports_its_own_lifecycle_verdict() {
        run_lifecycle_cases(SignerSorts::Second, "signer-second");
    }

    // Structural faults precede the signature check, so every root would
    // report them identically; the first root's answer is final and is
    // never reworded as a trust problem.
    fn structural_wire(with_signature: Option<Vec<u8>>) -> Vec<u8> {
        let mut entries: Vec<(Value, Value)> = vec![
            (Value::Text("version".into()), Value::Integer(1.into())),
            (
                Value::Text("device_id".into()),
                Value::Text("operator-test".into()),
            ),
        ];
        if let Some(sig) = with_signature {
            entries.push((Value::Text("signature".into()), Value::Bytes(sig)));
        }
        let mut wire = Vec::new();
        ciborium::ser::into_writer(&Value::Map(entries), &mut wire).unwrap();
        wire
    }

    #[test]
    fn missing_signature_is_reported_as_such_with_two_roots() {
        let signer = SigningKey::generate();
        let wire = structural_wire(None);
        let result = verify_with_two_roots("missing-sig", &signer, SignerSorts::First, &wire);
        assert_signer_error(result, "bundle has no signature field");
    }

    #[test]
    fn non_der_signature_is_reported_as_such_with_two_roots() {
        let signer = SigningKey::generate();
        let wire = structural_wire(Some(vec![0xde, 0xad, 0xbe, 0xef]));
        let result = verify_with_two_roots("bad-der", &signer, SignerSorts::First, &wire);
        assert_signer_error(result, "signature not ASN.1 DER");
    }

    // The fall-through is intact: when NO trusted root signed the bundle,
    // the answer is the untrusted-CA hint, naming the signer's ca_id when
    // the bundle carries one and saying so when it does not.
    #[test]
    fn bundle_signed_by_neither_root_reports_untrusted_ca() {
        let stranger = SigningKey::generate();
        let root_a = SigningKey::generate();
        let wire = mint_fixture_bundle(&stranger, 86_400, 3600, true);
        // `stranger` is the "signer" argument only so the helper has a key
        // for the first slot; it is deliberately NOT in the store.
        let result = verify_with_two_roots("neither", &root_a, SignerSorts::First, &wire);
        let err = result.expect_err("unsigned-by-any-root bundle must be refused");
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
        let msg = format!("{err:?}");
        assert!(
            msg.contains("does not trust that CA") && msg.contains("test-ca"),
            "expected the untrusted-CA hint naming ca_id, got: {msg}"
        );
    }

    #[test]
    fn bundle_signed_by_neither_root_without_ca_id_reports_generic_untrusted_ca() {
        let stranger = SigningKey::generate();
        let root_a = SigningKey::generate();
        // mint_bundle_with_expires carries no ca_id.
        let wire = mint_bundle_with_expires(&stranger, Some(Value::Integer(86_400.into())));
        let result = verify_with_two_roots("neither-no-caid", &root_a, SignerSorts::First, &wire);
        let err = result.expect_err("unsigned-by-any-root bundle must be refused");
        assert_eq!(err.code(), "E_BUNDLE_INVALID");
        let msg = format!("{err:?}");
        assert!(
            msg.contains("names no ca_id"),
            "expected the generic untrusted-CA hint, got: {msg}"
        );
    }
}

// SPDX-License-Identifier: Apache-2.0
//
// Time + URI-SAN parsing helpers used by both panoscope and flashlight.

/// Convert an `Instant` plus the current `SystemTime::now()` to a millis
/// stamp suitable for serialization. We use `SystemTime` for cross-process
/// timestamps; `Instant` is used internally for monotonic measurement.
pub fn now_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}

/// Parse the principal value from a goat cert URI-SAN, scheme-agnostically.
///
/// The URI-SAN scheme is per-goatnet (nova-a `urn:goat-nova:vendor=<v>`, ZAGROS
/// `urn:goat:zagros:org=<v>`); see docs/design/portal-lexicon.md §"Cert identity
/// layer". Any `urn:goat…` URN carrying a `<kind>=<value>` tail yields
/// `<value>`. A goat URN with no `=` (e.g. `urn:goat-nova:operator`) or a
/// non-goat string returns `None`. Hardcoding `urn:goat-nova:vendor=` here is
/// what silently broke for ZAGROS before the severance.
pub fn extract_vendor(uri_san: &str) -> Option<String> {
    let rest = uri_san.strip_prefix("urn:goat")?;
    rest.rsplit_once('=')
        .map(|(_, v)| v.to_string())
        .filter(|v| !v.is_empty())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn extract_vendor_parses_uri_san() {
        assert_eq!(
            extract_vendor("urn:goat-nova:vendor=org11x"),
            Some("org11x".to_string())
        );
        // ZAGROS per-goatnet scheme must parse too (the severance).
        assert_eq!(
            extract_vendor("urn:goat:zagros:org=57abfbfde0bbae3b4ddc8fb0de46729d"),
            Some("57abfbfde0bbae3b4ddc8fb0de46729d".to_string())
        );
        assert_eq!(extract_vendor("urn:goat-nova:operator"), None); // no '=' tail
        assert_eq!(extract_vendor("urn:goat:zagros:org="), None); // empty value
        assert_eq!(extract_vendor("not-a-urn"), None);
    }

    #[test]
    fn now_ms_returns_recent_timestamp() {
        let t = now_ms();
        // Sanity: after Jan 1 2026 (date >= 1735689600000 ms)
        assert!(t > 1_735_689_600_000);
    }
}

// goat-cli glue for the slot alias display layer (ADR 1039). The resolver
// itself lives in panoscope-core::alias; this is the thin per-command load +
// display helper.

use crate::profile::Profile;
use panoscope_core::alias::{AliasRegistry, ResolveError};
use panoscope_core::envelope::EnvelopeRegistry;

/// Build the alias registry from the active profile's portal, degrading to an
/// empty (UUID-only) registry on any error — a missing profile, no portal, an
/// unreachable portal, a non-2xx. Aliases are an enhancement, never a
/// dependency: a portal hiccup must never block a command (ADR 1039 §4.3).
pub(crate) async fn load_aliases(profile: &Profile) -> AliasRegistry {
    let Ok(client) = profile.portal_client() else {
        return AliasRegistry::empty();
    };
    match client.list_aliases().await {
        Ok(rows) => AliasRegistry::from_rows(rows),
        Err(_) => AliasRegistry::empty(),
    }
}

/// Build the throughput-envelope registry from the active profile's portal
/// (ADR 1045), degrading to an empty registry on any error — same contract as
/// `load_aliases`: an envelope is an enhancement (`goat top` shows an honest
/// `—` when none is known), never a dependency, so a portal hiccup never
/// blocks the dashboard.
pub(crate) async fn load_envelopes(profile: &Profile) -> EnvelopeRegistry {
    let Ok(client) = profile.portal_client() else {
        return EnvelopeRegistry::empty();
    };
    match client.list_envelopes().await {
        Ok(rows) => EnvelopeRegistry::from_rows(rows),
        Err(_) => EnvelopeRegistry::empty(),
    }
}

/// Render a (canonical, UUID-anchored) key for human display, honoring
/// `--uuid` which forces the raw form. `--json` callers must use the raw key
/// directly and never route through here.
pub(crate) fn display_key(reg: &AliasRegistry, uuid_mode: bool, key: &str) -> String {
    if uuid_mode {
        key.to_string()
    } else {
        reg.render_key(key)
    }
}

/// Resolve aliases in a key, accounting for the reserved `volatile/` prefix
/// (ADR 1032) where the identity anchor is the SECOND segment
/// (`volatile/<peer>/...`) rather than the leading one. Routing a volatile key
/// straight through `resolve_key` would treat the literal `volatile` as the
/// slot anchor and fail closed against a known alias set. Outside `volatile/`
/// this is identical to `reg.resolve_key`.
pub(crate) fn resolve_key_volatile_aware(
    reg: &AliasRegistry,
    key: &str,
) -> Result<String, ResolveError> {
    match key.strip_prefix("volatile/") {
        Some(rest) => Ok(format!("volatile/{}", reg.resolve_key(rest)?)),
        None => reg.resolve_key(key),
    }
}

/// Display counterpart to `resolve_key_volatile_aware`: render the peer alias
/// inside a `volatile/<peer>/...` key (or the leading segment otherwise),
/// honoring `--uuid`. Never errors.
pub(crate) fn display_key_volatile_aware(
    reg: &AliasRegistry,
    uuid_mode: bool,
    key: &str,
) -> String {
    if uuid_mode {
        return key.to_string();
    }
    match key.strip_prefix("volatile/") {
        Some(rest) => format!("volatile/{}", reg.render_key(rest)),
        None => reg.render_key(key),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use panoscope_core::portal::AliasRow;

    const ANCHOR: &str = "e7a9c75b532e3d434bb09fefd3ce1ec7";

    fn reg() -> AliasRegistry {
        AliasRegistry::from_rows([AliasRow {
            slot_id: ANCHOR.to_string(),
            alias: "maintainer-operator".into(),
        }])
    }

    #[test]
    fn display_key_renders_alias_by_default() {
        assert_eq!(
            display_key(&reg(), false, &format!("{ANCHOR}/obs/v1")),
            "maintainer-operator/obs/v1"
        );
    }

    #[test]
    fn display_key_uuid_mode_is_canonical_passthrough() {
        let canonical = format!("{ANCHOR}/obs/v1");
        assert_eq!(display_key(&reg(), true, &canonical), canonical);
    }

    #[test]
    fn display_key_empty_registry_falls_back_to_truncation() {
        // Portal-down degraded state: no alias known → truncated UUID.
        assert_eq!(
            display_key(&AliasRegistry::empty(), false, &format!("{ANCHOR}/obs/v1")),
            "e7a9c75b…/obs/v1"
        );
    }

    #[test]
    fn volatile_resolve_targets_the_peer_segment_not_the_literal() {
        // `volatile` is a reserved literal, not a slot anchor — the alias to
        // resolve is the peer in the SECOND segment. Routing the whole key
        // through resolve_key would reject `volatile` as an unknown alias.
        assert_eq!(
            resolve_key_volatile_aware(&reg(), "maintainer-operator/blob/model-v3").unwrap(),
            format!("{ANCHOR}/blob/model-v3")
        );
        assert_eq!(
            resolve_key_volatile_aware(&reg(), "volatile/maintainer-operator/blob/model-v3").unwrap(),
            format!("volatile/{ANCHOR}/blob/model-v3")
        );
    }

    #[test]
    fn volatile_resolve_unknown_peer_fails_closed() {
        // A typo in the peer segment of a volatile key must still fail closed
        // against a known alias set (no phantom-prefix F-177 trap).
        assert!(resolve_key_volatile_aware(&reg(), "volatile/notapeer/blob/x").is_err());
    }

    #[test]
    fn volatile_display_renders_the_peer_segment() {
        assert_eq!(
            display_key_volatile_aware(&reg(), false, &format!("volatile/{ANCHOR}/blob/model-v3")),
            "volatile/maintainer-operator/blob/model-v3"
        );
        // --uuid mode is a canonical passthrough.
        let canonical = format!("volatile/{ANCHOR}/blob/model-v3");
        assert_eq!(
            display_key_volatile_aware(&reg(), true, &canonical),
            canonical
        );
    }
}

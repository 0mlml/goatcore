// `goat ui [<target>]` — Phase D of the goat-cli terminal inspection
// surface: the merged picker.
//
// The picker coalesces two vantage points on "what can I point at?":
//   - DECLARED — the portal catalog (vendors → topics), via PortalClient.
//   - LIVE     — panoscope's `/api/snapshot` (what's actually flowing).
// They join on the topic key, and the high-value signal is the *mismatch*
// between the two facets — `declared ✓ + live ✗` (registered but idle) and
// `live ✓ + declared ✗` (publishing but never declared — the DRS/F-162
// shape). Both are invisible in either single surface; the merged roster
// promotes them to a glanceable column.
//
// From a selected row the operator hops to a lens on the same target
// (Block 94 "hops preserve the target"): `d` → Diagnose (stethoscope),
// `t` → Tail (live slice). Each lens owns its own terminal lifecycle, so
// the orchestration loop here tears the picker down, runs the lens, then
// re-launches the picker (re-fetching live state) with the target
// preselected.
//
// DELEGATION GUARDRAIL (docs/design/goat-cli-tui.md): the picker only
// *aggregates* two reads and *marks membership* (declared/live/both).
// Calling a mismatch "unhealthy" is a verdict — that stays in stethoscope,
// one keystroke away via the Diagnose lens. No verdict logic lives here.
//
// MVP scope (Phase D): topic-level merged roster + local search + detail
// + lens hops + graceful degrade (declared-only when snapshot is
// unreachable; live-only when the portal is disabled). Peer pivot
// (Sources/Derivers/Sinks) and `/api/index`-backed typeahead are noted
// follow-ons in the design doc.

// The panoscope `/api/snapshot` wire model lives in `cmd/flow.rs` — that
// command is the one whose whole job is per-topic flow state, and a second
// copy here would drift (it already did: this module read the undecayed
// `ema_eps` and dropped `last_event_ms` entirely).
use crate::cmd::flow::{fetch_snapshot, Snapshot};
use crate::tui::picker::{self, DeclaredFacet, LiveFacet, PeerRow, PickerAction, RosterRow};
use clap::Args;
use futures::stream::{self, StreamExt};
use std::collections::BTreeMap;

/// Cap on concurrent per-vendor `list_topics` reads in [`fetch_declared`]. A
/// fleet has ~90 vendors; unbounded `join_all` fired all of them at once and
/// self-inflicted a portal 429 storm (every request landing in the same
/// ~200µs window). 16 keeps the read fast (a handful of batches, not 90
/// sequential round-trips — the "tens of seconds" the concurrent design was
/// built to avoid) without hammering the portal in one burst.
const FETCH_DECLARED_CONCURRENCY: usize = 16;

/// What happened while assembling the declared-topic roster — kept
/// alongside the topics themselves so a caller can say honestly whether the
/// list is complete, rather than a vendor's read failing (rate-limited,
/// timeout, whatever) silently rendering identically to "this vendor has
/// zero declared topics".
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub(crate) struct DeclaredFetchStatus {
    /// No fetch was attempted at all — the caller never had a portal to ask
    /// (disabled for this profile). Distinct from `roster_unreachable`: a
    /// disabled portal isn't a failed call, and saying "portal reports zero
    /// declared topics" for a portal that was never asked would be its own
    /// false claim.
    pub not_queried: bool,
    /// `list_vendors()` itself failed — `declared` is empty because we never
    /// even got the vendor list, not because the goatnet has none.
    pub roster_unreachable: bool,
    /// How many vendors' `list_topics()` call failed (rate-limited, timeout,
    /// non-2xx, ...). Their topics are silently missing from `declared`.
    pub vendors_unreachable: usize,
    /// Total vendors the roster named, for an "N/M" caveat.
    pub total_vendors: usize,
}

/// The two collections the picker browses: the topic-spine roster
/// (declared ⊕ live) and the fabric-only peer pivot.
pub(crate) struct Roster {
    pub topics: Vec<RosterRow>,
    pub peers: Vec<PeerRow>,
}

#[derive(Args, Debug)]
pub struct UiArgs {
    /// Optional target to pre-select on open (a topic key, e.g.
    /// `org11x/observations/v1`).
    pub target: Option<String>,
    /// Override the panoscope snapshot endpoint. Default: derived from
    /// the profile's portal_endpoint (`portal.*` → `panoscope.*`), also
    /// honors `$GOAT_PANOSCOPE_ENDPOINT`.
    #[arg(long)]
    pub snapshot_endpoint: Option<String>,
}

pub async fn run(ctx: &super::Ctx, args: UiArgs) -> anyhow::Result<()> {
    // TTY-gate FIRST — a piped/CI `goat ui` refuses cheaply before any
    // portal/snapshot read, exactly like the other lenses.
    crate::tui::require_tty("goat ui")?;

    // Resolve the two read sources up front. Both are allowed to be
    // absent: the picker degrades to whichever facet it can see.
    let portal = ctx.profile.portal_client().ok();
    let snapshot_endpoint =
        crate::cmd::inspect::resolve_panoscope_endpoint(args.snapshot_endpoint.as_deref(), &ctx.profile)
            .ok();
    let token = ctx.profile.portal_token().unwrap_or_default();

    let mut preselect = args.target.clone();
    loop {
        let roster = build_roster(portal.as_ref(), snapshot_endpoint.as_deref(), &token).await;
        match picker::run(roster.topics, roster.peers, preselect.take())? {
            PickerAction::Quit => break,
            // The picker hands back a fully-resolved target. Diagnose
            // carries a `topic=…`/`vendor=…` param (build_url-ready);
            // Tail carries a key expression to subscribe to. Re-select
            // the same target when the lens hands control back.
            PickerAction::Diagnose { target, preselect: ps } => {
                let endpoint = crate::cmd::inspect::resolve_endpoint(None, &ctx.profile)?;
                let url = crate::cmd::inspect::build_url(&endpoint, &target)?;
                crate::tui::diagnose::run(url, target).await?;
                preselect = ps;
            }
            PickerAction::Tail { key_expr, preselect: ps } => {
                // `.ok()` used to make "no portal.toml has a portal_endpoint"
                // (expected, degrades to raw-only decoding) and "portal_client()
                // errored for some other reason" (e.g. a profile read failure)
                // render the exact same "no portal in this profile" message —
                // warn on the actual cause instead of collapsing it.
                let decode = match ctx.profile.portal_client() {
                    Ok(p) => Some(super::decode::DecodeCache::new(p)),
                    Err(e) => {
                        eprintln!("⚠ decoding unavailable, showing raw bytes only: {e}");
                        None
                    }
                };
                let session = super::pubsub::open_session_for(&ctx.profile).await?;
                crate::tui::tail::run(
                    session,
                    ctx.profile.router_endpoint_label(),
                    decode,
                    key_expr,
                )
                .await?;
                preselect = ps;
            }
        }
    }
    Ok(())
}

/// Build the merged declared⊕live roster from whichever sources are
/// reachable. Pure assembly on top of two reads; the join itself
/// (`merge_roster`) is a separate testable function.
async fn build_roster(
    portal: Option<&panoscope_core::portal::PortalClient>,
    snapshot_endpoint: Option<&str>,
    token: &str,
) -> Roster {
    let declared = match portal {
        Some(p) if p.is_enabled() => {
            let (declared, status) = fetch_declared(p).await;
            // Same degrade-visibly pattern as the snapshot fetch below: a
            // failed read used to render identically to "nothing to report".
            if status.roster_unreachable {
                eprintln!("⚠ vendor roster unreachable, showing no declared topics");
            } else if status.vendors_unreachable > 0 {
                eprintln!(
                    "⚠ {}/{} vendor(s) unreachable while listing declared topics — roster may be incomplete",
                    status.vendors_unreachable, status.total_vendors
                );
            }
            declared
        }
        _ => Vec::new(),
    };
    // One snapshot read feeds both the live topic facets and the peer
    // pivot, so fetch it once here rather than twice.
    //
    // `.ok()` used to drop a real fetch error on the floor, identical to
    // "no endpoint configured" (`None`) — PickerState has no field to hold
    // an error, so "portal_client() erroring" and "no portal in this
    // profile" rendered the same "no portal" message, misdiagnosing the
    // first case. Warn on stderr (not woven into PickerState, to avoid
    // rearchitecting the picker's tested render path for a degrade path)
    // so the distinction is at least visible instead of silent.
    let snap = match snapshot_endpoint {
        Some(ep) => match fetch_snapshot(ep, token).await {
            Ok(s) => Some(s),
            Err(e) => {
                eprintln!("⚠ live snapshot fetch from {ep} failed, showing declared-only: {e}");
                None
            }
        },
        None => None,
    };
    // Ages are computed against panoscope's OWN clock (`generated_at_ms`),
    // not this laptop's — otherwise every age is off by the operator's NTP
    // drift. Fall back to the local clock only for older servers that don't
    // stamp the envelope.
    let as_of_ms = snap
        .as_ref()
        .map(|s| s.generated_at_ms)
        .filter(|ms| *ms > 0)
        .unwrap_or_else(now_unix_ms_u64);
    let live = snap
        .as_ref()
        .map(|s| aggregate_live(s, as_of_ms))
        .unwrap_or_default();
    let peers = snap.as_ref().map(aggregate_peers).unwrap_or_default();
    Roster {
        topics: merge_roster(declared, live),
        peers,
    }
}

/// Declared topics across every vendor the portal lists, plus an honest
/// account of what couldn't be read. The per-vendor topic reads run
/// **concurrently, capped at [`FETCH_DECLARED_CONCURRENCY`]** (a fleet has
/// ~90 vendors; fully sequential reads blocked the picker's first draw for
/// tens of seconds, and fully unbounded reads self-inflicted a portal 429
/// storm — every request landing in the same instant). Best-effort: a
/// vendor whose topic list errors contributes nothing to `declared`, but
/// the caller gets a count of how many vendors that happened to, instead of
/// an incomplete roster silently rendering as a complete one.
pub(crate) async fn fetch_declared(
    portal: &panoscope_core::portal::PortalClient,
) -> (Vec<DeclaredTopic>, DeclaredFetchStatus) {
    let vendors = match portal.list_vendors().await {
        Ok(v) => v,
        Err(_) => {
            return (
                Vec::new(),
                DeclaredFetchStatus {
                    roster_unreachable: true,
                    ..Default::default()
                },
            )
        }
    };
    let total_vendors = vendors.len();
    let per_vendor: Vec<Result<Vec<panoscope_core::portal::Topic>, ()>> =
        stream::iter(vendors.iter())
            .map(|v| async move { portal.list_topics(&v.prefix).await.map_err(|_| ()) })
            .buffer_unordered(FETCH_DECLARED_CONCURRENCY)
            .collect()
            .await;
    let vendors_unreachable = per_vendor.iter().filter(|r| r.is_err()).count();
    let declared = per_vendor
        .into_iter()
        .filter_map(Result::ok)
        .flatten()
        .map(|t| DeclaredTopic {
            key: t.key_pattern,
            vendor: t.vendor_prefix,
            format: t.format,
            schema_ref: t.schema_ref,
            description: t.description,
            last_seen_at: t.last_seen_at,
        })
        .collect();
    (
        declared,
        DeclaredFetchStatus {
            not_queried: false,
            roster_unreachable: false,
            vendors_unreachable,
            total_vendors,
        },
    )
}

/// A declared topic, flattened from the portal's per-vendor lists.
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct DeclaredTopic {
    pub key: String,
    pub vendor: String,
    pub format: String,
    pub schema_ref: String,
    pub description: String,
    /// The registry's own monotonic last-publish stamp. `None` on every
    /// portal build that has not yet wired the audit tailer to stamp it —
    /// which is all of them today, hence `goat flow`'s portal-registry tier
    /// reporting itself skipped rather than inventing a zero.
    pub last_seen_at: Option<i64>,
}

/// A live topic, aggregated from the panoscope snapshot.
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct LiveTopic {
    pub key: String,
    pub eps: f64,
    pub pubs: usize,
    pub subs: usize,
    /// Raw stamp, folded across a topic's edges. `None` when panoscope has
    /// no observation — NOT zero, which would render as epoch-1970. Only
    /// used to derive `last_age_ms`; the renderer consumes the age.
    pub last_event_ms: Option<u64>,
    /// Precomputed against the snapshot's own clock, so the renderer needs
    /// no clock of its own.
    pub last_age_ms: Option<i64>,
}

fn now_unix_ms_u64() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}

/// Fold the snapshot nodes into the fabric-only peer pivot: one row per
/// peer, carrying its role class (Sources/Derivers/Sinks), vendor, and
/// publish/subscribe topic lists. Pure → unit-tested.
fn aggregate_peers(snap: &Snapshot) -> Vec<PeerRow> {
    let mut peers: Vec<PeerRow> = snap
        .nodes
        .iter()
        .map(|n| PeerRow {
            name: n
                .display_name
                .clone()
                .filter(|s| !s.is_empty())
                .unwrap_or_else(|| n.peer_id.clone()),
            vendor: n.vendor.clone().filter(|s| !s.is_empty()),
            role_class: n.role_class.clone().filter(|s| !s.is_empty()),
            publishes: n.publishes.clone(),
            subscribes: n.subscribes.clone(),
        })
        .collect();
    // Group visually by role class (Sources/Derivers/Sinks), then name.
    peers.sort_by(|a, b| {
        a.role_class
            .as_deref()
            .unwrap_or("~")
            .cmp(b.role_class.as_deref().unwrap_or("~"))
            .then_with(|| a.name.cmp(&b.name))
    });
    peers
}

/// Fold a raw snapshot into per-topic live facets: rate summed across a
/// topic's edges, last-event the max, pub/sub counts from node
/// publish/subscribe lists. Pure over the parsed snapshot so it's
/// testable without HTTP.
///
/// Two honesty rules live here. The rate comes from `SnapEdge::rate()`,
/// which prefers panoscope's `projected_eps` — the raw `ema_eps` only
/// decays when an event ARRIVES, so summing it reports a stale non-zero
/// rate for a topic that went quiet an hour ago. And a `last_event_ms` of
/// 0 stays `None` rather than becoming a 1970 timestamp: zero means
/// "seeded from a node's publish list, never observed", not "published at
/// the epoch".
fn aggregate_live(snap: &Snapshot, as_of_ms: u64) -> Vec<LiveTopic> {
    crate::cmd::flow::fold_snapshot(snap)
        .topics
        .into_iter()
        .map(|(key, f)| LiveTopic {
            key,
            eps: f.eps,
            pubs: f.pubs,
            subs: f.subs,
            last_event_ms: f.last_event_ms,
            // Aged here, against the SNAPSHOT's clock, so the renderer needs
            // no clock of its own.
            last_age_ms: f
                .last_event_ms
                .map(|ms| as_of_ms.saturating_sub(ms) as i64),
        })
        .collect()
}

/// Join declared topics with live topics on the topic key. The union of
/// keys becomes the roster; each row carries whichever facets exist.
/// Sorted by key for a stable display. Pure — the heart of the picker,
/// fully unit-tested.
pub(crate) fn merge_roster(declared: Vec<DeclaredTopic>, live: Vec<LiveTopic>) -> Vec<RosterRow> {
    let mut rows: BTreeMap<String, RosterRow> = BTreeMap::new();
    for d in declared {
        rows.entry(d.key.clone())
            .or_insert_with(|| RosterRow::new(d.key.clone()))
            .declared = Some(DeclaredFacet {
            vendor: d.vendor,
            format: d.format,
            schema_ref: d.schema_ref,
            description: d.description,
        });
    }
    for l in live {
        rows.entry(l.key.clone())
            .or_insert_with(|| RosterRow::new(l.key.clone()))
            .live = Some(LiveFacet {
            eps: l.eps,
            pubs: l.pubs,
            subs: l.subs,
            last_age_ms: l.last_age_ms,
        });
    }
    rows.into_values().collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    // Only the tests name the edge/node types directly — the production
    // folds reach them through `Snapshot`'s fields.
    use crate::cmd::flow::{SnapEdge, SnapNode};

    fn dt(key: &str, vendor: &str) -> DeclaredTopic {
        DeclaredTopic {
            key: key.into(),
            vendor: vendor.into(),
            format: "protobuf:X".into(),
            schema_ref: "".into(),
            description: "".into(),
            last_seen_at: None,
        }
    }
    fn lt(key: &str, eps: f64) -> LiveTopic {
        LiveTopic {
            key: key.into(),
            eps,
            pubs: 1,
            subs: 2,
            last_event_ms: Some(100),
            last_age_ms: Some(0),
        }
    }
    fn edge(topic: &str, projected: f64, last_event_ms: u64) -> SnapEdge {
        SnapEdge {
            topic: topic.into(),
            ema_eps: projected,
            projected_eps: Some(projected),
            last_event_ms,
            folded_singletons_count: None,
        }
    }
    fn node(pubs: &[&str], subs: &[&str]) -> SnapNode {
        SnapNode {
            peer_id: "peer".into(),
            display_name: None,
            vendor: None,
            role_class: None,
            publishes: pubs.iter().map(|s| s.to_string()).collect(),
            subscribes: subs.iter().map(|s| s.to_string()).collect(),
        }
    }

    #[test]
    fn merge_unions_keys_and_carries_facets() {
        let rows = merge_roster(
            vec![dt("v/a/v1", "v"), dt("v/idle/v1", "v")],
            vec![lt("v/a/v1", 12.0), lt("rogue/undeclared", 3.0)],
        );
        let keys: Vec<&str> = rows.iter().map(|r| r.key.as_str()).collect();
        // sorted union of declared + live keys
        assert_eq!(keys, ["rogue/undeclared", "v/a/v1", "v/idle/v1"]);
    }

    #[test]
    fn merge_marks_three_membership_classes() {
        let rows = merge_roster(
            vec![dt("both/v1", "v"), dt("declared-idle/v1", "v")],
            vec![lt("both/v1", 5.0), lt("live-undeclared/v1", 9.0)],
        );
        let find = |k: &str| rows.iter().find(|r| r.key == k).unwrap();
        // both facets
        let both = find("both/v1");
        assert!(both.declared.is_some() && both.live.is_some());
        // declared, never live (registered but idle)
        let idle = find("declared-idle/v1");
        assert!(idle.declared.is_some() && idle.live.is_none());
        // live, never declared (the F-162 shape)
        let rogue = find("live-undeclared/v1");
        assert!(rogue.declared.is_none() && rogue.live.is_some());
    }

    #[test]
    fn aggregate_live_sums_eps_and_counts_pub_sub() {
        let snap = Snapshot {
            nodes: vec![
                node(&["t/1"], &[]),
                node(&[], &["t/1"]),
                node(&[], &["t/1"]),
            ],
            edges: vec![edge("t/1", 4.0, 50), edge("t/1", 6.0, 90)],
            generated_at_ms: 1_000,
        };
        let live = aggregate_live(&snap, 1_000);
        assert_eq!(live.len(), 1);
        let t = &live[0];
        assert_eq!(t.key, "t/1");
        assert!((t.eps - 10.0).abs() < 1e-9); // summed across edges
        assert_eq!(t.last_event_ms, Some(90)); // max
        assert_eq!(t.last_age_ms, Some(910)); // against the SNAPSHOT clock
        assert_eq!(t.pubs, 1);
        assert_eq!(t.subs, 2);
    }

    #[test]
    fn aggregate_live_uses_the_decayed_rate_not_the_raw_ema() {
        // A topic that published once an hour ago keeps a non-zero `ema_eps`
        // forever (it only decays on arrival), so summing the raw EMA showed
        // a dead topic as busy. `projected_eps` is what panoscope publishes
        // for read paths.
        let snap = Snapshot {
            nodes: vec![],
            edges: vec![SnapEdge {
                topic: "t/quiet".into(),
                ema_eps: 12.4,
                projected_eps: Some(0.0),
                last_event_ms: 5,
            folded_singletons_count: None,
        }],
            generated_at_ms: 3_600_005,
        };
        let live = aggregate_live(&snap, 3_600_005);
        assert_eq!(live[0].eps, 0.0);
        assert_eq!(live[0].last_age_ms, Some(3_600_000));
    }

    #[test]
    fn a_topic_only_seen_in_a_publish_list_has_no_last_event() {
        // Seeded from a node's `publishes` with no edge — zero here would
        // render as a 1970 timestamp rather than "never observed".
        let snap = Snapshot {
            nodes: vec![node(&["t/declared-only"], &[])],
            edges: vec![],
            generated_at_ms: 5_000,
        };
        let live = aggregate_live(&snap, 5_000);
        assert_eq!(live[0].last_event_ms, None);
        assert_eq!(live[0].last_age_ms, None);
    }

    #[test]
    fn empty_inputs_yield_empty_roster() {
        assert!(merge_roster(vec![], vec![]).is_empty());
    }

    #[test]
    fn aggregate_peers_carries_pub_sub_and_sorts_by_role_then_name() {
        let snap = Snapshot {
            nodes: vec![
                SnapNode {
                    peer_id: "p-zed".into(),
                    display_name: Some("zed".into()),
                    vendor: Some("v".into()),
                    role_class: Some("sink".into()),
                    publishes: vec![],
                    subscribes: vec!["t/1".into()],
                },
                SnapNode {
                    peer_id: "p-acme".into(),
                    display_name: Some("acme".into()),
                    vendor: Some("v".into()),
                    role_class: Some("source".into()),
                    publishes: vec!["t/1".into(), "t/2".into()],
                    subscribes: vec![],
                },
            ],
            edges: vec![],
            generated_at_ms: 0,
        };
        let peers = aggregate_peers(&snap);
        assert_eq!(peers.len(), 2);
        // sorted by role_class: "sink" < "source"
        assert_eq!(peers[0].name, "zed");
        assert_eq!(peers[0].role_class.as_deref(), Some("sink"));
        assert_eq!(peers[1].name, "acme");
        assert_eq!(peers[1].publishes, vec!["t/1", "t/2"]);
        // empty display_name falls back to peer_id
        let snap2 = Snapshot {
            nodes: vec![node(&["t/9"], &[])],
            edges: vec![],
            generated_at_ms: 0,
        };
        assert_eq!(aggregate_peers(&snap2)[0].name, "peer");
    }
}

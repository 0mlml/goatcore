// SPDX-License-Identifier: Apache-2.0
//
// What survives the hop, what does not, and why — stated here because
// republishing creates a NEW sample, and a republisher that quietly restamps
// data is rewriting provenance.
//
// ┌──────────────────┬───────────────────────────────────────────────────────┐
// │ payload          │ forwarded byte-for-byte                               │
// │ encoding         │ forwarded verbatim                                    │
// │ attachment       │ forwarded verbatim (see the note below)               │
// │ timestamp INSTANT│ PRESERVED — the observation time, not the crossing    │
// │ timestamp ID     │ REPLACED with our peering session's ZID               │
// │ SourceInfo       │ DROPPED                                               │
// │ sample kind      │ Put and Delete both forwarded                         │
// └──────────────────┴───────────────────────────────────────────────────────┘
//
// WHY THE INSTANT IS PRESERVED. A zenoh timestamp is an HLC pair: an NTP64
// instant and the identifier of the node that stamped it. The receiver uses
// the instant for last-writer-wins in its storage and for freshness. Restamping
// at the crossing would tell the receiver the data was observed when the link
// happened to carry it — false, and false in the direction that makes stale
// data look fresh. So the instant is copied verbatim.
//
// WHY THE IDENTIFIER IS NOT. The identifier is the ZID of one of our internal
// nodes. The receiver cannot verify it (the drill record already states that
// provenance markers cross unverified — nothing compares them to the transport
// peer), so it carries no evidence; and a node identifier is precisely the
// inventory a sovereign does not hand over (cross-sovereign-peering-transport.md
// §1, "Disclosure"). Replacing it with our peering session's ZID makes the
// timestamp say what is actually true and enforceable: this instant, asserted
// by the party publishing under `from/<us>/`.
//
// WHY SourceInfo IS DROPPED. Same two reasons, plus a third: it also carries
// the origin publisher's own sequence number, which means nothing in our
// sequence space. Forwarding it would place a second, unverifiable origin claim
// beside the one the far edge actually enforces — the `from/<us>/` prefix — and
// a reader comparing them would have no way to know which one is load-bearing.
// Dropped samples are COUNTED, so the choice is visible rather than invisible.
//
// THE HONEST GAP. `sample.timestamp()` is `None` when nothing upstream had HLC
// timestamping enabled. The origin instant is then simply not recoverable, and
// no republisher can invent it. Such samples cross with no timestamp, the
// receiving fabric stamps them on arrival, and that stamp is the CROSSING time,
// not the observation time. `goat_fed_republisher_untimestamped_total` counts
// them; a non-zero value means the receiver's clock for that feed is our link's
// clock. The fix is upstream (enable timestamping on the publishing session),
// not here.

use anyhow::Result;
use zenoh::time::{Timestamp, TimestampId};

/// Our peering session's ZID, as the identifier half of an HLC timestamp.
///
/// Taken from the little-endian bytes rather than the rendered hex string: the
/// string form drops leading zeros and the parser refuses them, so the
/// round-trip is a latent startup failure for a minority of otherwise valid
/// identifiers.
pub fn timestamp_id_from_zid(zid_le_bytes: &[u8]) -> Result<TimestampId> {
    TimestampId::try_from(zid_le_bytes)
        .map_err(|e| anyhow::anyhow!("the peering session zid is not a usable timestamp id: {e}"))
}

/// Preserve the instant, replace the identifier.
///
/// `None` in means `None` out: an origin instant that was never recorded cannot
/// be manufactured, and manufacturing one is the failure this whole module
/// exists to avoid.
pub fn rewire_timestamp(original: Option<&Timestamp>, ours: TimestampId) -> Option<Timestamp> {
    original.map(|t| Timestamp::new(*t.get_time(), ours))
}

#[cfg(test)]
mod tests {
    use super::*;
    use zenoh::time::NTP64;

    fn id(bytes: &[u8]) -> TimestampId {
        timestamp_id_from_zid(bytes).unwrap()
    }

    #[test]
    fn the_instant_survives_the_hop_exactly() {
        let origin = Timestamp::new(NTP64(0x0000_1234_5678_9abc), id(&[0xaa, 0xbb, 0xcc, 0xdd]));
        let out = rewire_timestamp(Some(&origin), id(&[0x11, 0x22, 0x33, 0x44])).unwrap();
        assert_eq!(out.get_time(), origin.get_time());
    }

    #[test]
    fn the_internal_node_identifier_does_not_cross() {
        let origin = Timestamp::new(NTP64(42), id(&[0xaa, 0xbb, 0xcc, 0xdd]));
        let ours = id(&[0x11, 0x22, 0x33, 0x44]);
        let out = rewire_timestamp(Some(&origin), ours).unwrap();
        assert_ne!(out.get_id(), origin.get_id());
        assert_eq!(out.get_id(), &ours);
    }

    #[test]
    fn an_absent_instant_is_never_invented() {
        assert!(rewire_timestamp(None, id(&[0x11, 0x22, 0x33, 0x44])).is_none());
    }

    #[test]
    fn an_unusable_zid_is_a_startup_error_not_a_silent_default() {
        assert!(timestamp_id_from_zid(&[0u8; 16]).is_err(), "all-zero");
        assert!(timestamp_id_from_zid(&[1u8; 17]).is_err(), "oversize");
    }

    #[test]
    fn a_zid_with_leading_zero_bytes_still_works() {
        // The hex-string route refuses these; the byte route must not.
        let ours = timestamp_id_from_zid(&[0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00])
            .expect("a low-numbered zid is legal");
        let origin = Timestamp::new(NTP64(7), id(&[0xaa]));
        assert_eq!(
            rewire_timestamp(Some(&origin), ours).unwrap().get_id(),
            &ours
        );
    }
}

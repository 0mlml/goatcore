package api

import (
	"log/slog"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// progressRow is the JSON shape served by /api/progress. max_level is
// derived monotonically so the UI doesn't have to repeat the logic.
type progressRow struct {
	Prefix        string `json:"prefix"`
	DisplayName   string `json:"display_name"`
	Synthetic     bool   `json:"synthetic"`
	L1At          *int64 `json:"l1_at,omitempty"`
	L2At          *int64 `json:"l2_at,omitempty"`
	L3At          *int64 `json:"l3_at,omitempty"`
	L4At          *int64 `json:"l4_at,omitempty"`
	LastPublishAt *int64 `json:"last_publish_at,omitempty"`
	PeerCount     int64  `json:"peer_count"`
	MaxLevel      int    `json:"max_level"`
	// Activity summary (from vendor_topic_activity, audit-tailer fed).
	PublishCount int64 `json:"publish_count"`
	// Pointer because unknown and zero are different answers: nil means the
	// activity source could not be asked, zero means the org has published
	// under no declared topic. Conflating them is what the old per-topic
	// table did.
	DistinctTopics *int64  `json:"distinct_topics,omitempty"`
	LastTopic      *string `json:"last_topic,omitempty"`
	// Where an onboarding actually stalled. The level ladder alone cannot say:
	// L1 (bundle minted) is something the PORTAL did, not something the person
	// did, so a row sitting at L1 could be waiting on a download, a mesh
	// enrolment, or a first publish. These three separate those cases.
	BundleDownloadedAt  *int64 `json:"bundle_downloaded_at,omitempty"`
	BundleDownloadCount int64  `json:"bundle_download_count"`
	RegisteredTopics    int64  `json:"registered_topics"`
	// Stage is the furthest step with POSITIVE evidence behind it, named rather
	// than numbered so a reader does not have to know what "L2" means.
	Stage string `json:"stage"`
}

// listProgress returns the vendor-progress swimlane data. Operator-only
// (no vendor-scoped view; this is a fabric-operator dashboard).
func (s *Server) listProgress(w http.ResponseWriter, r *http.Request) {
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "progress.read", "", "")
	includeSynthetic := r.URL.Query().Get("include") == "synthetic"
	rows, err := s.reg.ListProgress(r.Context(), includeSynthetic)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	// Distinct-topic counts come from the activity source, not the registry
	//. A failure here must not fail the swimlane: every other column
	// is registry-owned and still correct, so the count degrades to unknown
	// and the rest of the screen keeps working.
	prefixes := make([]string, 0, len(rows))
	for _, row := range rows {
		prefixes = append(prefixes, row.Prefix)
	}
	summaries, aerr := s.activity.ForOrgs(r.Context(), prefixes)
	if aerr != nil {
		slog.Warn("activity source failed; swimlane distinct-topic counts render as unknown",
			"source", s.activity.Name(), "err", aerr)
		summaries = nil
	}

	out := make([]progressRow, 0, len(rows))
	for _, row := range rows {
		// nil when unknown, so the JSON omits it and a client cannot mistake
		// "we could not ask" for "this org has published to zero topics".
		var distinct *int64
		if sum, ok := summaries[row.Prefix]; ok && sum.Available {
			n := sum.DistinctTopics
			distinct = &n
		}
		out = append(out, progressRow{
			Prefix:         row.Prefix,
			DisplayName:    row.DisplayName,
			Synthetic:      row.Synthetic,
			L1At:           row.L1At,
			L2At:           row.L2At,
			L3At:           row.L3At,
			L4At:           row.L4At,
			LastPublishAt:  row.LastPublishAt,
			PeerCount:      row.PeerCount,
			MaxLevel:       maxLevel(row.L1At, row.L2At, row.L3At, row.L4At),
			PublishCount:   row.PublishCount,
			DistinctTopics: distinct,
			LastTopic:      row.LastTopic,

			BundleDownloadedAt:  row.BundleDownloadedAt,
			BundleDownloadCount: row.BundleDownloadCount,
			RegisteredTopics:    row.RegisteredTopics,
			Stage:               stageOf(row),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// stageOf names the furthest step with positive evidence behind it. Ordered
// most-progressed first so the first match wins.
//
// Every arm is evidence of something that HAPPENED, never the absence of it.
// "bundle-not-downloaded" is deliberately absent as a stage: a row predating
// the download column has a nil timestamp because nobody was recording, not
// because nobody downloaded, and a stage name that asserts the negative would
// launder that gap into a claim. Absence shows up as the reader seeing
// `bundle_downloaded_at` omitted, which is honestly ambiguous.
func stageOf(row registry.VendorProgressRow) string {
	switch {
	case row.L4At != nil:
		return "publishing"
	case row.L3At != nil:
		return "hello-world-published"
	case row.RegisteredTopics > 0:
		return "topics-registered"
	case row.L2At != nil:
		return "mesh-enrolled"
	case row.BundleDownloadedAt != nil:
		return "bundle-downloaded"
	case row.L1At != nil:
		return "bundle-minted"
	default:
		return "registered"
	}
}

func maxLevel(l1, l2, l3, l4 *int64) int {
	switch {
	case l4 != nil:
		return 4
	case l3 != nil:
		return 3
	case l2 != nil:
		return 2
	case l1 != nil:
		return 1
	default:
		return 0
	}
}

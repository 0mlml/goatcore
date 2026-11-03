package access

import "sort"

// Access is one resolved write/read permission, carrying the deciding
// layer + record so the answer is auditable rather than asserted.
type Access struct {
	Topic  string `json:"topic"`
	Layer  string `json:"layer"`
	Reason string `json:"reason"`
}

// WhoResult is the forward query: given an identity, everything it may
// write and read, with the reason at each layer.
type WhoResult struct {
	Query    string     `json:"query"`
	Resolved string     `json:"resolved"` // node ID the query resolved to ("" if unresolved)
	Kind     NodeKind   `json:"kind,omitempty"`
	Vendor   string     `json:"vendor,omitempty"`
	State    ReconState `json:"state,omitempty"`
	// Affiliation/Nation are the declared identity-grammar attributes (ADR 1043),
	// display-only attribution — never an access input. Empty = none declared.
	Affiliation string `json:"affiliation,omitempty"`
	Nation      string `json:"nation,omitempty"`
	// ReconReason classifies a stray (state=out_of_band): untracked /
	// unregistered-key / no-declared-member — why it didn't reconcile.
	ReconReason ReconReason `json:"recon_reason,omitempty"`
	OnMesh      bool        `json:"on_mesh"`
	// MeshAttribution says WHERE a person's on-mesh answer comes from, so a
	// renderer can be honest about "we know it's absent" vs "we can't know"
	// (persons only; empty for other kinds):
	//   "peer" — the person's own enrolled device(s) matched by the
	//            peer-name custody link; on_mesh/connected/last_seen are theirs.
	//   "org"  — no own device matched; the answer is the org spine's roll-up
	//            (the legacy join — it describes the org, not this person).
	//   "none" — no own device matched and no org peer either: the graph has
	//            nothing to attribute, which is NOT the same as "not connected"
	//            (a device enrolled under a non-conventional name is invisible
	//            here). Render as "unattributed", never a confident "no".
	MeshAttribution string `json:"mesh_attribution,omitempty"`
	// LastSeenUnix is the entity's most recent mesh last_seen (0 = unknown).
	LastSeenUnix int64    `json:"last_seen_unix,omitempty"`
	Connected    bool     `json:"connected,omitempty"`
	Writes       []Access `json:"writes"`
	Reads        []Access `json:"reads"`
	Notes        []string `json:"notes,omitempty"`
	// AnchorLinkage says HOW this person's write anchor was attributed to them:
	// "recorded" (slots.identity_id, stamped at onboard — ground truth) or one of
	// the name-parsing fallbacks, "handle-join" / "tag-join". Empty for non-person
	// kinds and for a person with no anchor at all. Ownership drives revocation
	// targeting, so the surface has to say whether it knows or inferred.
	AnchorLinkage string `json:"anchor_linkage,omitempty"`
}

// MeshAttribution values (WhoResult.MeshAttribution / RosterRow.MeshAttribution).
const (
	MeshAttrPeer = "peer" // own attributed device(s) — the person-level truth
	MeshAttrOrg  = "org"  // org-spine roll-up — describes the org, not the person
	MeshAttrNone = "none" // nothing attributable — honest can't-know, not "no"
)

// resolve maps a free-text identity selector to a graph node. Match order:
// vendor prefix, then person (email / name / id), then mesh peer
// (name / id), then slot id. Returns the node and true on a hit.
func (g *Graph) resolve(query string) (Node, bool) {
	if n, ok := g.Nodes[nodeID(KindVendor, query)]; ok {
		return n, true
	}
	// Declared roster handle -> reconciled person (what `goat access who me`
	// resolves to for a per-person operator key, and what humans type).
	if nid, ok := g.HandleIndex[query]; ok {
		if n, ok := g.Nodes[nid]; ok {
			return n, true
		}
	}
	// persons by email/name, peers by name — scan once, preferring persons.
	var peerHit, slotHit *Node
	for id := range g.Nodes {
		n := g.Nodes[id]
		switch n.Kind {
		case KindPerson:
			if n.Label == query || lastSegment(n.ID) == query {
				return n, true
			}
		case KindMeshPeer:
			if n.Label == query || lastSegment(n.ID) == query {
				nn := n
				peerHit = &nn
			}
		case KindSlot:
			if lastSegment(n.ID) == query || n.Label == query {
				nn := n
				slotHit = &nn
			}
		}
	}
	if peerHit != nil {
		return *peerHit, true
	}
	if slotHit != nil {
		return *slotHit, true
	}
	return Node{}, false
}

// Who answers the forward query. Every identity maps to a vendor spine;
// writes are the org's namespace write grant + may-write edges (an individual
// publishes through the org's slot), reads are the identity's own read-tier
// edges plus the flat community-read grant when the graph carries it.
func (g *Graph) Who(query string) WhoResult {
	n, ok := g.resolve(query)
	if !ok {
		res := WhoResult{Query: query, Writes: []Access{}, Reads: []Access{}}
		res.Notes = append(res.Notes, "no identity matched `"+query+"` — try an org prefix, person email or roster handle, peer name, or slot id")
		return res
	}
	res := g.whoForNode(n)
	res.Query = query
	return res
}

// whoForNode is Who's pure core, computed directly from a resolved node so the
// bulk Roster query and the single forward query share one code path.
func (g *Graph) whoForNode(n Node) WhoResult {
	res := WhoResult{Query: n.Label, Writes: []Access{}, Reads: []Access{}}
	res.Resolved, res.Kind, res.Vendor, res.State = n.ID, n.Kind, n.Vendor, n.State
	res.Affiliation, res.Nation, res.ReconReason = n.Affiliation, n.Nation, n.ReconReason
	if n.Kind == KindPerson {
		res.AnchorLinkage = g.PersonAnchorSource[n.ID]
	}

	vendorNode := g.anchorNodeFor(n)
	vn, hasVendor := g.Nodes[vendorNode]

	// Writes — namespace grant first. A spine with a rendered write anchor may
	// publish its whole `<anchor>/**` namespace (the slot-pool per-uuid grant,
	// ADR 1025, or the seed vendor-prefix rule once ACL-rendered). This is the
	// canonical write line; without it slot-pool members show 0 writes because no
	// per-topic edge is rendered for an anchor that has not declared specific
	// topics.
	if hasVendor && vn.WriteAnchor != "" {
		res.Writes = append(res.Writes, Access{
			Topic: vn.WriteAnchor + "/**", Layer: "zenoh-acl",
			Reason: "write-anchor grant — may publish its own namespace `" + vn.WriteAnchor + "/**` (ADR 1025)",
		})
	}
	// Then any specifically-declared topics under the prefix (org-level
	// may-write edges, vendor → topic).
	for _, e := range g.Edges {
		if e.Kind == EdgeMayWrite && e.From == vendorNode {
			res.Writes = append(res.Writes, Access{Topic: g.label(e.To), Layer: e.Layer, Reason: e.Reason})
		}
	}

	// On-mesh + last-activity. A person's own attributed peers (the peer-name
	// custody edges) are the person-level truth and win; without any, fall back
	// to the org spine's roll-up (the legacy join — it describes the org).
	// MeshAttribution records which source answered, so a renderer can
	// distinguish "known" from "can't know" (the on-mesh honesty gap).
	ownPeers := false
	if n.Kind == KindPerson {
		for _, e := range g.Edges {
			if e.Kind == EdgeOnMesh && e.To == n.ID {
				ownPeers = true
				break
			}
		}
	}
	if ownPeers {
		res.OnMesh = true
		res.LastSeenUnix, res.Connected = n.LastSeenUnix, n.Connected
		res.MeshAttribution = MeshAttrPeer
	} else {
		if hasVendor {
			res.LastSeenUnix, res.Connected = vn.LastSeenUnix, vn.Connected
		}
		for _, e := range g.Edges {
			if e.Kind == EdgeOnMesh && e.To == vendorNode {
				res.OnMesh = true
				break
			}
		}
		if n.Kind == KindPerson {
			if res.OnMesh {
				res.MeshAttribution = MeshAttrOrg
			} else {
				res.MeshAttribution = MeshAttrNone
			}
		}
	}

	// Reads — the flat community-read grant first (every on-mesh identity may
	// read all topics, ADR 1025). Flat is the default; restricted read tiers add
	// their own explicit edges on top.
	if g.CommunityRead {
		res.Reads = append(res.Reads, Access{
			Topic: "**", Layer: "zenoh-acl",
			Reason: "flat community-read — every on-mesh identity may read all topics (ADR 1025)",
		})
	}
	// Then explicit read-tier edges: a person reports its own; a
	// vendor/peer/slot reports the union across the org's people.
	personFilter := ""
	if n.Kind == KindPerson {
		personFilter = n.ID
	}
	seenRead := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind != EdgeMayRead {
			continue
		}
		if personFilter != "" && e.From != personFilter {
			continue
		}
		if personFilter == "" && g.Nodes[e.From].Vendor != n.Vendor {
			continue
		}
		key := e.To + "|" + e.Reason
		if seenRead[key] {
			continue
		}
		seenRead[key] = true
		res.Reads = append(res.Reads, Access{Topic: g.label(e.To), Layer: e.Layer, Reason: e.Reason})
	}

	if n.State == StateCutOff {
		res.Notes = append(res.Notes, "this identity is CUT OFF (declared but negatively enforced) — edges shown are the declared intent, not live access")
	}
	if len(res.Writes) == 0 && n.Kind != KindPerson {
		res.Notes = append(res.Notes, "no write rule rendered for org `"+n.Vendor+"` — it can publish nothing until its Zenoh anchor is in the ACL")
	}
	if len(res.Writes) == 0 && n.Kind == KindPerson {
		// Honesty: zero write rows on a person means "no anchor attributed to
		// this identity", not "known unable to publish" — writes are cert-
		// gated at the router, and a cert/grant outside the vendor/slot
		// registry (e.g. the static operator ops/** rule) is invisible here.
		res.Notes = append(res.Notes, "writes unattributed — no slot/org anchor is linked to this identity in the registry; publishes through certs outside it (e.g. operator certs) are not shown")
	}
	sortAccess(res.Writes)
	sortAccess(res.Reads)
	return res
}

// anchorWriteRoot returns the write-anchor STRING for a node: its own when it
// has one (org spine), otherwise the anchor of the org node it resolves to
// (person). Empty when neither exists. Separate from anchorNodeFor, which
// returns a node ID rather than the anchor value.
func (g *Graph) anchorWriteRoot(n Node) string {
	if n.WriteAnchor != "" {
		return n.WriteAnchor
	}
	if vn, ok := g.Nodes[g.anchorNodeFor(n)]; ok {
		return vn.WriteAnchor
	}
	return ""
}

// anchorNodeFor resolves the vendor/anchor node an identity's org-level
// answers (writes, org on-mesh fallback) hop through. A person's own slot
// anchor (PersonAnchor — the alias↔handle join) wins; the legacy tag-keyed
// hop is the fallback and the answer for non-person kinds. Without the
// alias hop, every person on a slot-pool goatnet (UUID anchors, short-tag
// identities) resolved no anchor and reported 0 writes.
func (g *Graph) anchorNodeFor(n Node) string {
	if n.Kind == KindPerson {
		if vid, ok := g.PersonAnchor[n.ID]; ok {
			return vid
		}
	}
	return nodeID(KindVendor, n.Vendor)
}

// RosterRow is one entity's slim cross-layer summary for the bulk roster: the
// W/R counts + reconcile state + on-mesh/last-activity, for every identity in
// one assembly. It is the responsiveness answer — the CLI roster reads N rows
// from one call instead of one `who` round-trip per selection.
type RosterRow struct {
	ID          string      `json:"id"`    // the selector key (vendor prefix / person id / peer id)
	Label       string      `json:"label"` // human display name
	Kind        NodeKind    `json:"kind"`  // vendor | person | slot | mesh_peer
	Vendor      string      `json:"vendor,omitempty"`
	State       ReconState  `json:"state,omitempty"`
	Affiliation string      `json:"affiliation,omitempty"` // org kind: vendor|organic|partner (ADR 1043)
	Nation      string      `json:"nation,omitempty"`
	ReconReason ReconReason `json:"recon_reason,omitempty"` // why a stray didn't reconcile (safe-sweep signal)
	OnMesh      bool        `json:"on_mesh"`
	// MeshAttribution: where a person's on-mesh answer came from — "peer" (own
	// device) | "org" (org roll-up) | "none" (nothing attributable — render
	// "unattributed", not "no"). Empty for non-person rows.
	MeshAttribution string `json:"mesh_attribution,omitempty"`
	Connected       bool   `json:"connected,omitempty"`
	LastSeenUnix    int64  `json:"last_seen_unix,omitempty"`
	Writes          int    `json:"writes"` // count of write grants (namespace + declared topics)
	Reads           int    `json:"reads"`  // count of read grants (community + read tiers)
	WriteAnchor     string `json:"write_anchor,omitempty"`
	// AnchorLinkage: how this person's anchor was attributed — "recorded"
	// (slots.identity_id) vs the "handle-join" / "tag-join" name fallbacks. Empty
	// for org rows and for a person with no anchor.
	AnchorLinkage string `json:"anchor_linkage,omitempty"`
	// OnboardingStage is the furthest onboarding step with positive evidence
	// behind it, keyed off this row's write anchor (the /api/progress `stage`
	// vocabulary). Empty means no progress row was found for the anchor — the
	// consumer must render that as unknown, NOT as "did not start". The COP
	// carries it so "who is here" and "who never finished arriving" are one
	// question rather than two surfaces.
	OnboardingStage string `json:"onboarding_stage,omitempty"`
}

// Note is one COP note with its severity and a stable kind slug, so a consumer
// can tell "a layer is down, this picture is incomplete" from the standing
// doctrinal notes emitted on every healthy response — without string-matching
// the prose for "unavailable" / "unreachable" / "error:", which breaks the first
// time someone rewords a message.
//
// Text is the SAME string the parallel Notes[] entry carries, verbatim — the two
// are index-aligned, and the strings are unchanged from before this field
// existed, so nothing reading Notes[] needs to change.
type Note struct {
	Text     string `json:"text"`
	Kind     string `json:"kind"`     // stable slug, e.g. mesh_layer_unavailable
	Severity string `json:"severity"` // error | warn | info
}

// Note severities. A consumer deciding "is this picture trustworthy" reads
// SeverityError: it means a layer did not answer and the COP under-reports.
const (
	// SeverityError: a layer is unavailable, unreachable, or errored. Whatever
	// it would have contributed is MISSING from the graph, so absence in the
	// answer is not evidence of absence in the world.
	SeverityError = "error"
	// SeverityWarn: the answer is degraded but present — a stale snapshot, or an
	// attribution resolved by inference rather than a recorded fact.
	SeverityWarn = "warn"
	// SeverityInfo: a standing doctrinal note (the write model, the read model)
	// emitted on every healthy response. Never a fault signal.
	SeverityInfo = "info"
)

// NoteTexts flattens notes to the plain string list the pre-existing `notes[]`
// field carries, preserving order.
func NoteTexts(notes []Note) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Text)
	}
	return out
}

// RosterFreshness carries the declared-registry artifact's provenance so the
// COP can say how current the roster it reconciled against is — and let a
// client compare the served SourceDigest against its own checkout to catch a
// portal that wasn't redeployed after a roster regen (the "strays are suspect"
// caveat, made concrete). Stamped by gen-roster.py; nil when the roster path is
// unset or the file predates the stamp.
type RosterFreshness struct {
	GeneratedAt  string `json:"generated_at,omitempty"`  // ISO-8601 UTC, when gen-roster.py last changed the content
	SourceDigest string `json:"source_digest,omitempty"` // SHA-256 over orgs+members — the drift key
	MemberCount  int    `json:"member_count"`            // declared members in the served roster
	LoadedUnix   int64  `json:"loaded_unix,omitempty"`   // file mtime on the portal host (deploy time)
}

// RosterResult is the bulk forward query over every identity-bearing spine
// node (vendors + persons), one row each, assembled once.
type RosterResult struct {
	Rows  []RosterRow `json:"rows"`
	Notes []string    `json:"notes,omitempty"`
	// NoteDetails is Notes with a severity + kind on each entry, index-aligned
	// with it (same order, same count). Read this to distinguish a failure note
	// from a standing doctrinal one; Notes stays the plain-string rendering.
	NoteDetails []Note           `json:"note_details,omitempty"`
	Freshness   *RosterFreshness `json:"freshness,omitempty"`
}

// Roster returns a slim cross-layer summary for every identity in the graph,
// computed from one assembly. Vendors (the org/slot-anchor spine) and persons
// are the identity-bearing rows; topics, mesh peers, and release regions are
// not standalone roster entities (a peer rolls up to its vendor). The CLI
// keeps the per-selection `who` for the full per-layer reason lists.
func (g *Graph) Roster() RosterResult {
	res := RosterResult{Rows: []RosterRow{}}
	for id := range g.Nodes {
		n := g.Nodes[id]
		if n.Kind != KindVendor && n.Kind != KindPerson {
			continue
		}
		w := g.whoForNode(n)
		key := n.Vendor
		if n.Kind == KindPerson {
			key = lastSegment(n.ID)
		}
		res.Rows = append(res.Rows, RosterRow{
			ID:              key,
			Label:           n.Label,
			Kind:            n.Kind,
			Vendor:          n.Vendor,
			State:           n.State,
			Affiliation:     n.Affiliation,
			Nation:          n.Nation,
			ReconReason:     n.ReconReason,
			OnMesh:          w.OnMesh,
			MeshAttribution: w.MeshAttribution,
			Connected:       w.Connected,
			LastSeenUnix:    w.LastSeenUnix,
			Writes:          len(w.Writes),
			Reads:           len(w.Reads),
			WriteAnchor:     n.WriteAnchor,
			AnchorLinkage:   w.AnchorLinkage,
			// Keyed on the write anchor, which is the one identifier both
			// surfaces agree on: the progress table is anchor-keyed.
			//
			// A PERSON node carries no WriteAnchor of its own — the anchor sits
			// on the org spine it resolves to — so this must hop through the
			// same anchorNodeFor the write grants already use. Reading
			// n.WriteAnchor directly would make every person row render "unknown",
			// which is precisely the population an admin searches by (they type
			// an email, not a 32-hex anchor).
			//
			// A person with no resolvable anchor still gets nothing, correctly:
			// there is no onboarding to have progressed.
			OnboardingStage: g.OnboardingStage[g.anchorWriteRoot(n)],
		})
	}
	sort.SliceStable(res.Rows, func(i, j int) bool {
		if res.Rows[i].Label != res.Rows[j].Label {
			return res.Rows[i].Label < res.Rows[j].Label
		}
		return res.Rows[i].ID < res.Rows[j].ID
	})
	return res
}

// Writer is one identity that may write a topic, with its reason.
type Writer struct {
	Identity string   `json:"identity"`
	Kind     NodeKind `json:"kind"`
	Layer    string   `json:"layer"`
	Reason   string   `json:"reason"`
}

// Reader is one identity that may read a topic.
type Reader struct {
	Identity string `json:"identity"`
	Layer    string `json:"layer"`
	Reason   string `json:"reason"`
}

// TopicResult is the reverse query: given a topic/namespace, who may write
// it, who may read it, and which partners may receive it if released.
type TopicResult struct {
	Topic     string   `json:"topic"`
	Found     bool     `json:"found"`
	Writers   []Writer `json:"writers"`
	Readers   []Reader `json:"readers"`
	Receivers []string `json:"receivers"` // partner orgs via release-set
	Notes     []string `json:"notes,omitempty"`
}

// Topic answers the reverse query for an exact topic key.
func (g *Graph) Topic(key string) TopicResult {
	res := TopicResult{Topic: key, Writers: []Writer{}, Readers: []Reader{}, Receivers: []string{}}
	tid := nodeID(KindTopic, key)
	_, res.Found = g.Nodes[tid]
	if !res.Found {
		res.Notes = append(res.Notes, "topic `"+key+"` is not registered — reverse query is over declared topics")
		return res
	}
	for _, e := range g.Edges {
		switch {
		case e.Kind == EdgeMayWrite && e.To == tid:
			n := g.Nodes[e.From]
			res.Writers = append(res.Writers, Writer{Identity: n.Label, Kind: n.Kind, Layer: e.Layer, Reason: e.Reason})
		case e.Kind == EdgeMayRead && e.To == tid:
			n := g.Nodes[e.From]
			res.Readers = append(res.Readers, Reader{Identity: n.Label, Layer: e.Layer, Reason: e.Reason})
		}
	}
	// Receivers: release-set whose prefix covers this topic key.
	for _, n := range g.Nodes {
		if n.Kind != KindReleaseTopic {
			continue
		}
		if !prefixCovers(n.Label, key) {
			continue
		}
		for _, e := range g.Edges {
			if e.Kind == EdgeMayReceiveRel && e.To == n.ID {
				res.Receivers = append(res.Receivers, g.Nodes[e.From].Label)
			}
		}
	}
	if len(res.Writers) == 0 {
		res.Notes = append(res.Notes, "NO identity may write `"+key+"` — a publish here is silently denied at the router")
	}
	sort.Strings(res.Receivers)
	return res
}

// Step is one layer's verdict in a why-trace.
type Step struct {
	Layer   string `json:"layer"`   // mesh | identity | zenoh | release
	Verdict string `json:"verdict"` // allow | deny | n/a
	Reason  string `json:"reason"`
}

// WhyResult traces a single allow/deny decision for "can <id> write/read
// <key>?" across the four layers, naming the deciding record at each. The
// overall write/read verdict is the AND of the layers that gate it.
type WhyResult struct {
	Query        string   `json:"query"` // the identity selector
	Topic        string   `json:"topic"`
	Resolved     string   `json:"resolved,omitempty"`
	Vendor       string   `json:"vendor,omitempty"`
	WriteVerdict string   `json:"write_verdict"` // allow | deny
	ReadVerdict  string   `json:"read_verdict"`  // allow | deny
	Steps        []Step   `json:"steps"`
	Notes        []string `json:"notes,omitempty"`
}

// Why traces the write + read decision for one identity against one topic
// key across mesh -> identity -> zenoh -> release. Pure over the graph.
//
// Write is gated by mesh transport AND a Zenoh write rule whose anchor
// covers the topic prefix (Ory identity does not gate machine writes).
// Read is gated by mesh transport AND (flat community-read OR an explicit
// read-tier edge). The release step is informational.
func (g *Graph) Why(query, key string) WhyResult {
	res := WhyResult{Query: query, Topic: key, WriteVerdict: "deny", ReadVerdict: "deny", Steps: []Step{}}
	n, ok := g.resolve(query)
	if !ok {
		res.Notes = append(res.Notes, "no identity matched `"+query+"`")
		return res
	}
	res.Resolved, res.Vendor = n.ID, n.Vendor
	vendorNode := g.anchorNodeFor(n)
	tid := nodeID(KindTopic, key)
	_, topicExists := g.Nodes[tid]

	// mesh — a person's own attributed peer (the peer-name custody edge) is
	// checked first; the org spine is the fallback transport answer.
	meshOK := false
	meshReason := "no netbird peer matched org `" + n.Vendor + "` or this person's peer-name prefix"
	if n.Kind == KindPerson {
		for _, e := range g.Edges {
			if e.Kind == EdgeOnMesh && e.To == n.ID {
				meshOK, meshReason = true, e.Reason
				break
			}
		}
	}
	if !meshOK {
		for _, e := range g.Edges {
			if e.Kind == EdgeOnMesh && e.To == vendorNode {
				meshOK, meshReason = true, e.Reason
				break
			}
		}
	}
	res.Steps = append(res.Steps, Step{Layer: "mesh", Verdict: allowDeny(meshOK), Reason: meshReason})

	// identity (informational for write; reflects cut-off state)
	idVerdict, idReason := "n/a", "machine writes are cert-gated, not Ory-gated; identity governs human read"
	if n.Kind == KindPerson {
		if n.State == StateCutOff {
			idVerdict, idReason = "deny", "Ory identity is disabled / revoked (cut off)"
		} else {
			idVerdict, idReason = "allow", "Ory identity state="+string(n.State)
		}
	}
	res.Steps = append(res.Steps, Step{Layer: "identity", Verdict: idVerdict, Reason: idReason})

	// zenoh write rule
	writeOK := false
	writeReason := "no rendered write anchor covers prefix `" + n.Vendor + "/**` — a publish here is silently denied at the router"
	if topicExists {
		for _, e := range g.Edges {
			if e.Kind == EdgeMayWrite && e.From == vendorNode && e.To == tid {
				writeOK, writeReason = true, e.Reason
				break
			}
		}
	} else {
		writeReason = "topic `" + key + "` is not registered"
	}
	res.Steps = append(res.Steps, Step{Layer: "zenoh", Verdict: allowDeny(writeOK), Reason: writeReason})

	// read
	readOK, readReason := false, "no read-tier edge grants `"+key+"`"
	if topicExists && g.CommunityRead {
		readOK, readReason = true, "flat community-read: every on-mesh identity may read all topics (ADR 1025)"
	} else if topicExists {
		for _, e := range g.Edges {
			if e.Kind == EdgeMayRead && e.From == n.ID && e.To == tid {
				readOK, readReason = true, e.Reason
				break
			}
		}
	} else {
		readReason = "topic `" + key + "` is not registered"
	}

	// release (informational)
	for _, rn := range g.Nodes {
		if rn.Kind == KindReleaseTopic && prefixCovers(rn.Label, key) {
			res.Steps = append(res.Steps, Step{Layer: "release", Verdict: "allow", Reason: "topic is under release region " + rn.Label})
			break
		}
	}

	if meshOK && writeOK {
		res.WriteVerdict = "allow"
	}
	if meshOK && readOK {
		res.ReadVerdict = "allow"
	}
	if n.State == StateCutOff {
		res.WriteVerdict, res.ReadVerdict = "deny", "deny"
		res.Notes = append(res.Notes, "identity is CUT OFF — negative enforcement overrides any granted edge")
	}
	res.Notes = append(res.Notes, "read step: "+readReason)
	return res
}

func allowDeny(ok bool) string {
	if ok {
		return "allow"
	}
	return "deny"
}

func (g *Graph) label(id string) string {
	if n, ok := g.Nodes[id]; ok {
		return n.Label
	}
	return id
}

func lastSegment(nodeID string) string {
	for i := len(nodeID) - 1; i >= 0; i-- {
		if nodeID[i] == ':' {
			return nodeID[i+1:]
		}
	}
	return nodeID
}

func sortAccess(a []Access) {
	sort.SliceStable(a, func(i, j int) bool {
		if a[i].Topic != a[j].Topic {
			return a[i].Topic < a[j].Topic
		}
		return a[i].Layer < a[j].Layer
	})
}

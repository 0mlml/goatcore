package registry

import "context"

// CatalogSchema is one schema version of one topic. It carries the same
// fields the per-topic schema listing returns, so a reader that already
// renders those needs no second shape.
type CatalogSchema struct {
	Version    string `json:"version"`
	Format     string `json:"format"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
	UploadedAt int64  `json:"uploaded_at"`
}

// CatalogTopic is one declared topic as the catalog presents it: the record a
// reader needs to decide whether to subscribe, plus every schema version that
// exists for it.
type CatalogTopic struct {
	ID           int64           `json:"id"`
	KeyPattern   string          `json:"key_pattern"`
	Format       string          `json:"format"`
	Description  string          `json:"description,omitempty"`
	SchemaRef    string          `json:"schema_ref,omitempty"`
	Schemas      []CatalogSchema `json:"schemas"`
	LastSeenAt   *int64          `json:"last_seen_at,omitempty"`
	PublishCount int64           `json:"publish_count"`
	CreatedAt    int64           `json:"created_at"`
}

// CatalogOrg is one org and everything it has declared.
type CatalogOrg struct {
	Prefix      string `json:"prefix"`
	DisplayName string `json:"display_name"`
	Synthetic   bool   `json:"synthetic"`
	Approved    bool   `json:"approved"`
	// Alias is the slot's human handle (slots.assigned_to, joined on
	// slot_id = prefix). DISPLAY ONLY (ADR 1039): it is never a key segment,
	// never an ACL subject, and a reader must never compose either from it.
	// Empty on a per-vendor goatnet (no slot pool) and on an unassigned or
	// revoked slot.
	Alias string `json:"alias,omitempty"`
	// LastPublishAt and PublishCount are the per-ORG roll-up the audit tailer
	// keeps in vendor_progress: the most recent publish under this prefix that
	// the fabric audit log recorded (batched, so up to a minute behind) and
	// how many publishes it has counted. This is the only publish-time fact
	// the registry holds — the per-topic last_seen_at column is never written
	// — so a catalog reader shows this
	// once per org and nothing per topic. Nil / 0 = no publish recorded.
	LastPublishAt *int64         `json:"last_publish_at,omitempty"`
	PublishCount  int64          `json:"publish_count"`
	Topics        []CatalogTopic `json:"topics"`
}

// CatalogTotals are the counts a reader wants before reading any of it, and
// the one count that explains an apparently empty goatnet: OrgsWithoutTopics.
type CatalogTotals struct {
	Orgs              int `json:"orgs"`
	OrgsWithTopics    int `json:"orgs_with_topics"`
	OrgsWithoutTopics int `json:"orgs_without_topics"`
	Topics            int `json:"topics"`
	SchemaVersions    int `json:"schema_versions"`
	Formats           int `json:"formats"`
}

// Catalog is the whole declared surface of a goatnet in one value.
type Catalog struct {
	Totals CatalogTotals `json:"totals"`
	Orgs   []CatalogOrg  `json:"orgs"`
}

// ListCatalog returns every org, its declared topics, and each topic's schema
// versions.
//
// WHY THIS EXISTS. Assembling this in the browser costs one request for the org
// list, then one per org for its topics, then one per topic for its schema
// versions, fired unthrottled through nested Promise.all. Discovery is the first
// thing a new user does, so the surface with the widest audience would multiply
// one click into hundreds of server round-trips — which is the amplification
// ADR 1123 exists to forbid.
//
// The cost here is five flat reads regardless of goatnet size: all orgs, all
// live topics, all schema rows, the per-org progress rows, the slot aliases. They are joined in memory, which is the right
// trade at these cardinalities (hundreds of orgs, low thousands of topics) and
// keeps the query count flat as those grow.
//
// includeEmpty controls whether orgs that have declared nothing appear. The
// default is to omit them: a browsable catalog padded with empty entries is how
// a reader concludes there is nothing on the fabric. The totals report the
// omitted count so the omission is visible rather than silent.
func (r *Registry) ListCatalog(ctx context.Context, includeEmpty bool) (*Catalog, error) {
	vendors, err := r.ListVendors(ctx)
	if err != nil {
		return nil, err
	}

	var topics []Topic
	if err := r.reader().SelectContext(ctx, &topics,
		`SELECT * FROM topics WHERE retired_at IS NULL ORDER BY vendor_prefix, key_pattern`); err != nil {
		return nil, err
	}

	// Schema versions for every topic at once. Ordered so that appending in
	// scan order yields newest-first per topic, matching ListSchemasByTopic.
	var schemas []TopicSchema
	if err := r.reader().SelectContext(ctx, &schemas,
		`SELECT * FROM topic_schemas ORDER BY topic_id, uploaded_at DESC, id DESC`); err != nil {
		return nil, err
	}
	// Per-org publish roll-up and slot alias: two more flat reads, joined in
	// memory like everything else here. Both are LEFT-join shaped — an org with
	// no vendor_progress row or no slot row simply carries the zero value.
	var progress []struct {
		Prefix        string `db:"prefix"`
		LastPublishAt *int64 `db:"last_publish_at"`
		PublishCount  int64  `db:"publish_count"`
	}
	if err := r.reader().SelectContext(ctx, &progress,
		`SELECT prefix, last_publish_at, COALESCE(publish_count, 0) AS publish_count FROM vendor_progress`); err != nil {
		return nil, err
	}
	progressByOrg := make(map[string]struct {
		at *int64
		n  int64
	}, len(progress))
	for _, p := range progress {
		progressByOrg[p.Prefix] = struct {
			at *int64
			n  int64
		}{p.LastPublishAt, p.PublishCount}
	}
	aliases, err := r.ListAliases(ctx)
	if err != nil {
		return nil, err
	}
	aliasByOrg := make(map[string]string, len(aliases))
	for _, a := range aliases {
		aliasByOrg[a.SlotID] = a.Alias
	}

	schemasByTopic := make(map[int64][]CatalogSchema, len(topics))
	for _, s := range schemas {
		schemasByTopic[s.TopicID] = append(schemasByTopic[s.TopicID], CatalogSchema{
			Version:    s.Version,
			Format:     s.Format,
			SizeBytes:  s.SizeBytes,
			SHA256:     s.SHA256,
			UploadedAt: s.UploadedAt,
		})
	}

	topicsByOrg := make(map[string][]CatalogTopic, len(vendors))
	formats := make(map[string]struct{})
	schemaVersions := 0
	for _, t := range topics {
		vs := schemasByTopic[t.ID]
		schemaVersions += len(vs)
		if vs == nil {
			vs = []CatalogSchema{}
		}
		if t.Format != "" {
			formats[t.Format] = struct{}{}
		}
		topicsByOrg[t.VendorPrefix] = append(topicsByOrg[t.VendorPrefix], CatalogTopic{
			ID:           t.ID,
			KeyPattern:   t.KeyPattern,
			Format:       t.Format,
			Description:  t.Description,
			SchemaRef:    t.SchemaRef,
			Schemas:      vs,
			LastSeenAt:   t.LastSeenAt,
			PublishCount: t.PublishCount,
			CreatedAt:    t.CreatedAt,
		})
	}

	cat := &Catalog{Orgs: make([]CatalogOrg, 0, len(vendors))}
	for _, v := range vendors {
		ts := topicsByOrg[v.Prefix]
		cat.Totals.Orgs++
		if len(ts) == 0 {
			cat.Totals.OrgsWithoutTopics++
			if !includeEmpty {
				continue
			}
			ts = []CatalogTopic{}
		} else {
			cat.Totals.OrgsWithTopics++
		}
		pr := progressByOrg[v.Prefix]
		cat.Orgs = append(cat.Orgs, CatalogOrg{
			Prefix:        v.Prefix,
			DisplayName:   v.DisplayName,
			Synthetic:     v.Synthetic,
			Approved:      v.ApprovedAt != nil,
			Alias:         aliasByOrg[v.Prefix],
			LastPublishAt: pr.at,
			PublishCount:  pr.n,
			Topics:        ts,
		})
	}
	cat.Totals.Topics = len(topics)
	cat.Totals.SchemaVersions = schemaVersions
	cat.Totals.Formats = len(formats)
	return cat, nil
}

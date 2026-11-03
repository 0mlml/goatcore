// Package pubsubstate is the on-disk repo serialization that backs the
// pub/sub state-of-record substrate (Block 86). The portal exports its
// SQLite + schemastore state into this layout; the pubsub-apply
// reconciler reads this layout and applies it to a portal via REST.
//
// Authoritative design: docs/design/pubsub-state-as-code.md.
//
// Layout summary (full description in the design doc):
//
//	pubsub-state/
//	├── MANIFEST.yaml
//	├── README.md
//	└── vendors/<prefix>/
//	    ├── vendor.yaml
//	    └── topics/<key-segments>/
//	        ├── topic.yaml
//	        ├── schemas/v1.fds + v1.meta.yaml + v2.fds + v2.meta.yaml
//	        └── examples/<lang>.<ext>
//
// All YAML is rendered with gopkg.in/yaml.v3 default formatting. Files are
// written 0640 (group-readable), directories 0750. The package never
// reaches outside the supplied root.
package pubsubstate

// Manifest is the per-repo metadata file at MANIFEST.yaml. ContentSHA256
// is the canonical hash of the whole tree (excluding MANIFEST.yaml +
// README.md themselves), used by the reconciler to short-circuit a
// "already current" reconcile.
type Manifest struct {
	GeneratedAt   int64  `yaml:"generated_at"`
	PortalVersion string `yaml:"portal_version"`
	SchemaVersion int    `yaml:"schema_version"` // bump on layout-breaking changes
	ContentSHA256 string `yaml:"content_sha256"`
	VendorCount   int    `yaml:"vendor_count"`
	TopicCount    int    `yaml:"topic_count"`
	SchemaCount   int    `yaml:"schema_count"`
	ExampleCount  int    `yaml:"example_count"`
}

// CurrentSchemaVersion is the on-disk layout version this package emits +
// expects. Increment on any rename / restructure that would break a
// reconcile against an older repo.
const CurrentSchemaVersion = 1

// VendorDoc is the YAML body of vendors/<prefix>/vendor.yaml. Mirrors
// the subset of registry.Vendor that belongs in the state-of-record —
// secrets (api_key_hash, bundle_*) are deliberately excluded per the
// design doc's "Mission-data boundary" section.
type VendorDoc struct {
	Prefix      string `yaml:"prefix"`
	DisplayName string `yaml:"display_name"`
	Synthetic   bool   `yaml:"synthetic"`
	// ApprovedAt + Approver capture operator approval intent so the
	// rebuilt portal can re-render the ACL with the same membership.
	// Both nil = vendor was created but not yet approved.
	ApprovedAt *int64  `yaml:"approved_at,omitempty"`
	Approver   *string `yaml:"approver,omitempty"`
}

// TopicDoc is the YAML body of vendors/<v>/topics/<...>/topic.yaml.
// Mirrors registry.Topic minus runtime fields (last_seen_at,
// publish_count, created_at, retired_at). retired topics are omitted
// from the export entirely; the reconciler's "delete extras" pass
// retires them on the live portal.
type TopicDoc struct {
	KeyPattern       string `yaml:"key_pattern"`
	Format           string `yaml:"format"`
	SchemaRef        string `yaml:"schema_ref,omitempty"`
	Description      string `yaml:"description,omitempty"`
	PublishToStorage bool   `yaml:"publish_to_storage"`
}

// SchemaMeta is the YAML body of vendors/<v>/topics/<...>/schemas/<ver>.meta.yaml,
// the sibling of the binary <ver>.fds file. Mirrors the subset of
// registry.TopicSchema that is meaningful out-of-DB.
type SchemaMeta struct {
	Version       string `yaml:"version"`
	Compatibility string `yaml:"compatibility"`
	Format        string `yaml:"format"`
	SizeBytes     int64  `yaml:"size_bytes"`
	SHA256        string `yaml:"sha256"`
	UploadedAt    int64  `yaml:"uploaded_at"`
	UploadedBy    string `yaml:"uploaded_by"`
}

// ExampleMeta records the per-language metadata for the sibling code
// file. The file's extension is derived from the language (.py / .rs /
// .go / .ts / .js / .c / .cpp / .java / .sh). UpdatedBy captures who
// authored the override (vendor or operator handle).
type ExampleMeta struct {
	Language  string `yaml:"language"`
	UpdatedAt int64  `yaml:"updated_at"`
	UpdatedBy string `yaml:"updated_by"`
}

// LangExtension maps a registry language label to the on-disk file
// extension the example code is written to. Mirrors
// registry.allowedExampleLanguages.
var LangExtension = map[string]string{
	"rust":       "rs",
	"go":         "go",
	"python":     "py",
	"typescript": "ts",
	"javascript": "js",
	"c":          "c",
	"cpp":        "cpp",
	"java":       "java",
	"shell":      "sh",
}

// ExtToLang is the reverse map for the reader.
var ExtToLang = func() map[string]string {
	m := make(map[string]string, len(LangExtension))
	for lang, ext := range LangExtension {
		m[ext] = lang
	}
	return m
}()

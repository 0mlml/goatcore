package pubsubstate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// VendorState is the in-memory deserialization of one vendor's directory
// — used by both the exporter (build up + write) and the reconciler
// (read + diff). Keyed-by-string maps over slices so the reconciler can
// look up a topic by key_pattern without scanning.
type VendorState struct {
	Vendor VendorDoc
	Topics map[string]*TopicState // key = key_pattern
}

// TopicState carries the schemas + examples attached to one topic.
type TopicState struct {
	Topic    TopicDoc
	Schemas  map[string]SchemaBlob // key = version label (v1, v2, ...)
	Examples map[string]ExampleBlob
}

// SchemaBlob carries one schema version's bytes + metadata.
type SchemaBlob struct {
	Meta SchemaMeta
	Body []byte
}

// ExampleBlob carries one code example's source + metadata.
type ExampleBlob struct {
	Meta ExampleMeta
	Code string
}

// VendorsDir + TopicsDirName + SchemasDirName + ExamplesDirName centralize
// the path constants. Importers reference these instead of hardcoding.
const (
	VendorsDirName  = "vendors"
	TopicsDirName   = "topics"
	SchemasDirName  = "schemas"
	ExamplesDirName = "examples"
	VendorFile      = "vendor.yaml"
	TopicFile       = "topic.yaml"
	ManifestFile    = "MANIFEST.yaml"
	ReadmeFile      = "README.md"
)

// ReadRepo walks rootDir and returns the deserialized state. Missing
// directories are not errors (an empty repo returns an empty map).
func ReadRepo(rootDir string) (map[string]*VendorState, error) {
	vendorsRoot := filepath.Join(rootDir, VendorsDirName)
	state := map[string]*VendorState{}

	entries, err := os.ReadDir(vendorsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return nil, fmt.Errorf("read vendors dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := readVendor(filepath.Join(vendorsRoot, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read vendor %s: %w", e.Name(), err)
		}
		state[v.Vendor.Prefix] = v
	}
	return state, nil
}

func readVendor(dir string) (*VendorState, error) {
	var v VendorDoc
	if err := readYAML(filepath.Join(dir, VendorFile), &v); err != nil {
		return nil, err
	}
	vs := &VendorState{Vendor: v, Topics: map[string]*TopicState{}}

	topicsRoot := filepath.Join(dir, TopicsDirName)
	if err := walkTopics(topicsRoot, topicsRoot, vs); err != nil {
		return nil, err
	}
	return vs, nil
}

// walkTopics walks topicsRoot recursively. A directory is a topic when
// it contains a TopicFile (topic.yaml). Topic directories can be nested
// at any depth, mirroring the Zenoh key_pattern.
func walkTopics(topicsRoot, current string, vs *VendorState) error {
	entries, err := os.ReadDir(current)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// Look for a topic.yaml in this directory.
	hasTopic := false
	for _, e := range entries {
		if !e.IsDir() && e.Name() == TopicFile {
			hasTopic = true
			break
		}
	}
	if hasTopic {
		ts, err := readTopicDir(current)
		if err != nil {
			return fmt.Errorf("read topic dir %s: %w", current, err)
		}
		vs.Topics[ts.Topic.KeyPattern] = ts
		return nil // topic dirs do not nest further topics
	}
	// Otherwise recurse into subdirectories.
	for _, e := range entries {
		if e.IsDir() {
			if err := walkTopics(topicsRoot, filepath.Join(current, e.Name()), vs); err != nil {
				return err
			}
		}
	}
	return nil
}

func readTopicDir(dir string) (*TopicState, error) {
	var t TopicDoc
	if err := readYAML(filepath.Join(dir, TopicFile), &t); err != nil {
		return nil, err
	}
	ts := &TopicState{
		Topic:    t,
		Schemas:  map[string]SchemaBlob{},
		Examples: map[string]ExampleBlob{},
	}

	// Schemas: pair every <ver>.meta.yaml with its <ver>.<ext> body.
	schemasDir := filepath.Join(dir, SchemasDirName)
	if entries, err := os.ReadDir(schemasDir); err == nil {
		metaFiles := map[string]string{}
		bodyFiles := map[string]string{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasSuffix(name, ".meta.yaml") {
				version := strings.TrimSuffix(name, ".meta.yaml")
				metaFiles[version] = filepath.Join(schemasDir, name)
			} else if i := strings.LastIndex(name, "."); i > 0 {
				version := name[:i]
				bodyFiles[version] = filepath.Join(schemasDir, name)
			}
		}
		for version, metaPath := range metaFiles {
			var sm SchemaMeta
			if err := readYAML(metaPath, &sm); err != nil {
				return nil, err
			}
			bodyPath, ok := bodyFiles[version]
			if !ok {
				return nil, fmt.Errorf("schema %s.meta.yaml has no body sibling", version)
			}
			body, err := os.ReadFile(bodyPath)
			if err != nil {
				return nil, err
			}
			ts.Schemas[version] = SchemaBlob{Meta: sm, Body: body}
		}
	}

	// Examples: pair every <lang>.meta.yaml with its <lang>.<ext> source.
	examplesDir := filepath.Join(dir, ExamplesDirName)
	if entries, err := os.ReadDir(examplesDir); err == nil {
		metaFiles := map[string]string{}
		bodyFiles := map[string]string{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasSuffix(name, ".meta.yaml") {
				lang := strings.TrimSuffix(name, ".meta.yaml")
				metaFiles[lang] = filepath.Join(examplesDir, name)
			} else if i := strings.LastIndex(name, "."); i > 0 {
				ext := name[i+1:]
				if lang, ok := ExtToLang[ext]; ok {
					bodyFiles[lang] = filepath.Join(examplesDir, name)
				}
			}
		}
		for lang, metaPath := range metaFiles {
			var em ExampleMeta
			if err := readYAML(metaPath, &em); err != nil {
				return nil, err
			}
			bodyPath, ok := bodyFiles[lang]
			if !ok {
				return nil, fmt.Errorf("example %s.meta.yaml has no source sibling", lang)
			}
			body, err := os.ReadFile(bodyPath)
			if err != nil {
				return nil, err
			}
			ts.Examples[lang] = ExampleBlob{Meta: em, Code: string(body)}
		}
	}

	return ts, nil
}

// WriteRepo persists state under rootDir, replacing any prior content
// under rootDir/vendors atomically (via tmp-dir swap). The MANIFEST is
// regenerated from the actual written bytes.
func WriteRepo(rootDir string, state map[string]*VendorState, portalVersion string, generatedAt int64) (Manifest, error) {
	if err := os.MkdirAll(rootDir, 0o750); err != nil {
		return Manifest{}, fmt.Errorf("mkdir root: %w", err)
	}
	vendorsRoot := filepath.Join(rootDir, VendorsDirName)
	// Remove and rewrite from scratch; the reconciler's "current-state-only"
	// guarantee requires this.
	if err := os.RemoveAll(vendorsRoot); err != nil {
		return Manifest{}, fmt.Errorf("clean vendors dir: %w", err)
	}
	if err := os.MkdirAll(vendorsRoot, 0o750); err != nil {
		return Manifest{}, fmt.Errorf("mkdir vendors dir: %w", err)
	}

	var vendorCount, topicCount, schemaCount, exampleCount int
	prefixes := sortedKeys(state)
	for _, prefix := range prefixes {
		vs := state[prefix]
		vendorCount++
		vDir := filepath.Join(vendorsRoot, prefix)
		if err := os.MkdirAll(vDir, 0o750); err != nil {
			return Manifest{}, err
		}
		if err := writeYAML(filepath.Join(vDir, VendorFile), vs.Vendor); err != nil {
			return Manifest{}, err
		}
		topicsDir := filepath.Join(vDir, TopicsDirName)
		for _, key := range sortedKeys(vs.Topics) {
			ts := vs.Topics[key]
			topicCount++
			segments, err := topicKeyToPathSegments(prefix, key)
			if err != nil {
				return Manifest{}, err
			}
			tDir := filepath.Join(append([]string{topicsDir}, segments...)...)
			if err := os.MkdirAll(tDir, 0o750); err != nil {
				return Manifest{}, err
			}
			if err := writeYAML(filepath.Join(tDir, TopicFile), ts.Topic); err != nil {
				return Manifest{}, err
			}
			if len(ts.Schemas) > 0 {
				schemasDir := filepath.Join(tDir, SchemasDirName)
				if err := os.MkdirAll(schemasDir, 0o750); err != nil {
					return Manifest{}, err
				}
				for _, version := range sortedKeys(ts.Schemas) {
					sb := ts.Schemas[version]
					schemaCount++
					if err := writeYAML(filepath.Join(schemasDir, version+".meta.yaml"), sb.Meta); err != nil {
						return Manifest{}, err
					}
					ext := schemaExtForFormat(sb.Meta.Format)
					if err := writeFile(filepath.Join(schemasDir, version+"."+ext), sb.Body); err != nil {
						return Manifest{}, err
					}
				}
			}
			if len(ts.Examples) > 0 {
				examplesDir := filepath.Join(tDir, ExamplesDirName)
				if err := os.MkdirAll(examplesDir, 0o750); err != nil {
					return Manifest{}, err
				}
				for _, lang := range sortedKeys(ts.Examples) {
					eb := ts.Examples[lang]
					exampleCount++
					if err := writeYAML(filepath.Join(examplesDir, lang+".meta.yaml"), eb.Meta); err != nil {
						return Manifest{}, err
					}
					ext, ok := LangExtension[lang]
					if !ok {
						ext = "txt"
					}
					if err := writeFile(filepath.Join(examplesDir, lang+"."+ext), []byte(eb.Code)); err != nil {
						return Manifest{}, err
					}
				}
			}
		}
	}

	manifest := Manifest{
		GeneratedAt:   generatedAt,
		PortalVersion: portalVersion,
		SchemaVersion: CurrentSchemaVersion,
		VendorCount:   vendorCount,
		TopicCount:    topicCount,
		SchemaCount:   schemaCount,
		ExampleCount:  exampleCount,
	}
	hash, err := HashTree(rootDir)
	if err != nil {
		return Manifest{}, err
	}
	manifest.ContentSHA256 = hash
	if err := writeYAML(filepath.Join(rootDir, ManifestFile), manifest); err != nil {
		return Manifest{}, err
	}
	if err := writeFile(filepath.Join(rootDir, ReadmeFile), []byte(readmeBody)); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// HashTree walks rootDir/vendors and produces a deterministic SHA256 over
// (relative-path, body-bytes) pairs. MANIFEST.yaml + README.md are excluded
// — they are derived, not source.
func HashTree(rootDir string) (string, error) {
	h := sha256.New()
	vendorsRoot := filepath.Join(rootDir, VendorsDirName)
	var paths []string
	err := filepath.WalkDir(vendorsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, rel := range paths {
		body, err := os.ReadFile(filepath.Join(rootDir, rel))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(body))
		h.Write(body)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// topicKeyToPathSegments converts a Zenoh key_pattern into the on-disk
// path segments under vendors/<v>/topics/. The first segment must equal
// the vendor prefix (the registry enforces this); we strip it.
func topicKeyToPathSegments(vendorPrefix, keyPattern string) ([]string, error) {
	parts := strings.Split(keyPattern, "/")
	if len(parts) < 2 || parts[0] != vendorPrefix {
		return nil, fmt.Errorf("key_pattern %q: first segment must equal org prefix %q", keyPattern, vendorPrefix)
	}
	out := make([]string, 0, len(parts)-1)
	for _, p := range parts[1:] {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, "/\\") {
			return nil, fmt.Errorf("key_pattern %q: invalid segment %q", keyPattern, p)
		}
		out = append(out, p)
	}
	return out, nil
}

func schemaExtForFormat(format string) string {
	switch format {
	case "protobuf-fds":
		return "fds"
	default:
		return "bin"
	}
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func readYAML(path string, into any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, into)
}

func writeYAML(path string, body any) error {
	b, err := yaml.Marshal(body)
	if err != nil {
		return err
	}
	return writeFile(path, b)
}

func writeFile(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Read the existing MANIFEST.yaml if present. Returns (Manifest{}, nil) on
// missing file so the caller can treat that as "fresh repo, no prior
// state."
func ReadManifest(rootDir string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(rootDir, ManifestFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return m, err
	}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, nil
}

// Discard for any reader; provided for symmetry. Not used in v1.
var _ io.Reader = (*os.File)(nil)

const readmeBody = `# pub/sub state-of-record

This repo is the current-state mirror of one site's goat-zenoh-portal
registry: every vendor, topic, schema body, and per-language code
example the portal serves.

It is rewritten in full on every export — there is no "old state" to
preserve, the git history is the only audit trail. To rebuild a site's
fabric to match this repo:

    pubsub-apply --from .

To see what a reconcile would do without applying:

    pubsub-apply --from . --dry-run

Design + rationale: docs/design/pubsub-state-as-code.md (Block 86).
`

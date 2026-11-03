package pubsubstate

import (
	"context"
	"fmt"
	"io"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/schemastore"
)

// SchemaBodyReader opens a schema body by relative path. Implemented by
// *schemastore.Store; abstracted here so callers (and tests) can supply
// a fake.
type SchemaBodyReader interface {
	Open(rel string) (io.ReadCloser, error)
}

// Compile-time check that the real store satisfies the interface.
var _ SchemaBodyReader = (*schemastore.Store)(nil)

// Snapshot reads the current portal state (vendors + their non-retired
// topics + their schemas + their code examples) and returns it ready
// for WriteRepo. Pure-read operation; the registry handle must be open
// but is not mutated.
func Snapshot(ctx context.Context, reg *registry.Registry, schemas SchemaBodyReader) (map[string]*VendorState, error) {
	vendors, err := reg.ListVendors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vendors: %w", err)
	}
	state := make(map[string]*VendorState, len(vendors))
	for i := range vendors {
		v := vendors[i]
		vs := &VendorState{
			Vendor: VendorDoc{
				Prefix:      v.Prefix,
				DisplayName: v.DisplayName,
				Synthetic:   v.Synthetic,
				ApprovedAt:  v.ApprovedAt,
				Approver:    v.Approver,
			},
			Topics: map[string]*TopicState{},
		}
		topics, err := reg.ListVendorTopics(ctx, v.Prefix)
		if err != nil {
			return nil, fmt.Errorf("list topics for %s: %w", v.Prefix, err)
		}
		for j := range topics {
			t := topics[j]
			ts := &TopicState{
				Topic: TopicDoc{
					KeyPattern:       t.KeyPattern,
					Format:           t.Format,
					SchemaRef:        t.SchemaRef,
					Description:      t.Description,
					PublishToStorage: t.PublishToStorage,
				},
				Schemas:  map[string]SchemaBlob{},
				Examples: map[string]ExampleBlob{},
			}
			schemaRows, err := reg.ListSchemasByTopic(ctx, t.ID)
			if err != nil {
				return nil, fmt.Errorf("list schemas for topic %d: %w", t.ID, err)
			}
			for _, s := range schemaRows {
				if schemas == nil {
					return nil, fmt.Errorf("schema body reader is nil but topic %d has %d schema rows", t.ID, len(schemaRows))
				}
				rc, err := schemas.Open(s.FilePath)
				if err != nil {
					return nil, fmt.Errorf("open schema body %s: %w", s.FilePath, err)
				}
				body, err := io.ReadAll(rc)
				_ = rc.Close()
				if err != nil {
					return nil, fmt.Errorf("read schema body %s: %w", s.FilePath, err)
				}
				ts.Schemas[s.Version] = SchemaBlob{
					Meta: SchemaMeta{
						Version:       s.Version,
						Compatibility: s.Compatibility,
						Format:        s.Format,
						SizeBytes:     s.SizeBytes,
						SHA256:        s.SHA256,
						UploadedAt:    s.UploadedAt,
						UploadedBy:    s.UploadedBy,
					},
					Body: body,
				}
			}
			exampleRows, err := reg.ListCodeExamplesByTopic(ctx, t.ID)
			if err != nil {
				return nil, fmt.Errorf("list code examples for topic %d: %w", t.ID, err)
			}
			for _, e := range exampleRows {
				ts.Examples[e.Language] = ExampleBlob{
					Meta: ExampleMeta{
						Language:  e.Language,
						UpdatedAt: e.UpdatedAt,
						UpdatedBy: e.UpdatedBy,
					},
					Code: e.Code,
				}
			}
			vs.Topics[t.KeyPattern] = ts
		}
		state[v.Prefix] = vs
	}
	return state, nil
}

package pubsubstate

import (
	"context"
	"fmt"
	"log/slog"
)

// PlanAction is one entry in the reconciliation plan.
type PlanAction struct {
	Verb        string // create-vendor / update-vendor / delete-vendor / create-topic / update-topic / retire-topic / upload-schema / upsert-example
	VendorPrefix string
	KeyPattern   string // for topic-scoped actions
	Version      string // for schema-scoped actions
	Language     string // for example-scoped actions
	Reason       string // short human description
}

// Plan is the full diff between desired (repo) and live (portal) state.
type Plan struct {
	Actions []PlanAction
}

// IsEmpty returns true when the plan is a no-op.
func (p Plan) IsEmpty() bool { return len(p.Actions) == 0 }

// Reconciler combines a portal client with a desired-state tree.
type Reconciler struct {
	Client  *Client
	Desired map[string]*VendorState
	// VendorScope, when non-empty, narrows the reconcile to a single
	// vendor (the rest of the desired state is ignored AND no
	// other-vendor deletions fire). Useful for partial rebuilds.
	VendorScope string
	// AutoApprove decides whether newly-created vendors land approved.
	// Defaults true for trial-fabric expedience; can be flipped via
	// CLI flag.
	AutoApprove bool

	// liveExamplesByTopic is stashed by buildPlan for execute(). Not
	// part of the public surface.
	liveExamplesByTopic map[int64]map[string]ExampleRow
}

// Apply computes the plan and (unless the underlying client is in
// dry-run mode) executes every action in dependency order.
func (r *Reconciler) Apply(ctx context.Context) (Plan, error) {
	plan, liveVendors, liveTopicsByVendor, liveSchemasByTopic, err := r.buildPlan(ctx)
	if err != nil {
		return Plan{}, err
	}
	if err := r.execute(ctx, plan, liveVendors, liveTopicsByVendor, liveSchemasByTopic); err != nil {
		return plan, err
	}
	return plan, nil
}

func (r *Reconciler) buildPlan(ctx context.Context) (
	Plan,
	map[string]VendorRow,
	map[string]map[string]TopicRow,
	map[int64]map[string]SchemaRow,
	error,
) {
	plan := Plan{}

	liveVendors := map[string]VendorRow{}
	vs, err := r.Client.ListVendors(ctx)
	if err != nil {
		return plan, nil, nil, nil, fmt.Errorf("list vendors: %w", err)
	}
	for _, v := range vs {
		liveVendors[v.Prefix] = v
	}

	// Determine the vendor set we care about for this run.
	scopeOK := func(prefix string) bool {
		return r.VendorScope == "" || r.VendorScope == prefix
	}

	// Pre-fetch topics for each live vendor we're going to touch.
	liveTopicsByVendor := map[string]map[string]TopicRow{}
	for prefix := range liveVendors {
		if !scopeOK(prefix) {
			continue
		}
		topics, err := r.Client.ListTopics(ctx, prefix)
		if err != nil {
			return plan, nil, nil, nil, fmt.Errorf("list topics for %s: %w", prefix, err)
		}
		liveTopicsByVendor[prefix] = map[string]TopicRow{}
		for _, t := range topics {
			liveTopicsByVendor[prefix][t.KeyPattern] = t
		}
	}

	// Schemas + examples for every live topic in scope.
	liveSchemasByTopic := map[int64]map[string]SchemaRow{}
	liveExamplesByTopic := map[int64]map[string]ExampleRow{}
	for prefix, topics := range liveTopicsByVendor {
		for _, t := range topics {
			schemas, err := r.Client.ListSchemas(ctx, prefix, t.ID)
			if err != nil {
				return plan, nil, nil, nil, fmt.Errorf("list schemas for %s/%s: %w", prefix, t.KeyPattern, err)
			}
			byVersion := map[string]SchemaRow{}
			for _, s := range schemas {
				byVersion[s.Version] = s
			}
			liveSchemasByTopic[t.ID] = byVersion

			examples, err := r.Client.ListCodeExamples(ctx, prefix, t.ID)
			if err != nil {
				return plan, nil, nil, nil, fmt.Errorf("list examples for %s/%s: %w", prefix, t.KeyPattern, err)
			}
			byLang := map[string]ExampleRow{}
			for _, e := range examples {
				byLang[e.Language] = e
			}
			liveExamplesByTopic[t.ID] = byLang
		}
	}
	// Stash on receiver for execute() to consult.
	r.liveExamplesByTopic = liveExamplesByTopic

	// --- Plan creates + updates from desired side -------------------
	for _, prefix := range sortedKeys(r.Desired) {
		if !scopeOK(prefix) {
			continue
		}
		vs := r.Desired[prefix]
		live, ok := liveVendors[prefix]
		if !ok {
			plan.Actions = append(plan.Actions, PlanAction{
				Verb:         "create-vendor",
				VendorPrefix: prefix,
				Reason:       fmt.Sprintf("desired vendor %s missing on portal", prefix),
			})
		} else if live.DisplayName != vs.Vendor.DisplayName {
			plan.Actions = append(plan.Actions, PlanAction{
				Verb:         "update-vendor",
				VendorPrefix: prefix,
				Reason:       fmt.Sprintf("display_name %q → %q", live.DisplayName, vs.Vendor.DisplayName),
			})
		}

		// Topics: create or update.
		liveTopics := liveTopicsByVendor[prefix] // may be nil for newly-created vendors
		for _, key := range sortedKeys(vs.Topics) {
			td := vs.Topics[key].Topic
			lt, exists := liveTopics[key]
			if !exists {
				plan.Actions = append(plan.Actions, PlanAction{
					Verb:         "create-topic",
					VendorPrefix: prefix,
					KeyPattern:   key,
					Reason:       fmt.Sprintf("desired topic %s missing", key),
				})
			} else if !topicEqual(td, lt) {
				plan.Actions = append(plan.Actions, PlanAction{
					Verb:         "update-topic",
					VendorPrefix: prefix,
					KeyPattern:   key,
					Reason:       "field drift (format/schema_ref/description/publish_to_storage)",
				})
			}
		}
		// Schemas: upload any version present in repo but missing on portal.
		for _, key := range sortedKeys(vs.Topics) {
			ts := vs.Topics[key]
			lt, exists := liveTopics[key]
			if !exists {
				// Topic will be created above; upload all versions after creation.
				for _, ver := range sortedKeys(ts.Schemas) {
					plan.Actions = append(plan.Actions, PlanAction{
						Verb:         "upload-schema",
						VendorPrefix: prefix,
						KeyPattern:   key,
						Version:      ver,
						Reason:       "new topic; upload all schema versions",
					})
				}
				continue
			}
			liveVersions := liveSchemasByTopic[lt.ID]
			for _, ver := range sortedKeys(ts.Schemas) {
				if _, present := liveVersions[ver]; !present {
					plan.Actions = append(plan.Actions, PlanAction{
						Verb:         "upload-schema",
						VendorPrefix: prefix,
						KeyPattern:   key,
						Version:      ver,
						Reason:       fmt.Sprintf("schema version %s missing on portal", ver),
					})
				}
			}
		}
		// Code examples: upsert when code differs, delete when the
		// portal has examples not present in the repo.
		for _, key := range sortedKeys(vs.Topics) {
			ts := vs.Topics[key]
			lt, exists := liveTopics[key]
			var liveExamples map[string]ExampleRow
			if exists {
				liveExamples = liveExamplesByTopic[lt.ID]
			}
			// Upsert path: repo has it, portal doesn't OR codes differ.
			for _, lang := range sortedKeys(ts.Examples) {
				eb := ts.Examples[lang]
				live, present := liveExamples[lang]
				if !present || live.Code != eb.Code {
					plan.Actions = append(plan.Actions, PlanAction{
						Verb:         "upsert-example",
						VendorPrefix: prefix,
						KeyPattern:   key,
						Language:     lang,
						Reason:       "code differs from portal (or missing)",
					})
				}
			}
			// Delete path: portal has it, repo doesn't. Only fires when
			// the topic already exists on portal (otherwise the topic
			// itself is being created and has no live examples).
			if exists {
				for lang := range liveExamples {
					if _, present := ts.Examples[lang]; !present {
						plan.Actions = append(plan.Actions, PlanAction{
							Verb:         "delete-example",
							VendorPrefix: prefix,
							KeyPattern:   key,
							Language:     lang,
							Reason:       "live example not in repo",
						})
					}
				}
			}
		}
	}

	// --- Plan deletions from live side ------------------------------
	// Topics: retire ones not in repo (or whose vendor is not in repo).
	for prefix, topics := range liveTopicsByVendor {
		desired, hasDesired := r.Desired[prefix]
		for _, t := range topics {
			if hasDesired {
				if _, present := desired.Topics[t.KeyPattern]; present {
					continue
				}
			}
			plan.Actions = append(plan.Actions, PlanAction{
				Verb:         "retire-topic",
				VendorPrefix: prefix,
				KeyPattern:   t.KeyPattern,
				Reason:       "live topic not in repo",
			})
		}
	}
	// Vendors: delete ones not in repo. (Only fires when their topics
	// all retired in the step above.)
	for prefix := range liveVendors {
		if !scopeOK(prefix) {
			continue
		}
		if _, present := r.Desired[prefix]; present {
			continue
		}
		plan.Actions = append(plan.Actions, PlanAction{
			Verb:         "delete-vendor",
			VendorPrefix: prefix,
			Reason:       "live vendor not in repo",
		})
	}

	return plan, liveVendors, liveTopicsByVendor, liveSchemasByTopic, nil
}

func (r *Reconciler) execute(ctx context.Context, plan Plan,
	liveVendors map[string]VendorRow,
	liveTopicsByVendor map[string]map[string]TopicRow,
	liveSchemasByTopic map[int64]map[string]SchemaRow,
) error {
	// Stable execution order ensures dependent actions land after
	// their prerequisites. The plan slice is built in dependency-safe
	// order by buildPlan, so we just iterate.
	for _, a := range plan.Actions {
		switch a.Verb {
		case "create-vendor":
			vs := r.Desired[a.VendorPrefix]
			if err := r.Client.CreateVendor(ctx, a.VendorPrefix, vs.Vendor.DisplayName, vs.Vendor.Synthetic, r.AutoApprove); err != nil {
				return fmt.Errorf("create vendor %s: %w", a.VendorPrefix, err)
			}
			// Refresh live state so subsequent topic creations + lookups
			// see the new vendor.
			topics, _ := r.Client.ListTopics(ctx, a.VendorPrefix)
			liveTopicsByVendor[a.VendorPrefix] = map[string]TopicRow{}
			for _, t := range topics {
				liveTopicsByVendor[a.VendorPrefix][t.KeyPattern] = t
			}
			slog.Info("created vendor", "prefix", a.VendorPrefix)

		case "update-vendor":
			vs := r.Desired[a.VendorPrefix]
			if err := r.Client.UpdateVendorDisplayName(ctx, a.VendorPrefix, vs.Vendor.DisplayName); err != nil {
				return fmt.Errorf("update vendor %s: %w", a.VendorPrefix, err)
			}
			slog.Info("updated vendor", "prefix", a.VendorPrefix)

		case "create-topic":
			td := r.Desired[a.VendorPrefix].Topics[a.KeyPattern].Topic
			id, err := r.Client.CreateTopic(ctx, a.VendorPrefix, td)
			if err != nil {
				return fmt.Errorf("create topic %s/%s: %w", a.VendorPrefix, a.KeyPattern, err)
			}
			// 409-OK: re-list and pick the existing ID.
			if id == 0 {
				topics, err := r.Client.ListTopics(ctx, a.VendorPrefix)
				if err != nil {
					return fmt.Errorf("re-list topics for %s: %w", a.VendorPrefix, err)
				}
				for _, t := range topics {
					if t.KeyPattern == a.KeyPattern {
						id = t.ID
						break
					}
				}
			}
			if id == 0 {
				return fmt.Errorf("create topic %s/%s: portal accepted but topic ID unresolved", a.VendorPrefix, a.KeyPattern)
			}
			liveTopicsByVendor[a.VendorPrefix][a.KeyPattern] = TopicRow{
				ID: id, VendorPrefix: a.VendorPrefix, KeyPattern: a.KeyPattern,
				Format: td.Format, SchemaRef: td.SchemaRef, Description: td.Description,
				PublishToStorage: td.PublishToStorage,
			}
			slog.Info("created topic", "vendor", a.VendorPrefix, "key", a.KeyPattern, "id", id)

		case "update-topic":
			td := r.Desired[a.VendorPrefix].Topics[a.KeyPattern].Topic
			lt := liveTopicsByVendor[a.VendorPrefix][a.KeyPattern]
			if err := r.Client.UpdateTopic(ctx, a.VendorPrefix, lt.ID, td); err != nil {
				return fmt.Errorf("update topic %s/%s: %w", a.VendorPrefix, a.KeyPattern, err)
			}
			slog.Info("updated topic", "vendor", a.VendorPrefix, "key", a.KeyPattern)

		case "upload-schema":
			ts := r.Desired[a.VendorPrefix].Topics[a.KeyPattern]
			lt := liveTopicsByVendor[a.VendorPrefix][a.KeyPattern]
			if lt.ID == 0 {
				return fmt.Errorf("upload schema %s/%s/%s: topic ID unresolved", a.VendorPrefix, a.KeyPattern, a.Version)
			}
			sb := ts.Schemas[a.Version]
			if err := r.Client.UploadSchema(ctx, a.VendorPrefix, lt.ID, sb.Meta, sb.Body); err != nil {
				return fmt.Errorf("upload schema %s/%s/%s: %w", a.VendorPrefix, a.KeyPattern, a.Version, err)
			}
			slog.Info("uploaded schema", "vendor", a.VendorPrefix, "key", a.KeyPattern, "version", a.Version)

		case "upsert-example":
			ts := r.Desired[a.VendorPrefix].Topics[a.KeyPattern]
			lt := liveTopicsByVendor[a.VendorPrefix][a.KeyPattern]
			if lt.ID == 0 {
				return fmt.Errorf("upsert example %s/%s/%s: topic ID unresolved", a.VendorPrefix, a.KeyPattern, a.Language)
			}
			eb := ts.Examples[a.Language]
			if err := r.Client.UpsertCodeExample(ctx, a.VendorPrefix, lt.ID, a.Language, eb.Code); err != nil {
				return fmt.Errorf("upsert example %s/%s/%s: %w", a.VendorPrefix, a.KeyPattern, a.Language, err)
			}
			slog.Info("upserted example", "vendor", a.VendorPrefix, "key", a.KeyPattern, "lang", a.Language)

		case "delete-example":
			lt := liveTopicsByVendor[a.VendorPrefix][a.KeyPattern]
			if err := r.Client.DeleteCodeExample(ctx, a.VendorPrefix, lt.ID, a.Language); err != nil {
				return fmt.Errorf("delete example %s/%s/%s: %w", a.VendorPrefix, a.KeyPattern, a.Language, err)
			}
			slog.Info("deleted example", "vendor", a.VendorPrefix, "key", a.KeyPattern, "lang", a.Language)

		case "retire-topic":
			lt := liveTopicsByVendor[a.VendorPrefix][a.KeyPattern]
			if err := r.Client.RetireTopic(ctx, a.VendorPrefix, lt.ID); err != nil {
				return fmt.Errorf("retire topic %s/%s: %w", a.VendorPrefix, a.KeyPattern, err)
			}
			slog.Info("retired topic", "vendor", a.VendorPrefix, "key", a.KeyPattern)

		case "delete-vendor":
			if err := r.Client.DeleteVendor(ctx, a.VendorPrefix); err != nil {
				return fmt.Errorf("delete vendor %s: %w", a.VendorPrefix, err)
			}
			slog.Info("deleted vendor", "prefix", a.VendorPrefix)

		default:
			return fmt.Errorf("unknown plan verb %q", a.Verb)
		}
	}
	return nil
}

// topicEqual returns true when every field the reconciler is willing to
// drive matches between desired (TopicDoc) and live (TopicRow).
func topicEqual(d TopicDoc, l TopicRow) bool {
	return d.Format == l.Format &&
		d.SchemaRef == l.SchemaRef &&
		d.Description == l.Description &&
		d.PublishToStorage == l.PublishToStorage
}

package main

// machine-mesh-alignment decision D6a — gear bundles are issued FROM the
// portal registry.
//
// The free-text --acl-groups list is exactly the hand-picked shape that
// puts unrelated gear in one group (an effector ground station, a sensor gateway
// and a laptop minted into one group in a single sitting). For GEAR the
// groups must be DERIVED from the machine's born-as node-class, and the only
// place that class + its custody edge exist is the portal registry. So:
//
//   - `--from-machine <anchor>` reads GET /api/machines/{anchor} on
//     --portal-url (operator key from $GOAT_PORTAL_OPERATOR_KEY, never a
//     flag), refuses when the born-as class has no derivation
//     (mesh_derivable:false — the portal names the re-stamp fix), sets the
//     bundle's acl_groups to the returned mesh_groups, and stamps
//     enrollment_anchor + node_class into the bundle so a validator or the
//     provenance-guard reconcile can tie the joined peer to its custody edge
//     without a portal round-trip.
//   - a hand-typed --acl-groups list is validated against
//     ops/ansible/netbird_acl_taxonomy.yml and REFUSED if any group resolves
//     to an ADR 1116 gear class. Infra/site groups (<SITE>, <SITE>-INFRA …)
//     stay allowed — that is the pre-existing legitimate use.
//
// Bundles are created at a connected signing station, so portal reachability
// at ISSUANCE is a fair assumption; the offline part of offline enrollment is
// the JOIN.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// portalMachine is the subset of the portal's GET /api/machines/{anchor}
// response bundle-create consumes.
type portalMachine struct {
	SlotID        string   `json:"slot_id"`
	SystemHandle  string   `json:"system_handle"`
	NodeClass     string   `json:"node_class"`
	MeshGroups    []string `json:"mesh_groups"`
	MeshDerivable bool     `json:"mesh_derivable"`
	MeshNote      string   `json:"mesh_note"`
}

// fetchPortalMachine reads one registry machine from the portal. The operator
// key rides as the Bearer exactly like every other portal client.
func fetchPortalMachine(ctx context.Context, client *http.Client, portalURL, anchor, operatorKey string) (*portalMachine, error) {
	u := strings.TrimRight(portalURL, "/") + "/api/machines/" + url.PathEscape(anchor)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+operatorKey)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("portal GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("portal GET %s: read response: %w", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("portal GET %s returned %d: %s", u, resp.StatusCode, truncateForErr(string(body)))
	}
	var m portalMachine
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("portal GET %s: decode: %w", u, err)
	}
	if m.SlotID == "" {
		return nil, fmt.Errorf("portal GET %s: response carries no slot_id", u)
	}
	return &m, nil
}

// resolveFromMachine runs the --from-machine leg: fetch, refuse the
// underivable, and return (aclGroupsRaw, anchor, nodeClass).
func resolveFromMachine(args runArgs) (string, string, string, error) {
	client := args.portalHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := fetchPortalMachine(ctx, client, args.PortalURL, args.FromMachine, args.PortalOperatorKey)
	if err != nil {
		return "", "", "", fmt.Errorf("--from-machine: %w", err)
	}
	if !m.MeshDerivable {
		return "", "", "", fmt.Errorf("--from-machine %s: born-as node-class %q has no mesh derivation — %s",
			args.FromMachine, m.NodeClass, m.MeshNote)
	}
	if len(m.MeshGroups) == 0 {
		return "", "", "", fmt.Errorf("--from-machine %s: portal returned mesh_derivable=true with no mesh_groups; refusing an empty grant", args.FromMachine)
	}
	return strings.Join(m.MeshGroups, ","), m.SlotID, m.NodeClass, nil
}

func truncateForErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// ---------------------------------------------------------------------------
// Hand-typed --acl-groups guard: refuse gear-class groups against the taxonomy.
// ---------------------------------------------------------------------------

// defaultTaxonomyRelPath is the repo-relative home of the group taxonomy —
// the flag default; resolveTaxonomyPath walks up from the working directory
// so the guard works from any directory inside the repo.
const defaultTaxonomyRelPath = "ops/ansible/netbird_acl_taxonomy.yml"

const gearGroupRefusal = "gear-class groups are derived from the machine's node-class — enroll the machine in the portal and use --from-machine"

// taxonomyDoc mirrors just the parts of netbird_acl_taxonomy.yml this guard
// needs: every declared site/exercise prefix, and the gear-class vocabulary.
// The gear classes are declared once as the opt-in add-on template
// (netbird_exercise_gear_template — the set the `gear: true` exercise rows
// pull in); the per-site gear blocks are kept identical to it by the file's
// own keep-in-sync rule, so the template is the single source read here.
type taxonomyDoc struct {
	Sites map[string]struct {
		SitePrefix string `yaml:"site_prefix"`
	} `yaml:"netbird_acl_taxonomy"`
	GearTemplate struct {
		Groups []struct {
			Suffix string `yaml:"suffix"`
		} `yaml:"groups"`
	} `yaml:"netbird_exercise_gear_template"`
	Exercises map[string]struct {
		SitePrefix string `yaml:"site_prefix"`
	} `yaml:"netbird_exercises"`
}

// resolveTaxonomyPath returns the taxonomy file to read. An explicit
// --taxonomy path must exist as given; the repo-relative default is resolved
// by walking up from the working directory (so `go test` package dirs and
// operator shells anywhere inside the repo both find it). Fail-closed: no
// taxonomy, no hand-typed --acl-groups.
func resolveTaxonomyPath(p string) (string, error) {
	if p != "" && p != defaultTaxonomyRelPath {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("--taxonomy %s: %w", p, err)
		}
		return p, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		cand := filepath.Join(dir, defaultTaxonomyRelPath)
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("taxonomy %s not found walking up from the working directory — pass --taxonomy explicitly (the gear-class check is fail-closed, not skippable)", defaultTaxonomyRelPath)
}

// loadTaxonomy parses the taxonomy into (known scope prefixes, gear-class
// suffix set). Prefixes are sorted longest-first so a longer prefix wins over a shorter one
// when stripping.
func loadTaxonomy(path string) ([]string, map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read taxonomy %s: %w", path, err)
	}
	var doc taxonomyDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse taxonomy %s: %w", path, err)
	}
	gear := make(map[string]bool, len(doc.GearTemplate.Groups))
	for _, g := range doc.GearTemplate.Groups {
		if g.Suffix != "" {
			gear[g.Suffix] = true
		}
	}
	if len(gear) == 0 {
		return nil, nil, fmt.Errorf("taxonomy %s carries no netbird_exercise_gear_template groups — refusing to validate --acl-groups against an empty gear vocabulary", path)
	}
	var prefixes []string
	for _, s := range doc.Sites {
		if s.SitePrefix != "" {
			prefixes = append(prefixes, s.SitePrefix)
		}
	}
	for _, e := range doc.Exercises {
		if e.SitePrefix != "" {
			prefixes = append(prefixes, e.SitePrefix)
		}
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	return prefixes, gear, nil
}

// refuseHandTypedGearGroups is the guard on the legacy --acl-groups path:
// strip any known site/exercise prefix from each listed group and refuse if
// the remaining suffix (or the bare name) is an ADR 1116 gear class.
// Infra/site groups (<SITE>, <SITE>-INFRA …) pass — the existing legitimate use.
func refuseHandTypedGearGroups(taxonomyPath string, groups []string) error {
	path, err := resolveTaxonomyPath(taxonomyPath)
	if err != nil {
		return err
	}
	prefixes, gear, err := loadTaxonomy(path)
	if err != nil {
		return err
	}
	for _, g := range groups {
		suffix := g
		for _, p := range prefixes {
			if strings.HasPrefix(g, p+"-") {
				suffix = g[len(p)+1:]
				break
			}
		}
		if gear[suffix] {
			return fmt.Errorf("--acl-groups includes %q (gear class %s): %s", g, suffix, gearGroupRefusal)
		}
	}
	return nil
}

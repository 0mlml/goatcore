package access

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// releaseSetsFile is the on-disk shape of ops/permissions/release-sets.yaml
// (ADR 1027, data-access program B2). Extra keys are ignored; only what the
// COP graph + the B2.1 ACL carve consume is read.
type releaseSetsFile struct {
	Version     int `yaml:"version"`
	ReleaseSets []struct {
		Set     string   `yaml:"set"`
		Prefix  string   `yaml:"prefix"`
		Members []string `yaml:"members"`
	} `yaml:"release_sets"`
}

// LoadReleaseSets reads the committed release-set registry at path and returns
// the []ReleaseIn the Data-Access COP graph (Assemble) consumes and the B2.1
// ACL renderer reads. Fail-closed: a missing/unreadable/malformed file is an
// error to the caller — an empty release picture must be a DECLARED empty file,
// never a read failure masquerading as "no partner may receive anything".
//
// Normalization: each set's name/prefix is trimmed; a blank `prefix` defaults
// to `release/<set>/`; blank/duplicate members are dropped so the reveal and
// the carve never emit an empty or doubled lane. A set with a blank name is
// skipped (it cannot name a lane).
func LoadReleaseSets(path string) ([]ReleaseIn, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read release-sets %s: %w", path, err)
	}
	var rf releaseSetsFile
	if err := yaml.Unmarshal(raw, &rf); err != nil {
		return nil, fmt.Errorf("parse release-sets %s: %w", path, err)
	}
	var out []ReleaseIn
	for _, rs := range rf.ReleaseSets {
		set := strings.TrimSpace(rs.Set)
		if set == "" {
			continue
		}
		prefix := strings.TrimSpace(rs.Prefix)
		if prefix == "" {
			prefix = "release/" + set + "/"
		}
		seen := map[string]bool{}
		var members []string
		for _, m := range rs.Members {
			m = strings.TrimSpace(m)
			if m == "" || seen[m] {
				continue
			}
			seen[m] = true
			members = append(members, m)
		}
		out = append(out, ReleaseIn{Set: set, Partners: members, Prefix: prefix})
	}
	return out, nil
}

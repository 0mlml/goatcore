package handlers

// Group-vocabulary fence for the raw identity forms (create user / edit
// groups) on the tenant realm. The Kratos identity schema deliberately accepts
// ANY uppercase token, so an admin could type a NetBird mesh-role name
// (DEVELOPER, OBSERVER, ...) into the Ory groups field and the identity would
// save — but those names gate nothing on any web surface (the Ory vocabulary
// lives in ops/permissions/group-permissions.yml). The console refuses groups
// outside the known vocabulary with a message that names the valid set — and
// calls out the mesh-role trap explicitly.
//
// Scope: tenant realm only. Other realms (e.g. the obs-utility operator IdP)
// carry their own schema enums and are not validated against this matrix.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// meshRoleNames are NetBird auto_group suffixes (SoT
// ops/ansible/netbird_acl_taxonomy.yml) an admin plausibly types here by
// mistake. They are mesh-plane roles granted by onboarding — never Ory groups.
var meshRoleNames = map[string]bool{
	"ADMIN": true, "OPERATOR": true, "DEVELOPER": true, "OBSERVER": true,
	"MONITOR": true, "USER": true, "PEER": true, "BOOTSTRAP": true, "VENDOR": true,
}

// dataGrammarRe mirrors the closed data-access grammar the identity schema
// admits (SoT ops/permissions/data-access-roles.yaml).
var dataGrammarRe = regexp.MustCompile(`^data\.(view\.(all|notional|org:[a-z0-9][a-z0-9-]*|producer:[a-z0-9][a-z0-9-]*)|write\.producer:[a-z0-9][a-z0-9-]*|govern\.org:[a-z0-9][a-z0-9-]*)$`)

// validateGroupVocabulary refuses a tenant-realm group list that carries
// values outside the known vocabulary: matrix groups (group-permissions.yml,
// exact case) or data-access grammar values. Mesh-role names get the specific
// trap message; anything else gets the valid set spelled out. nil error =
// every value is known.
func (s *Server) validateGroupVocabulary(groups []string) error {
	valid := map[string]bool{}
	var names []string
	if s.Permissions != nil {
		for _, g := range s.Permissions.Groups {
			valid[g.Name] = true
			names = append(names, g.Name)
		}
	}
	var mesh, unknown []string
	for _, g := range groups {
		gg := strings.TrimSpace(g)
		switch {
		case gg == "", valid[gg], dataGrammarRe.MatchString(gg):
		case meshRoleNames[strings.ToUpper(gg)]:
			mesh = append(mesh, gg)
		default:
			unknown = append(unknown, gg)
		}
	}
	if len(mesh) == 0 && len(unknown) == 0 {
		return nil
	}
	var msg strings.Builder
	if len(mesh) > 0 {
		fmt.Fprintf(&msg, "%s is a NetBird mesh role, not a login group — mesh roles are granted by onboarding (Onboarding → Invite), never here. ",
			strings.Join(mesh, ", "))
	}
	if len(unknown) > 0 {
		fmt.Fprintf(&msg, "Unknown group(s): %s. ", strings.Join(unknown, ", "))
	}
	if len(names) > 0 {
		fmt.Fprintf(&msg, "Valid login groups: %s — or a data-access value (data.view.all, data.view.notional, data.view.org:<org>, data.view.producer:<p>, data.write.producer:<p>, data.govern.org:<org>).",
			strings.Join(names, ", "))
	} else {
		msg.WriteString("Valid values are the groups in group-permissions.yml or data-access grammar values (data.view.*, data.write.producer:<p>, data.govern.org:<org>).")
	}
	return errors.New(msg.String())
}

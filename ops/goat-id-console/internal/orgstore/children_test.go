package orgstore

import "testing"

// The downward direction must agree with Ancestors on the SAME tree. If the two
// disagree, one tree reads differently depending on which way you walk it, and
// that is worse than having only one direction.
func TestChildrenAndDescendantsMirrorAncestors(t *testing.T) {
	s := open(t)
	for _, id := range []string{"root", "mid-a", "mid-b", "leaf-a1", "leaf-a2", "unrelated"} {
		mk(t, s, id)
	}
	for _, e := range [][2]string{
		{"mid-a", "root"}, {"mid-b", "root"},
		{"leaf-a1", "mid-a"}, {"leaf-a2", "mid-a"},
	} {
		if err := s.SetParent(e[0], e[1], "op", ""); err != nil {
			t.Fatalf("SetParent %s->%s: %v", e[0], e[1], err)
		}
	}

	// direct children only, ordered
	kids, err := s.Children("root")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(kids); len(got) != 2 || got[0] != "mid-a" || got[1] != "mid-b" {
		t.Errorf("Children(root) = %v, want [mid-a mid-b]", got)
	}

	// full subtree, excluding the root itself
	desc, err := s.Descendants("root")
	if err != nil {
		t.Fatal(err)
	}
	got := ids(desc)
	if len(got) != 4 {
		t.Fatalf("Descendants(root) = %v, want 4 entries", got)
	}
	for _, want := range []string{"mid-a", "mid-b", "leaf-a1", "leaf-a2"} {
		if !contains(got, want) {
			t.Errorf("Descendants(root) missing %q: %v", want, got)
		}
	}
	if contains(got, "root") {
		t.Error("Descendants must exclude the org itself")
	}
	if contains(got, "unrelated") {
		t.Error("Descendants leaked an org outside the subtree")
	}

	// the mirror property: every descendant has root among its ancestors
	for _, d := range desc {
		anc, err := s.Ancestors(d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !contains(ids(anc), "root") {
			t.Errorf("%s is a descendant of root but root is not among its ancestors (%v)", d.ID, ids(anc))
		}
	}

	// a leaf has no children, and that is not an error
	kids, err = s.Children("leaf-a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 0 {
		t.Errorf("Children(leaf-a1) = %v, want empty", ids(kids))
	}
}

// Re-parenting closes the old edge and opens a new one. The downward view must
// follow, or a moved subtree would still appear under its former parent.
func TestChildrenFollowsReparenting(t *testing.T) {
	s := open(t)
	for _, id := range []string{"old-parent", "new-parent", "mover"} {
		mk(t, s, id)
	}
	if err := s.SetParent("mover", "old-parent", "op", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetParent("mover", "new-parent", "op", "re-org"); err != nil {
		t.Fatal(err)
	}

	oldKids, err := s.Children("old-parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(oldKids) != 0 {
		t.Errorf("old parent still claims %v — Children must read the OPEN edge only", ids(oldKids))
	}
	newKids, err := s.Children("new-parent")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(newKids); len(got) != 1 || got[0] != "mover" {
		t.Errorf("Children(new-parent) = %v, want [mover]", got)
	}
}

func ids(v []OrgView) []string {
	out := make([]string, 0, len(v))
	for _, o := range v {
		out = append(out, o.ID)
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

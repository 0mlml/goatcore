package handlers

// sponsorKeySet and sponsorAllows are the one predicate every sponsor-scoped
// read runs through: the proposal tabs, the invites, the counts and the find
// index. The HTTP tests exercise them end to end; this pins the normalisation
// itself so a change to it fails at the function, not three pages later.

import "testing"

func TestSponsorKeySet_NormalisesAndDropsBlanks(t *testing.T) {
	keys := sponsorKeySet(" 0ff404dd ", "Pat-Example", "SPONSORA@example.org", "", "   ")
	if len(keys) != 3 {
		t.Fatalf("key set = %v, want three normalised keys and no blank", keys)
	}
	for _, k := range []string{"0ff404dd", "pat-example", "sponsora@example.org"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("key set lacks %q: %v", k, keys)
		}
	}
}

func TestSponsorAllows_ExactAfterNormalisation(t *testing.T) {
	keys := sponsorKeySet("0ff404dd", "pat-example", "sponsora@example.org")
	cases := []struct {
		sponsor string
		want    bool
	}{
		{"sponsora@example.org", true},
		{"SPONSORA@EXAMPLE.ORG", true},
		{"  sponsora@example.org\t", true},
		{"0ff404dd", true},
		{"Pat-Example", true},
		{"", false},
		{"   ", false},
		{"sponsora", false}, // a prefix of a key is not the key
		{"xsponsora@example.org", false},
		{"sponsora@example.org.example", false},
		{"0ff404d", false},
		{"someone-else@example.org", false},
	}
	for _, c := range cases {
		if got := sponsorAllows(keys, c.sponsor); got != c.want {
			t.Errorf("sponsorAllows(%q) = %v, want %v", c.sponsor, got, c.want)
		}
	}
}

// A sponsor profile whose fields are all blank must not match a row whose
// sponsor column is also blank: the blank is dropped from the set, never
// stored as a key.
func TestSponsorAllows_EmptySetAdmitsNobody(t *testing.T) {
	for _, keys := range []map[string]struct{}{sponsorKeySet(), sponsorKeySet("", " ")} {
		for _, sponsor := range []string{"", " ", "anyone@example.org"} {
			if sponsorAllows(keys, sponsor) {
				t.Errorf("empty key set allowed %q", sponsor)
			}
		}
	}
}

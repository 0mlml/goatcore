package config

import (
	"strings"
	"unicode"
)

// Lexicon is the display-only vocabulary of the portal web UI. It exists so
// one image can serve every goatnet with goatnet-appropriate wording: nova-a
// renders "Zenoh Trial — Vendor Portal" (the trial-fabric framing where
// participants really are synthetic vendors), while ZAGROS renders "ZAGROS Fabric
// Portal" with "org" as the top-level entity (its participants are
// organizations — commands, units, and vendors alike).
//
// INVARIANT — this is render-time substitution only. The display layer never
// changes a path, key, or token: API routes stay /api/vendors and the registry
// stays vendor-keyed regardless of wording. Changing a word here never changes
// a machine surface. Mirror of the slot-alias-display-layer invariant (display
// projects over the machine anchor; the anchor never moves).
//
// NOTE — the cert *identity* layer is NOT part of this display-only invariant.
// The minted cert URI-SAN / CN / DNS-SAN are per-goatnet machine values set
// by the role (config.CertSign* fields), NOT lexicon tokens: nova-a mints
// urn:goat-nova:vendor=<v>, ZAGROS mints urn:goat:zagros:org=<anchor>. Those are
// real, frozen-on-cert grammar — generalized so a new goatnet specifies its
// own rather than inheriting the nova string. See docs/design/portal-lexicon.md.
//
// Defaults reproduce the nova-a wording so an unconfigured portal is byte-stable
// with what shipped before the lexicon existed. ZAGROS overrides the handful of
// fields below via PORTAL_LEX_* env (wired by the ansible role from
// group_vars). Tokens are substituted into the embedded HTML at serve time and
// served as JSON at GET /api/branding for the frontend JS.
type Lexicon struct {
	// ProductName is the bare product name used as a title prefix
	// ("<ProductName> — Topics"). nova-a: "Zenoh Trial". ZAGROS: "ZAGROS Fabric".
	ProductName string
	// ProductTitle is the full masthead <h1>. Kept distinct from ProductName
	// because the two don't compose uniformly: nova-a wants
	// "Zenoh Trial — Vendor Portal" but ZAGROS wants the shorter
	// "ZAGROS Fabric Portal", not "ZAGROS Fabric — Org Portal".
	ProductTitle string
	// Org / Orgs / OrgTitle / OrgsTitle are the top-level-entity noun in its
	// four surface forms (singular/plural × lower/title case). nova-a:
	// vendor/vendors/Vendor/Vendors. ZAGROS: org/orgs/Org/Orgs. Only Org need be
	// set; the rest derive (regular pluralization + first-letter case) unless
	// explicitly overridden for an irregular noun.
	Org       string
	Orgs      string
	OrgTitle  string
	OrgsTitle string
	// OrgUpper is the all-caps singular, for shell-variable examples in the
	// peering docs (`export VENDOR=…` / `export ORG=…`). Derived from Org.
	OrgUpper string
	// PanoscopeURL is the masthead "Panoscope ↗" nav link target. Per-goatnet
	// (nova panoscope ≠ ZAGROS panoscope); historically hardcoded to the nova host
	// in the static HTML, which is why an ZAGROS portal linked operators back to
	// the nova surface. Now a lexicon field so each env points at its own.
	PanoscopeURL string
}

// normalize fills unset fields with derived or nova-a-default values. Called
// once after flag/env parsing. Idempotent.
func (l *Lexicon) normalize() {
	if l.ProductName == "" {
		l.ProductName = "Zenoh Trial"
	}
	if l.Org == "" {
		l.Org = "vendor"
	}
	if l.Orgs == "" {
		l.Orgs = l.Org + "s"
	}
	if l.OrgTitle == "" {
		l.OrgTitle = titleFirst(l.Org)
	}
	if l.OrgsTitle == "" {
		l.OrgsTitle = titleFirst(l.Orgs)
	}
	if l.OrgUpper == "" {
		l.OrgUpper = strings.ToUpper(l.Org)
	}
	if l.ProductTitle == "" {
		// nova-a default: "Zenoh Trial — Vendor Portal". ZAGROS overrides the whole
		// string, so this composition only ever produces the nova wording.
		l.ProductTitle = l.ProductName + " — " + l.OrgTitle + " Portal"
	}
	if l.PanoscopeURL == "" {
		l.PanoscopeURL = "https://panoscope.nova.netbird.nova.example/"
	}
}

// Tokens maps the HTML substitution tokens to their resolved values. The
// render layer applies these in a single non-overlapping pass, so a value that
// happens to contain a token string is never re-substituted.
func (l Lexicon) Tokens() map[string]string {
	return map[string]string{
		"__LEX_PRODUCT_NAME__":  l.ProductName,
		"__LEX_PRODUCT_TITLE__": l.ProductTitle,
		"__LEX_ORG__":           l.Org,
		"__LEX_ORGS__":          l.Orgs,
		"__LEX_ORG_TITLE__":     l.OrgTitle,
		"__LEX_ORGS_TITLE__":    l.OrgsTitle,
		"__LEX_ORG_UPPER__":     l.OrgUpper,
		"__LEX_PANOSCOPE_URL__": l.PanoscopeURL,
	}
}

// titleFirst upper-cases the first rune and leaves the rest untouched. Unlike
// strings.Title it won't mangle interior letters of acronyms or hyphenated
// nouns ("org" → "Org", "land-unit" → "Land-unit").
func titleFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

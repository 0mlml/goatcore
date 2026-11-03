package handlers

import (
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardlint"
)

// lintRegression is the review editor's backstop against a correction that
// wedges the proposal. The approve gate re-lints the manifest and refuses one
// that fails, so an edit that broke the document at review used to surface
// later, as an approval that would not go through (zagros: a
// cleared display name). Both doors call this before the write, with the
// pre-edit and post-edit documents. It compares verdicts, not documents: a
// manifest that already failed before the edit is not held hostage to it,
// because the edit may be the very correction that fixes it.
func lintRegression(before, after []byte) *fieldEditError {
	if !onboardLint(before, "json").OK {
		return nil
	}
	res := onboardLint(after, "json")
	if res.OK {
		return nil
	}
	return badRequest("this correction was not saved: the proposal would no longer pass the onboard lint gate (%s)", lintFindingsSummary(res.Findings))
}

// lintFindingsSummary renders lint findings as one line, "field: message"
// joined by semicolons, so a refusal names what to fix.
func lintFindingsSummary(fs []onboardlint.Finding) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		if f.Field == "" {
			parts = append(parts, f.Message)
			continue
		}
		parts = append(parts, f.Field+": "+f.Message)
	}
	return strings.Join(parts, "; ")
}

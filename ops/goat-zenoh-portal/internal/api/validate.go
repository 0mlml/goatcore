package api

import (
	"fmt"
	"unicode/utf8"
)

// Field caps on the documented write operations. The same numbers are the
// maxLength values in web/static/openapi.yaml; change both together.
const (
	maxDisplayName   = 200
	maxKeyPattern    = 512
	maxFormat        = 32
	maxSchemaRef     = 512
	maxDescription   = 2000
	maxSchemaVersion = 64
	maxCompatibility = 32
	maxImportTopics  = 1000
)

// fieldCap is one bound on a request string, checked by capped().
type fieldCap struct {
	name  string
	value string
	max   int
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// capped returns the first field whose length passes its cap, as a
// validation error that names the field and the cap, never the value.
func capped(fields ...fieldCap) error {
	for _, f := range fields {
		if utf8.RuneCountInString(f.value) > f.max {
			return fmt.Errorf("%s must be at most %d characters", f.name, f.max)
		}
	}
	return nil
}

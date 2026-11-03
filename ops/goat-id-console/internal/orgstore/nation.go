package orgstore

import "github.com/usareur-af/goatcore/ops/goat-id-console/internal/nation"

// normalizeNation folds an alpha-2 country code onto its alpha-3 form so either
// spelling names the same country. The map moved to internal/nation when the
// coalition package became its third consumer — see that package for why.
func normalizeNation(n string) string { return nation.Normalize(n) }

// Package compat holds the names and built-in defaults that existing
// snitch deployments depend on and that therefore cannot change with the
// rest of the source.
//
// names.go: names stored on disk or used from outside the binary. The
// PostgreSQL tables, columns and indexes below hold the satellite-link PoP
// history on every collector, so they keep their names. The CLI subcommand,
// serve flag, HTTP routes and JSON key below are accepted (or emitted)
// beside the current names so existing scripts, bookmarks and API clients
// keep working.
package compat

// Satellite-link PoP history tables (PostgreSQL).
const (
	SatlinkProbesTable       = "satlink_probes"
	SatlinkMeasurementsTable = "satlink_measurements"
	SatlinkRangeColumn       = "in_satlink_range"
	SatlinkProbesTSIndex     = "idx_satlink_probes_ts"
	SatlinkMeasProbeIndex    = "idx_satlink_meas_probe"
)

// Interface names accepted beside the current ones.
const (
	// SatlinkProbeCommand is the earlier name of the `satlink-probe` subcommand.
	SatlinkProbeCommand = "satlink-probe"
	// SatlinkIntervalFlag is the earlier name of the serve flag `--satlink-interval`.
	SatlinkIntervalFlag = "satlink-interval"
	// SatlinkPageRoute is the earlier path of the `/satlink` page.
	SatlinkPageRoute = "/satlink"
	// SatlinkAPIPrefix is the earlier prefix of the `/api/satlink/` routes.
	SatlinkAPIPrefix = "/api/satlink/"
	// SatlinkRangeJSONKey is the earlier JSON key of `in_satlink_range`;
	// API responses carry both keys.
	SatlinkRangeJSONKey = "in_satlink_range"
)

// Package web exposes the embedded static frontend assets to main.
//
// The embed directive must live next to the directory being embedded;
// putting it in cmd/portal/ would require web/static to be inside
// cmd/portal/, which conflates layout. Tiny package, one job.
package web

import "embed"

//go:embed all:static
var Static embed.FS

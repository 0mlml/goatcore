// Package web embeds the HTML templates and static assets so the
// service ships as a single static binary.
package web

import "embed"

//go:embed templates/*.html static/*
var FS embed.FS

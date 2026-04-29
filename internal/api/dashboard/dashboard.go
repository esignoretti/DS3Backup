package dashboard

import "embed"

// Content holds the embedded dashboard static files.
//
//go:embed index.html
var Content embed.FS

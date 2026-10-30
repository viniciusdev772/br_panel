// Package web embeds the HTML templates and static assets served by the panel.
package web

import "embed"

// FS contains templates/ and static/.
//
//go:embed templates static
var FS embed.FS

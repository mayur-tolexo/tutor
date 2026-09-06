// Package web embeds the built PWA (web/dist) so one binary serves the app.
// Build the frontend before the Go binary; an empty dist serves the API only.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built frontend rooted at index.html, or nil when the
// frontend has not been built.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

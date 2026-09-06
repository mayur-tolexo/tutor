// Package contentfs embeds the exercise library so the server binary is
// self-contained; the loader reads it as an fs.FS.
package contentfs

import "embed"

// FS holds every track directory under content/.
//
//go:embed all:cbse-11
var FS embed.FS

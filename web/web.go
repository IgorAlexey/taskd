// Package web holds the browser interface, embedded into the binary.
package web

import "embed"

//go:embed index.html oat.min.css oat.min.js
var FS embed.FS

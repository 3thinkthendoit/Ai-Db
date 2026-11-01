// Package frontend exposes the built web UI (frontend/dist) as an embedded
// filesystem so the server binary can serve it same-origin. Same-origin hosting
// is what lets the HttpOnly session cookie (SameSite=Strict) work in a plain
// browser with no CORS or reverse-proxy setup.
//
// go:embed cannot reference a parent directory, so this package lives beside
// dist/ rather than under cmd/server. The desktop build embeds frontend/dist
// separately from the repo-root main.go and never imports this package, so the
// two binaries stay independent.
//
// The embedded tree requires frontend/dist to exist at build time — the same
// prerequisite the desktop build already has (dist is gitignored). Run
// `npm run build` in frontend/ (or `wails build`) before `go build ./cmd/server`.
package frontend

import (
	"embed"
	"io/fs"
)

// dist is the built frontend. The all: prefix includes files that begin with "_"
// or "." so nothing the bundler emits is skipped.
//
//go:embed all:dist
var dist embed.FS

// Dist returns the embedded frontend/dist tree rooted at its contents, i.e. the
// returned fs has index.html and assets/ at the top level (the leading "dist/"
// is stripped). Callers serve it with an SPA fallback (see cmd/server).
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}

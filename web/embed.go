// Package web embeds the application's HTML templates and static CSS/JavaScript
// so the released binary only needs config.yaml next to it.
package web

import (
	"embed"
	"io/fs"
)

// Templates holds web/templates/*.html.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds web/static/css/*.css and web/static/js/*.js.
//
//go:embed static
var Static embed.FS

// StaticFS returns the embedded static assets rooted at "static", ready to be
// served under the /static/ URL prefix.
func StaticFS() (fs.FS, error) {
	return fs.Sub(Static, "static")
}

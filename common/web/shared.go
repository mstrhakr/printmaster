// Package web provides shared web assets for PrintMaster UIs
package web

import _ "embed"

// SharedCSS contains the common stylesheet used by both agent and server UIs
// This is embedded at compile time from shared.css and served as /static/shared.css
//
//go:embed shared.css
var SharedCSS string

// SharedJS contains common JavaScript utilities used by both agent and server UIs
// This is embedded at compile time from shared.js and served as /static/shared.js
//
//go:embed shared.js
var SharedJS string

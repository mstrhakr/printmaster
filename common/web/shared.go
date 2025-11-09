// Package web provides shared web assets for PrintMaster UIs
package web

import "embed"

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

// Flatpickr vendor assets (optional, can replace with upstream minified files)
// Place real flatpickr files at common/web/vendor/flatpickr/flatpickr.min.js and flatpickr.min.css
// and they'll be embedded here and served by the server as /static/vendor/flatpickr/...
//go:embed vendor/flatpickr/*
var VendorFiles embed.FS

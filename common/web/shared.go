// Package web provides shared web assets for PrintMaster UIs
// Package web provides shared web assets for PrintMaster UIs.
package web

import "embed"

// SharedCSS contains the common stylesheet used by both agent and server UIs.
// This is embedded at compile time from shared.css and served as /static/shared.css
//
//go:embed shared.css
var SharedCSS string

// SharedJS contains common JavaScript utilities used by both agent and server UIs.
// This is embedded at compile time from shared.js and served as /static/shared.js
//
//go:embed shared.js
var SharedJS string

// MetricsJS contains the shared metrics UI implementation extracted from the agent UI.
// It is embedded at compile time from metrics.js and served as /static/metrics.js
//
//go:embed metrics.js
var MetricsJS string

// CardsJS contains shared card rendering and related helpers (renderSavedCard,
// checkDatabaseRotationWarning). Embedded from cards.js and served as
// /static/cards.js
//
//go:embed cards.js
var CardsJS string

// Flatpickr vendor assets (optional). Place real flatpickr files at
// common/web/vendor/flatpickr/flatpickr.min.js and flatpickr.min.css and they'll
// be embedded here and served by the server as /static/vendor/flatpickr/...
//
//go:embed vendor/flatpickr/*
var VendorFiles embed.FS

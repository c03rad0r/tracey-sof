package web

import _ "embed"

// These assets are embedded into the Go binary and every generated HTML report.
//
//go:embed index.html
var Index string

//go:embed graph.js
var GraphJS string

//go:embed app.js
var AppJS string

//go:embed style.css
var CSS string

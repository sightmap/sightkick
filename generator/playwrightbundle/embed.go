// Package playwrightbundle embeds the generated Playwright executor.
package playwrightbundle

import _ "embed"

//go:generate go run ./internal/gen

// JS is built from packages/playwright. Do not hand-edit the embedded copy.
//
//go:embed executor.mjs
var JS string

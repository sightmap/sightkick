// Package skills embeds the sightkick skill files so the binary
// (`sightkick skills install`) can extract them without a repo checkout.
//
// The canonical skill corpora live at the repository root (<repo>/skills/) so
// they present as a first-class, standalone skills/plugin directory. Because
// go:embed cannot reach outside the Go module (rooted at <repo>/generator) or
// follow symlinks, the directories below are committed copies regenerated from
// the canonical source. Do not edit them by hand — edit <repo>/skills/<name>/
// and run `go generate ./skills/...` from the generator/ directory.
package skills

import "embed"

//go:generate go run ./internal/gen

// FS contains the embedded skill directories (one per top-level directory).
// When a skill is added under <repo>/skills/, regenerate the copies and add its
// name here.
//
//go:embed sightkick-debug
var FS embed.FS

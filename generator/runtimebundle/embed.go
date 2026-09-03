// Package runtimebundle embeds the compiled sightkick runtime bundle so the
// binary (`sightkick runtime`) can emit it without a repo checkout. The bundle
// is the ~19 KB payload an agent injects into a live page: evaluating it sets
// window.__sightkick, then window.__sightkick.load(ir) registers the IR's tools
// on document.modelContext (see the sightkick-debug skill).
//
// The canonical bundle is produced by esbuild (packages/runtime/build.mjs ->
// packages/runtime/dist/sightkick-runtime.js). Because go:embed cannot reach
// outside the Go module (rooted at <repo>/generator) or follow symlinks, the
// file below is a committed copy regenerated from that build output. Do not edit
// it by hand — rebuild the runtime (`pnpm --filter @sightkick/runtime build`)
// and run `go generate ./runtimebundle/...` from the generator/ directory.
package runtimebundle

import _ "embed"

//go:generate go run ./internal/gen

// JS is the compiled runtime bundle (an IIFE that installs window.__sightkick).
//
//go:embed sightkick-runtime.js
var JS []byte

---
"@sightmap/sightkick": minor
---

Add `sightkick browser <corpus-dir>` — a one-command wrapper for the debug/drive
loop. It builds the IR, starts a sightmap browser session (auto-opening the
corpus's home-view URL unless `--url`; `--webmcp` adds the native-modelContext
blink flags; `--profile`/`--cdp-port`/`--extensions`/`--chrome-flag` pass through
to sightmap), and persist-injects the runtime + IR so the tools register on the
page and survive navigations. Runs sightmap from the corpus dir so the session
lands in its `.sightmap/` (discoverable + avoids the sessionless-fallback
footgun); `--no-start` injects into an already-running session. The
`sightkick-debug` skill documents it as the quick-start path. Requires the
`sightmap` CLI on PATH.

---
"@sightmap/sightkick": minor
---

`sightkick browser --webmcp` now auto-loads the WebMCP inspector extension (an
in-browser client that reads the tools sightkick registers on
`document.modelContext`), alongside sightmap's overlay — no more manual
`--extensions`. The inspector is vendored from its upstream source
(`beaufortfrancois/model-context-tool-inspector`, Apache-2.0) via
`npm run vendor-inspector` and embedded in the CLI. Pass `--no-inspector` for the
native surface without it; any explicit `--extensions` are merged in.

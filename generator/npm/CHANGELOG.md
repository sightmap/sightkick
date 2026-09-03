# @sightmap/sightkick

## 0.5.0

### Minor Changes

- 1eecdfd: `sightkick browser --webmcp` now auto-loads the WebMCP inspector extension (an
  in-browser client that reads the tools sightkick registers on
  `document.modelContext`), alongside sightmap's overlay — no more manual
  `--extensions`. The inspector is vendored from its upstream source
  (`beaufortfrancois/model-context-tool-inspector`, Apache-2.0) via
  `npm run vendor-inspector` and embedded in the CLI. Pass `--no-inspector` for the
  native surface without it; any explicit `--extensions` are merged in.

## 0.4.0

### Minor Changes

- 9ebad83: Add `sightkick browser <corpus-dir>` — a one-command wrapper for the debug/drive
  loop. It builds the IR, starts a sightmap browser session (auto-opening the
  corpus's home-view URL unless `--url`; `--webmcp` adds the native-modelContext
  blink flags; `--profile`/`--cdp-port`/`--extensions`/`--chrome-flag` pass through
  to sightmap), and persist-injects the runtime + IR so the tools register on the
  page and survive navigations. Runs sightmap from the corpus dir so the session
  lands in its `.sightmap/` (discoverable + avoids the sessionless-fallback
  footgun); `--no-start` injects into an already-running session. The
  `sightkick-debug` skill documents it as the quick-start path. Requires the
  `sightmap` CLI on PATH.
- 6c9a46d: `sightkick call --via webmcp` now drives the tool through the sightmap CLI's own
  WebMCP driver (`sightmap browser mcp call`) instead of a baked-in `executeTool`
  implementation. That CLI command speaks the standard `document.modelContext`
  surface and owns the native-vs-polyfill argument and result differences, so
  sightkick no longer duplicates that handling (and no longer reaches for the
  runtime's private `window.__sightkick.call` global) — `--via webmcp` now genuinely
  exercises the standard-surface contract a real WebMCP client depends on.
  sightkick still unwraps the returned `CallToolResult` envelope into the runtime's
  typed `ToolResult`.

  Requires the sightmap CLI to provide `browser mcp call` with native-argument
  serialization (sightmap >= 0.31.x including that fix).

- bca9553: The tool layer moves from a single `webmcp.tools.yaml` file to a `.sightkick/`
  directory (a sibling of the corpus's `.sightmap/`). Every `*.yaml` file inside is
  merged into one manifest — tools and journeys concatenated, no dependencies
  between files — so a large tool layer can be split however helps (e.g. one file
  per view). `corpus:` now defaults to the sibling `../.sightmap` and `name:` to the
  app dir's name, so both are usually omitted. `sightkick build/browser/call` accept
  an app dir (or a `.sightkick` dir) as before. No migration path: rename your
  `webmcp.tools.yaml` to `.sightkick/tools.yaml` and drop the `corpus:` line.

## 0.3.0

### Minor Changes

- 39c6c0e: Add the `sightkick-authoring` skill, which documents the `webmcp.tools.yaml`
  grammar (tools, steps, `returns` value/list, guards, params, component queries,
  and journeys) with a worked example — so the manifest is authorable from the
  installed skills alone, without reading the repo's examples. Also: `sightkick
build --help` (and `-h`/top-level help) now print usage instead of erroring on a
  missing file, and the `sightkick-debug` skill documents `window.__sightkick.call`
  as the reliable scripted drive path (with the native-`document.modelContext`
  caveat) and cross-links the new authoring skill.

## 0.2.0

### Minor Changes

- ad1e7ce: Add `sightkick runtime [-o file.js]`, which emits the runtime bundle embedded in
  the binary — the payload injected into a live page (`window.__sightkick.load(ir)`)
  to register the compiled tools. This makes the debug/inject loop work from the
  installed CLI alone, with no repo checkout: `sightkick runtime -o rt.js` +
  `sightkick build <corpus> -o ir.json`, then inject both via `sightmap browser`.
  The `sightkick-debug` skill is updated to use it.

## 0.1.0

### Minor Changes

- b3d928e: First public release. Ships the sightkick generator CLI (compile a
  `webmcp.tools.yaml` + `.sightmap` corpus into WebMCP tool IR) and the
  `sightkick-debug` agent skill, installable via `sightkick skills install`
  (which also pulls in the supporting sightmap skills). The runtime is injected
  into a live page for debugging via `sightmap browser` — no bespoke browser
  extension.

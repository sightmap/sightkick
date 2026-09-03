# @sightmap/sightkick

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

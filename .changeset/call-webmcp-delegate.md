---
"@sightmap/sightkick": minor
---

`sightkick call --via webmcp` now drives the tool through the sightmap CLI's own
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

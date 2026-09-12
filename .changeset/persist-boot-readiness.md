---
"@sightmap/sightkick": patch
---

Fix persisted (document_start) runtime injection so tools survive a full page reload on an SPA.

The persisted runtime script re-fires at `document_start` on every navigation, before the SPA has bootstrapped and before the WebMCP native surface (`document.modelContext`) exists. Booting there either polyfilled `document.modelContext` and registered tools onto the polyfill — so a reloaded page had a native surface but no tools — or, once it waited for the native surface, registered mid-bootstrap and crashed the renderer (`RESULT_CODE_KILLED_BAD_MESSAGE`) on Angular/Zone pages like JetBlue.

A new readiness gate (`whenBootable`) defers auto-boot: a live inject or a direct install into an already-loaded document boots synchronously (the known-safe timing, unchanged), while a `document_start` re-injection waits for the document to finish loading and, on a native page, for the native surface to appear, then a short settle before registering. The launcher now sets `window.__sightkick_ir` before the bundle instead of appending `window.__sightkick.load(ir)`, since the deferred boot means the global isn't defined yet when a trailing line would run.

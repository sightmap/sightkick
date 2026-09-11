---
"@sightmap/sightkick": minor
---

Emit a `sightkick:tool` DOM event for every tool call.

Both call paths — an agent's through `document.modelContext` (native or polyfilled) and the console/host `window.__sightkick.call` — now dispatch a `CustomEvent` on `document`: `phase: "start"` as the call begins and `phase: "end"` when it finishes, sharing a `callId`. The end event carries `ok`, `skipped`, `durationMs` and a 200-char-truncated `error`, alongside `tool`, `via`, `argKeys`, `path`, `polyfilled` and the IR name.

Argument values and result values are never included — only argument key names — so a page can measure how agents use its tools without its telemetry picking up what a user typed. sightkick ships no analytics adapters: it dispatches the event and the page forwards it wherever it already sends things (see the README's Events section).

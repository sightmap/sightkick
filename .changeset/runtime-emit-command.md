---
"@sightmap/sightkick": minor
---

Add `sightkick runtime [-o file.js]`, which emits the runtime bundle embedded in
the binary — the payload injected into a live page (`window.__sightkick.load(ir)`)
to register the compiled tools. This makes the debug/inject loop work from the
installed CLI alone, with no repo checkout: `sightkick runtime -o rt.js` +
`sightkick build <corpus> -o ir.json`, then inject both via `sightmap browser`.
The `sightkick-debug` skill is updated to use it.

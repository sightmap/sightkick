---
"@sightmap/sightkick": patch
---

`sightkick browser` now clears any script it previously persisted before
injecting the runtime + IR, so a rebuilt runtime **replaces** the old one
instead of stacking on top of it. Previously each run (especially with
`--no-start`) added another persisted injection; on the next reload the daemon
re-fired them all in document order, so an older runtime generation could
re-register *after* the freshly built one and silently clobber it — a
convincing false negative while iterating on the runtime. Clearing is
best-effort: with no session (or if inject is unsupported) it is a no-op.

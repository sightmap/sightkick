---
"@sightmap/sightkick": minor
---

First public release. Ships the sightkick generator CLI (compile a
`webmcp.tools.yaml` + `.sightmap` corpus into WebMCP tool IR) and the
`sightkick-debug` agent skill, installable via `sightkick skills install`
(which also pulls in the supporting sightmap skills). The runtime is injected
into a live page for debugging via `sightmap browser` — no bespoke browser
extension.

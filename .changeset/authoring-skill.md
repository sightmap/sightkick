---
"@sightmap/sightkick": minor
---

Add the `sightkick-authoring` skill, which documents the `webmcp.tools.yaml`
grammar (tools, steps, `returns` value/list, guards, params, component queries,
and journeys) with a worked example — so the manifest is authorable from the
installed skills alone, without reading the repo's examples. Also: `sightkick
build --help` (and `-h`/top-level help) now print usage instead of erroring on a
missing file, and the `sightkick-debug` skill documents `window.__sightkick.call`
as the reliable scripted drive path (with the native-`document.modelContext`
caveat) and cross-links the new authoring skill.

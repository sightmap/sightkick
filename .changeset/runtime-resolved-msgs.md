---
"@sightmap/sightkick": patch
---

runtime: interrupt/timeout/no-element messages now interpolate params, so an agent sees the RESOLVED selector it actually searched for (e.g. `code="JFK"`, `tier="ZZZ"`) instead of the raw authored placeholder (`code="{{origin}}"`). A raw placeholder in a timeout reason misled agents into permuting the argument value when the argument was never the problem. `exec_actions` and `get_fragments` also now return a structured error envelope for any unexpected throw (e.g. a step racing a navigation teardown) instead of a null result + surfaced TypeError. And view-scoped tools are registered against the live `document.modelContext` (the same context the always-on meta tools use) rather than the boot-captured reference, so a WebMCP client resolving a tool from `getTools()` and passing it back to `executeTool` sees one consistent registration context.

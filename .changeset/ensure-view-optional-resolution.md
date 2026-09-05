---
"@sightmap/sightkick": patch
---

Fix `compile.query-ref` failing with `Available: (none)` for a tool that omits
`ensure_view` (sightkick#9). An ensure_view-less tool is not scoped to a route,
so it now resolves component names against the **whole corpus** — every view's
components plus globals — instead of globals only. Previously `view == nil`
dropped every view-scoped component from the candidate set, so a query over a
component that `sightmap validate` accepted still failed to compile unless you
added a redundant `ensure_view`. `ensure_view` is now genuinely optional: set it
to scope resolution to one view (disambiguating same-named components) and to
route-scope the tool at runtime; omit it to resolve corpus-wide. Behavior when a
view IS named is unchanged. The `sightkick-authoring` skill doc is updated to
match.

---
"@sightmap/sightkick": minor
---

Expose the resumable executor as always-on WebMCP tools, and carry corpus vocabulary into fragments.

**exec_actions + get_fragments (fallback surface).** Two always-on WebMCP tools now register alongside the view-scoped tools. `get_fragments` lists the action fragments available on the current view — each a pluckable `{id, tool, op, label, uses}`. `exec_actions` runs an agent-composed list of those fragment ids resumably: it consumes fragment **references** (not raw compiled steps), so raw selectors/URLs never cross the tool boundary; on an interruption (an unexpected modal, a missing field) it stops instead of hanging and returns the reason plus the **remaining fragment refs**, which the agent re-submits to resume. Registration dedups against the shared native registry (`getTools()`) so the tools appear exactly once even when the bundle boots in several execution contexts (Angular/Zone SPAs).

**Semantic fragment targets.** The generator now carries each step's corpus-vocabulary target into the IR (`Step.target`): the source compquery for a query step (`FormField[label*="First" i] FieldInput`), the destination view name for navigate/goto/waitFor-view (a `goto` deep link resolves to its view), the key for keypress. Fragment labels and the execActions tail/interrupt projection render this instead of reconstructing raw locators, so an agent composes and reads in component/view names. It is label/provenance only — the runtime still selects via the compiled query, so the IR firewall holds.

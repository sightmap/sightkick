---
"@sightmap/sightkick": minor
---

`sightkick outline`/`sightkick explain` — a cheap, offline discovery surface for resolving a
Gherkin scenario into a plan, so an agent no longer reads the full compiled IR (mostly runtime
DOM-addressing detail) or the raw YAML corpus just to see what tools and journeys exist. `outline`
prints journeys (name, description, ordered tool names — the first artifact that surfaces a
journey's `description` at all) plus every tool's one-line summary, grouped by view; `explain`
fills in full plan-time detail (description, params, `ensure_view`, returns shape) for a subset
named by tool name and/or repeatable `--journey`/`--view` flags, which union rather than
intersect. Both support `--json` with a stable, tiered field shape. Neither adds an IR field or
changes `build`'s output — stored plans hash `sightkick build`'s stdout, so that stays untouched.
Aligned to sightmap's own CLI conventions: tiering by separate command (not a `--detail` flag),
`--json` as a boolean opt-in, unresolvable names erroring with a candidate list. See
`docs/scenario-testing.md` §6.1.

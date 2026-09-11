---
"@sightmap/sightkick": patch
---

get_fragments: return the current view's base set plus a 1-hop guidance horizon, tiered by distance.

Previously get_fragments returned only the tools offered on the current view. It now also includes the immediate next step — the tools one navigation away, reached via the current view's tools' guidance graph — so an agent can pre-compose across a navigation instead of hitting a hard cut at the view boundary. Each fragment is tagged with `view` (its owning tool's view) and `distance` (0 = current view, 1 = one hop away). Reachability rides the per-tool `guidance` (Suggestion[]) only and stops at one hop deliberately; deeper is journey territory.

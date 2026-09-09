---
"@sightmap/sightkick": patch
---

Fix the runtime click hanging forever when `requestAnimationFrame` is starved.

`clickElement`'s hit-test settle loop (added to commit below-the-fold dropdown options) awaited the next animation frame, and its 300ms wall-clock deadline was only checked *between* frames. So when rAF stops firing — a backgrounded/throttled tab, or a wedged SPA renderer that has stopped painting — the `await` never resolved and the whole tool promise hung indefinitely (every other step is bounded, so this was the sole unbounded path).

`nextFrame()` now races rAF against a short timer, so the settle loop keeps turning and honors its deadline (falling back to node dispatch) even when no frame ever fires. When rAF is healthy it still wins the race, so the settle fast-path is unchanged.

Surfaced driving jetblue checkout: `set_passenger` hung in the Title option click while `requestAnimationFrame` fired 0× in 2s.

---
"@sightmap/sightkick": minor
---

Support the `raw_text` extract mode (SEP-0013).

The generator now compiles `extract: raw_text` to a first-class extractor kind — previously it fell through to a bogus raw-CSS fallback (`within: "raw_text"`), so `--via webmcp` silently failed. The runtime reads `raw_text` as the node's **own literal text** (the concatenation of its direct text-node children, whitespace-normalized) — never the accessibility name and never `innerText`. This mirrors the sightmap lib's offline `node.RawText`, so a `raw_text` predicate matches identically offline and at runtime.

Verified live on jetblue.com: `select_fare` now resolves `SubFare[tier="Main"]` — a fare-tile heading whose accessibility name welds in a CSS `::after` "Most popular" badge — and commits the leg (was a silent timeout).

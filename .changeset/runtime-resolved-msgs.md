---
"@sightmap/sightkick": patch
---

runtime: interrupt/timeout/no-element messages now interpolate params, so an agent sees the RESOLVED selector it actually searched for (e.g. `code="JFK"`, `tier="ZZZ"`) instead of the raw authored placeholder (`code="{{origin}}"`). A raw placeholder in a timeout reason misled agents into permuting the argument value when the argument was never the problem. `exec_actions` also now returns a structured error envelope for any unexpected throw (e.g. a step racing a navigation teardown) instead of a null result + surfaced TypeError.

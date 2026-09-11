---
"@sightmap/sightkick": minor
---

Desugar signal completion predicates into IR waits.

Add a `wait_for {signal: name}` form and a step-level `then:` post-condition. Both reference a corpus signal (the SEP-0007 state-signal subset — a named boolean over a component's presence or a view's route) and desugar into an ordinary `waitFor` step: a component ref becomes a present-wait on the component's selectors, a view ref a route-wait. Resolution runs against the whole corpus (`Corpus.ResolveSignal`), so a post-condition may name a subject in the destination view, outside the tool's `ensure_view` scope (the common click-continue → wait-for-next-view case). A step with `then:` completes only once the named signal holds, not merely when the action dispatched. The runtime is unchanged — these desugar to query/route waits it already runs.

---
"@sightmap/sightkick": minor
---

Add the built-in meta tools, switched on with a `meta:` block in `.sightkick/`.

```yaml
meta:
  request_tool: true
  agent_feedback: true
```

compiles to an IR `meta: { requestTool, agentFeedback }` field, and the runtime registers the enabled tools itself — on every view, since "the tool I need isn't here" is not a per-view fact. `request_tool(name, description, example_call?)` lets an agent ask for a tool the layer doesn't have yet; `agent_feedback(tool?, rating, note?)` lets it say how a call went. Both carry a typed input schema, run no DOM steps, and record what they're told as a `sightkick:meta` DOM event (every string trimmed and length-capped) — sightkick stores nothing and calls no endpoint.

With `agent_feedback` enabled, a failed tool result also gains a guidance breadcrumb pointing at it, so the agent is asked while the failure is fresh. `request_tool` needs no such nudge: it is listed on every view already.

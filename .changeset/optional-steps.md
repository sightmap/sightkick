---
"@sightmap/sightkick": minor
---

Optional/skippable tool steps, so a grouped tool can carry optional fields
(e.g. `set_passenger(title?, middleName?, …)`). A step is **auto-skipped** when
any `{{param}}` it interpolates is absent from the call args (an omitted optional
param — required params are guaranteed present by the tool's input schema). An
omitted param is distinct from an explicit empty string, which is a real value
and does not skip. An explicit step-level `when: "{{param}}"` guard skips the
step when the interpolated value is empty, for a step that gates on a param it
doesn't otherwise reference. Spans the generator (`when` on `StepBody`/`Step`,
compiled + validated into the IR) and the runtime (skip logic in the executor).
The `sightkick-authoring` skill gains guidance on grouping tools by meaning with
typed, optionally-required params, two-click custom dropdowns, and ordering
dependent fields.

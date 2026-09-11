# @sightmap/sightkick

## 0.8.0

### Minor Changes

- b80c5e5: Desugar signal completion predicates into IR waits.

  Add a `wait_for {signal: name}` form and a step-level `then:` post-condition. Both reference a corpus signal (the SEP-0007 state-signal subset — a named boolean over a component's presence or a view's route) and desugar into an ordinary `waitFor` step: a component ref becomes a present-wait on the component's selectors, a view ref a route-wait. Resolution runs against the whole corpus (`Corpus.ResolveSignal`), so a post-condition may name a subject in the destination view, outside the tool's `ensure_view` scope (the common click-continue → wait-for-next-view case). A step with `then:` completes only once the named signal holds, not merely when the action dispatched. The runtime is unchanged — these desugar to query/route waits it already runs.

## 0.7.0

### Minor Changes

- 2c10d22: Support the `raw_text` extract mode (SEP-0013).

  The generator now compiles `extract: raw_text` to a first-class extractor kind — previously it fell through to a bogus raw-CSS fallback (`within: "raw_text"`), so `--via webmcp` silently failed. The runtime reads `raw_text` as the node's **own literal text** (the concatenation of its direct text-node children, whitespace-normalized) — never the accessibility name and never `innerText`. This mirrors the sightmap lib's offline `node.RawText`, so a `raw_text` predicate matches identically offline and at runtime.

  Verified live on jetblue.com: `select_fare` now resolves `SubFare[tier="Main"]` — a fare-tile heading whose accessibility name welds in a CSS `::after` "Most popular" badge — and commits the leg (was a silent timeout).

### Patch Changes

- 5112839: Fix the runtime click hanging forever when `requestAnimationFrame` is starved.

  `clickElement`'s hit-test settle loop (added to commit below-the-fold dropdown options) awaited the next animation frame, and its 300ms wall-clock deadline was only checked _between_ frames. So when rAF stops firing — a backgrounded/throttled tab, or a wedged SPA renderer that has stopped painting — the `await` never resolved and the whole tool promise hung indefinitely (every other step is bounded, so this was the sole unbounded path).

  `nextFrame()` now races rAF against a short timer, so the settle loop keeps turning and honors its deadline (falling back to node dispatch) even when no frame ever fires. When rAF is healthy it still wins the race, so the settle fast-path is unchanged.

  Surfaced driving jetblue checkout: `set_passenger` hung in the Title option click while `requestAnimationFrame` fired 0× in 2s.

- 5112839: Fix `sightkick browser --webmcp` naming Chrome flags that aren't real. It forced
  `--enable-blink-features=ModelContext,ModelContextTesting` and
  `--enable-features=DevToolsWebMCPSupport`; neither is a Chromium feature, so Chrome
  silently ignores them. The path still reached native WebMCP in practice because the
  managed Chrome for Testing enables the feature by default — which is exactly why the
  bogus flags went unnoticed. `--webmcp` now passes the real switch,
  `--enable-features=WebMCPTesting` (the command-line form of
  `chrome://flags/#enable-webmcp-testing`): correct on its own, and required if the
  session runs against Google Chrome stable rather than the managed CfT. The
  `sightkick-debug` skill and the vendored inspector `NOTES.md` are corrected to
  match. See sightmap/sightmap#413.

## 0.6.0

### Minor Changes

- 373da18: Optional/skippable tool steps, so a grouped tool can carry optional fields
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
- e45f9b3: `sightkick outline`/`sightkick explain` — a cheap, offline discovery surface for resolving a
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

### Patch Changes

- 61c5d51: `sightkick browser` now clears any script it previously persisted before
  injecting the runtime + IR, so a rebuilt runtime **replaces** the old one
  instead of stacking on top of it. Previously each run (especially with
  `--no-start`) added another persisted injection; on the next reload the daemon
  re-fired them all in document order, so an older runtime generation could
  re-register _after_ the freshly built one and silently clobber it — a
  convincing false negative while iterating on the runtime. Clearing is
  best-effort: with no session (or if inject is unsupported) it is a no-op.
- 92aa823: Fix `compile.query-ref` failing with `Available: (none)` for a tool that omits
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
- eed1049: Runtime `clickElement` now dispatches at the hit-tested element rather than the
  resolved node. A real click lands on the topmost element at the target's
  coordinates — often an inner child (e.g. `jb-select-option > div.body`) where the
  handler lives — so a synthetic sequence dispatched on the resolved ancestor
  never reached it and silently no-opped. `clickElement` now scrolls the target
  into view and is `async`: it retries across animation frames until
  `document.elementFromPoint` at the target's center resolves into the target's own
  subtree, then dispatches there (falling back to the node on budget expiry). This
  fixes custom dropdown options that failed to commit, including below-the-fold
  options where an immediate hit-test raced the post-scroll paint. Unblocks driving
  custom `<select>`-style components with the same two clicks a user makes.

## 0.5.0

### Minor Changes

- 1eecdfd: `sightkick browser --webmcp` now auto-loads the WebMCP inspector extension (an
  in-browser client that reads the tools sightkick registers on
  `document.modelContext`), alongside sightmap's overlay — no more manual
  `--extensions`. The inspector is vendored from its upstream source
  (`beaufortfrancois/model-context-tool-inspector`, Apache-2.0) via
  `npm run vendor-inspector` and embedded in the CLI. Pass `--no-inspector` for the
  native surface without it; any explicit `--extensions` are merged in.

## 0.4.0

### Minor Changes

- 9ebad83: Add `sightkick browser <corpus-dir>` — a one-command wrapper for the debug/drive
  loop. It builds the IR, starts a sightmap browser session (auto-opening the
  corpus's home-view URL unless `--url`; `--webmcp` adds the native-modelContext
  blink flags; `--profile`/`--cdp-port`/`--extensions`/`--chrome-flag` pass through
  to sightmap), and persist-injects the runtime + IR so the tools register on the
  page and survive navigations. Runs sightmap from the corpus dir so the session
  lands in its `.sightmap/` (discoverable + avoids the sessionless-fallback
  footgun); `--no-start` injects into an already-running session. The
  `sightkick-debug` skill documents it as the quick-start path. Requires the
  `sightmap` CLI on PATH.
- 6c9a46d: `sightkick call --via webmcp` now drives the tool through the sightmap CLI's own
  WebMCP driver (`sightmap browser mcp call`) instead of a baked-in `executeTool`
  implementation. That CLI command speaks the standard `document.modelContext`
  surface and owns the native-vs-polyfill argument and result differences, so
  sightkick no longer duplicates that handling (and no longer reaches for the
  runtime's private `window.__sightkick.call` global) — `--via webmcp` now genuinely
  exercises the standard-surface contract a real WebMCP client depends on.
  sightkick still unwraps the returned `CallToolResult` envelope into the runtime's
  typed `ToolResult`.

  Requires the sightmap CLI to provide `browser mcp call` with native-argument
  serialization (sightmap >= 0.31.x including that fix).

- bca9553: The tool layer moves from a single `webmcp.tools.yaml` file to a `.sightkick/`
  directory (a sibling of the corpus's `.sightmap/`). Every `*.yaml` file inside is
  merged into one manifest — tools and journeys concatenated, no dependencies
  between files — so a large tool layer can be split however helps (e.g. one file
  per view). `corpus:` now defaults to the sibling `../.sightmap` and `name:` to the
  app dir's name, so both are usually omitted. `sightkick build/browser/call` accept
  an app dir (or a `.sightkick` dir) as before. No migration path: rename your
  `webmcp.tools.yaml` to `.sightkick/tools.yaml` and drop the `corpus:` line.

## 0.3.0

### Minor Changes

- 39c6c0e: Add the `sightkick-authoring` skill, which documents the `webmcp.tools.yaml`
  grammar (tools, steps, `returns` value/list, guards, params, component queries,
  and journeys) with a worked example — so the manifest is authorable from the
  installed skills alone, without reading the repo's examples. Also: `sightkick
build --help` (and `-h`/top-level help) now print usage instead of erroring on a
  missing file, and the `sightkick-debug` skill documents `window.__sightkick.call`
  as the reliable scripted drive path (with the native-`document.modelContext`
  caveat) and cross-links the new authoring skill.

## 0.2.0

### Minor Changes

- ad1e7ce: Add `sightkick runtime [-o file.js]`, which emits the runtime bundle embedded in
  the binary — the payload injected into a live page (`window.__sightkick.load(ir)`)
  to register the compiled tools. This makes the debug/inject loop work from the
  installed CLI alone, with no repo checkout: `sightkick runtime -o rt.js` +
  `sightkick build <corpus> -o ir.json`, then inject both via `sightmap browser`.
  The `sightkick-debug` skill is updated to use it.

## 0.1.0

### Minor Changes

- b3d928e: First public release. Ships the sightkick generator CLI (compile a
  `webmcp.tools.yaml` + `.sightmap` corpus into WebMCP tool IR) and the
  `sightkick-debug` agent skill, installable via `sightkick skills install`
  (which also pulls in the supporting sightmap skills). The runtime is injected
  into a live page for debugging via `sightmap browser` — no bespoke browser
  extension.

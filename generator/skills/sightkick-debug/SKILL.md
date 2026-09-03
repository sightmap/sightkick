---
name: sightkick-debug
description: Drive and debug a generated sightkick WebMCP tool script on a live page via `sightmap browser`, with no bespoke sightkick extension. Use when you have a sightkick corpus + `webmcp.tools.yaml` (in the sightkick repo's `examples/` or a `sites/<name>/` dir) and want to inject its compiled tools into a running site and exercise them — either agent-driven (getTools/executeTool over `sightmap browser eval`) or Gemini-driven via the vendored WebMCP inspector.
activation:
  - a sightkick `webmcp.tools.yaml` + `.sightmap/` corpus is present and you want to run its tools on a live page
  - debugging why a generated WebMCP tool does/doesn't register or fire on a real site
---

# sightkick-debug: run generated WebMCP tools on a live page

sightkick compiles a `.sightmap/` corpus + a `webmcp.tools.yaml` into an **IR**,
and a ~19 KB runtime bundle turns that IR into WebMCP tools on
`document.modelContext`. You do **not** need a bespoke extension to get them onto
a page: `sightmap browser eval` injects the runtime bundle, then
`window.__sightkick.load(ir)` registers the tools. The page's own agent surface
(native `document.modelContext`, or our polyfill) then exposes them.

## Prerequisites

This skill uses two CLIs: **`sightkick`** (to build the IR and emit the runtime
bundle) and **`sightmap`** (to drive the live browser session). Install whichever
isn't already on your PATH, plus the supporting sightmap skills:

```sh
npm i -g @sightmap/sightkick             # the sightkick CLI (build + runtime + skills)
npx @sightmap/sightmap skills install    # or: sightmap skills install (if already on PATH)
sightmap browser install                 # Chrome-for-Testing; needs >=152 for native document.modelContext
```

That installs the **`sightmap-browser`** skill (driving a live session) and
**`sightmap-authoring`** skill (building the `.sightmap/` corpus) alongside this
one — everything needed to build and test a `webmcp.tools.yaml`. No repo checkout
is required; `<CORPUS_DIR>` below is any directory holding a `webmcp.tools.yaml`
+ `.sightmap/` corpus.

To **write** the `webmcp.tools.yaml` (its tool/step/`returns`/`journeys` grammar),
see the **`sightkick-authoring`** skill — this skill assumes it already exists and
compiles.

## Quick start: `sightkick browser`

For the common case, one command does the whole setup — build the IR, start a
sightmap session, and persist-inject the runtime + tools:

```sh
sightkick browser <CORPUS_DIR>
#   auto-opens the corpus's home-view URL (or pass --url URL)
#   --webmcp         expose the native document.modelContext (adds the blink flags)
#   --extensions P   load an unpacked extension (e.g. a WebMCP inspector)
#   --profile/--cdp-port/--chrome-flag   passed through to sightmap
#   --no-start       inject into an already-running session (e.g. after editing the corpus)
```

It runs sightmap from `<CORPUS_DIR>`, so the session lives in that dir's
`.sightmap/` — **drive it from there** (`cd <CORPUS_DIR> && sightmap browser
eval …`, see §4). The inject is **persisted**, so on SPA route changes (and, once
sightmap's persist re-injection lands, full navigations too) the tools
re-register automatically. Re-run `sightkick browser <CORPUS_DIR> --no-start`
after editing the corpus to refresh the injected IR.

The manual steps below (§1–§3) are the same thing unbundled — reach for them for
finer control, to understand what's happening, or when driving a site the wrapper
didn't start.

## 1. Build the two artifacts

Both come straight from the installed `sightkick` CLI:

```sh
# The payload: the runtime bundle (exposes window.__sightkick.load).
sightkick runtime -o /tmp/sightkick-runtime.js

# The IR for the corpus you're testing (any dir with webmcp.tools.yaml + .sightmap/).
sightkick build <CORPUS_DIR> -o /tmp/x.ir.json
```

Rebuild the IR whenever the corpus/manifest changes. The runtime bundle is
embedded in the CLI, so re-emit it after upgrading `sightkick`.

## 2. Pick a mode, start the session

Both modes use the **same inject step** (§3). They differ only in whether
`document.modelContext` is the page's **native** surface or our **polyfill**, and
therefore who drives.

### Mode A — agent-driven (polyfill; simplest)
No flags. If the page has no native `modelContext`, our runtime polyfills one, and
you drive it yourself over `eval` with the simple `{name}` call shape.

```sh
sightmap browser start --detach --url <SITE_URL> --profile /tmp/sk-dbg
```

### Mode B — inspector/Gemini-driven (native WebMCP surface)
Turn on the blink flags so Chrome exposes the real `document.modelContext`, and
load the **WebMCP inspector** (a drive-with-Gemini sidebar). Our tools register on
the native surface, so the inspector reads them like any site's own. The inspector
isn't shipped with the CLI — use the vendored copy in the sightkick repo
(`vendor/webmcp-tool/unpacked`, whose `NOTES.md` explains the flag/CfT-version
rationale) or install it from the Chrome Web Store. Point `<INSPECTOR_DIR>` at its
unpacked directory:

```sh
sightmap browser start --detach --url <SITE_URL> --profile /tmp/sk-dbg \
  --extensions ~/.sightmap/extension,<INSPECTOR_DIR> \
  --chrome-flag=--enable-blink-features=ModelContext,ModelContextTesting \
  --chrome-flag=--enable-features=DevToolsWebMCPSupport
```

(All `--extensions` entries must be absolute; listing any `--extensions` replaces
the auto-loaded overlay, so include `~/.sightmap/extension` explicitly.)

Then, in either mode, wait for the page and confirm readiness:

```sh
sightmap browser wait-for --selector <SOME_SELECTOR> --timeout-ms 10000
sightmap browser eval "typeof document.modelContext"          # object
```

## 3. Inject the runtime + IR

```sh
sightmap browser eval "$(cat /tmp/sightkick-runtime.js)"       # sets window.__sightkick
sightmap browser eval "window.__sightkick.load($(cat /tmp/x.ir.json))"
```

The bundle fits in an `eval` arg. `load(ir)` registers the tools that are
**view-scoped to the current URL** — so tools only appear on pages whose view
route matches (an empty `getTools()` on the wrong URL is correct, not a bug).

Confirm registration (works in both modes):

```sh
# getTools() is async — stash to a global, then read it.
sightmap browser eval "window.__t='RUN';document.modelContext.getTools().then(function(ts){window.__t=JSON.stringify(ts.map(function(x){return x.name}))});'go'"
sleep 1; sightmap browser eval "window.__t"                    # -> ["tool_a","tool_b",...]
```

## 4. Drive the tools

### Mode A (agent/scripted): drive via `window.__sightkick.call`
The **reliable** scripted entry point is `window.__sightkick.call(name, args)` — it
invokes a tool by name and resolves to the raw `ToolResult` directly, regardless
of whether the page's `document.modelContext` is our polyfill or a native surface:

```sh
sightmap browser eval "window.__r='RUN';window.__sightkick.call('add_task',{title:'Water the plants'}).then(function(r){window.__r=JSON.stringify(r)}).catch(function(e){window.__r='ERR '+e});'go'"
sleep 2; sightmap browser eval "window.__r"                    # the ToolResult JSON (ok/value/items/skipped/guidance)
```

The `ToolResult` carries `ok`, `value`/`items`, `skipped` (idempotency guard
hit), and `guidance` (next-step breadcrumbs). The sightkick repo's
`packages/runtime/eval/run.mjs` is a full scripted example of this loop.

> **Don't script `document.modelContext.executeTool` directly.** Its call shape
> differs by surface: the polyfill takes a bare `{name}`, but a **native**
> `document.modelContext` (present on Chrome ≥150 even with no blink flags) is
> stricter and rejects it with `Failed to parse input arguments`, and wraps the
> result in an envelope (`{content:[{type:'text',text:<ToolResult JSON>}]}`).
> `window.__sightkick.call` sidesteps both differences — prefer it for Mode A, and
> leave `executeTool` to the inspector in Mode B.

### Mode B (native): drive via the inspector
Open the inspector's sidebar and prompt Gemini — it enumerates
`document.modelContext.getTools()` (now including ours) and calls them through the
native surface. This is the "does a real WebMCP agent complete the flow" test.

> Native `executeTool` is stricter than the polyfill (it takes the full
> RegisteredTool from `getTools()` and its own argument serialization, and
> returns a **stringified** envelope). Let the inspector make those calls; for
> scripted/agent checks prefer Mode A, whose call shape is simple.

## 5. Navigation & re-injection

`eval`-injection lives on **one document**:

- **SPA** (client-side routing — e.g. the search demo): inject once; the
  runtime re-registers view-scoped tools on route changes. `wait-for --url` /
  `--selector` after an action that routes.
- **MPA** (a real page load — the URL changes with a full document load): the
  injected script is gone after the load. **Re-run §3** on the new page.

(When the sightmap `browser inject --persist` facility lands — CDP
`addScriptToEvaluateOnNewDocument` — inject once and it survives navigations,
retiring the per-nav re-inject. Until then, re-inject.)

## 6. Clean up

```sh
sightmap browser stop
rm -rf /tmp/sk-dbg
```

## Gotchas

- **Empty `getTools()` is usually right** — the tools are view-scoped; you're on a
  URL no view matches. Check `location.pathname` against the corpus's view routes.
- **Native surface needs Chrome ≥150 and the blink flags on the command line** —
  toggling `chrome://flags` in the automation profile does nothing (it's a Blink
  runtime feature). `sightmap browser install` should pull ≥152.
- **The bundle is ~19 KB** — well within an `eval` arg; no hosting or fs-access
  needed. Rebuild it after runtime changes.
- **A stale daemon collides** — `sightmap browser stop` before `start`, and give
  `--detach` a beat for the content tab to open before page commands (poll
  `status` / `wait-for`).

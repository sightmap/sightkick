# sightkick

Compile a [sightmap](https://github.com/sightmap/sightmap) corpus plus a tool
layer into a runtime-agnostic **IR**, and run it on arbitrary sites. sightkick
lets an agent (or a stored, replayable plan — see
[`docs/scenario-testing.md`](docs/scenario-testing.md)) call named actions
(`add_todo`, `search`, …) on a site instead of clicking around blind. Tools are
**UI-driven** — they use the same affordances a user has, so every action
produces visible feedback and there are no under-the-UI state pokes.

The compiled IR — every component reference resolved to a concrete locator,
every property to an extractor — is the thing that's runtime-agnostic, not any
one way of executing it. [WebMCP](https://github.com/webmachinelearning/webmcp)
(`document.modelContext`, when the runtime is installed on a page) and the
CLI (`sightkick call --via cli`, shelling to real `sightmap browser` commands,
no runtime install needed) are two independent, already-working consumers of
that same artifact — see **Runtimes** below. A Playwright emitter is a
straightforward third consumer, not yet built; the mapping is specified in
`docs/scenario-testing.md`.

**Coordination model.** A *tool* is anything doable at a single point in time
(same "page", no navigation crossing) — it may bundle several simultaneous
sub-actions and returns a structured result. Multi-step flows are **not** run by
a runtime executor; instead a *journey* is a compile-time transition graph over
tools that is projected into **guidance breadcrumbs** attached to each tool's
result ("consider `set_filters` and/or `list_flights` next"). The agent does the
sequencing; we hand it a good map. This sidesteps WebMCP's unsolved
cross-document-response problem entirely.

sightkick is a **library-level consumer** of sightmap: the offline generator
depends on the sightmap reference library
([`github.com/sightmap/sightmap/go`](https://pkg.go.dev/github.com/sightmap/sightmap/go/sightmap),
pinned) for all corpus reading, `$ref` expansion, hierarchy flattening, and route
matching — so there's no second implementation of the spec to keep honest. The
compiled **IR** is the firewall: the runtime consumes it and never sees a
sightmap construct, so it stays pure TypeScript.

## Layout

```
generator/     # Go CLI: .sightkick/ tool layer + .sightmap corpus -> IR (JSON)
               #   (consumes github.com/sightmap/sightmap/go); embeds + installs the skills
packages/
  runtime/     # browser: boot + atomic-tool execution + WebMCP registration
skills/        # canonical agent skills (sightkick-debug); embedded into the generator
examples/
  todo/        # single-view example (tools + same-view guidance)
  search/      # two-view example (transition + cross-view guidance + rich returns)
  saucedemo/   # real external site, no app shipped — corpus + tools + Gherkin
               #   scenarios + stored plans; see docs/scenario-testing.md
docs/
  scenario-testing.md  # scenario -> corpus -> tools/journeys -> plan, worked end to end
scripts/
  run-plan.mjs # replays a stored plan (examples/saucedemo/plans/*.json), no agent
vendor/
  webmcp-tool/ # WebMCP inspector, vendored from upstream + embedded in the CLI
               #   (`--webmcp` auto-loads it to drive injected tools)
```

The generator is Go and the runtime is TypeScript — a deliberate polyglot split
across the IR firewall. The two sides share no code; they share the IR JSON
contract.

## Runtimes

The compiled IR has exactly one shape; three things can execute it:

| Runtime | How | Status |
|---|---|---|
| WebMCP (`document.modelContext`) | The runtime bundle installed on the page, registering IR tools as native WebMCP tools | Built |
| CLI (`sightkick call --via cli`) | Shells to real `sightmap browser click`/`fill`/`wait-for` commands — no runtime install, reaches portal-rendered elements a runtime's synthetic clicks can't | Built |
| Playwright | Would emit `page.locator()`/`.click()`/`.fill()` from the same resolved locators/extractors | Not built — mapping specified in [`docs/scenario-testing.md`](docs/scenario-testing.md#7-the-output-is-runtime-agnostic) |

`sightkick call`'s own `--via` flag switches between the first two today. Neither is more
"canonical" than the other — they're peers over the same IR, chosen by what's available on the
target page (a runtime install) versus what's always available (a running browser session).

## Try the generator

```sh
cd generator
go run . build ../examples/todo
```

This compiles `examples/todo/.sightkick/` against
`examples/todo/.sightmap/` into a self-contained **IR**: every component
reference resolved to concrete DOM locators, every property `value`/`list`
resolved to a DOM extractor, and unresolved refs reported at compile time with
candidate lists.

Tools reference corpus components by their sightmap identity through a
CSS-shaped **component query** (`OptionGroup[name="Protein" i] OptionButton[label="{{option}}" i]`),
so there is no raw-CSS escape hatch to keep honest — the query resolves against
the corpus at compile time.

## Try the runtime

```sh
cd packages/runtime
pnpm install   # from repo root the first time
pnpm test      # headless: atomic-tool execution + WebMCP surface, driven by golden IR
pnpm demo      # rebuilds + serves the search demo
```

`pnpm demo` serves the **search** demo — a two-view SPA (`/` → `/results`) wired
to the generated `search` IR. Tools register **per view** and re-register on the
client-side route change (no reload); `search`'s result carries `after_navigation`
guidance toward the results view. The page boots the runtime, which installs a
spec-shaped `document.modelContext` polyfill (when the browser has no native
WebMCP) and registers the IR tools. Drive them the way a browser agent would —
through the standard WebMCP surface:

```js
(await document.modelContext.getTools()).map(t => t.name)   // ["search"] on /
const [s] = (await document.modelContext.getTools()).filter(t => t.name === 'search')
await document.modelContext.executeTool(s, { query: 'ATL to LHR' })  // fills, clicks, navigates
// now on /results: getTools() -> ["list_results","set_sort",...]
```

Either way it happens through real clicks/fills — the same affordances a user has.
This flow is verified agent-driven against a real browser with the `sightmap
browser` CLI (using `examples/search/.sightmap`).

## Run the tools on a live page

To exercise the compiled tools against a real third-party site (no direct install
required), inject the runtime bundle + IR into a running page and drive them —
either agent-driven over `sightmap browser eval`, or with the vendored **WebMCP
inspector** (a Gemini sidebar) reading the tools off the page's native
`document.modelContext`. The full loop — build the bundle + IR, launch the
session with the right Chrome-for-Testing flags, inject, drive, and re-inject
across navigations — is the **`sightkick-debug`** skill
([`skills/sightkick-debug/SKILL.md`](skills/sightkick-debug/SKILL.md)). The
Chrome-for-Testing flags and their rationale live in
[`vendor/webmcp-tool/NOTES.md`](vendor/webmcp-tool/NOTES.md).

Install the skills (this also pulls in the supporting sightmap skills):

```sh
sightkick skills install     # or, from a release: npx @sightmap/sightkick skills install
```

### WebMCP notes

- `document.modelContext` is **tools-only** today (no resources/prompts); "skills"
  is an open question upstream. Multi-step coordination is therefore carried as
  guidance in tool results, not as a protocol construct.
- Cross-document tool responses are **unspecified** (WebMCP issue #135) and
  effectively unsupported in reference clients — which is why tools never cross a
  navigation.

## The two formats

- **`.sightmap/` corpus** — the sightmap authority (views, components,
  properties, routes). sightkick reads it; it never writes it.
- **`.sightkick/` tool layer** — a consumer format (not a sightmap SEP), sibling
  to `.sightmap/`: any number of `*.yaml` files, merged into one manifest, of
  atomic view-scoped tools that reference corpus components by component query,
  plus a `journeys:` transition graph that compiles into guidance. Live DOM flows
  only; no `mode: fetch`.

## Status

Early, but end-to-end for the single-page and SPA cases:

- **Generator** (Go) compiles corpus + manifest → IR. `cd generator && go test ./...`
  (golden-IR check on the todo/search examples).
- **Runtime** (TS) executes IR tools against the live DOM as real WebMCP tools.
  `cd packages/runtime && pnpm test` drives the fixtures with the generator's
  golden IR, covering the whole pipeline across the IR firewall.

Journeys compile into **guidance breadcrumbs** carried in each tool result; tools
register **per view** (only the current view's tools are offered), and a
cross-view edge yields `after_navigation` guidance ("read the results there").
The `search` example exercises this end-to-end
(`generator/.../testdata/search.ir.json` driven by a runtime test across a
simulated navigation), with rich returns (machine ids + human fields) and
same-page idempotency guards.

The served two-view SPA is verified in a real browser via `sightmap browser`:
view-scoped tools, SPA re-registration without reload, cross-view guidance, and
rich returns all confirmed live. A deterministic eval harness
(`packages/runtime/eval/run.mjs`) replays the whole flow tools-in / DOM-out.

Turning a corpus + tool layer into a database of scenario tests — Gherkin →
plan → replay without an agent — is worked through end to end, on a real
external site, in [`docs/scenario-testing.md`](docs/scenario-testing.md), which
also states plainly what that pipeline doesn't do yet (scenario→plan resolution
and a Playwright emitter are both designed, not built) and two permanent
sightmap-level limitations found building it.

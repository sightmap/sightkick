# sightkick

Generate [WebMCP](https://github.com/webmachinelearning/webmcp) tools from
[sightmap](https://github.com/sightmap/sightmap) corpora, and run them on
arbitrary sites via a browser extension.

sightkick lets an agent call named actions (`add_todo`, `search`, …) on a site
instead of clicking around blind. Tools are **UI-driven** — they use the same
affordances a user has, so every action produces visible feedback and there are
no under-the-UI state pokes.

A single compiled artifact runs the **same way** whether it's installed directly
on a site (`<script>` / userscript) or injected by the sightkick browser
extension into a third-party site. The extension is *only* an injector — there's
no separate "mediated" behavior, no background state, no runtime executor.

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
compiled **IR** is the firewall: the runtime and extension consume it and never
see a sightmap construct, so they stay pure TypeScript.

## Layout

```
generator/     # Go CLI: webmcp.tools.yaml + .sightmap corpus -> IR (JSON)
               #   (consumes github.com/sightmap/sightmap/go)
packages/
  runtime/     # browser: boot + atomic-tool execution + WebMCP registration
  extension/   # MV3 extension: inject the generated artifact into 3rd-party sites (next)
examples/
  todo/        # single-view example (tools + same-view guidance)
  search/      # two-view example (transition + cross-view guidance + rich returns)
```

The generator is Go and the runtime/extension are TypeScript — a deliberate
polyglot split across the IR firewall. The two sides share no code; they share
the IR JSON contract.

## Try the generator

```sh
cd generator
go run . build ../examples/todo
```

This compiles `examples/todo/webmcp.tools.yaml` against
`examples/todo/.sightmap/` into a self-contained **IR**: every component
reference resolved to concrete CSS locators, every property `where`/extract
resolved to a DOM extractor, and unresolved refs reported at compile time with
candidate lists.

## Try the runtime

```sh
cd packages/runtime
pnpm install   # from repo root the first time
pnpm test      # headless: atomic-tool execution + WebMCP surface, driven by golden IR
pnpm demo      # rebuilds + serves the todo demo
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
// now on /results: getTools() -> ["list_results","set_sort"]
```

Either way it happens through real clicks/fills — the same affordances a user has.
This flow is verified agent-driven against a real browser with the `sightmap
browser` CLI (using `examples/search/.sightmap`).

To test with a real client, note that **Chrome's Ask Gemini gates WebMCP on
`https:`**; `pnpm demo` auto-switches to HTTPS once a local cert is present —
see [`packages/runtime/certs/README.md`](packages/runtime/certs/README.md)
(one-time mkcert setup).

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
- **`webmcp.tools.yaml`** — the *skill layer*. A consumer format (not a sightmap
  SEP): atomic, view-scoped tools that reference corpus components by name (`css:`
  is the escape hatch), plus a `journeys:` transition graph that compiles into
  guidance. Live DOM flows only; no `mode: fetch`.

## Status

Early. The atomic-tools baseline is end-to-end for the single-page case:

- **Generator** (Go) compiles corpus + manifest → IR. `cd generator && go test ./...`
  (golden-IR check on the todo example).
- **Runtime** (TS) executes IR tools against the live DOM as real WebMCP tools.
  `cd packages/runtime && pnpm test` drives the fixture with the generator's
  golden IR, covering the whole pipeline across the IR firewall.

Journeys now compile into **guidance breadcrumbs** carried in each tool result;
tools register **per view** (only the current view's tools are offered), and a
cross-view edge yields `after_navigation` guidance ("read the results there").
The `search` example exercises this end-to-end (`generator/.../testdata/search.ir.json`
driven by a runtime test across a simulated navigation), with rich returns
(machine ids + human fields).

The served two-view SPA is verified in a real browser via `sightmap browser`:
view-scoped tools, SPA re-registration without reload, cross-view guidance, and
rich returns all confirmed live.

Next: same-page idempotency guards, then the extension injector (semantically
identical to direct install). See `.yaks/` (local) for the working task herd.

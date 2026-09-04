# AGENTS.md

Guidance for coding agents working in **sightkick** — the tool that compiles a
[sightmap](https://github.com/sightmap/sightmap) corpus and a tool layer into a
runtime-agnostic IR, and runs it on live sites via
[WebMCP](https://github.com/webmachinelearning/webmcp) or a CLI executor (peers,
not a primary and a fallback — see the README's Runtimes section). Start with
[`README.md`](README.md) for the architecture (the IR firewall, tools vs.
journeys, the polyglot Go/TS split); this file only adds agent-facing working
notes.

## Layout

| Path | What it is |
|---|---|
| `generator/` | Go CLI: `.sightkick/` tool layer + `.sightmap` corpus → IR JSON. Consumes `github.com/sightmap/sightmap/go` (pinned). Also embeds + installs the agent skills (`sightkick skills install`). |
| `packages/runtime/` | TS: boots the IR, executes atomic tools against the live DOM, registers `document.modelContext`. |
| `skills/` | Canonical agent skills (`sightkick-debug`). Embedded into the generator (`generator/skills/`, generated) and installable via the CLI. |
| `examples/` | `todo` (single view), `search` (two-view SPA), `saucedemo` (real external site, no app shipped — see `docs/scenario-testing.md`). |
| `docs/scenario-testing.md` | Scenario → corpus → tools/journeys → plan, worked end to end; states what's built vs. designed. |
| `scripts/run-plan.mjs` | Replays a stored plan (`examples/saucedemo/plans/*.json`) with no agent; drift-checks it against the scenario file and the compiled manifest first. |
| `vendor/webmcp-tool/` | WebMCP inspector extension, vendored from upstream (`npm run vendor-inspector`) and embedded into the CLI (`generator/webmcpinspector/`) so `sightkick browser --webmcp` auto-loads it. See its [`NOTES.md`](vendor/webmcp-tool/NOTES.md). |

## Build / test

```sh
pnpm install && pnpm build     # -r across packages; builds the runtime bundle
pnpm test                      # -r; runtime golden-IR tests
cd generator && go test ./...  # generator golden-IR check
```

To (re)compile an example's IR: `cd generator && go run . build ../examples/<name>`.

After editing the canonical skills under `skills/`, regenerate the embedded copy
and confirm no drift (this is the CI "Verify" step):

```sh
cd generator
go build ./... && go generate ./skills/... && git diff --exit-code -- skills && go test ./...
```

The compiled runtime bundle is also embedded in the binary (so `sightkick
runtime` can emit it with no repo checkout). After changing runtime source,
rebuild it and re-sync the embedded copy, then commit both:

```sh
pnpm --filter @sightkick/runtime build          # -> packages/runtime/dist/sightkick-runtime.js
cd generator && go generate ./runtimebundle/... # copies it to generator/runtimebundle/
```

CI's runtime job rebuilds the bundle and fails on any drift from the committed
copy.

The **WebMCP inspector** is embedded the same way so `sightkick browser --webmcp`
can auto-load it. Its canonical copy at `vendor/webmcp-tool/unpacked/` is vendored
from upstream (built from source — see `vendor/webmcp-tool/NOTES.md`) and the
embedded copy under `generator/webmcpinspector/` is generated. To bump it:

```sh
npm run vendor-inspector                          # pull + build a newer upstream pin
cd generator && go generate ./webmcpinspector/... # sync the embedded copy
```

Commit both the `vendor/` and `generator/webmcpinspector/` copies; CI fails on
drift. Don't hand-edit either.

## Running the tools on a live page

sightkick has **no bespoke extension**. The generated tools are injected into a
running page via `sightmap browser eval` (the ~19 KB runtime bundle +
`window.__sightkick.load(ir)`) and driven either by the agent or by the vendored
WebMCP inspector. The full loop is the **`sightkick-debug`** skill
([`skills/sightkick-debug/SKILL.md`](skills/sightkick-debug/SKILL.md)); the
Chrome-for-Testing flags and their rationale live in
[`vendor/webmcp-tool/NOTES.md`](vendor/webmcp-tool/NOTES.md).

## Conventions

- Corpora are read, never written — sightkick is a consumer of sightmap, not a
  second implementation of the spec.
- Keep the IR firewall intact: the runtime consumes IR JSON and must never learn
  about sightmap constructs.
- `examples/*/.sightmap/config.yaml` is force-added — the repo `.gitignore` has a
  broad `config.yaml` rule, so `git add -f` new example configs.
- Don't hand-edit the generated embedded skills under `generator/skills/` — edit
  the canonical copy in `skills/` and regenerate.

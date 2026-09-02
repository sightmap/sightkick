# AGENTS.md

Guidance for coding agents working in **sightkick** — the tool that compiles
[WebMCP](https://github.com/webmachinelearning/webmcp) tools from
[sightmap](https://github.com/sightmap/sightmap) corpora and injects them into
sites via a browser extension. Start with [`README.md`](README.md) for the
architecture (the IR firewall, tools vs. journeys, the polyglot Go/TS split);
this file only adds agent-facing working notes.

## Layout

| Path | What it is |
|---|---|
| `generator/` | Go CLI: `webmcp.tools.yaml` + `.sightmap` corpus → IR JSON. Consumes `github.com/sightmap/sightmap/go` (pinned). |
| `packages/runtime/` | TS: boots the IR, executes atomic tools against the live DOM, registers `document.modelContext`. |
| `packages/extension/` | MV3 extension that injects the compiled artifact into third-party sites. `dist/` is a **gitignored build output**. |
| `examples/` | `todo`, `search`, `burrito` (external corpus), `jetblue` (real third-party site). |
| `vendor/webmcp-tool/` | Vendored, unpacked WebMCP inspector extension (temporary/playground). See its [`NOTES.md`](vendor/webmcp-tool/NOTES.md). |

## Build / test

```sh
pnpm install && pnpm build     # -r across packages; builds packages/extension/dist
pnpm test                      # -r; runtime golden-IR tests
cd generator && go test ./...  # generator golden-IR check
```

To (re)compile an example's IR: `cd generator && go run . build ../examples/<name>`.

## Testing sightkick end-to-end in a real browser

The extension is exercised by loading it into a `sightmap browser` session
**together with** the built-in sightmap overlay and the vendored WebMCP
inspector — all three at once. Two easy-to-miss constraints (both verified
against the `sightmap browser` source):

- Passing `--extensions` **replaces** the overlay the CLI would otherwise
  auto-load from `~/.sightmap/extension/`, so the overlay must be listed
  explicitly alongside the others.
- The CLI abs-resolves the whole comma-separated `--extensions` string, so every
  entry must be **absolute** (use `~` / `$PWD`, which the shell expands).

The canonical launch command (build the extension first) lives in
[`README.md`](README.md#test-end-to-end-in-a-real-browser-extension), with the
Chrome-for-Testing flags and their rationale in
[`vendor/webmcp-tool/NOTES.md`](vendor/webmcp-tool/NOTES.md). Treat those two as
the source of truth.

## Conventions

- Corpora are read, never written — sightkick is a consumer of sightmap, not a
  second implementation of the spec.
- Keep the IR firewall intact: the runtime/extension consume IR JSON and must
  never learn about sightmap constructs.
- `examples/*/.sightmap/config.yaml` is force-added — the repo `.gitignore` has a
  broad `config.yaml` rule, so `git add -f` new example configs.
- This repo uses **yaks** (`.yaks/`, local-only) for task tracking.

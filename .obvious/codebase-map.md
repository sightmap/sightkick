# sightkick — codebase map

Folder-level map (depth <= 2). 105 tracked files; two build sides joined only by the IR JSON contract.

| Path | What it is |
|---|---|
| `generator/` | Go 1.23 CLI (module `sightkick/generator`) — compiles a `.sightkick/` tool layer + `.sightmap/` corpus into IR JSON; embeds and installs the agent skills |
| `generator/cmd_*.go` | Subcommands and their tests: `build` (compile to IR), `browser` (live session + inject), `call` (invoke one tool), `runtime` (emit the bundle), `skills install` |
| `generator/internal/` | Compile pipeline internals — `internal/gen` reads the corpus via the pinned `github.com/sightmap/sightmap/go` library, resolves component queries, emits IR; golden files in `internal/gen/testdata/` |
| `generator/skills/` | GENERATED embedded copy of `skills/` — never hand-edit; sync with `go generate ./skills/...` |
| `generator/runtimebundle/` | GENERATED embedded copy of the built runtime bundle — sync with `go generate ./runtimebundle/...` |
| `generator/npm/` | npm packaging for `@sightmap/sightkick` (published meta package + release-time build scripts) |
| `packages/runtime/` | TypeScript browser runtime (`@sightkick/runtime`): boot, atomic-tool execution, WebMCP registration |
| `packages/runtime/src/` | `boot.ts`, `executor.ts`, `webmcp.ts`, `dom.ts`, `channel.ts`, `ir.ts`, `client.ts`, `errors.ts`, `index.ts` |
| `packages/runtime/test/` | vitest suites (executor, webmcp surface, search SPA route change, dom, channel, ...) — driven by the generator's golden IR |
| `packages/runtime/demo/` | Demo apps: `search.html` + SPA JS (two-view `/` -> `/results`), `todo.html` + app JS |
| `packages/runtime/eval/` | Deterministic eval harness (`run.mjs`) — replays the search flow tools-in/DOM-out via `sightmap browser eval` |
| `skills/` | Canonical agent skills: `sightkick-debug` (live-page loop), `sightkick-authoring` — embedded into the generator |
| `examples/todo/` | Single-view example: `.sightkick/` tool layer + `.sightmap/` corpus |
| `examples/search/` | Two-view SPA example: transition + cross-view guidance + rich returns |
| `vendor/webmcp-tool/` | Vendored, unpacked WebMCP inspector extension (+ `NOTES.md` on Chrome-for-Testing flags) |
| `.github/workflows/` | `ci.yml` (generator + runtime jobs with drift checks), `release.yml` (changesets -> goreleaser -> npm trusted publishing) |
| `.changeset/` | Changesets config |

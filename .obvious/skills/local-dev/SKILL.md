---
name: local-dev
description: Bring the sightkick repo sandbox to a working local dev environment — toolchain installs, canonical pnpm setup, demo server, and browser e2e; recorded during the 2026-09-03 autobuild onboarding.
---

# local-dev — sightkick sandbox setup

What the onboarding run did to make this sandbox a working dev environment, and how to re-verify or
rebuild it.

## What was installed (absent from the base image)

| Tool | Version | Where / how |
|---|---|---|
| Node | v22.23.2 | `/usr/local/node22`, symlinked into `/usr/local/bin` (base image had v20.20.2 — too old for pinned pnpm 11.17) |
| Go | 1.23.12 | `/usr/local/go` (CI pins "1.23") |
| pnpm | 11.17.0 | corepack shim in `/usr/local/bin` (`corepack prepare pnpm@11.17.0 --activate`; `packageManager`-pinned) |
| sightmap CLI | 0.31.1 | `npm i -g @sightmap/sightmap` |
| Chrome-for-Testing | 152.0.7977.75 | `sightmap browser install` -> `~/.sightmap/browsers/` |
| Chrome system libs | — | apt: libnspr4 libnss3 libatk1.0-0 libatk-bridge2.0-0 libcups2 libdbus-1-3 libxkbcommon0 libasound2 libgbm1 libxcomposite1 libxdamage1 libxfixes3 libxrandr2 libatspi2.0-0 fonts-liberation |

Base-image quirk: the sandbox image pre-installed dependencies with **bun** (root-owned
`node_modules/`, a `workspaces` field injected into `package.json`, untracked `bun.lock`). Onboarding
reverted `package.json` to HEAD, deleted `bun.lock` and the bun `node_modules` (needs `sudo` — they
are root-owned), then installed canonically with `pnpm install --frozen-lockfile`. Repeat that if a
fresh session shows the same dirty state.

## Bring-up sequence

1. `pnpm install --frozen-lockfile` (repo root)
2. `pnpm -r build && pnpm -r typecheck && pnpm -r test`
3. `cd generator && go build ./... && go test ./...`
4. Demo server: `cd packages/runtime && node serve.mjs` -> http://127.0.0.1:5174/ (port fixed in
   `serve.mjs`; reuse an already-running server — the eval harness does the same)
5. Browser e2e (optional; needs 1 + 4): `cd generator && go test -tags e2e -run TestCallE2E .`

No env vars, no secrets, no database, no Docker. Ports: **5174** (dev demo), 7891/7892 (sightmap
daemon + CDP — transient, managed by `sightmap browser start/stop`).

## Verified at onboarding (2026-09-03)

- pnpm build / typecheck / test: 41/41 tests green; runtime bundle 19.3 KB; embedded copies in sync.
- go build / test (golden IR) / `go vet -tags e2e` green; both drift checks in sync.
- CLI compiles both examples to IR; `skills install --target <dir>` works.
- Real-browser e2e: `TestCallE2E` 12/12 subtests (via webmcp AND via cli), `TestCallE2EErrors` 2/2.
- Manual WebMCP-surface transcript: `getTools()` on `/` -> `["search"]`; `executeTool(search, ...)`
  fills/clicks/navigates to `/results`; `getTools()` there -> `["book","list_results","select_flight","set_sort"]`.

## Findings — Chrome 152 native WebMCP drift (non-blocking; CI unaffected)

Chrome-for-Testing 152 ships a **native** `document.modelContext`, so the runtime no longer installs
its `__sightkickPolyfill` (it only polyfills when the browser has none). Two repo harnesses were
written against the polyfill-era API and fail against the native binding's stricter rules:

1. `pnpm eval` (`packages/runtime/eval/run.mjs`): it passes a `{name}`-only tool object and an
   object-literal input to `executeTool`; native WebIDL requires a full `RegisteredTool` (with
   `description`) and JSON-**string** input, so the first tool call throws a TypeError. The
   registration check just before it (view-scoped `getTools()`) passes. Fix direction: pass the tool
   object returned by `getTools()` and `JSON.stringify` the input.
2. `go test -tags e2e -run TestCallE2ENavigation` (`via_webmcp` subtest): the navigating tool's
   cross-document response now surfaces as an opaque native exception with empty stdout, which the
   test's `callRaw` helper cannot parse (it `t.Fatalf`s before its assertions). The subtest's intent
   — the tool acts, cannot report, and the page still lands on `/results` — still holds. The README
   itself flags cross-document tool responses as unspecified upstream (WebMCP issue #135).

`TestCallE2E`, `TestCallE2EErrors`, and everything CI runs stay green — treat these two as
harness-vs-browser drift to fix in-repo, not as a broken dev stack.

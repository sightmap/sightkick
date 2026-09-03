# sightkick — agent guide

**Repo:** `sightmap/sightkick` — generate [WebMCP](https://github.com/webmachinelearning/webmcp) tools
from [sightmap](https://github.com/sightmap/sightmap) corpora and run them on arbitrary sites.

Companion docs: [`README.md`](../README.md) (architecture — the IR firewall, tools vs. journeys),
[`AGENTS.md`](../AGENTS.md) (agent conventions), [`codebase-map.md`](codebase-map.md),
[`skills/local-dev/SKILL.md`](skills/local-dev/SKILL.md) (how this sandbox environment was built).

## Stack

| Side | Language | Toolchain | Role |
|---|---|---|---|
| `generator/` | Go 1.23 | Go modules (`generator/go.mod`) | CLI: `.sightkick/` tool layer + `.sightmap/` corpus → IR JSON; embeds + installs the skills |
| `packages/runtime/` | TypeScript 5, ESM | pnpm 11.17.0 (pinned via `packageManager`), esbuild, vitest 2 + happy-dom | Browser runtime: boots the IR, executes atomic tools against the live DOM, registers `document.modelContext` |

Polyglot pnpm monorepo (`pnpm-workspace.yaml`: `packages/*`, `generator/npm`). **No Docker, no
database, no required env vars or secrets.** The two sides share only the IR JSON contract (the
"IR firewall") — the runtime must never learn sightmap constructs.

Toolchain pins: Node >= 22 in practice (root `engines` says >= 20, but pinned pnpm 11.17 needs
Node >= 22 — CI runs Node 22); Go 1.23 (CI pins "1.23").

## Commands (verified 2026-09-03)

```sh
pnpm install --frozen-lockfile        # workspace deps (51 packages)
pnpm -r build                         # -> packages/runtime/dist/sightkick-runtime.js (19.3 KB) + demo bundles
pnpm -r typecheck                     # tsc --noEmit
pnpm -r test                          # vitest: 8 files / 41 tests, headless (happy-dom), golden IR
cd generator && go build ./... && go test ./...    # generator golden-IR check
cd generator && go run . build ../examples/todo    # compile an example corpus to IR (stdout)
cd packages/runtime && node serve.mjs               # serve the search demo SPA on http://127.0.0.1:5174/
```

CI parity (`.github/workflows/ci.yml`):
- runtime job — `pnpm install --frozen-lockfile && pnpm -r build && pnpm -r typecheck && pnpm -r test`, then `go generate ./runtimebundle/...` must produce no diff.
- generator job — `go build ./... && go generate ./skills/... && git diff --exit-code -- skills && go test ./... && go vet -tags e2e ./...`.

Drift rules (enforced by CI):
- After editing `skills/`: run `cd generator && go generate ./skills/...` — `generator/skills/` must not diff.
- After editing runtime source: `pnpm --filter @sightkick/runtime build && cd generator && go generate ./runtimebundle/...` — `generator/runtimebundle/` must not diff.

## Local Verification Summary

Onboarding run 2026-09-03 (sandbox `cmp_zmhYcLhU`), all green:

- `pnpm install --frozen-lockfile` — clean; lockfile passes supply-chain policies.
- `pnpm -r build` — runtime bundle 19.3 KB + demo bundles; embedded copies in sync.
- `pnpm -r typecheck` — clean. `pnpm -r test` — **41/41 tests in 8 files**.
- `go build ./...`, `go test ./...` (golden IR), `go vet -tags e2e ./...` — green; both drift checks in sync.
- CLI: `go run . build ../examples/todo` and `../examples/search` compile to IR.
- Demo server: `GET /` and `GET /results` → 200 on http://127.0.0.1:5174/ (port is fixed in `serve.mjs`).
- Real-browser e2e (`go test -tags e2e`, Chrome-for-Testing 152 + sightmap CLI 0.31.1):
  `TestCallE2E` **12/12 subtests PASS** (booking journey via `--via webmcp` and `--via cli`);
  `TestCallE2EErrors` **2/2 PASS**; screenshots of both demo views captured.
- Known non-blocking drift on Chrome 152 native WebMCP: `pnpm eval` and the
  `TestCallE2ENavigation/via_webmcp` subtest fail on the native binding's stricter argument rules —
  details and workarounds in [`skills/local-dev/SKILL.md`](skills/local-dev/SKILL.md) ("Findings").
  Neither runs in CI.

## Local verification (quick health check)

The autobuild snapshot ships with Node 22 / Go 1.23 / pnpm 11.17 / sightmap CLI + Chrome-for-Testing
(+ its system libraries) installed and `node_modules/` populated. To check a session is still healthy:

```sh
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:5174/    # 200 = demo still up
node packages/runtime/serve.mjs                                    # (re)start it otherwise
pnpm -r test && (cd generator && go test ./...)
```

Browser e2e (needs the demo on :5174 and the sightmap CLI on PATH; not run in CI):

```sh
cd generator && go test -tags e2e -run TestCallE2E .    # the primary user journey, both call paths
```

## Codebase map

Folder-level overview: [`codebase-map.md`](codebase-map.md). Quick facts:

- `examples/todo` (single view) and `examples/search` (two-view SPA) each carry `.sightkick/` + `.sightmap/`.
- `examples/*/.sightmap/config.yaml` is force-added — the root `.gitignore` has a broad `config.yaml` rule (`git add -f` new ones).
- Never hand-edit `generator/skills/` or `generator/runtimebundle/` — both are generated; regenerate instead.
- Corpora are read, never written (sightkick is a consumer, not a second spec implementation).
- Releases: changesets on main → goreleaser → npm trusted publishing (`.github/workflows/release.yml`).

## Snapshot

- Computer: `cmp_zmhYcLhU` (repo sandbox; E2B template `tqf1pejlbombikacg3v8:default`)
- Snapshot built: **2026-09-03T17:38:30.838Z** (sandboxId `i7b264wrjsocghlvtm3k0`)
- State at snapshot: dev stack healthy per the Local Verification Summary above; demo server running on :5174.

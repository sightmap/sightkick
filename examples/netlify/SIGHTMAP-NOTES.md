# Netlify (app.netlify.com) — sightkick example notes

Goal: prove sightkick end-to-end against a real third-party site — author a
sightmap corpus for the Netlify web app, compile `webmcp.tools.yaml` into IR, and
drive the tools through `navigator.modelContext` via the sightkick extension.

Target flows (chosen with the account owner):

1. **Sites list → project → deploys** — read-heavy.
2. **Environment variables** — list / add / delete. (Edit is modeled in the
   corpus but has no tool yet.)

**Writes are confined to the scratch project `westernwoodlands`**
(<https://app.netlify.com/projects/westernwoodlands/overview>). Nothing else on
the account may be written.

## Status

| Piece | State |
| --- | --- |
| `.sightmap/components.yaml` (globals) | done — app shell, rails, both navs, ProjectCrumbs, HelpMenu, ConfirmDialog |
| `.sightmap/views/projects.yaml` | done, 0 orphaned, captured |
| `.sightmap/views/project-overview.yaml` | done, 0 orphaned, captured |
| `.sightmap/views/project-deploys.yaml` | done, 0 orphaned, captured |
| `.sightmap/views/project-env-vars.yaml` | done, 0 orphaned, captured |
| `webmcp.tools.yaml` | done — 7 tools, 2 journeys |
| IR build | done — `packages/extension/corpora/netlify.ir.json` |
| Extension wiring | done — `corpora/index.json` entry + `build.mjs` copy; `pnpm build` passes |
| End-to-end | **verified** for all 7 tools on the live site (see below) |

Coverage at last run:

```
Projects         135 interactive · 76 T1 (56%) · 0 orphaned ✓
ProjectOverview  109 interactive · 37 T1 (34%) · 0 orphaned ✓
ProjectDeploys   133 interactive · 84 T1 (63%) · 0 orphaned ✓
ProjectEnvVars   110 interactive · 66 T1 (60%) · 0 orphaned ✓
```

## End-to-end results

Chrome for Testing 151 ships a native `navigator.modelContext` (`ModelContext`
with `registerTool`, `getTools`, `executeTool`, `ontoolchange`). **Netlify
registers no tools of its own** — `getTools()` returns `[]` without the
extension. With the extension loaded, the console logs
`[sightkick] loaded IR "netlify" (7 tools, injected, native modelContext)` and
`getTools()` lists only the tools whose `ensure_view` route matches the current
path (4 on `/deploys`, 3 on `/configuration/env`).

Verified via `window.__sightkick.call(name, args)` and via the native
`navigator.modelContext.executeTool(toolObject, '{"…"}')` — **the native call
takes the tool object from `getTools()` and a JSON *string* for input**; a plain
object fails with "Failed to parse input arguments".

| Tool | Result |
| --- | --- |
| `list_env_vars` | rows with key / scope_summary / secret / updated |
| `add_env_var` | creates the row; second call skips via `guard: present` |
| `delete_env_var` | removes it through ConfirmDialog; second call skips via `guard: absent` |
| `get_production_commit` | `fa49817` |
| `list_production_deploys` | 3 rows, predicate `rows:` query works |
| `list_deploys` | 6 rows with all fields |
| `search_deploys` | branch filter; verified with two queries in a row |

## Open items

1. **`search_deploys` — fixed, verified.** The box is a react-select
   combobox. The runtime `fill` (native value setter + input/change events)
   updates its state correctly, but the Search button never commits the typed
   text — choosing the "Search in branch “…”" suggestion (or Enter) does. The
   tool now clicks `ListHeader SearchSuggestion`, waits for the committed
   `SearchValue` chip, then waits for `DeployRow[branch="{{query}}"]` because
   committing empties the list before the filtered rows arrive. Searches match
   branch names only; a commit sha returns no rows. Verified live: one branch
   → 1 row, `main` → 3 rows, and a second search replaces the first filter.
2. **Compiler name collisions — fixed, verified.** Child names are unique per
   parent (every dropdown has a `Trigger`); the compiler used to resolve tool
   queries corpus-wide. PR #1 (`descendantOf` in
   `generator/internal/gen/compile.go`) resolves each part inside the previous
   part's subtree. The corpus uses the shared names again (`Trigger`,
   `MenuItem`, `Summary`, `SearchButton`, `CancelButton`) and add / delete pass
   live. The IR in `corpora/` is built from that patched compiler.
3. **`generator/go.mod` dead `replace`** — see below; untracked `go.work` is the
   workaround. Decide whether to commit it.
4. **Deploy status is not isolated.** `DeployRow.summary` ends with the status
   word; `title` is the same text without it. The status `<span>` has no hook
   and the offline matcher supports none of `:not(:has())`, `:nth-child`,
   `:first-child`, `:last-child`.
5. `edit_env_var` tool — corpus has `VariableForm` (`mode` = submit label) and
   `OptionsMenu › OptionsItem[label="Edit"]`; no tool written.
6. `generator/go.work` is untracked and machine-specific; snapshot captures
   under `.sightmap/snapshots/` are not tracked either — re-`capture` each view
   before running `coverage`.

## Two things that will bite you

### 1. `generator/go.mod` has a dead `replace`

It points at another machine's checkout:

```
replace github.com/sightmap/sightmap/go => /Users/joel/src/fs/subtext/sightmap/go
```

Nothing in `generator/` builds without a local override. There is an
**untracked** `generator/go.work`:

```
go 1.23
use .
replace github.com/sightmap/sightmap/go => /Users/chip/src/sightmap/sightmap/go
```

With it, `go run . build` works. `TestVerifyFieldEmpty` still fails because the
local sightmap `main` lacks the offline text-parity extraction; that path only
runs under `sightkick build --verify`.

### 2. The bundled sightmap skill was stale

The `subtext` plugin ships its own `sightmap-authoring` copy that documents
`transform:`, `text_only`, `inner_text`, and raw-CSS sub-selectors — all removed
in sightmap 0.29 (SEP-0010). `sightmap skills install --target ~/.claude/skills`
installs the CLI's own skills, which shadow it.

**Extraction is tree-closed.** Exactly four modes: `text`, `attr=NAME`,
`Child.prop`, `exists:Child`. Promote a sub-element to a child component to
read it. **Offline pseudo-class support is `:not()`, `:is()`, `:where()`,
`:has()` only** — `:not(:has(…))` and all structural pseudo-classes match live
but return 0 offline (`sel-probe` prints `⚠ offline/live divergence`).

## Netlify DOM gotchas

- **`header.app-header` is the mobile chrome** (`lg:hidden`). The visible
  desktop chrome is `.secondary-nav-container` (rail) and `.app-main` (page).
- **`#root` has three children**: `[role="status"]` announcer, `div.app`, and a
  `section[aria-label^="Notifications"]` toaster — siblings of the app.
- **`tw-*` classes are Tailwind utilities.** Do not select on them. Stable
  hooks: `data-component`, `data-testid`, hand-authored class names
  (`card-hero`, `card.table`, `table-header`, `table-body`, `dropdown`,
  `media-figure`, `sidebar`), `[title]`/`[aria-label]`/`[name]` on inputs and
  buttons, ids like `#section-environment-variables` and `#env-var-form`.
- **Downshift menus** (`div.dropdown`) open only on real pointer events;
  `element.click()` from `browser eval` does nothing. Drive them with
  `sightmap browser click 'Query'`. Menu items are in the DOM only while open.
- **Env-var rows are `<details>`.** The expanded section (values table,
  Options menu) is in the DOM while collapsed, but its buttons are *covered*
  until the row is open. Clicking `VarSummary` toggles, so a second click closes
  it — `delete_env_var` assumes a collapsed row.
- **The delete confirmation is a react-modal portal** outside `#root`
  (`.ReactModalPortal [role="dialog"]`, aria-label "Delete environment
  variable") — the `ConfirmDialog` global.
- **The add form is inline**, mounted above the table after
  `AddVariableMenu › AddVariableOption "Add a single variable"`. No toast on
  success; the new `VarRow` just appears.
- **A fresh tab navigates twice.** After `browser start`, the deploys page
  renders rows, then reloads once more a few seconds later. A tool call in
  that window fails with `fill: no element` or `unknown tool`. Wait for the
  URL to hold steady (about 6 s) before driving a freshly started session.
- **`snapshot --url` reloads the page** and dismisses any open menu or modal.
  Use bare `snapshot` to inspect transient state.
- Project rows lazy-load; trust the offline count from `sel-probe`.
- `SiteHero.heading` folds in the visibility badge; read `visibility` separately.

## Browser session

```sh
cd examples/netlify
sightmap browser start --detach \
  --url 'https://app.netlify.com/projects/westernwoodlands/deploys' \
  --extensions /Users/chip/src/sightmap/sightkick/packages/extension/dist
sightmap browser status
```

- CLI is **0.29.0**. `--detach` and `--extensions` both exist. After `pnpm
  build`, restart the session to reload the extension.
- Profile is `~/.sightmap/profiles/app.netlify.com`; the GitHub OAuth login
  persists, so login is one-time.
- **Pass `--tab <ID>` on every command** when more than one content tab is
  open; another session shares this profile and opens its own tabs.
- `sightmap explain` does not exist in 0.29. Use `gap`, `sel-probe -- 'sel'`,
  and `browser eval`.

## Build

```sh
cd generator && go run . build ../examples/netlify -o ../packages/extension/corpora/netlify.ir.json
cd ../packages/extension && pnpm build     # -> dist/, load unpacked or pass --extensions
```

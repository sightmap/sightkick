---
name: sightkick-authoring
description: Author a .sightkick/ tool layer — the sightkick manifest that turns a sightmap corpus into named WebMCP tools (atomic view-scoped actions + guidance journeys), then compile it to IR with `sightkick build`. Use when you have (or are building) a `.sightmap/` corpus and want to define the tools an agent can call on that app. Pairs with sightmap-authoring (writes the corpus this reads) and sightkick-debug (runs the compiled tools on a live page).
activation:
  - a `.sightmap/` corpus exists (or is being authored) and you want to define WebMCP tools over it
  - writing or editing a `.sightkick/` tool layer
  - a `sightkick build` reports unresolved component/property/view references
---

# sightkick-authoring: write a .sightkick/ tool layer

sightkick compiles two inputs into a self-contained **IR**: a `.sightmap/` corpus
(the app's component map — authored with the **`sightmap-authoring`** skill) and a
**`.sightkick/`** tool layer (authored here). `sightkick build` resolves every
reference against the corpus and reports unresolved ones with candidate lists.
Once it compiles, drive the tools on a live page with the **`sightkick-debug`**
skill.

The two sit side by side in the app directory:

```
<app>/
  .sightmap/     # the corpus (components + views), read by sightkick
  .sightkick/    # the tool layer, authored here
    *.yaml       # any number of files — all merged into one manifest
```

`.sightkick/` mirrors `.sightmap/`: every `*.yaml` file inside is **merged** into
one manifest (tools and journeys concatenated), with **no dependencies** between
files — the whole directory is the manifest. Split a large tool layer however
helps (e.g. one file per view, plus a `journeys.yaml`); keep a tiny one in a
single `tools.yaml`.

**Prerequisite:** the corpus must exist first — tools reference corpus
**component names** and **declared properties**, so if a name/property isn't in
the corpus, author it there (sightmap-authoring) before referencing it here.

## Mental model

- A **tool** is one atomic action at a single point in time — no navigation
  crossing mid-tool. It bundles ordered `steps` (fill/click/…) and/or a
  `returns` read, and yields a structured result.
- Multi-step flows are **not executed** by a runtime. A **journey** is a
  compile-time ordering over tools that compiles into **guidance breadcrumbs**
  ("after `add_task`, consider `list_tasks`") attached to each tool's result. The
  agent sequences; you hand it the map.
- Tools address elements by **component query** (component identity + extracted
  properties + descendant scope), never raw CSS.

## File shape

Each `.sightkick/*.yaml` file may set any of these top-level keys; they're merged
across the directory:

```yaml
version: 1                 # optional (defaults to 1); set it once
name: myapp                # optional — the IR name; defaults to the app dir's name
corpus: ../.sightmap       # optional — path to the corpus, relative to .sightkick/;
                           #   defaults to the sibling ../.sightmap
tools: [ ... ]             # the tools (at least one across the whole directory)
journeys: [ ... ]          # optional
```

The singular fields (`version`/`name`/`corpus`) are taken from whichever file
sets them (a conflict warns); `tools` and `journeys` accumulate. Most apps set
`version`/`name` in the first file and never touch `corpus`.

## Tools

```yaml
- name: add_task           # required, unique
  description: Add a task. # shown in getTools(); a result-shape hint is appended automatically
  mode: live               # live (default) drives the DOM; api is opt-in reads-only
  ensure_view: Home        # a corpus VIEW name — scopes component resolution to that
                           #   view (+ globals) AND view-scopes the tool at runtime
                           #   (it only registers on pages whose route matches)
  params:                  # become the tool's input schema; referenced as {{name}}
    - name: title
      type: string         # string | number | boolean | enum
      required: true
      description: The task title.
      # values: [A, B, C]  # required when type: enum
  guard:                   # optional idempotency guard — exactly one of present/absent
    present: { query: 'TaskItem[title="{{title}}"]' }   # SKIP the steps when this exists
    # absent: { query: ... }                            # SKIP when it does NOT exist
  steps: [ ... ]           # ordered actions (below)
  returns: { ... }         # the structured result (below)
```

A `live` tool needs **at least one `step` or a `returns`**. `ensure_view` is
optional but recommended — it both disambiguates component names and controls
which page the tool appears on.

### Steps (each is a single-key mapping: the op)

| Step | Body | Does |
|------|------|------|
| `fill` | `query`, `value` | Type `value` (supports `{{param}}`) into the matched input. |
| `click` | `query` | Click the matched element. |
| `wait_for` | `query`, `timeout_ms` (default 5000) | Wait until the query matches — use after a mutating action to confirm the visible result. |
| `navigate` | `view` | Client-navigate to a corpus **view** by name. |
| `goto` | `url` | Navigate to a URL template (`{{param}}` interpolated). |

Reads are **not** steps — declare them with `returns`. A tool that ends with a
mutation should `wait_for` its own visible feedback before returning.

### Returns (exactly one of `value` or `list`, or description-only)

```yaml
returns:
  description: One task title.        # optional; also folded into the tool description
  value:                             # a single scalar
    query: 'TaskItem[title="{{title}}"]'
    property: title                  # a DECLARED corpus property of the matched component
```

```yaml
returns:
  description: The current task rows.
  list:                              # an array of objects, one per match
    rows: TaskItem                   # a compquery; every match is a row
    fields:                          # outputName: <declared property of the row>
      title: title                   # scalar shorthand, or: title: { property: title }
      done: done
```

## Component queries (CSS-shaped, over corpus components)

- `Component` — by corpus component name.
- `Component[prop="v"]` — filter on an **extracted property** (not a raw DOM
  attribute). Operators: `=` exact, `^=` prefix, `*=` substring; append ` i` for
  case-insensitive (e.g. `[label="done" i]`).
- `A B` — descendant; the **last** component is the target, an ancestor predicate
  scopes it (`TaskItem[title="{{title}}"] TaskToggle`). There is **no `>`** child
  combinator — use whitespace.
- `Component#N` — 0-based occurrence when several match (weak fallback; prefer a
  distinguishing property).
- `{{param}}` interpolates a tool param into any query/value/url.

Every property you filter on or read must be **declared in the corpus**. If a
label is CSS-uppercased on screen but lowercase in the DOM text, match
case-insensitively with ` i`.

## Journeys → guidance (not execution)

```yaml
journeys:
  - name: add_and_review
    description: Add a task, then review the list.
    steps:
      - add_task                                   # bare tool name
      - tool: list_tasks                           # or a mapping with a reason
        reason: see the task you just added
```

Each journey needs **≥2 steps** to produce guidance edges. A tool shared across
journeys accumulates the union of its successors. Journeys never navigate or run
anything — they only shape the breadcrumbs in results.

## Worked example (a task-list app)

One file, `.sightkick/tools.yaml` (no `corpus:` — it defaults to the sibling
`../.sightmap`). Split it across several `.sightkick/*.yaml` files once it grows.

```yaml
version: 1
name: tasks
tools:
  - name: list_tasks
    description: List the current tasks and whether each is done.
    ensure_view: Home
    returns:
      description: The task rows (title + done-state).
      list:
        rows: TaskItem
        fields:
          title: title
          done: done

  - name: add_task
    description: Add a task to the list.
    ensure_view: Home
    params:
      - name: title
        type: string
        required: true
        description: The task title.
    steps:
      - fill:  { query: NewTaskInput, value: "{{title}}" }
      - click: { query: AddTaskButton }
      - wait_for: { query: 'TaskItem[title="{{title}}"]' }
    returns:
      description: The title of the new task.
      value: { query: 'TaskItem[title="{{title}}"]', property: title }

  - name: complete_task
    description: Mark a task done by clicking its toggle.
    ensure_view: Home
    params:
      - name: title
        type: string
        required: true
        description: The task to complete.
    steps:
      - click: { query: 'TaskItem[title="{{title}}"] TaskToggle' }
      - wait_for: { query: 'TaskItem[title="{{title}}"] TaskToggle[label="Undo"]' }
    returns:
      value: { query: 'TaskItem[title="{{title}}"]', property: done }

journeys:
  - name: add_and_review
    description: Add a task, then review the list.
    steps:
      - add_task
      - tool: list_tasks
        reason: confirm the task you just added
```

## Build & fix

```sh
sightkick build <APP_DIR> -o /tmp/x.ir.json  # <APP_DIR> holds .sightkick/ + .sightmap/
sightkick build <APP_DIR> --verify           # also checks returns extractors against captured
                                             #   view snapshots; warns on fields empty on every row
```

The compiler is your validator. Common diagnostics and fixes:

- **unresolved component / property / view** — the name isn't in the corpus (for
  that view). `build` prints candidates; fix the query, or declare the
  component/property in the corpus (sightmap-authoring). Remember property refs
  resolve against the **row/target** component.
- **`returns has both value and list`** — pick one.
- **`live tool needs at least one step or a returns`** — add a step or a read.
- **`unrecognized step op`** / **`not a single-key mapping`** — each step is one
  op key (`fill`/`click`/`wait_for`/`navigate`/`goto`) with its body.
- **`--verify` says a field resolves empty on every row** — the declared property
  extracts nothing on the live DOM; fix the property's extractor in the corpus.

For `--verify` you need a captured snapshot of the view (`sightmap capture` /
`snapshot` in the sightmap-browser skill). Once `build` is clean, run the tools
on a live page with the **sightkick-debug** skill.

# Scenario-driven testing with sightkick

This is a status report on a working thin slice, not a finished product. It says plainly, section
by section, what runs today versus what's designed but not built — that distinction matters more
here than usual, because the whole point is deciding whether to invest further.

Everything concrete in this document lives in [`examples/saucedemo/`](../examples/saucedemo/), a
sightkick corpus and tool layer targeting the real [saucedemo.com](https://www.saucedemo.com/) —
no app shipped in this repo, no toy DOM. Every claim below was checked against that corpus; where
something didn't work, that's said directly, including two real, permanent limitations found while
building it.

## 1. The problem BDD actually has

Gherkin itself has held up for over a decade: a `.feature` file reads like a spec because it *is*
one — `Given/When/Then` in plain language, reviewable by anyone who understands the product, not
just whoever wrote the automation.

What hasn't held up is the layer underneath it. Classic Cucumber (and `playwright-bdd`, which
inherits the same shape) matches each Gherkin line against a hand-written *step definition* — a
regex plus imperative browser code with selectors baked in. That glue is a second codebase, grown
one step definition at a time, that has to be kept in sync with the app by hand. It rots
independently of the spec: a selector changes, a flow gains a step, and the step definition is
wrong in a way the `.feature` file gives no hint of. The spec stays readable; the thing that
executes it becomes exactly the brittle, why-is-this-failing artifact BDD was meant to replace.

sightkick's bet is that this glue doesn't need to be hand-written at all, because most of it is
mechanical: given a semantic model of the app and a natural-language line, resolving "click the
add-to-cart button for this product" to a specific element is a lookup, not creative work. An
agent does that lookup once, at plan time; nothing about it needs to happen again at every test run.

## 2. Four layers

```
scenario  →  corpus         →  tools + journeys       →  plan
(intent)     (what the app     (what you can do, and      (a resolved,
              actually is)      the order people do it)    stored sequence)
```

- **Scenario** — a `.feature` file. Intent, in business language, unaware that sightkick exists.
- **Corpus** (`.sightmap/`) — the app's semantic model: views, components, properties, routes.
  Owned by `sightmap`, authored once, re-seeded when the UI changes underneath it.
- **Tools + journeys** (`.sightkick/`) — atomic actions over the corpus (`add_to_cart`,
  `place_order`), plus journeys: a compiled hint graph over which tools tend to follow which,
  attached to each tool's result as guidance. Authored once per app, not once per scenario.
- **Plan** — a scenario resolved against the tool layer: each Gherkin line mapped to a tool call
  and an expectation, hashed against both the scenario and the compiled manifest, checked in.

**The explicit Fitnesse comparison**, since it's a fair one: FitNesse has the same three-layer
shape — a wiki-authored spec, fixture code that executes it, a system under test. The difference
here isn't the shape, it's where the fixture layer comes from and how it's kept honest. A FitNesse
fixture is hand-written Java/C#/etc., verified only by running it. A sightkick tool layer is
declarative (component queries, not imperative code), continuously checked against a live semantic
model (`sightkick build` fails on any reference the corpus doesn't have), and — see §11 — the
*ordering* knowledge (which journey step follows which) can eventually come from observed usage
instead of an author's guess. FitNesse's fixtures don't have anything analogous to a corpus to be
checked against, and nothing analogous to a journey at all.

## 3. The corpus

A `.sightmap/` corpus declares **views** (a route + the components on it) and **components** (a
CSS selector, optionally scoped inside a parent, optionally carrying **properties** — named,
extracted values). From `examples/saucedemo/.sightmap/components.yaml`:

```yaml
- name: AddToCartButton
  selector: '.btn_inventory'
  description: >
    Toggles between "Add to cart" and "Remove" — same element, same class,
    only the label and the data-test id change. One component with a label
    property, not two.
  properties:
    - name: label
      extract: text
```

A **component query** like `InventoryItem[name*="Backpack"] AddToCartButton` addresses this the
way a person would describe it — by what it *is*, not by a raw selector. That's the reason it
survives a redesign that a raw-CSS Cucumber step definition wouldn't: if the button's class
changes, exactly one line in the corpus changes, and every tool and every plan that references
`AddToCartButton` by name keeps working. A test written against `.btn_inventory` directly would
need to be found and fixed at every call site.

## 4. Tools

A tool is one atomic action at a single point in time — no navigation crossing mid-tool. It
declares the view it expects to run from (`ensure_view`), an optional `guard` (for idempotency),
ordered `steps` (fill/click/wait_for), and an optional `returns` read. From
`examples/saucedemo/.sightkick/catalog.yaml`:

```yaml
- name: add_to_cart
  description: Add a product to the cart by name, from the Inventory list. Idempotent.
  ensure_view: Inventory
  params:
    - name: name
      type: string
      required: true
  guard:
    present:
      query: 'InventoryItem[name*="{{name}}"] AddToCartButton[label="Remove"]'
  steps:
    - click:
        query: 'InventoryItem[name*="{{name}}"] AddToCartButton'
    - wait_for:
        query: 'InventoryItem[name*="{{name}}"] AddToCartButton[label="Remove"]'
```

"Atomic and single-point-in-time" is the granularity that makes a tool reusable across scenarios
instead of scenario-specific: `add_to_cart` doesn't know or care whether it's being called from a
happy-path checkout, a multi-item cart test, or a manual exploration session. A step definition
scoped to one Cucumber scenario usually does know that, implicitly, in how it's written — which is
exactly what makes it hard to reuse.

## 5. Journeys — and what they are not

A journey is a hand-authored sequence of tool names with a reason for each step. The compiler
walks it pairwise and attaches "consider this next" guidance to each tool's own result:

```yaml
- name: purchase
  steps:
    - log_in
    - tool: open_item
      reason: browse to a product's detail page
    - tool: add_current_item_to_cart
      reason: cart the item shown
    # ...
```

Say plainly what this is not, because it's the most likely misreading: **a journey does not grant
a capability, and a planner does not need one to compose a valid tool sequence.** The compiler's
own logic is a dumb adjacency walk over hand-authored pairs — no inference, no graph search. It
exists to help a *live*, turn-by-turn agent that only sees one tool's result at a time decide what
to try next without re-reading the whole manifest. A planner that reads the full manifest up front
(exactly what scenario→plan resolution does, §6) can see every tool's own description and compose
any valid sequence itself, journey or no journey. Skipping journey authoring does not shrink what's
plannable; it only removes a convenience for a different consumer.

## 6. Scenario → plan

Resolving a scenario is: read every tool's `description`/`params`/`ensure_view`/`returns`, then for
each Gherkin line pick the tool, the params, and (where the line asserts something) the
expectation. Concretely, `features/checkout.feature`'s

```gherkin
When I open "Sauce Labs Backpack"
```

resolves to

```json
{
  "gherkin": "When I open \"Sauce Labs Backpack\"",
  "tool": "open_item",
  "params": { "name": "Sauce Labs Backpack" },
  "expect": { "ok": true }
}
```

A line that can't be resolved is a gap: either no tool exists for it (author one, following §4),
or the request supplies a value no tool declares a param for. This is not a hypothetical — it's
exactly how `examples/saucedemo`'s tool layer grew. Live driving surfaced that there was no way
back from Cart or an item's detail page to the catalog at all (no tool's steps ever navigated
there); `go_to_menu`-equivalent tools (`back_to_products`, `continue_shopping`) got added *because*
a scenario needed the gap filled, not speculatively. See `features/multi-item.feature` and
`plans/multi-item.plan.json` for the resulting scenario.

## 7. The output is runtime-agnostic

This is the load-bearing technical claim, and it's a description of what already exists, not a
roadmap item. `sightkick build` compiles the corpus + tool layer into a self-contained IR with
every reference already resolved — real CSS locators, real extractors, real predicates, no
sightmap concepts left for a consumer to know about. Full compiled `add_to_cart`, from
`examples/saucedemo`:

```json
{
  "name": "add_to_cart",
  "guard": {
    "kind": "present",
    "query": { "parts": [
      { "locators": [".inventory_item"],
        "preds": [{ "property": "name",
          "extractor": { "kind": "text", "within": "[data-test$=\"-title-link\"]" },
          "op": "*=", "value": "{{name}}" }] },
      { "locators": [".inventory_item .btn_inventory"],
        "preds": [{ "property": "label", "extractor": { "kind": "text" },
          "op": "=", "value": "Remove" }] }
    ] }
  },
  "steps": [
    { "op": "click", "query": { "parts": [ /* same two-part query */ ] } },
    { "op": "waitFor", "query": { /* … */ }, "timeoutMs": 5000 }
  ]
}
```

Two consumers of exactly this artifact exist today: `document.modelContext` (WebMCP, when the
runtime is installed on the page) and `sightkick call --via cli` (shells to `sightmap browser`
commands — no runtime install needed, and it reaches portal-rendered elements a runtime's
synthetic events can't). A Playwright emitter would be a third, straightforward but **not
built** — the mapping is direct enough to show honestly:

| IR piece | Playwright |
|---|---|
| a `locators` array (comma-joined CSS alternatives) | `page.locator(sel)` |
| a `preds` entry with an `attr` extractor | folds into the same CSS selector: `[data-test="remove-{value}"]` |
| a `preds` entry with a `text` extractor | `.filter({ hasText: value })` — can't fold into CSS |
| `guard.kind: "present"` | `if (await locator.count()) return;` before running `steps` |
| `op: "click"` / `"fill"` | `.click()` / `.fill(value)` |
| `op: "waitFor"` on a `view` | `page.waitForURL(routePattern)` |
| `op: "waitFor"` on a `query` | `.waitFor()` on the same resolved locator |

Nothing here is exotic Playwright — the emitter is a straightforward code-gen pass over the IR.
It's listed as future work (§13) because writing and testing it is real effort, not because the
mapping is unclear.

## 8. The meaty details

**Auth.** Two real shapes, both confirmed live this pass:
- *Simple*: `examples/saucedemo`'s `log_in(username, password)` is an ordinary tool — fill two
  fields, click submit, wait for the destination view. No special handling needed.
- *Hard*: Fullstory's own internal verify skill needs Google OAuth followed by an org chooser with
  no addressable corpus component at all. Both live **outside** the manifest by necessity — OAuth
  because there is no programmatic way through a real third-party consent screen, the org chooser
  because it has no stable selectors to model. The honest pattern: document the bootstrap as a
  fixed, non-tool sequence a session runs once, and let every tool assume it already happened.

**Site graph.** Views + routes + `ensure_view` *are* the graph. A tool's `ensure_view` states its
precondition; a `wait_for: {view: X}` on its last step states its postcondition. Two findings from
building `examples/saucedemo` worth generalizing:
- **A missing back-edge is a missing capability, not a planning problem.** Before
  `back_to_products`/`continue_shopping` existed, there was no way to express "add a second item"
  at all — not because the planner couldn't figure it out, because no tool's steps ever went there.
- **Leaving a wizard can silently reset it.** saucedemo's checkout always restarts at step one if
  you navigate away and back, even though the cart itself is untouched. `add_to_cart`'s own
  description says so; a plan that doesn't know this re-fills from scratch instead of assuming an
  earlier pass survived.

**Fixtures.** saucedemo's own published test users are the whole mechanism:
`standard_user`/`locked_out_user`/`problem_user`/`performance_glitch_user`, one shared password.
`locked_out_user` gives `features/locked-out-user.feature` a real, deterministic error branch with
zero fixture infrastructure to build. Most real apps won't have this for free — the fallback is
whatever your own app already uses (seeded test accounts, a `?scenario=` flag, staging-only
toggles); sightkick doesn't need its own fixture system, it just needs the resulting *state* to be
addressable as a corpus component (an error banner, a confirmation heading) the way any other UI
state is.

## 9. Stored plans run without an agent

A plan (§6) is JSON, checked in, one file per scenario — see `examples/saucedemo/plans/*.plan.json`.
Two fields make it self-checking:

- **`scenario.hash`** — a hash of the `.feature` file's own text. Changes when the spec changes.
- **`irHash`** — a hash of the compiled manifest. Changes when the corpus or tool layer changes.

`scripts/run-plan.mjs` recomputes both on every run and refuses to proceed on a mismatch (`--stale-
ok` overrides). This is deliberately the *only* thing that can fail a run for a reason other than a
real expectation mismatch — confirmed both directions: editing a tool's `required:` flag and
re-running immediately reports the drift and refuses; reverting the edit makes it pass again; a
comment-only edit to the same file changes nothing (the hash is over the *compiled* IR, not the raw
YAML, so it's insensitive to changes that don't affect behavior).

Each step in a plan carries the Gherkin line it came from, so a failure names the business-language
assertion that broke, not a stack trace three layers removed from it:

```json
{
  "gherkin": "Then the order is confirmed",
  "tool": "read_confirmation",
  "expect": { "value": { "contains": "Thank you for your order" } }
}
```

The cost story this enables: an agent reads the manifest and produces a plan once. Every run after
that is `node scripts/run-plan.mjs plan.json` — no LLM call, no token cost, until a hash moves and
it's time to re-plan.

## 10. Keeping it true

What catches drift automatically, today:
- **`sightkick build`** fails on any unresolved component/property/view reference — a tool can't
  silently point at something the corpus no longer has.
- **`sightkick build --verify`** checks every tool's `returns` extractor against captured
  snapshots and warns when a field resolves empty on every one. This is how the two real
  limitations in §11 were *found*, not theorized.
- **Plan hashes** (§9) catch the spec or the compiled manifest changing under a stored plan.

What can still break silently: a corpus that's stale relative to the *live* site (re-seeding is a
manual trigger, not automatic), and — see the two findings below — a property that resolves to
something other than empty but still isn't what a human would expect.

**Two real, permanent limitations, found live while building `examples/saucedemo`, not
hypothesized:**

1. `extract: text` only resolves on elements with their own accessibility-tree role (a link,
   button, heading, combobox — anything AX exposes). A plain `<div>`/`<span>` holding exactly the
   text a property wants extracts **empty**, silently, even though the identical selector resolves
   correctly for a click. Where a semantic wrapper exists nearby, retargeting the property there is
   the fix (`ItemName` moved to the product's wrapping `<a>` rather than the inner `<div>`). Where
   none exists — a price, an order total — there is no fix today; see the `memory:` notes on
   `ItemPrice`/`SubtotalLabel`/`TaxLabel`/`TotalLabel`.
2. Native `<select>` dropdowns aren't automatable via click/fill/keypress at all — confirmed live
   on the inventory sort control: a real click and a real ArrowDown/Enter keypress change neither
   `document.activeElement` nor the select's value. This is a known, general gap in CDP input-event
   automation for native select popups; Playwright's answer is a dedicated `selectOption()` API
   that sets the value directly rather than simulating input, which sightmap doesn't implement.

Both are stated in the example's own `README.md` and in `memory:` notes on the affected corpus
components — the discipline is: a limitation lives next to the thing it limits, not only in this
document.

## 11. Journeys from real usage — direction, not shipped

Nothing below this line runs today. The idea: Subtext already records real user sessions against
an app with a sightmap corpus attached, which means a session is, in principle, a sequence of
named components interacted with in a real order — the same shape a hand-authored `journeys:` list
already has. If observed trajectories could be mined into journey entries automatically, the
guidance an agent gets (§5) would track how the app is actually being used rather than an author's
best guess at launch time, and drift as real usage drifts. This does not change what's plannable
(§5's point stands — journeys are guidance, not capability) but it would change whether the
guidance stays accurate for free. This needs a defined pipeline (which session events count,
how conflicting orderings across sessions get reconciled, how confidence is represented) that does
not exist yet.

## 12. Getting started

Roughly 30 minutes for a first tool, on an app you already have a running instance of:

1. Point `sightmap` at the app, seed a corpus for one view (`sightmap-authoring` skill covers this
   in depth — snapshot, check coverage, iterate to 0 orphaned).
2. Write one tool in `.sightkick/tools.yaml` for the simplest real action on that view.
3. `sightkick build .` — fix whatever it reports unresolved.
4. Write one `.feature` file with a single scenario using that tool.
5. Hand-resolve it into a one-step plan (or have an agent do it), `--stamp` it.
6. `node scripts/run-plan.mjs plan.json` — green, with no agent in the loop.

Everything past that is repetition: more views, more tools, more scenarios, following §3–§6.

## 13. Status

| Piece | Status |
|---|---|
| `.sightmap/` corpus, `.sightkick/` tool layer, multi-file merge | Built, shipped |
| `sightkick build`, `--verify` | Built, shipped |
| `sightkick call --via cli` / `--via webmcp` | Built, shipped |
| Journeys → guidance breadcrumbs | Built, shipped |
| `examples/saucedemo` — corpus, tools, journeys, 4 `.feature` files | Built, this pass |
| Stored plan format (`*.plan.json`), scenario/IR hashing | Built, this pass — example-local, not yet a sightkick spec |
| `scripts/run-plan.mjs` — replay without an agent, drift detection | Built, this pass — confirmed both directions of drift detection live |
| Live click execution for JS-`onClick`-driven buttons via `--via cli` | **Blocked**, this environment — see §10 and the example's own README; native-behavior clicks (form submit, focus) work, JS-handler clicks do not |
| Scenario → plan resolution | Demonstrated manually (§6); no automated resolver yet |
| Gap report (unresolvable scenario line → suggested new tool) | Not built |
| Playwright emitter | Not built; mapping specified in §7 |
| Subtext session → journey mining | Not built; direction only, §11 |

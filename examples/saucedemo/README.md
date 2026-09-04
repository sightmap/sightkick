# saucedemo

A sightkick example targeting a real, external site — [saucedemo.com](https://www.saucedemo.com/),
the standard Selenium/Playwright demo shop — rather than an app shipped in this repo. There is no
served app here: `.sightmap/` and `.sightkick/` are the entire example, seeded from the live site.

## Why this site

- **A login wall and a real multi-view graph**: Login → Inventory → Item → Cart → CheckoutInfo →
  CheckoutOverview → Complete.
- **Published test users are literal fixtures**: `standard_user` and `locked_out_user` (password
  `secret_sauce` for both) give a real happy path and a real, deterministic error branch with no
  fixture mechanism to build.
- **It resets itself** (`Reset App State` in the sidebar) — safe to drive repeatedly.

## Layout

```
.sightmap/                the corpus — seeded from the live site, not hand-written
.sightkick/
  auth.yaml                log_in, log_out, read_login_error
  catalog.yaml              open_item, add_to_cart, add_current_item_to_cart, remove_from_cart
  cart.yaml                 go_to_cart, go_to_checkout, continue_shopping, back_to_products, read_cart
  checkout.yaml             fill_checkout_info, read_checkout_error, place_order, read_confirmation
  journeys.yaml             purchase, locked_out
features/                   Gherkin scenarios — the scenario database
plans/                      compiled, checked-in plans (see docs/scenario-testing.md)
```

## Try it

```sh
cd generator
go run . build ../examples/saucedemo          # compile the manifest against the corpus
go run . build --verify ../examples/saucedemo # + check returns against captured snapshots
```

`--verify` needs captured snapshots, which aren't checked in (`.sightmap/snapshots/` is gitignored —
they're a local cache of a live site's DOM, not source). Regenerate them by driving the corpus's
views live once with `sightmap capture`.

Driving it live needs a running `sightmap browser` session pointed at `https://www.saucedemo.com/`
and `--via cli` (the default `--via webmcp` needs a sightmap release this environment doesn't have
— see the repo README's Runtimes section):

```sh
sightmap browser start --detach --headless --url https://www.saucedemo.com/
go run . call ../examples/saucedemo log_in --param username=standard_user --param password=secret_sauce --via cli
```

**Known limitation, confirmed live, not yet fixed:** clicks that rely on a *native* browser
behavior work over `--via cli` in this environment; clicks that only work because a JS `onClick`
handler fires do not. `log_in` (fills two fields, clicks a `<input type="submit">` inside a
`<form>`) runs correctly end to end — form submission is a native behavior triggered by any real
click, CDP-dispatched or not. `open_item`/`add_to_cart` (click a plain `<button type="button">`
whose entire effect is a React `onClick`) do not: the click lands on the exact right element
(confirmed via `elementFromPoint`) but the handler never fires. Most likely cause: this app's
click handling is pointer-event-based, and sightmap's `ClickAt` only dispatches legacy
`Input.dispatchMouseEvent`, not pointer events — a known category of CDP/React interaction gap,
not a targeting bug. The corpus and every tool's query resolution were verified independently
(every predicate confirmed via `elementFromPoint`/`sel-probe` to resolve to the exact right
element); what's unverified end to end is click *execution* for JS-handler-driven buttons against
this specific site, in this environment. Non-headless has a second, unrelated issue: on a
HiDPI/Retina display, coordinate-based clicks land at roughly half the intended position (a
`devicePixelRatio` mismatch) — headless avoids that one, but not the pointer-event gap above.

## Two real gaps found authoring this corpus (not bugs in this example — sightmap-level limitations)

- **`extract: text` only resolves on elements with their own accessibility-tree role** (link,
  button, heading, combobox, …). A plain `<div>`/`<span>` with no ARIA role — even one holding
  exactly the text a property wants — always extracts empty, confirmed live and reproducibly.
  Where a semantic wrapper exists nearby (a product title's wrapping `<a>`), retargeting the
  property there is the fix — see `ItemName`'s corpus comment. Where none exists (the price, the
  order subtotal/tax/total — plain divs all the way up), there is no fix: those values are simply
  not readable through a sightkick tool today. See the `memory:` notes on `ItemPrice`,
  `SubtotalLabel`, and `Quantity`.
- **Native `<select>` dropdowns aren't automatable via click/fill/keypress.** Confirmed live on the
  inventory sort control: a real click and a real ArrowDown/Enter keypress change neither
  `document.activeElement` nor the select's value. See `SortSelect`'s memory note.

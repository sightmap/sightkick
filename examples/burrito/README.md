# burrito example

sightkick tools for the **burrito** Potemkin app — a real, third-party
single-page ordering app that has **no WebMCP of its own**. This example shows
sightkick acting as an external consumer of another project's sightmap corpus:
the manifest here references the app's `.sightmap/` in a sibling repo and adds a
tool layer on top of it (no changes to the app or its corpus).

## Layout

- `webmcp.tools.yaml` — the tool + journey layer. `corpus:` points at
  `../../../potemkin/village/burrito/.sightmap`, i.e. the `potemkin` repo checked
  out as a sibling of `sightkick` under the same parent directory.

## Build

```sh
cd generator
go run . build ../examples/burrito -o /tmp/burrito.ir.json
```

Produces 7 view-scoped tools over the app's five views (Menu `/`, Item detail
`/items/**`, Cart `/cart`, Checkout `/checkout`, Confirmation `/confirmation`),
with the ordering journey compiled into guidance breadcrumbs
(`list_menu → open_item → choose_option → add_to_cart → view_cart →
go_to_checkout → place_order`).

## Run it against the live app

1. Start the app: `npm run dev -- burrito` in the `potemkin` repo (serves
   `http://localhost:5173/`).
2. Follow the **`sightkick-debug`** skill
   ([`../../skills/sightkick-debug/SKILL.md`](../../skills/sightkick-debug/SKILL.md)):
   launch a `sightmap browser` session against `http://localhost:5173/`, inject
   the runtime bundle + the IR built above, and drive the ordering flow either
   agent-driven or through the vendored WebMCP inspector.

## Known authoring limitations surfaced here

- **Parent-scoped selection.** A `where` clause filters the *target* component by
  its own property, so it can't express "the OptionButton labelled X *within* the
  OptionGroup named Y". Option labels that repeat across groups (e.g. `none` in
  Rice/Beans/Salsa/Extras) are therefore ambiguous, and the checkout delivery
  fields (which have no distinctive placeholder) can't be filled individually.
  The happy path sidesteps this — distinctive option labels work, and checkout is
  pre-filled — but scoping a selection to an ancestor component is a real
  follow-up (see the composition/nesting work).

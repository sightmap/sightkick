# jetblue example

sightkick tools for **jetblue.com** — a real, live third-party airline site with
**no WebMCP of its own**. This example shows sightkick layering a tool surface
over a hand-authored sightmap corpus of a production site the project doesn't
control.

Unlike the `burrito` example (which references an app's corpus in a sibling
repo), this example is **self-contained**: it carries its own copy of the corpus
under `.sightmap/`, so `corpus: ./.sightmap` resolves with no external checkout.

## Layout

- `webmcp.tools.yaml` — the tool + journey layer. `corpus: ./.sightmap`.
- `.sightmap/` — the authored corpus: `config.yaml`, global `components.yaml`,
  and one file per view under `views/`.

## The site: two apps under one domain

jetblue.com is really two apps sharing an origin, and the corpus models both:

- a **Next.js marketing site** at `/`, whose stable selector hook is the
  `data-fs-element` attribute (react-aria `_R_…` ids and Tailwind classes are
  not stable and are avoided as anchors);
- a separate **Angular booking SPA** at `/booking/**` (`cb-*` / `jb-*` custom
  elements) with its own header/footer chrome.

Each app's global chrome legitimately 0-matches the other app's views — an
expected consequence of the two-apps-under-one-domain layout, documented in the
corpus `memory:` notes.

## Build

```sh
cd generator
go run . build ../examples/jetblue -o /tmp/jetblue.ir.json
```

Produces 9 view-scoped tools across the four modeled views — Home (`/`),
SelectFlights (`/booking/flights`), Cart (`/booking/cart`), and Checkout
(`/booking/checkout`) — with the booking journey compiled into guidance
breadcrumbs (`search_flights → list_flights → select_fare → list_cart →
continue_to_checkout → set_passenger → continue_checkout_step`).

## Known authoring limitations surfaced here

- **Trusted-input walls.** Two of the Home search controls only respond to
  user-activation (trusted) input: the origin/destination airport combobox opens
  its suggestion listbox only on trusted keystrokes, and the fare calendar opens
  only on a trusted click. Page-JS actuation can't cross either wall, so the
  `search_flights` tool sidesteps the visible form entirely and **deep-links** to
  the results page with the query in URL params — robust and trusted-input-free.
  The corpus `memory:` notes on `OriginDestination`/`DateField` document the wall
  in detail for anyone driving the form directly (e.g. via a host-trusted CDP
  fill/click).

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

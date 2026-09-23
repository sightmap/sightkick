# Typed Playwright modules

`sightkick build <app-dir> --target playwright -o app.mjs` emits a self-contained
ES module and `app.d.mts`. The `.d.mts` extension makes declarations resolve for
`.mjs` imports under TypeScript NodeNext. The default JSON build remains unchanged,
including stored-plan hashes. The caller installs `playwright` and owns its Page:

```ts
import { chromium } from "playwright";
import { createSightkick } from "./app.mjs";

const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  await page.goto("http://localhost:3000");
  const { tools, views } = createSightkick(page, { timeoutMs: 5000 });
  const result = await tools.search({ query: "ATL to LHR" });
  if (!result.ok) throw new Error(result.message);
  await views.Results.waitFor();
  console.log((await tools.list_results()).items);
} finally {
  await browser.close();
}
```

The exported names and parameter/result types come from the compiled IR. Bracket
access works for names that are not JavaScript identifiers. Tool calls return the
same `ok`, `value`/`items`, `skipped`, and `guidance` envelope as WebMCP, including
`ok: false` on execution or parameter errors. Check `ok` before proceeding.
Inputs are validated at runtime as well as through TypeScript. Unknown parameters,
missing required values, wrong types, and invalid enum values fail before actions.
Only live tools are supported; emission rejects other modes explicitly.

## Execution contract

The executor uses the existing runtime's DOM query resolver and extractors, bundled
inside the emitted module. That preserves accessible-name text, descendant queries,
case-insensitive predicates, selector-alternative order, occurrence indices, open
shadow roots, and the runtime's documented extractor approximations. Data is
serialized separately from executable expressions, including prototype-like names.
No selectors or extraction rules are independently translated to Playwright filters.

Clicks and keyboard input use trusted Playwright events. Fill clears the target and
types characters, opening role=combobox controls with ArrowDown as the WebMCP runtime
does. Actions prefer the first visible match, then the first match. Playwright waits
for actionability; disabled or covered controls can time out rather than accepting a
synthetic event. Targets are resolved once per action; a detached target fails rather
than repeating a potentially consequential action. Concurrent calls on the same Page
are not serialized: await each action in your program.

Guards and query waits mean DOM presence, including hidden elements. `ensure_view`
and legacy `navigate` remain advisory. A view wait checks only the pathname using
the shared route matcher; it does not guarantee hydration. Wait for destination
content in the next tool when needed. `goto` is an actual awaited navigation, and
bindings survive new documents because each query evaluates its helper afresh.
There is no persistent runtime injection or page-global helper. Post-navigation
returns read the destination document; unlike a page-installed runtime the host
executor survives unload. Timeout options must be finite and positive. A compiled
step timeout takes precedence over the binding's default; there is no whole-program
timeout or cancellation API.

The module exposes tools and view waits. Named component reads, a `sightkick run`
command, and an MCP execution server are not supported. Run generated modules in
your own JavaScript process; the emitter does not provide an execution sandbox.

## Development

```sh
pnpm install
pnpm -r build
pnpm --filter @sightkick/playwright exec playwright install chromium
pnpm -r typecheck
pnpm -r test
cd generator
go generate ./playwrightbundle/...
go test ./...
```

`packages/playwright` is a private build package. Its bundled executor is committed
under `generator/playwrightbundle` for standalone native CLI distribution. CI
rebuilds and checks that copy; edit the TypeScript source, not the embedded file.

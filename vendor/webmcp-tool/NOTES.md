# WebMCP — Model Context Tool Inspector (vendored)

A local copy of the **WebMCP – Model Context Tool Inspector** Chrome extension
([upstream](https://github.com/beaufortfrancois/model-context-tool-inspector),
Apache-2.0; also on the
[Chrome Web Store](https://chromewebstore.google.com/detail/webmcp-model-context-tool/gbpdfapgefenggkahomfgkhfehlcenpd)),
so it can be loaded into our Chrome-for-Testing instance alongside the sightmap
overlay to drive injected sightkick tools with a real WebMCP client.

`sightkick browser <corpus> --webmcp` **auto-loads this inspector** (it's embedded
in the CLI via `generator/webmcpinspector/`), so day to day you don't touch these
files. This directory is the canonical vendored copy that the embed is generated
from — plus the notes below on how it's produced and the Chrome gotchas.

## How it's vendored (`unpacked/`)

`unpacked/` is generated, not hand-copied — **don't edit it by hand.** Refresh it
from upstream with:

```sh
npm run vendor-inspector            # pins the SHA in the script; or:
node scripts/vendor-webmcp-inspector.mjs <ref>
cd generator && go generate ./webmcpinspector/...   # sync the embedded copy
```

The script (`scripts/vendor-webmcp-inspector.mjs`) pins an upstream commit,
downloads that tarball, and runs the upstream build — `npm install`, whose
`postinstall` esbuild-bundles `@google/genai` into `js-genai.js` and then deletes
`node_modules`, leaving a directly-loadable unpacked extension. The repo does
**not** commit that 640 KB bundle, which is why a plain checkout isn't loadable and
we build it here. Provenance (exact ref, extension version, file list, content
hash) is recorded in `.vendored.json`.

### Manifest edits (applied by the vendor script)

The script strips three keys from `manifest.json` so it loads as a plain local
unpacked extension on any Chrome-for-Testing:
- `key` — pins the store id + makes Chrome treat the copy as store-managed.
- `update_url` — points at the Web Store; not wanted for a local copy.
- `minimum_chrome_version` — would block loading on an older CfT; stripped so the
  copy loads regardless of which CfT `sightmap browser` selects.

(The upstream **source** manifest already omits `key`; it carries `update_url` +
`minimum_chrome_version`, both removed here. Earlier this copy was lifted straight
from a Web Store CRX install, which is why these notes exist — sourcing from the
repo is cleaner: no `_metadata/` signature dir to strip, and Apache-2.0 makes
redistribution inside our binary unambiguous.)

## The two gotchas that made this actually work

1. **CfT version / API surface.** The WebMCP script API moved from
   `navigator.modelContext` (CfT **149**) to `document.modelContext` (Chrome
   **150+**). This extension calls `document.modelContext` exclusively, so it
   errors on 149 regardless of flags. `sightmap browser install` pulls a modern
   CfT (Stable ≥150), which `browser start` auto-selects (newest wins), and there
   `document.modelContext` is the native surface.

2. **The flag must be forced on the command line.** Enabling *WebMCP for testing*
   in `chrome://flags` (persisted in the profile's `Local State`
   `enabled_labs_experiments`) is **not applied** under the automation-launched
   profile — the flag is a Blink runtime feature that must be passed explicitly:
   `--enable-blink-features=ModelContext,ModelContextTesting`
   (plus `--enable-features=DevToolsWebMCPSupport` for the DevTools panel).
   `--webmcp` passes both; the feature names come from the CfT framework
   (`blink/renderer/core/script_tools/model_context.cc`).

Verified: on a modern CfT with those flags, `document.modelContext` is native, and
after sightkick injects the runtime bundle + IR, `document.modelContext.getTools()`
on the target site returns sightkick's tools — so the inspector reads them instead
of throwing.

## Loading it by hand

`--webmcp` is the easy path. To load it manually (a non-sightkick session, or for
control), run from the **sightkick repo root**:

```sh
sightmap browser start --detach \
  --extensions ~/.sightmap/extension,"$PWD/vendor/webmcp-tool/unpacked" \
  --chrome-flag=--enable-blink-features=ModelContext,ModelContextTesting \
  --chrome-flag=--enable-features=DevToolsWebMCPSupport
```

Two extensions load together: the built-in **sightmap overlay**
(`~/.sightmap/extension`) and this **WebMCP inspector**
(`vendor/webmcp-tool/unpacked`). The sightkick tools themselves are not an
extension — they're injected into the page (see the `sightkick-debug` skill).

Two constraints, both verified against the `sightmap browser` source:
- **All entries must be absolute** and comma-separated — the CLI runs
  `filepath.Abs` over the *whole* `--extensions` string, so relative paths break.
  `~` and `$PWD` are shell-expanded to absolute before the CLI sees them.
- **The overlay must be listed explicitly.** Passing `--extensions` at all
  *replaces* the overlay that `sightmap browser` otherwise auto-extracts to
  `~/.sightmap/extension/` — so leaving it out drops the overlay entirely.
  (`--webmcp` re-adds it for you when it exists.)

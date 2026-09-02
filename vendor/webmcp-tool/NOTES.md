# WebMCP - Model Context Tool Inspector (vendored, unpacked)

A local **unpacked** copy of the Chrome Web Store extension
[WebMCP – Model Context Tool](https://chromewebstore.google.com/detail/webmcp-model-context-tool/gbpdfapgefenggkahomfgkhfehlcenpd)
(store id `gbpdfapgefenggkahomfgkhfehlcenpd`), so it can be loaded into our
Chrome-for-Testing instance alongside the sightmap overlay + the sightkick
extension. **Temporary / playground vendoring** — revisit the long-term story.

## Why it's copied, not downloaded

- The store UI refuses to install to Chrome for Testing ("Switch to Chrome…").
- The direct CRX endpoint (`clients2.google.com/service/update2/crx`) returns
  **204 No Content** for our CfT `prodversion` — because the extension declares
  `minimum_chrome_version: 150.0.7861.0` and our CfT is **149.0.7827.54**, so no
  compatible CRX is served.
- It *was* already installed in real Google Chrome, so `unpacked/` is copied from
  `~/Library/Application Support/Google/Chrome/Default/Extensions/gbpdfapgefenggkahomfgkhfehlcenpd/1.9.13_0/`
  (with `_metadata/` dropped — the store-signature dir trips integrity checks on
  unpacked loads).

## Manifest edits (in `unpacked/manifest.json`)

Removed three fields so it loads as a plain local unpacked extension:
- `key` — pinned the store id + made Chrome treat the unpacked copy as store-managed.
- `update_url` — pointed at the Web Store; not wanted for a local copy.
- `minimum_chrome_version` (`150.0.7861.0`) — now redundant (we run CfT ≥150),
  but harmless to leave stripped.

## The two gotchas that made this actually work

1. **CfT version / API surface.** The WebMCP script API moved from
   `navigator.modelContext` (CfT **149**) to `document.modelContext` (Chrome
   **150+**). This extension calls `document.modelContext` exclusively, so it
   errors on 149 regardless of flags. Fix: `sightmap browser install` pulled CfT
   **152.0.7977.75** (Stable), which `browser start` auto-selects (newest wins),
   and on 152 `document.modelContext` is the native surface.

2. **The flag must be forced on the command line.** Enabling *WebMCP for testing*
   in `chrome://flags` (persisted in the profile's `Local State`
   `enabled_labs_experiments`) is **not applied** under the automation-launched
   profile — the flag is a Blink runtime feature that must be passed explicitly:
   `--enable-blink-features=ModelContext,ModelContextTesting`
   (plus `--enable-features=DevToolsWebMCPSupport` for the DevTools panel). Found
   the feature names by `strings`-grepping the CfT framework
   (`blink/renderer/core/script_tools/model_context.cc`).

Verified: on 152 with those flags, `document.modelContext` is native, and
`document.modelContext.getTools()` on jetblue.com returns sightkick's tools —
so the inspector reads them instead of throwing.

## Load command (canonical)

```sh
sightmap browser start --detach \
  --extensions /Users/joel/.sightmap/extension,/Users/joel/src/fs/subtext/sightkick/packages/extension/dist,/Users/joel/src/fs/subtext/sightkick/vendor/webmcp-tool/unpacked \
  --chrome-flag=--enable-blink-features=ModelContext,ModelContextTesting \
  --chrome-flag=--enable-features=DevToolsWebMCPSupport
```
(All three extension paths absolute + comma-separated. The built-in overlay path
must be listed explicitly — `--extensions` currently *replaces* the auto-loaded
overlay rather than appending; see sightmap yak `sightmap-d0cb`.)

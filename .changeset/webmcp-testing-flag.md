---
"@sightmap/sightkick": patch
---

Fix `sightkick browser --webmcp` naming Chrome flags that aren't real. It forced
`--enable-blink-features=ModelContext,ModelContextTesting` and
`--enable-features=DevToolsWebMCPSupport`; neither is a Chromium feature, so Chrome
silently ignores them. The path still reached native WebMCP in practice because the
managed Chrome for Testing enables the feature by default — which is exactly why the
bogus flags went unnoticed. `--webmcp` now passes the real switch,
`--enable-features=WebMCPTesting` (the command-line form of
`chrome://flags/#enable-webmcp-testing`): correct on its own, and required if the
session runs against Google Chrome stable rather than the managed CfT. The
`sightkick-debug` skill and the vendored inspector `NOTES.md` are corrected to
match. See sightmap/sightmap#413.

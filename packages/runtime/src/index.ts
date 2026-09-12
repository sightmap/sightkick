import { boot } from "./boot.js";
import { installIrChannel } from "./channel.js";
import { whenBootable } from "./autoboot.js";

export { boot, detectMode, type SightkickGlobal, type Mode, type BootOptions } from "./boot.js";
export { runTool, routeMatches, type ToolResult, type RunOptions } from "./executor.js";
export { createClient, type SightkickClient } from "./client.js";
export { ensureModelContext, isPolyfilled, type ModelContext, type RegisteredTool } from "./webmcp.js";
export { installIrChannel } from "./channel.js";
export { whenBootable, type ReadyOptions } from "./autoboot.js";
export * from "./ir.js";

/**
 * Entry point for the bundled artifact. Installs window.__sightkick and loads an
 * IR from window.__sightkick_ir if the host set one before the bundle ran. A
 * host that loads the IR later can call window.__sightkick.load(ir).
 *
 * Boot is gated on readiness (sites-67f3): when this bundle is (re-)injected at
 * document_start on a fresh navigation, the SPA and the WebMCP native surface
 * don't exist yet, so we wait until the page can host tools before booting.
 * whenBootable runs synchronously when the document is already loaded (a live
 * inject or a direct install), so nothing changes for those paths.
 */

/**
 * Only boot in a real, top-level page document. The persisted document_start
 * injection also fires on the transient about:blank Chrome shows before it
 * navigates, and in subframes/iframes. Booting in the about:blank document is
 * actively harmful when the native WebMCP surface persists across the same-frame
 * navigation: that boot registers tools bound to the (about-to-be-stale) blank
 * context, and the real page's boot then skips re-registering them because they
 * already appear in the shared registry — so they run against location "blank"
 * instead of the real path. Gating to a real http(s) top document keeps
 * registration in the context whose location and IR are correct.
 */
function isBootableDocument(): boolean {
  try {
    if (window.top !== window.self) return false; // subframe
  } catch {
    return false; // cross-origin top access threw → we're framed; don't boot
  }
  const proto = window.location.protocol;
  return proto === "http:" || proto === "https:"; // excludes about:blank, data:, etc.
}

if (typeof window !== "undefined" && isBootableDocument()) {
  whenBootable(() => {
    const api = boot(window.__sightkick_ir);
    window.__sightkick = api;
    // No IR at boot (e.g. a host injected the bundle at document_start)? Wait for
    // a host bridge to hand one over via the DOM channel.
    if (!window.__sightkick_ir) installIrChannel(api);
  });
}

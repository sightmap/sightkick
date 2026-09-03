import { boot } from "./boot.js";
import { installIrChannel } from "./channel.js";

export { boot, detectMode, type SightkickGlobal, type Mode, type BootOptions } from "./boot.js";
export { runTool, routeMatches, type ToolResult, type RunOptions } from "./executor.js";
export { createClient, type SightkickClient } from "./client.js";
export { ensureModelContext, isPolyfilled, type ModelContext, type RegisteredTool } from "./webmcp.js";
export { installIrChannel } from "./channel.js";
export * from "./ir.js";

/**
 * Entry point for the bundled artifact. Installs window.__sightkick and loads an
 * IR from window.__sightkick_ir if the host set one before the bundle ran. A
 * host that loads the IR later can call window.__sightkick.load(ir).
 */
if (typeof window !== "undefined") {
  const api = boot(window.__sightkick_ir);
  window.__sightkick = api;
  // No IR at boot (e.g. a host injected the bundle at document_start)? Wait for
  // a host bridge to hand one over via the DOM channel.
  if (!window.__sightkick_ir) installIrChannel(api);
}

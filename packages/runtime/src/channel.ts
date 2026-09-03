/**
 * The IR handover channel for a privileged host injecting at document_start.
 *
 * A host that boots the runtime from outside the page (a browser extension's
 * ISOLATED-world content script, a CDP driver, etc.) often can't set the page's
 * `window.__sightkick_ir` directly. So it writes the IR onto the shared DOM
 * (attributes on <html>, then a bare event) — the DOM is the one thing both
 * sides see, and attributes are pure data, so no CSP rule can block them. The
 * in-page runtime (this module) reads them and boots.
 *
 * Same page, same result as a direct install: the runtime boots at document_start
 * with no IR, then loads whatever the bridge hands over — before an agent's first
 * getTools().
 */
import type { SightkickGlobal } from "./boot.js";
import type { IR } from "./ir.js";

export const IR_ATTR = "data-sightkick-ir";
export const HOST_ATTR = "data-sightkick-host";
export const IR_EVENT = "sightkick:ir";

function loadFromDom(api: SightkickGlobal): boolean {
  if (typeof document === "undefined" || !document.documentElement) return false;
  const de = document.documentElement;
  const raw = de.getAttribute(IR_ATTR);
  if (!raw) return false;
  const host = de.getAttribute(HOST_ATTR);
  // Consume the payload so we never load twice.
  de.removeAttribute(IR_ATTR);
  de.removeAttribute(HOST_ATTR);
  try {
    const ir = JSON.parse(raw) as IR;
    if (host) {
      try {
        (window as unknown as { __sightkick_host?: unknown }).__sightkick_host = JSON.parse(host);
        api.mode = "injected"; // provenance: this arrived via a host bridge
      } catch {
        /* provenance is best-effort */
      }
    }
    api.load(ir);
    return true;
  } catch (e) {
    console.warn("[sightkick] IR channel: bad payload", e);
    return false;
  }
}

/**
 * Wait for the bridge to hand over an IR. Handles either order (bridge before or
 * after us) via an initial read, the ready event, and a short poll as a backstop.
 */
export function installIrChannel(api: SightkickGlobal): void {
  if (typeof document === "undefined") return;
  if (loadFromDom(api)) return;

  let tries = 0;
  const poll = setInterval(() => {
    if (loadFromDom(api) || ++tries > 40) clearInterval(poll); // ~2s @ 50ms
  }, 50);
  document.addEventListener(
    IR_EVENT,
    () => {
      if (loadFromDom(api)) clearInterval(poll);
    },
    { once: true },
  );
}

// Readiness gate for the bundled artifact's auto-boot (sites-67f3).
//
// A persisted document_start injection (CDP Page.addScriptToEvaluateOnNewDocument)
// re-runs at the very start of every new document — before the SPA has bootstrapped
// and before the browser has installed the WebMCP native surface (document.modelContext).
// Two failure modes follow if boot runs there naively:
//
//   1. No native surface yet → the runtime polyfills document.modelContext and
//      registers tools onto the polyfill; the page's real (native) surface then
//      arrives empty. Symptom: a reloaded page has a native modelContext but no
//      sightkick tools (the original sites-67f3 report).
//   2. Registering onto the native surface WHILE the SPA is still bootstrapping
//      (Angular/Zone on JetBlue) sends a malformed WebMCP Mojo message and CRASHES
//      the renderer (RESULT_CODE_KILLED_BAD_MESSAGE). Native registerTool is only
//      known-safe once the app has settled — which is why a live inject (run well
//      after load) works.
//
// So the gate defers boot until the page can safely host tools: the document has
// finished loading AND (for a native page) the native surface is present, plus a
// short settle to let the SPA finish bootstrapping before we register. This
// approximates the known-safe live-inject timing for the persisted document_start
// script.

export interface ReadyOptions {
  /**
   * After the page is loaded (and, on a native page, the native surface is up),
   * wait this long before booting — a settle window that lets the SPA finish
   * bootstrapping so native registerTool doesn't land mid-bootstrap.
   */
  settleMs?: number;
  /**
   * On a page that has finished loading but exposes no native surface, wait this
   * long for a late native install before concluding the page is non-native and
   * booting on the polyfill. Bounds the extra delay on genuinely non-native pages.
   */
  nonNativeGraceMs?: number;
  /** Poll interval while waiting. */
  pollMs?: number;
  doc?: Document;
  win?: Window & typeof globalThis;
  now?: () => number;
}

/** True when the page already exposes a (native) document.modelContext surface. */
function hasModelContext(doc: Document): boolean {
  return (doc as unknown as { modelContext?: unknown }).modelContext != null;
}

/**
 * Defer `run` until the page can safely host tools, then invoke it exactly once.
 *
 * - If the document is already past initial parse (a live inject or a direct
 *   install into a loaded page — the known-safe timing), boot immediately.
 * - If we are running at document_start (readyState "loading", i.e. a persisted
 *   re-injection on a fresh navigation), wait for the document to finish loading
 *   and, on a native page, for the native modelContext to appear; then wait a
 *   short settle before booting so registration lands after the SPA has
 *   bootstrapped rather than during it.
 */
export function whenBootable(run: () => void, opts: ReadyOptions = {}): void {
  const doc = opts.doc ?? (typeof document !== "undefined" ? document : undefined);
  const win = opts.win ?? (typeof window !== "undefined" ? (window as Window & typeof globalThis) : undefined);
  if (!doc || !win) return;

  // Live inject / direct install into an already-parsed document: this is the
  // known-safe timing (the page has loaded), so boot now.
  if (doc.readyState !== "loading") {
    run();
    return;
  }

  const settleMs = opts.settleMs ?? 800;
  const nonNativeGraceMs = opts.nonNativeGraceMs ?? 500;
  const pollMs = opts.pollMs ?? 50;
  const now = opts.now ?? (() => Date.now());

  const t0 = now();
  let settleStart = 0; // when the page first became "ready to settle"; 0 until then
  let done = false;

  const fire = () => {
    if (done) return;
    done = true;
    run();
  };

  const bootable = (): boolean => {
    if (!settleStart) {
      const loaded = doc.readyState === "complete";
      if (loaded && hasModelContext(doc)) {
        // Native page, fully loaded: begin the settle before registering natively.
        settleStart = now();
      } else if (loaded && now() - t0 >= nonNativeGraceMs) {
        // Loaded, and no native surface has appeared within the grace → treat as a
        // non-native page and boot on the polyfill (after the settle below).
        settleStart = now();
      } else {
        return false;
      }
    }
    return now() - settleStart >= settleMs;
  };

  const tick = () => {
    if (done) return;
    if (bootable()) fire();
    else win.setTimeout(tick, pollMs);
  };

  // Poll, and also re-check promptly on the lifecycle transitions that move a
  // page toward bootable, so we don't sit out a whole poll interval after them.
  doc.addEventListener("DOMContentLoaded", tick, { once: true });
  win.addEventListener("load", tick, { once: true });
  tick();
}

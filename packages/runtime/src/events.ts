/**
 * Tool-call telemetry, published as a plain DOM CustomEvent on `document`.
 *
 * A page that cares how agents use its tools already has an analytics pipeline
 * of its own, so sightkick ships no adapter for any of them: it dispatches an
 * event and stops. The page subscribes and forwards whatever it wants, wherever
 * it wants.
 *
 * The detail carries argument KEY NAMES and never argument or result values —
 * a tool call routinely holds what a user typed (a name, an address, a card),
 * and telemetry is exactly where that leaks.
 */

export const TOOL_EVENT = "sightkick:tool";

/** How the caller reached the tool: the WebMCP surface, or the console/host API. */
export type ToolVia = "modelContext" | "call";

/**
 * The `detail` of a `sightkick:tool` event. A `start` event carries the call's
 * identity only; the matching `end` event (same `callId`) adds the outcome.
 */
export interface ToolEventDetail {
  phase: "start" | "end";
  tool: string;
  callId: string;
  via: ToolVia;
  /** Argument names only — never their values. */
  argKeys: string[];
  path: string;
  polyfilled: boolean;
  /** The IR name (which tool layer this page is running). */
  ir: string;
  ok?: boolean;
  skipped?: boolean;
  durationMs?: number;
  /** The failure message, truncated. Never a result value. */
  error?: string;
}

/** Longest error message an event carries; a stack-shaped message is not telemetry. */
const MAX_ERROR = 200;

// Page-level facts the executor cannot know on its own (it only ever sees one
// tool). Boot sets them when an IR loads; a bare executor consumer reports the
// empty defaults rather than nothing at all.
let pageContext: { ir: string; polyfilled: boolean } = { ir: "", polyfilled: false };

export function setEventContext(ctx: { ir: string; polyfilled: boolean }): void {
  pageContext = ctx;
}

/** Trim and bound a free-text field so an event can't carry an unbounded payload. */
export function clip(s: string, max: number): string {
  const t = s.trim();
  return t.length > max ? t.slice(0, max) : t;
}

export function emit(name: string, detail: unknown): void {
  // No document (a non-DOM consumer of the executor) means no events, not a throw.
  if (typeof document === "undefined") return;
  document.dispatchEvent(new CustomEvent(name, { detail }));
}

const now = (): number => (typeof performance !== "undefined" ? performance.now() : Date.now());

/**
 * Emit one call's `start` event and return the emitter for its `end` event, so
 * both sides of a call share a `callId` and the span is measured in one place.
 */
export function startToolEvent(
  tool: string,
  via: ToolVia,
  args: Record<string, unknown>,
  path: string,
): (result: { ok?: boolean; skipped?: boolean; message?: string }) => void {
  const base = {
    tool,
    callId: Math.random().toString(36).slice(2, 10),
    via,
    argKeys: Object.keys(args),
    path,
    polyfilled: pageContext.polyfilled,
    ir: pageContext.ir,
  };
  const started = now();
  emit(TOOL_EVENT, { phase: "start", ...base } satisfies ToolEventDetail);
  return (result) => {
    const detail: ToolEventDetail = {
      phase: "end",
      ...base,
      ok: !!result.ok,
      skipped: !!result.skipped,
      durationMs: Math.round(now() - started),
    };
    if (!result.ok && result.message) detail.error = clip(result.message, MAX_ERROR);
    emit(TOOL_EVENT, detail);
  };
}

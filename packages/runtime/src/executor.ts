import type { Field, Guard, Return, Step, Suggestion, Tool } from "./ir.js";
import { clickElement, extract, interpolate, resolveQuery, setNativeValue } from "./dom.js";

export interface ToolResult {
  ok: boolean;
  /** Single value result (from returns.kind == "value"). */
  value?: string;
  /** List result (from returns.kind == "list"). */
  items?: Record<string, string>[];
  /** Human-readable note (errors, warnings). */
  message?: string;
  /** True when the guard held and the steps were skipped (idempotent no-op). */
  skipped?: boolean;
  /** Compiled next-step guidance (the primary coordination channel). */
  guidance?: Suggestion[];
}

export interface RunOptions {
  /** Poll interval for waitFor, ms. */
  pollMs?: number;
  /** Override the current location path (tests). Defaults to window.location.pathname. */
  currentPath?: string;
  log?: (msg: string) => void;
  /** Cancels an in-flight tool (e.g. an agent's stop button). */
  signal?: AbortSignal;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

interface ResolvedOptions {
  pollMs: number;
  currentPath: string;
  log: (msg: string) => void;
  signal?: AbortSignal;
}

function resolveOptions(options: RunOptions = {}): ResolvedOptions {
  return {
    pollMs: options.pollMs ?? 100,
    currentPath: options.currentPath ?? (typeof window !== "undefined" ? window.location.pathname : "/"),
    log: options.log ?? ((m) => console.warn(`[sightkick] ${m}`)),
    signal: options.signal,
  };
}

/**
 * Minimal route matcher for standalone ensure_view verification. Mirrors the
 * lib's glob semantics: literal, :param/* (one segment), ** (zero-or-more
 * trailing segments).
 */
export function routeMatches(pattern: string, path: string): boolean {
  const norm = (p: string) => {
    const bare = p.split("#")[0]!.split("?")[0]!;
    const trimmed = bare.length > 1 && bare.endsWith("/") ? bare.slice(0, -1) : bare;
    return trimmed || "/";
  };
  const pat = norm(pattern);
  const pth = norm(path);
  if (pat === "/") return pth === "/";

  const segs = pat.split("/").filter((s) => s.length > 0);
  const rx =
    "^" +
    segs
      .map((seg) => {
        if (seg === "**") return "(?:/.+)?";
        if (seg === "*" || seg.startsWith(":")) return "/[^/]+";
        return "/" + seg.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      })
      .join("") +
    "$";
  return new RegExp(rx).test(pth);
}

/** Does the guard hold for the current DOM? (present = something matches). */
function guardHolds(guard: Guard, args: Record<string, unknown>): boolean {
  const matches = resolveQuery(guard.query, args).length;
  return guard.kind === "present" ? matches > 0 : matches === 0;
}

/** Human-readable target for error messages. */
function describeTarget(step: Step): string {
  const parts = step.query?.parts ?? [];
  return `query ${JSON.stringify(parts.map((p) => p.locators.join("|")))}`;
}

async function runStep(step: Step, args: Record<string, unknown>, opts: ResolvedOptions): Promise<void> {
  // Every DOM-addressing step resolves the same way: a compquery to the target
  // element. Reads are not steps — the result is declared by returns.
  const target = (): Element | undefined => (step.query ? resolveQuery(step.query, args)[0] : undefined);

  switch (step.op) {
    case "navigate": {
      const path = opts.currentPath;
      if (step.route && !routeMatches(step.route, path)) {
        opts.log(`navigate: single-page slice cannot leave ${path} for ${step.route} (deferred to journey work)`);
      }
      return;
    }
    case "fill": {
      const el = target();
      if (!el) throw new Error(`fill: no element for ${describeTarget(step)}`);
      setNativeValue(el, interpolate(step.value ?? "", args));
      return;
    }
    case "click": {
      const el = target();
      if (!el) throw new Error(`click: no element for ${describeTarget(step)}`);
      clickElement(el);
      return;
    }
    case "waitFor": {
      const deadline = Date.now() + (step.timeoutMs ?? 5000);
      for (;;) {
        if (opts.signal?.aborted) throw new Error("aborted");
        if (target()) return;
        if (Date.now() >= deadline) {
          throw new Error(`waitFor: timed out after ${step.timeoutMs ?? 5000}ms for ${describeTarget(step)}`);
        }
        await sleep(opts.pollMs);
      }
    }
    default:
      throw new Error(`unknown step op ${(step as Step).op}`);
  }
}

/** Extract a row's declared fields into a flat object. */
function extractFields(el: Element, fields: Record<string, Field>): Record<string, string> {
  const obj: Record<string, string> = {};
  for (const [name, f] of Object.entries(fields)) {
    obj[name] = extract(el, f.extractor);
  }
  return obj;
}

function computeReturn(ret: Return, args: Record<string, unknown>): ToolResult {
  if (ret.kind === "list") {
    const rows = ret.query ? resolveQuery(ret.query, args) : [];
    const fields = ret.fields ?? {};
    return { ok: true, items: rows.map((el) => extractFields(el, fields)) };
  }
  // value
  const el = ret.query ? resolveQuery(ret.query, args)[0] : undefined;
  const value = el && ret.extractor ? extract(el, ret.extractor) : undefined;
  const out: ToolResult = { ok: true };
  if (value !== undefined) out.value = value;
  return out;
}

/**
 * Execute a tool's steps against the live DOM using the same affordances a user
 * has (fill, click, wait, read). Single-page only in this slice; cross-page
 * journeys are handled by the (later) resumable state machine.
 */
export async function runTool(tool: Tool, args: Record<string, unknown> = {}, options: RunOptions = {}): Promise<ToolResult> {
  const opts = resolveOptions(options);

  if (tool.ensureView && !routeMatches(tool.ensureView.route, opts.currentPath)) {
    opts.log(`ensure_view: "${tool.name}" expects ${tool.ensureView.view} (${tool.ensureView.route}) but path is ${opts.currentPath}; proceeding best-effort`);
  }

  // Idempotency guard: if the effect is already applied, skip the steps and
  // return the current state (with skipped:true) rather than re-applying.
  if (tool.guard && guardHolds(tool.guard, args)) {
    const skipped: ToolResult = tool.returns
      ? { ...computeReturn(tool.returns, args), skipped: true }
      : { ok: true, skipped: true };
    skipped.message = "guard satisfied; steps skipped (already applied)";
    if (tool.guidance && tool.guidance.length) skipped.guidance = tool.guidance;
    return skipped;
  }

  try {
    for (const step of tool.steps) {
      await runStep(step, args, opts);
    }
  } catch (err) {
    return { ok: false, message: (err as Error).message };
  }

  const result = tool.returns ? computeReturn(tool.returns, args) : { ok: true };
  // Attach next-step guidance so the agent gets it in the tool's own response —
  // the reliable channel (more so than proactively re-reading getTools()).
  if (tool.guidance && tool.guidance.length) result.guidance = tool.guidance;
  return result;
}

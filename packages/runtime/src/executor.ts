import type { Field, Guard, IR, Pred, Return, Step, Suggestion, Tool } from "./ir.js";
import { clickElement, extract, interpolate, resolveQuery, typeInto } from "./dom.js";

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

/** Render one predicate as its authored discriminator, e.g. `[label*="flights only" i]`. */
function renderPred(p: Pred): string {
  const prop = p.property ?? "";
  const ci = p.ci ? " i" : "";
  return `[${prop}${p.op}"${p.value}"${ci}]`;
}

/**
 * A compact, legible rendering of a step's target: the descendant chain of
 * selectors, each carrying its predicate discriminators. This is the readable
 * form used in error messages AND in the resumable status projection (sites-be76)
 * — it deliberately keeps the predicate values (`label*="flights only"`), which
 * are the semantic handle an author wrote, rather than dumping the raw locator
 * JSON. Locator noise stays minimal by showing the first locator per part (the
 * representative one); the raw Step is untouched and remains the resubmit shape.
 */
function renderTarget(step: Step): string {
  // Prefer the generator's semantic corpus label (component-query / view / key)
  // over reconstructing raw locators from the compiled query (sites-6a1a). The
  // fallbacks below keep older IRs (no `target`) rendering as before.
  if (step.target) return step.target;
  const parts = step.query?.parts;
  if (parts && parts.length) {
    return parts
      .map((p) => (p.locators[0] ?? "*") + (p.preds ?? []).map(renderPred).join(""))
      .join(" ");
  }
  if (step.view) return step.view;
  if (step.route) return `route ${step.route}`;
  if (step.url) return step.url;
  if (step.key) return `key ${step.key}`;
  return step.op;
}

/** Collect the {{param}} names a template string references into `out`. */
function templateParamNames(s: string | undefined, out: Set<string>): void {
  if (!s) return;
  for (const m of s.matchAll(/\{\{\s*([\w.]+)\s*\}\}/g)) out.add(m[1]!);
}

/** Every param a step interpolates: its value/url/key plus any query pred values. */
function stepParams(step: Step): string[] {
  const out = new Set<string>();
  templateParamNames(step.value, out);
  templateParamNames(step.url, out);
  templateParamNames(step.key, out);
  for (const part of step.query?.parts ?? []) {
    for (const pred of part.preds ?? []) templateParamNames(pred.value, out);
  }
  return [...out];
}

/**
 * Optional/skippable steps, so a grouped tool can carry optional fields. An
 * explicit `when` guard skips the step when it interpolates to empty. Otherwise a
 * step AUTO-skips when any {{param}} it interpolates is absent from args (an
 * omitted optional param — required params are guaranteed present by the tool's
 * input schema). Omitted (undefined) is distinct from an explicit empty string,
 * which is a real value and does not skip.
 */
function shouldSkipStep(step: Step, args: Record<string, unknown>): boolean {
  if (step.when !== undefined) {
    return interpolate(step.when, args).trim() === "";
  }
  for (const p of stepParams(step)) {
    if (!(p in args) || args[p] === undefined) return true;
  }
  return false;
}

// isActionable skips responsive-hidden duplicates when picking an ACTION target:
// layouts keep both a mobile and desktop copy in the DOM (Tailwind md:hidden /
// max-md:hidden), and clicking/filling the hidden one is a silent no-op. Uses
// layout signals only, so in a layout-less test DOM it reports false for
// everything and the caller falls back to the first match (behavior unchanged).
function isActionable(el: Element): boolean {
  const h = el as HTMLElement;
  if (typeof h.getBoundingClientRect !== "function") return false;
  // offsetParent is null for display:none subtrees (and position:fixed, which we
  // exempt since fixed elements are still actionable).
  const style = typeof getComputedStyle === "function" ? getComputedStyle(h) : null;
  if (h.offsetParent === null && style?.position !== "fixed") return false;
  const r = h.getBoundingClientRect();
  return r.width > 0 && r.height > 0;
}

async function runStep(step: Step, args: Record<string, unknown>, opts: ResolvedOptions): Promise<void> {
  // Every DOM-addressing step resolves the same way: a compquery to the target
  // element. Reads are not steps — the result is declared by returns. For an
  // ACTION target we prefer the first VISIBLE match so a responsive hidden
  // duplicate never gets a no-op click/fill (falls back to the first match).
  const target = (): Element | undefined => {
    if (!step.query) return undefined;
    const matches = resolveQuery(step.query, args);
    return matches.find(isActionable) ?? matches[0];
  };

  switch (step.op) {
    case "navigate": {
      const path = opts.currentPath;
      if (step.route && !routeMatches(step.route, path)) {
        opts.log(`navigate: single-page slice cannot leave ${path} for ${step.route} (deferred to journey work)`);
      }
      return;
    }
    case "goto": {
      // A real cross-page navigation to an interpolated URL — the escape hatch for
      // destinations that can't be reached by in-page actuation (e.g. a search
      // whose form is user-activation-gated: deep-link straight to the results).
      // goto is terminal: the navigation tears down this context, so we defer it a
      // tick to let the tool's result + guidance reach the caller before unload.
      const url = interpolate(step.url ?? "", args);
      if (url && typeof window !== "undefined") {
        setTimeout(() => window.location.assign(url), 0);
      }
      return;
    }
    case "fill": {
      const el = target();
      if (!el) throw new Error(`fill: no element for ${renderTarget(step)}`);
      typeInto(el, interpolate(step.value ?? "", args));
      return;
    }
    case "click": {
      const el = target();
      if (!el) throw new Error(`click: no element for ${renderTarget(step)}`);
      await clickElement(el);
      return;
    }
    case "keypress": {
      // No query of its own — targets whatever a preceding fill/click left
      // focused, matching the CLI's own `browser keypress KEY`. Exists for a
      // form gate a fill's own per-character keydown/keyup can't stand in for
      // (e.g. a field that only reveals a dependent control once Enter is
      // pressed as its own discrete event, not as part of typing a value).
      const key = step.key ?? "";
      if (!key) throw new Error("keypress: no key given");
      const el = document.activeElement ?? document.body;
      el.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
      el.dispatchEvent(new KeyboardEvent("keyup", { key, bubbles: true }));
      return;
    }
    case "waitFor": {
      // Two forms: a DOM query (the common case, resolved via target() above)
      // or a bare route (for a tool whose last act navigates away and has no
      // view-scoped content of its own to wait on instead). The route form
      // re-reads window.location on every poll, unlike opts.currentPath's
      // one-time snapshot, because it exists specifically to observe a
      // same-context client-side route change happening *during* this wait.
      const routeSatisfied = (): boolean =>
        !!step.route &&
        routeMatches(step.route, typeof window !== "undefined" ? window.location.pathname : opts.currentPath);
      const satisfied = step.query ? () => !!target() : routeSatisfied;
      const deadline = Date.now() + (step.timeoutMs ?? 5000);
      for (;;) {
        if (opts.signal?.aborted) throw new Error("aborted");
        if (satisfied()) return;
        if (Date.now() >= deadline) {
          throw new Error(`waitFor: timed out after ${step.timeoutMs ?? 5000}ms for ${renderTarget(step)}`);
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
      if (shouldSkipStep(step, args)) {
        opts.log(`skip ${step.op} step (optional field absent)`);
        continue;
      }
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

// ---------------------------------------------------------------------------
// L1 resumable executor (spike — sites-10b3)
//
// Runs an arbitrary action list with the SAME atomic step machinery as runTool,
// but swaps the outcome envelope: instead of throwing/hanging at the first action
// that doesn't cleanly resolve, it returns a structured status — how far we got,
// what interrupted us, and the untried tail — so the caller (an agent) can
// observe, handle the exception, and RESUME by re-submitting `remaining`.
//
// Continuation stays shallow on purpose: `remaining` is just the tail of the
// action list the caller already holds (beginning with the interrupted action),
// not a captured stack. Consumes compiled Steps (the runtime's firewall shape);
// the human-facing {op, query-string} grammar is a generator/CLI graft for later.
// ---------------------------------------------------------------------------

export interface ActionStatus {
  /** Count of actions that fully completed (or auto-skipped) before stopping. */
  completedThrough: number;
  /** Size of the submitted action list. */
  total: number;
  /** True when every action completed. */
  done: boolean;
  /** Present when an action didn't cleanly resolve (timeout / no-match / abort). */
  interrupt?: {
    /** 0-based index of the action we stopped on. */
    at: number;
    /** Human-readable summary of that action. */
    action: string;
    /** Why it didn't resolve (the underlying step error). */
    reason: string;
    /** Cheap state snapshot to orient recovery; enriched (view/components) later. */
    observed: { path: string };
  };
  /** The untried tail, beginning with the interrupted action. Re-submit to resume. */
  remaining: Step[];
  /**
   * A readable, index-aligned projection of `remaining` (op + target label). It is
   * the legend that makes the raw tail intelligible: an agent reads THIS to decide
   * what to do, then either resubmits `remaining` verbatim (opaque continuation) or
   * edits a step and resubmits (escape hatch) — one tail, two modes. `remaining`
   * stays the untouched compiled Steps so a blind resume is always exact.
   */
  remainingView: ActionView[];
}

/** A readable one-action projection: its op and compact target label. */
export interface ActionView {
  op: string;
  /** Compact target (selectors + predicate discriminators), not raw locator JSON. */
  target: string;
  /** The step's when-guard, if any — surfaced so a conditionally-skipped action reads clearly. */
  when?: string;
}

function projectAction(step: Step): ActionView {
  const view: ActionView = { op: step.op, target: renderTarget(step) };
  if (step.when !== undefined) view.when = step.when;
  return view;
}

function describeAction(step: Step): string {
  const target = renderTarget(step);
  return target && target !== step.op ? `${step.op} ${target}` : step.op;
}

/**
 * A reusable action fragment: one authored step of a tool, projected for agent
 * composition (sites-7eba). An agent building an execActions list treats tool
 * steps as a PARTS BIN; this index lets it pluck (say) a fill by the param it
 * fills — `uses` includes "firstName" — and address it by a stable id, instead of
 * scanning ir.tools[].steps and string-matching interpolation values.
 */
export interface Fragment {
  /** Stable within the loaded IR: `<tool>.<stepIndex>`. */
  id: string;
  /** Owning tool (provenance). */
  tool: string;
  /** 0-based index of the step within its tool. */
  index: number;
  /** The action op (fill/click/waitFor/navigate/goto/keypress). */
  op: string;
  /** Readable action phrase: op + target (selectors + predicate discriminators). */
  label: string;
  /** Param names the fragment interpolates — pluck fragments by these. */
  uses: string[];
}

/** Every param a fragment interpolates, including its when-guard (sorted, deduped). */
function fragmentUses(step: Step): string[] {
  const out = new Set<string>(stepParams(step));
  templateParamNames(step.when, out);
  return [...out].sort();
}

/**
 * Project a loaded IR into a flat fragment index: one entry per tool step, in
 * tool-then-step order. Pure over the IR (no DOM, no execution), so a caller can
 * build it once and compose action lists from it.
 */
export function projectFragments(ir: IR): Fragment[] {
  const out: Fragment[] = [];
  for (const tool of ir.tools) {
    tool.steps.forEach((step, index) => {
      out.push({
        id: `${tool.name}.${index}`,
        tool: tool.name,
        index,
        op: step.op,
        label: describeAction(step),
        uses: fragmentUses(step),
      });
    });
  }
  return out;
}

export async function execActions(
  actions: Step[],
  args: Record<string, unknown> = {},
  options: RunOptions = {},
): Promise<ActionStatus> {
  const opts = resolveOptions(options);
  const livePath = () => (typeof window !== "undefined" ? window.location.pathname : opts.currentPath);
  for (let i = 0; i < actions.length; i++) {
    const step = actions[i]!;
    if (shouldSkipStep(step, args)) {
      opts.log(`skip ${step.op} action (optional field absent)`);
      continue;
    }
    try {
      await runStep(step, args, opts);
    } catch (err) {
      return {
        completedThrough: i,
        total: actions.length,
        done: false,
        interrupt: {
          at: i,
          action: describeAction(step),
          reason: (err as Error).message,
          observed: { path: livePath() },
        },
        remaining: actions.slice(i),
        remainingView: actions.slice(i).map(projectAction),
      };
    }
  }
  return { completedThrough: actions.length, total: actions.length, done: true, remaining: [], remainingView: [] };
}

import type { IR, Step, Tool } from "./ir.js";
import {
  execActions as execActionList,
  projectFragments,
  routeMatches,
  runTool,
  type ActionStatus,
  type ActionView,
  type Fragment,
  type RunOptions,
  type ToolResult,
} from "./executor.js";
import {
  ensureModelContext,
  isPolyfilled,
  type ModelContext,
  type ToolResultEnvelope,
  type WebMCPToolDef,
} from "./webmcp.js";
import { describeError } from "./errors.js";

export interface BootOptions {
  /** Override the current path (tests). Defaults to window.location.pathname. */
  currentPath?: string;
}

/**
 * Provenance only. A privileged host (e.g. a browser extension or a CDP driver)
 * can inject the *same* generated artifact into a third-party page; that is not
 * semantically different from a direct install, so there is no "mediated"
 * execution mode — this just records how we got here.
 */
export type Mode = "direct" | "injected";

export interface SightkickGlobal {
  mode: Mode;
  ir: IR | null;
  modelContext: ModelContext | undefined;
  polyfilled: boolean;
  /** Load (or replace) the active IR and re-register its tools. */
  load(ir: IR): void;
  /** Re-evaluate view-scoped registration for the current path (fires toolchange). */
  refresh(): void;
  /** Names + descriptions of currently registered tools. */
  tools(): { name: string; description?: string }[];
  /** Console convenience: invoke a tool by name. */
  call(name: string, args?: Record<string, unknown>, options?: RunOptions): Promise<ToolResult>;
  /**
   * L1 (sites-10b3): run an action list resumably. Takes a raw Step[] ONLY — an
   * agent composes the list itself (from fragments/guidance) or slices a prior
   * status's `remaining` to resume after handling an interrupt. It deliberately
   * does NOT dispatch a tool by name: running a named tool's canned steps is a
   * separate, explicit act (read `ir.tools[].steps`), so the engine never doubles
   * as an opaque-tool backdoor. Returns a structured status instead of throwing or
   * hanging at the first action that doesn't cleanly resolve.
   */
  execActions(
    actions: Step[],
    args?: Record<string, unknown>,
    options?: RunOptions,
  ): Promise<ActionStatus>;
  /**
   * L1 (sites-7eba): the fragment index for the loaded IR — every tool step as a
   * pluckable {id, tool, op, label, uses}. An agent composing an execActions list
   * reads this to find fragments by the params they use, instead of scanning
   * ir.tools[].steps and string-matching interpolation values. Empty when no IR
   * is loaded.
   */
  fragments(): Fragment[];
}

declare global {
  interface Window {
    __sightkick?: SightkickGlobal;
    __sightkick_ir?: IR;
    /** Present only when injected by a host bridge (provenance, not behavior). */
    __sightkick_host?: unknown;
  }
}

export function detectMode(): Mode {
  return typeof window !== "undefined" && window.__sightkick_host != null ? "injected" : "direct";
}

function findTool(ir: IR | null, name: string): Tool | undefined {
  return ir?.tools.find((t) => t.name === name);
}

function toEnvelope(result: ToolResult) {
  return { content: [{ type: "text" as const, text: JSON.stringify(result) }], isError: !result.ok };
}

/** Wrap an arbitrary meta-tool payload in the WebMCP text envelope. */
function metaEnvelope(payload: unknown, isError = false): ToolResultEnvelope {
  return { content: [{ type: "text", text: JSON.stringify(payload) }], isError };
}

/**
 * Resolve a fragment id (`<tool>.<index>`, as projectFragments emits) to its
 * compiled Step against `ir`. tool names carry no dots, so the last dot splits
 * name from index.
 */
function fragmentStep(ir: IR | null, ref: string): Step | undefined {
  if (!ir) return undefined;
  const dot = ref.lastIndexOf(".");
  if (dot < 0) return undefined;
  const idx = Number(ref.slice(dot + 1));
  if (!Number.isInteger(idx) || idx < 0) return undefined;
  const tool = ir.tools.find((t) => t.name === ref.slice(0, dot));
  return tool?.steps[idx];
}

function resolveFragmentRefs(ir: IR | null, refs: string[]): { steps: Step[]; unknown: string[] } {
  const steps: Step[] = [];
  const unknown: string[] = [];
  for (const ref of refs) {
    const step = fragmentStep(ir, ref);
    if (step) steps.push(step);
    else unknown.push(ref);
  }
  return { steps, unknown };
}

/** The exec_actions tool's result: an ActionStatus with its tail re-expressed as fragment refs. */
interface ExecActionsResult {
  done: boolean;
  completedThrough: number;
  total: number;
  interrupt?: ActionStatus["interrupt"];
  /** Fragment refs still to run, beginning with the interrupted one — re-submit to resume. */
  remaining: string[];
  /** Readable, index-aligned projection of `remaining` (op + semantic target). */
  remainingView: ActionView[];
}

/**
 * Project an execActions ActionStatus back into the fragment-ref grammar the tool
 * speaks: the untried Step tail becomes the untried REF tail (the refs are
 * index-aligned with the steps, and completedThrough is the stop index), so an
 * agent resumes by re-calling exec_actions with `remaining` rather than ever
 * handling raw compiled steps.
 */
function projectExecStatus(status: ActionStatus, refs: string[]): ExecActionsResult {
  return {
    done: status.done,
    completedThrough: status.completedThrough,
    total: status.total,
    interrupt: status.interrupt,
    remaining: refs.slice(status.completedThrough),
    remainingView: status.remainingView,
  };
}

// SPA route changes don't fire an event, so patch history once to emit one. This
// lets an injected runtime notice client-side navigations on sites we don't
// control, without the site cooperating.
let historyPatched = false;
function patchHistory(): void {
  if (historyPatched || typeof history === "undefined" || typeof window === "undefined") return;
  historyPatched = true;
  const wrap = (orig: History["pushState"]): History["pushState"] =>
    function (this: History, data, unused, url) {
      const r = orig.call(this, data, unused, url);
      window.dispatchEvent(new Event("sightkick:navigate"));
      return r;
    };
  history.pushState = wrap(history.pushState.bind(history));
  history.replaceState = wrap(history.replaceState.bind(history));
}

/**
 * Build the sightkick global and register the IR's tools on document.modelContext.
 * Tools are atomic and same-page; multi-step coordination is carried as guidance
 * in each tool's result, not by any runtime executor.
 */
export function boot(initial?: IR, opts: BootOptions = {}): SightkickGlobal {
  const ctx = ensureModelContext();
  const currentPath = () => opts.currentPath ?? (typeof window !== "undefined" ? window.location.pathname : "/");
  let registrations: AbortController[] = [];
  let registered: { name: string; description?: string }[] = [];

  const unregisterAll = () => {
    for (const c of registrations) c.abort();
    registrations = [];
    registered = [];
  };

  const refresh = () => {
    unregisterAll();
    const ir = api.ir;
    if (!ir || !ctx) return;
    const path = currentPath();
    for (const tool of ir.tools) {
      // View-scoped registration: a tool is offered only on its view. This is
      // how the tool set changes per page — each page load (or a host's per-page
      // injection) boots fresh and registers just that view's tools.
      if (tool.ensureView && !routeMatches(tool.ensureView.route, path)) continue;
      const controller = new AbortController();
      registrations.push(controller);
      registered.push({ name: tool.name, description: tool.description });
      // registerTool is fire-and-forget, but a rejected native call must NOT
      // become an "Uncaught (in promise) {}" — surface the real reason.
      Promise.resolve(
        ctx.registerTool(
          {
            name: tool.name,
            description: tool.description ?? "",
            inputSchema: tool.inputSchema,
            execute: async (args, options) =>
              toEnvelope(await runTool(tool, args, { signal: options?.signal, currentPath: path })),
          },
          { signal: controller.signal },
        ),
      ).catch((e) => console.warn(`[sightkick] registerTool "${tool.name}" rejected: ${describeError(e)}`));
    }
  };

  // The current view's fragment base set: fragments of tools offered on this view
  // (same ensure_view rule as tool registration), plus view-agnostic tools. This
  // is deliberately just the base set; the 1-hop guidance horizon + distance
  // tiering is sites-90dc.
  const currentFragments = (): Fragment[] => {
    const ir = api.ir;
    if (!ir) return [];
    const path = currentPath();
    return projectFragments(ir).filter((f) => {
      const tool = ir.tools.find((t) => t.name === f.tool);
      return !tool?.ensureView || routeMatches(tool.ensureView.route, path);
    });
  };

  // Always-on meta tools (sites-b573): the universal fallback surface. exec_actions
  // runs an agent-composed list of fragment refs resumably; get_fragments lists the
  // refs available here. Registered once and NOT torn down by refresh()'s per-view
  // churn, so they persist across SPA navigations.
  const metaControllers: AbortController[] = [];
  let metaRegistered = false;
  // Re-read document.modelContext at call time rather than closing over the ctx
  // captured at boot: on an SPA that defers boot to post-settle (sites-67f3),
  // Angular's Zone re-wraps document.modelContext between boot and the async
  // registerMetaTools pass, so the captured ctx can be stale and its registerTool
  // never lands in the live registry. The synchronous view-tool registration in
  // refresh() doesn't hit this because it runs before any await.
  const liveCtx = (): ModelContext | undefined =>
    (typeof document !== "undefined" && (document as unknown as { modelContext?: ModelContext }).modelContext) || ctx;
  const registerMetaTool = async (def: WebMCPToolDef) => {
    const target = liveCtx();
    if (!target) return;
    const controller = new AbortController();
    metaControllers.push(controller);
    try {
      // Await so back-to-back registrations don't race on the native surface
      // (JetBlue drops the second of two same-tick registerTool calls).
      await target.registerTool(def, { signal: controller.signal });
    } catch (e) {
      console.warn(`[sightkick] registerTool "${def.name}" rejected: ${describeError(e)}`);
    }
  };
  // Register the always-on meta tools exactly once PER DOCUMENT, not per boot
  // instance. On an SPA like JetBlue the injected bundle boots in several
  // execution contexts (isolated worlds), and Angular's Zone re-wraps
  // document.modelContext per access, so a window/ctx-property flag can't dedup
  // across them — only the shared native registry can. So we skip any meta tool
  // already present in `ctx.getTools()`. (The per-instance `metaRegistered` flag
  // still short-circuits a repeat call within one context.)
  const registerMetaTools = async () => {
    if (metaRegistered || !ctx) return;
    metaRegistered = true;
    let present = new Set<string>();
    try {
      const target = liveCtx();
      if (target) present = new Set((await target.getTools()).map((t) => t.name));
    } catch {
      /* getTools may reject on a transitional surface; fall back to registering */
    }
    if (!present.has("exec_actions")) {
      await registerMetaTool({
        name: "exec_actions",
      description:
        "Run an ordered list of action fragments resumably. Pass fragment ids (from get_fragments) " +
        "in `refs` and any parameter values in `args`. Returns how far it got; if an action is " +
        "interrupted (e.g. an unexpected modal, a missing field), it STOPS instead of hanging and " +
        "returns the reason plus the remaining fragment refs. Handle the interruption (dismiss the " +
        "modal, call another tool), then call exec_actions again with the returned `remaining` refs " +
        "to resume where it left off.",
      inputSchema: {
        type: "object",
        properties: {
          refs: {
            type: "array",
            items: { type: "string" },
            description: "Fragment ids to run in order, e.g. [\"set_contact.0\", \"set_contact.1\"].",
          },
          args: {
            type: "object",
            description: "Parameter values the fragments interpolate (see each fragment's `uses`).",
          },
        },
        required: ["refs"],
      },
      execute: async (rawArgs, options) => {
        const refs = Array.isArray(rawArgs?.refs) ? (rawArgs.refs as unknown[]).map(String) : [];
        const callArgs = (rawArgs?.args as Record<string, unknown>) ?? {};
        if (!api.ir) return metaEnvelope({ error: "no IR loaded" }, true);
        if (!refs.length) return metaEnvelope({ error: "exec_actions needs a non-empty `refs` array" }, true);
        const { steps, unknown } = resolveFragmentRefs(api.ir, refs);
        if (unknown.length) {
          return metaEnvelope(
            { error: `unknown fragment ref(s): ${unknown.join(", ")}`, hint: "call get_fragments for valid ids" },
            true,
          );
        }
        const status = await execActionList(steps, callArgs, { signal: options?.signal, currentPath: currentPath() });
        return metaEnvelope(projectExecStatus(status, refs), !status.done);
      },
      });
    }
    if (!present.has("get_fragments")) {
      await registerMetaTool({
        name: "get_fragments",
      description:
        "List the action fragments available on the current view. Each is a pluckable step " +
        "{id, tool, op, label, uses}: `label` reads in component/view vocabulary, `uses` names the " +
        "parameters it needs. Compose an ordered list of `id`s and pass them to exec_actions.",
      inputSchema: { type: "object", properties: {} },
      execute: async () => metaEnvelope({ fragments: currentFragments() }),
      });
    }
  };

  const api: SightkickGlobal = {
    mode: detectMode(),
    ir: null,
    modelContext: ctx,
    polyfilled: isPolyfilled(ctx),
    load(ir: IR) {
      this.ir = ir;
      refresh();
      registerMetaTools();
      console.info(
        `[sightkick] loaded IR "${ir.name}" (${ir.tools.length} tools, ${this.mode}, ` +
          `${this.polyfilled ? "polyfilled" : "native"} modelContext)`,
      );
    },
    tools() {
      return registered.slice();
    },
    refresh,
    call(name, args = {}, options) {
      const tool = findTool(this.ir, name);
      if (!tool) return Promise.resolve({ ok: false, message: `unknown tool "${name}"` });
      return runTool(tool, args, options);
    },
    execActions(actions, args = {}, options) {
      return execActionList(actions, args, options);
    },
    fragments() {
      return this.ir ? projectFragments(this.ir) : [];
    },
  };

  // Re-register the view-scoped tool set when the SPA route changes. Skipped
  // when a fixed currentPath is supplied (tests / non-DOM).
  if (typeof window !== "undefined" && opts.currentPath === undefined) {
    let lastPath = currentPath();
    const onNav = () => {
      const p = currentPath();
      if (p !== lastPath) {
        lastPath = p;
        refresh();
      }
    };
    window.addEventListener("popstate", onNav);
    window.addEventListener("sightkick:navigate", onNav);
    patchHistory();
  }

  if (initial) api.load(initial);
  return api;
}

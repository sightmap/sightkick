import type { IR, Step, Tool } from "./ir.js";
import {
  execActions as execActionList,
  projectFragments,
  routeMatches,
  runTool,
  type ActionStatus,
  type Fragment,
  type RunOptions,
  type ToolResult,
} from "./executor.js";
import { ensureModelContext, isPolyfilled, type ModelContext } from "./webmcp.js";
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

  const api: SightkickGlobal = {
    mode: detectMode(),
    ir: null,
    modelContext: ctx,
    polyfilled: isPolyfilled(ctx),
    load(ir: IR) {
      this.ir = ir;
      refresh();
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

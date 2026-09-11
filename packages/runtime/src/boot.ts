import type { IR, Tool } from "./ir.js";
import { routeMatches, runTool, type RunOptions, type ToolResult } from "./executor.js";
import { ensureModelContext, isPolyfilled, type ModelContext, type WebMCPToolDef } from "./webmcp.js";
import { describeError } from "./errors.js";
import { setEventContext } from "./events.js";
import { metaTools, withFeedbackNudge, type MetaTool } from "./meta.js";

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
  let metaByName = new Map<string, MetaTool>();

  const unregisterAll = () => {
    for (const c of registrations) c.abort();
    registrations = [];
    registered = [];
    metaByName = new Map();
  };

  const refresh = () => {
    unregisterAll();
    const ir = api.ir;
    if (!ir || !ctx) return;
    const mc = ctx; // narrowed once, for the closures below
    const path = currentPath();
    const register = (def: WebMCPToolDef) => {
      const controller = new AbortController();
      registrations.push(controller);
      registered.push({ name: def.name, description: def.description });
      // registerTool is fire-and-forget, but a rejected native call must NOT
      // become an "Uncaught (in promise) {}" — surface the real reason.
      Promise.resolve(mc.registerTool(def, { signal: controller.signal })).catch((e) =>
        console.warn(`[sightkick] registerTool "${def.name}" rejected: ${describeError(e)}`),
      );
    };

    for (const tool of ir.tools) {
      // View-scoped registration: a tool is offered only on its view. This is
      // how the tool set changes per page — each page load (or a host's per-page
      // injection) boots fresh and registers just that view's tools.
      if (tool.ensureView && !routeMatches(tool.ensureView.route, path)) continue;
      register({
        name: tool.name,
        description: tool.description ?? "",
        inputSchema: tool.inputSchema,
        execute: async (args, options) =>
          toEnvelope(
            withFeedbackNudge(
              await runTool(tool, args, { signal: options?.signal, currentPath: path, via: "modelContext" }),
              ir.meta,
            ),
          ),
      });
    }

    // Meta tools are not view-scoped: an agent hits the gap they cover on
    // whatever page it happens to be on.
    for (const mt of metaTools(ir.meta, { ir: ir.name, path: currentPath })) {
      metaByName.set(mt.name, mt);
      register({
        name: mt.name,
        description: mt.description,
        inputSchema: mt.inputSchema,
        execute: async (args) => toEnvelope(mt.run(args ?? {})),
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
      // Tool events name the tool layer and the surface they came from; only
      // boot knows either, so hand them to the executor's emitter here.
      setEventContext({ ir: ir.name, polyfilled: this.polyfilled });
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
      const ir = this.ir;
      const meta = metaByName.get(name);
      // Meta tools are registered, so calling one by name has to work here too.
      if (meta) return Promise.resolve(meta.run(args));
      const tool = findTool(ir, name);
      if (!tool) return Promise.resolve({ ok: false, message: `unknown tool "${name}"` });
      return runTool(tool, args, options).then((r) => withFeedbackNudge(r, ir?.meta));
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

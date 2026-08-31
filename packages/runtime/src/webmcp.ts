/**
 * The WebMCP surface (`document.modelContext`) per the W3C WebML explainer
 * (webmachinelearning/webmcp): a page registers tools that in-page or built-in
 * agents discover with getTools() and invoke with executeTool(). Dynamic tool
 * sets are announced via a `toolchange` event.
 *
 * Today no shipping browser exposes this natively, so sightkick installs a
 * spec-shaped **polyfill** when it's absent. That's the "project skills down to
 * tools" story in standalone mode: our IR tools become real WebMCP tools that
 * any WebMCP-aware agent — or our own bundled client — can drive through the
 * standard API. When a native implementation exists, we use it unchanged.
 */

/** MCP-style tool result content. */
export interface ToolContent {
  type: "text";
  text: string;
}
export interface ToolResultEnvelope {
  content: ToolContent[];
  isError?: boolean;
}

export interface ToolExecuteOptions {
  signal?: AbortSignal;
}

/** A tool as registered by a provider. */
export interface WebMCPToolDef {
  name: string;
  description?: string;
  inputSchema?: unknown;
  execute: (args: Record<string, unknown>, options?: ToolExecuteOptions) => ToolResultEnvelope | Promise<ToolResultEnvelope>;
}

export interface RegisterToolOptions {
  signal?: AbortSignal;
  exposedTo?: string[];
}

/** A tool as seen by a consumer via getTools(). */
export interface RegisteredTool {
  name: string;
  description?: string;
  inputSchema?: unknown;
  origin?: string;
}

export interface GetToolsOptions {
  fromOrigins?: string[];
}

export interface ModelContext extends EventTarget {
  registerTool(def: WebMCPToolDef, options?: RegisterToolOptions): Promise<void>;
  getTools(options?: GetToolsOptions): Promise<RegisteredTool[]>;
  executeTool(
    tool: RegisteredTool | { name: string },
    args?: Record<string, unknown>,
    options?: ToolExecuteOptions,
  ): Promise<ToolResultEnvelope>;
}

const POLYFILL_FLAG = "__sightkickPolyfill";

/** Minimal spec-shaped polyfill of document.modelContext. */
class ModelContextPolyfill extends EventTarget implements ModelContext {
  readonly [POLYFILL_FLAG] = true;
  private tools = new Map<string, WebMCPToolDef>();

  registerTool(def: WebMCPToolDef, options?: RegisterToolOptions): Promise<void> {
    this.tools.set(def.name, def);
    if (options?.signal) {
      options.signal.addEventListener(
        "abort",
        () => {
          if (this.tools.get(def.name) === def) {
            this.tools.delete(def.name);
            this.dispatchEvent(new Event("toolchange"));
          }
        },
        { once: true },
      );
    }
    this.dispatchEvent(new Event("toolchange"));
    return Promise.resolve();
  }

  getTools(): Promise<RegisteredTool[]> {
    const origin = typeof location !== "undefined" ? location.origin : "null";
    return Promise.resolve(
      [...this.tools.values()].map((t) => ({
        name: t.name,
        description: t.description,
        inputSchema: t.inputSchema,
        origin,
      })),
    );
  }

  executeTool(tool: RegisteredTool | { name: string }, args: Record<string, unknown> = {}, options?: ToolExecuteOptions): Promise<ToolResultEnvelope> {
    const def = this.tools.get(tool.name);
    if (!def) {
      return Promise.reject(new Error(`unknown tool "${tool.name}"`));
    }
    return Promise.resolve(def.execute(args, options));
  }
}

/**
 * Return the page's ModelContext, installing the polyfill on `document` if the
 * browser has none. Returns undefined only in a non-DOM context.
 */
export function ensureModelContext(): ModelContext | undefined {
  if (typeof document === "undefined") return undefined;
  const doc = document as unknown as { modelContext?: ModelContext };
  if (doc.modelContext) return doc.modelContext;
  const poly = new ModelContextPolyfill();
  Object.defineProperty(doc, "modelContext", { value: poly, configurable: true, writable: true });
  return poly;
}

/** True when the active modelContext is sightkick's polyfill (not native). */
export function isPolyfilled(ctx: ModelContext | undefined): boolean {
  return !!ctx && (ctx as unknown as Record<string, unknown>)[POLYFILL_FLAG] === true;
}

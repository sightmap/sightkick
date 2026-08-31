import { ensureModelContext, type ModelContext, type RegisteredTool, type ToolResultEnvelope } from "./webmcp.js";
import { describeError } from "./errors.js";

/**
 * A minimal in-page WebMCP consumer — exactly what a browser/built-in agent does:
 * discover tools with getTools(), invoke with executeTool(). We use it to test
 * and demo that sightkick's tools are genuinely WebMCP-native (driven through the
 * standard surface, not a private back door). The future sidebar CUA builds on
 * this.
 */
export interface SightkickClient {
  listTools(): Promise<RegisteredTool[]>;
  callTool(
    name: string,
    args?: Record<string, unknown>,
    options?: { signal?: AbortSignal },
  ): Promise<ToolResultEnvelope>;
}

export function createClient(ctx: ModelContext | undefined = ensureModelContext()): SightkickClient {
  if (!ctx) throw new Error("createClient: no document.modelContext available");
  return {
    async listTools() {
      try {
        return await ctx.getTools();
      } catch (e) {
        throw new Error(`getTools failed: ${describeError(e)}`);
      }
    },
    async callTool(name, args = {}, options) {
      const tools = await ctx.getTools();
      const tool = tools.find((t) => t.name === name);
      if (!tool) throw new Error(`unknown tool "${name}"`);
      // Chrome's native document.modelContext returns the CallToolResult as a
      // JSON string; our polyfill returns it as an object. Normalize to an
      // object so every consumer sees one shape.
      let raw: unknown;
      try {
        raw = await ctx.executeTool(tool, args, options);
      } catch (e) {
        throw new Error(`executeTool "${name}" failed: ${describeError(e)}`);
      }
      return (typeof raw === "string" ? (JSON.parse(raw) as ToolResultEnvelope) : (raw as ToolResultEnvelope));
    },
  };
}

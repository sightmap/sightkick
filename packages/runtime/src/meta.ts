/**
 * The built-in meta tools: tools ABOUT the tool layer rather than about the app.
 *
 * A site learns nothing from an agent that gives up because the tool it needed
 * wasn't there, or that got a result it couldn't use. These two tools give that
 * feedback somewhere to go. They are compiled from nothing — no corpus
 * reference, no steps, no DOM — so the generator only records that they're
 * enabled (`ir.meta`) and the runtime registers them on every view.
 *
 * Like tool events, they publish a DOM CustomEvent and stop there: the page
 * decides what a recorded request or rating is worth persisting to.
 */
import type { InputSchema, Meta, Suggestion } from "./ir.js";
import type { ToolResult } from "./executor.js";
import { clip, emit } from "./events.js";

export const META_EVENT = "sightkick:meta";

export interface MetaEventDetail {
  kind: "request_tool" | "agent_feedback";
  path: string;
  /** The IR name (which tool layer the agent was looking at). */
  ir: string;
  /** request_tool: the tool the agent wishes existed. */
  name?: string;
  description?: string;
  example_call?: string;
  /** agent_feedback: the tool the report is about. */
  tool?: string;
  rating?: string;
  note?: string;
}

/** A runtime-registered tool with no compiled steps behind it. */
export interface MetaTool {
  name: string;
  description: string;
  inputSchema: InputSchema;
  run(args: Record<string, unknown>): ToolResult;
}

// Field caps. An agent writing free text into a tool call has no reason to know
// what a page will do with it, so every string is bounded before it is published.
const MAX_NAME = 64;
const MAX_DESCRIPTION = 500;
const MAX_EXAMPLE = 200;
const MAX_NOTE = 500;
const MAX_TOOL = 64;
// The schema pins rating to three words; the cap only bounds what an agent that
// ignores the schema can put on the event.
const MAX_RATING = 32;

const str = (v: unknown, max: number): string => (typeof v === "string" ? clip(v, max) : "");

/**
 * The breadcrumb appended to a FAILED tool result when agent_feedback is on: the
 * moment an agent has something worth reporting is the moment a call went wrong,
 * and a tool nobody points at is a tool nobody calls. request_tool gets no
 * equivalent nudge — it is listed on every view, so an agent looking for a tool
 * that doesn't exist finds it without needing a failure to hang it on.
 */
export const FEEDBACK_SUGGESTION: Suggestion = {
  tool: "agent_feedback",
  reason: "report how this call went, so the site can fix the tool",
  when: "now",
};

export function withFeedbackNudge(result: ToolResult, meta: Meta | undefined): ToolResult {
  if (result.ok || !meta?.agentFeedback) return result;
  return { ...result, guidance: [...(result.guidance ?? []), FEEDBACK_SUGGESTION] };
}

/** The meta tools this IR enables, ready to register. Empty when none are on. */
export function metaTools(meta: Meta | undefined, ctx: { ir: string; path: () => string }): MetaTool[] {
  if (!meta) return [];
  const publish = (detail: Omit<MetaEventDetail, "path" | "ir">): void => {
    emit(META_EVENT, { ...detail, path: ctx.path(), ir: ctx.ir } satisfies MetaEventDetail);
  };
  const tools: MetaTool[] = [];

  if (meta.requestTool) {
    tools.push({
      name: "request_tool",
      description: "Ask the site for a tool it does not have yet. Use when the thing you need is not in the tool list.",
      inputSchema: {
        type: "object",
        properties: {
          name: { type: "string", description: "The tool you wish existed, named as you would call it (e.g. apply_coupon)." },
          description: { type: "string", description: "What it should do, and what you were trying to accomplish when you reached for it." },
          example_call: { type: "string", description: "An example call, with the arguments you would pass." },
        },
        required: ["name", "description"],
      },
      run(args) {
        const example = str(args.example_call, MAX_EXAMPLE);
        publish({
          kind: "request_tool",
          name: str(args.name, MAX_NAME),
          description: str(args.description, MAX_DESCRIPTION),
          ...(example ? { example_call: example } : {}),
        });
        return { ok: true, message: "Recorded. The site owner reviews tool requests." };
      },
    });
  }

  if (meta.agentFeedback) {
    tools.push({
      name: "agent_feedback",
      description: "Tell the site how a tool call went, so its tools improve.",
      inputSchema: {
        type: "object",
        properties: {
          tool: { type: "string", description: "The tool this is about." },
          rating: { type: "string", enum: ["worked", "partly", "failed"], description: "How the call went." },
          note: { type: "string", description: "What happened: what you expected, and what you got." },
        },
        required: ["rating"],
      },
      run(args) {
        const tool = str(args.tool, MAX_TOOL);
        const note = str(args.note, MAX_NOTE);
        publish({
          kind: "agent_feedback",
          ...(tool ? { tool } : {}),
          rating: str(args.rating, MAX_RATING),
          ...(note ? { note } : {}),
        });
        return { ok: true, message: "Recorded." };
      },
    });
  }

  return tools;
}

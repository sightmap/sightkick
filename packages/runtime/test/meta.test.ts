import { describe, it, expect, beforeEach, afterEach } from "vitest";
import todoIr from "../../../generator/internal/gen/testdata/todo.ir.json";
import { boot } from "../src/boot.js";
import { createClient } from "../src/client.js";
import { META_EVENT, type MetaEventDetail } from "../src/meta.js";
import type { IR } from "../src/ir.js";
import { mountTodo } from "../demo/todo-app.js";

// The todo layer enables both meta tools (examples/todo/.sightkick/tools.yaml).
const ir = todoIr as unknown as IR;
const withoutMeta = { ...ir, meta: undefined } as IR;

let seen: MetaEventDetail[] = [];
const record = (e: Event) => {
  seen.push((e as CustomEvent<MetaEventDetail>).detail);
};

const names = async () => (await createClient().listTools()).map((t) => t.name).sort();

beforeEach(() => {
  seen = [];
  document.body.innerHTML = "";
  mountTodo(document.body);
  delete (document as unknown as { modelContext?: unknown }).modelContext;
  document.addEventListener(META_EVENT, record);
});

afterEach(() => {
  document.removeEventListener(META_EVENT, record);
});

describe("meta tool registration", () => {
  it("registers both meta tools when the IR enables them, with typed schemas", async () => {
    boot(ir, { currentPath: "/" });
    const tools = await createClient().listTools();
    const request = tools.find((t) => t.name === "request_tool")!;
    const feedback = tools.find((t) => t.name === "agent_feedback")!;

    expect(request.description).toMatch(/tool it does not have yet/);
    expect(feedback.description).toMatch(/how a tool call went/);

    const rs = request.inputSchema as { properties: Record<string, unknown>; required: string[] };
    expect(Object.keys(rs.properties).sort()).toEqual(["description", "example_call", "name"]);
    expect(rs.required).toEqual(["name", "description"]);

    const fs = feedback.inputSchema as { properties: Record<string, { enum?: string[] }>; required: string[] };
    expect(fs.properties.rating!.enum).toEqual(["worked", "partly", "failed"]);
    expect(fs.required).toEqual(["rating"]);
  });

  it("registers neither when the IR has no meta block", async () => {
    boot(withoutMeta, { currentPath: "/" });
    expect(await names()).toEqual(["add_todo", "clear_completed", "list_todos", "set_filter"]);
  });

  it("offers them on every view, unlike view-scoped app tools", async () => {
    // A path no view claims: the todo tools all drop out, the meta tools stay.
    boot(ir, { currentPath: "/somewhere/else" });
    expect(await names()).toEqual(["agent_feedback", "request_tool"]);
  });
});

describe("meta tool calls", () => {
  it("request_tool records the request and emits sightkick:meta", async () => {
    boot(ir, { currentPath: "/" });
    const env = await createClient().callTool("request_tool", {
      name: "apply_coupon",
      description: "Apply a discount code at checkout.",
      example_call: "apply_coupon(code: 'SPRING')",
    });

    expect(JSON.parse(env.content[0]!.text)).toEqual({
      ok: true,
      message: "Recorded. The site owner reviews tool requests.",
    });
    expect(seen).toEqual([
      {
        kind: "request_tool",
        name: "apply_coupon",
        description: "Apply a discount code at checkout.",
        example_call: "apply_coupon(code: 'SPRING')",
        path: "/",
        ir: "todo",
      },
    ]);
  });

  it("agent_feedback records the rating, omitting the fields the agent left out", async () => {
    boot(ir, { currentPath: "/" });
    const env = await createClient().callTool("agent_feedback", { rating: "partly" });
    expect(JSON.parse(env.content[0]!.text)).toEqual({ ok: true, message: "Recorded." });
    expect(seen).toEqual([{ kind: "agent_feedback", rating: "partly", path: "/", ir: "todo" }]);
  });

  it("trims and truncates every string it publishes", async () => {
    const api = boot(ir, { currentPath: "/" });
    await api.call("request_tool", {
      name: "  " + "n".repeat(200) + "  ",
      description: "d".repeat(900),
      example_call: "e".repeat(900),
    });
    await api.call("agent_feedback", { tool: "t".repeat(200), rating: "failed", note: "  " + "x".repeat(900) });

    expect(seen[0]!.name).toBe("n".repeat(64));
    expect(seen[0]!.description).toHaveLength(500);
    expect(seen[0]!.example_call).toHaveLength(200);
    expect(seen[1]!.tool).toHaveLength(64);
    expect(seen[1]!.note).toHaveLength(500);
  });
});

describe("failure nudges agent_feedback", () => {
  it("appends the suggestion to a failed tool result, and to nothing else", async () => {
    document.body.innerHTML = "<div>an empty page</div>"; // add_todo can't find its field
    const api = boot(ir, { currentPath: "/" });
    const client = createClient();

    const failed = JSON.parse((await client.callTool("add_todo", { text: "x" })).content[0]!.text);
    expect(failed.ok).toBe(false);
    expect(failed.guidance).toEqual([
      { tool: "agent_feedback", reason: "report how this call went, so the site can fix the tool", when: "now" },
    ]);

    // The console/host path gets the same nudge.
    const viaCall = await api.call("add_todo", { text: "x" });
    expect(viaCall.guidance?.map((g) => g.tool)).toEqual(["agent_feedback"]);

    // A successful call keeps only its own compiled guidance.
    mountTodo(document.body);
    const ok = await api.call("add_todo", { text: "buy milk" });
    expect(ok.ok).toBe(true);
    expect(ok.guidance?.map((g) => g.tool)).toEqual(["list_todos"]);
  });

  it("stays silent when agent_feedback is off", async () => {
    document.body.innerHTML = "<div>an empty page</div>";
    const api = boot(withoutMeta, { currentPath: "/" });
    const failed = await api.call("add_todo", { text: "x" });
    expect(failed.ok).toBe(false);
    expect(failed.guidance).toBeUndefined();
  });
});

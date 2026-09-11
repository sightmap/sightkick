import { describe, it, expect, beforeEach, afterEach } from "vitest";
import todoIr from "../../../generator/internal/gen/testdata/todo.ir.json";
import { boot } from "../src/boot.js";
import { createClient } from "../src/client.js";
import { runTool } from "../src/executor.js";
import { TOOL_EVENT, type ToolEventDetail } from "../src/events.js";
import type { IR, Tool } from "../src/ir.js";
import { mountTodo } from "../demo/todo-app.js";

const ir = todoIr as unknown as IR;

let seen: ToolEventDetail[] = [];
const record = (e: Event) => {
  seen.push((e as CustomEvent<ToolEventDetail>).detail);
};

beforeEach(() => {
  seen = [];
  document.body.innerHTML = "";
  mountTodo(document.body);
  delete (document as unknown as { modelContext?: unknown }).modelContext;
  document.addEventListener(TOOL_EVENT, record);
});

afterEach(() => {
  document.removeEventListener(TOOL_EVENT, record);
});

describe("sightkick:tool events", () => {
  it("a call through the polyfilled modelContext emits start then end", async () => {
    boot(ir, { currentPath: "/" });
    const env = await createClient().callTool("add_todo", { text: "buy milk" });
    expect(JSON.parse(env.content[0]!.text).ok).toBe(true);

    expect(seen.map((d) => d.phase)).toEqual(["start", "end"]);
    const [start, end] = seen as [ToolEventDetail, ToolEventDetail];

    expect(start).toMatchObject({
      phase: "start",
      tool: "add_todo",
      via: "modelContext",
      argKeys: ["text"],
      path: "/",
      polyfilled: true,
      ir: "todo",
    });
    // The start event states the call, not its outcome.
    expect(start.ok).toBeUndefined();
    expect(start.durationMs).toBeUndefined();

    expect(end).toMatchObject({
      phase: "end",
      tool: "add_todo",
      callId: start.callId, // both halves of one call
      via: "modelContext",
      ok: true,
      skipped: false,
      argKeys: ["text"],
      path: "/",
      polyfilled: true,
      ir: "todo",
    });
    expect(end.error).toBeUndefined();
    expect(typeof end.durationMs).toBe("number");
    expect(end.durationMs).toBeGreaterThanOrEqual(0);
  });

  it("carries argument names but never argument or result values", async () => {
    boot(ir, { currentPath: "/" });
    await createClient().callTool("add_todo", { text: "1600 Pennsylvania Ave" });
    // The value the agent typed, and the value the tool read back, are both in
    // the tool's own result — and in neither event.
    const serialized = JSON.stringify(seen);
    expect(serialized).not.toContain("Pennsylvania");
    expect(seen.every((d) => d.argKeys.join() === "text")).toBe(true);
  });

  it("reports a failure with a truncated message and ok:false", async () => {
    // A locator this long only exists to overrun the 200-char error cap; the
    // step fails because nothing matches it.
    const locator = ".missing-" + "x".repeat(400);
    const failing = {
      name: "doomed",
      mode: "live",
      inputSchema: { type: "object", properties: {} },
      steps: [{ op: "click", query: { parts: [{ locators: [locator] }] } }],
    } as unknown as Tool;

    const res = await runTool(failing, {}, { pollMs: 5 });
    expect(res.ok).toBe(false);

    const end = seen.at(-1)!;
    expect(end).toMatchObject({ phase: "end", tool: "doomed", via: "call", ok: false, skipped: false });
    expect(end.error).toHaveLength(200);
    expect(res.message!.length).toBeGreaterThan(200);
    expect(res.message!.startsWith(end.error!)).toBe(true);
  });

  it("distinguishes the console/host path from the WebMCP one", async () => {
    const api = boot(ir, { currentPath: "/" });
    await api.call("list_todos");
    expect(seen.map((d) => d.via)).toEqual(["call", "call"]);
  });
});

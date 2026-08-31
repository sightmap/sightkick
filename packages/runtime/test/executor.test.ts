import { describe, it, expect, beforeEach } from "vitest";
// The runtime consumes the generator's real output: this JSON is the golden IR
// the Go generator emits for examples/todo. Importing it here tests across the
// IR firewall end-to-end.
import todoIr from "../../../generator/internal/gen/testdata/todo.ir.json";
import { boot } from "../src/index.js";
import { runTool } from "../src/executor.js";
import type { IR, Tool } from "../src/ir.js";
import { mountTodo } from "../demo/todo-app.js";

const ir = todoIr as unknown as IR;
const tool = (name: string): Tool => {
  const t = ir.tools.find((x) => x.name === name);
  if (!t) throw new Error(`no tool ${name}`);
  return t;
};

const fast = { pollMs: 5 };

beforeEach(() => {
  document.body.innerHTML = "";
  mountTodo(document.body);
});

describe("live executor on the todo fixture", () => {
  it("add_todo fills, clicks, verifies, and returns the new row text", async () => {
    const res = await runTool(tool("add_todo"), { text: "buy milk" }, fast);
    expect(res.ok).toBe(true);
    expect(res.value).toBe("buy milk");
    const texts = Array.from(document.querySelectorAll(".todo-item-text")).map((e) => e.textContent);
    expect(texts).toContain("buy milk");
  });

  it("list_todos collects rows via the compiled extractor", async () => {
    const res = await runTool(tool("list_todos"), {}, fast);
    expect(res.ok).toBe(true);
    expect(res.items?.map((r) => r.text)).toEqual([
      "Write the runtime",
      "Test on the todo app",
      "Ship it",
    ]);
  });

  it("set_filter clicks the filter matched by its label (where-clause)", async () => {
    const res = await runTool(tool("set_filter"), { filter: "Completed" }, fast);
    expect(res.ok).toBe(true);
    const active = document.querySelector(".filter-bar-filter.active");
    expect(active?.textContent).toBe("Completed");
    // Only the completed seed item ("Test on the todo app") should remain visible.
    const visible = Array.from(document.querySelectorAll(".todo-item-text")).map((e) => e.textContent);
    expect(visible).toEqual(["Test on the todo app"]);
  });

  it("clear_completed removes completed rows", async () => {
    const res = await runTool(tool("clear_completed"), {}, fast);
    expect(res.ok).toBe(true);
    const texts = Array.from(document.querySelectorAll(".todo-item-text")).map((e) => e.textContent);
    expect(texts).toEqual(["Write the runtime", "Ship it"]);
  });

  it("reports failure when a target is absent", async () => {
    document.body.innerHTML = "<div>empty</div>";
    const res = await runTool(tool("add_todo"), { text: "x" }, { ...fast });
    expect(res.ok).toBe(false);
    expect(res.message).toMatch(/fill: no element/);
  });

  it("surfaces compiled journey guidance in the tool result", async () => {
    const res = await runTool(tool("add_todo"), { text: "buy milk" }, fast);
    expect(res.guidance?.[0]).toMatchObject({ tool: "list_todos", when: "now", reason: "see the todo you just added" });
    // A tool in no journey carries none.
    const cleared = await runTool(tool("clear_completed"), {}, fast);
    expect(cleared.guidance).toBeUndefined();
  });
});

describe("boot / console driver", () => {
  it("exposes tools and calls them by name", async () => {
    const api = boot(ir);
    expect(api.mode).toBe("direct");
    expect(api.tools().map((t) => t.name).sort()).toEqual([
      "add_todo",
      "clear_completed",
      "list_todos",
      "set_filter",
    ]);
    const res = await api.call("add_todo", { text: "via boot" }, fast);
    expect(res.ok).toBe(true);
    expect(res.value).toBe("via boot");
  });

  it("returns an error for an unknown tool", async () => {
    const api = boot(ir);
    const res = await api.call("nope");
    expect(res.ok).toBe(false);
    expect(res.message).toMatch(/unknown tool/);
  });
});

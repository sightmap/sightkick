import { describe, it, expect, beforeEach } from "vitest";
import todoIr from "../../../generator/internal/gen/testdata/todo.ir.json";
import { boot } from "../src/boot.js";
import { createClient } from "../src/client.js";
import type { ModelContext } from "../src/webmcp.js";
import type { IR } from "../src/ir.js";
import { mountTodo } from "../demo/todo-app.js";

const ir = todoIr as unknown as IR;

beforeEach(() => {
  document.body.innerHTML = "";
  mountTodo(document.body);
  // Reset the polyfilled surface so each test boots a fresh modelContext.
  delete (document as unknown as { modelContext?: unknown }).modelContext;
});

describe("WebMCP-native registration", () => {
  it("installs a polyfilled document.modelContext in standalone mode", () => {
    const api = boot(ir);
    expect(api.mode).toBe("direct");
    expect(api.polyfilled).toBe(true);
    expect((document as unknown as { modelContext?: unknown }).modelContext).toBeDefined();
  });

  it("a WebMCP client discovers the tools via getTools()", async () => {
    boot(ir);
    const client = createClient();
    const tools = await client.listTools();
    expect(tools.map((t) => t.name).sort()).toEqual(["add_todo", "clear_completed", "list_todos", "set_filter"]);
    const add = tools.find((t) => t.name === "add_todo")!;
    expect(add.description).toMatch(/Add a new todo/);
    expect((add.inputSchema as any).required).toEqual(["text"]);
    expect(add.origin).toBeDefined();
  });

  it("executeTool runs the tool through the standard surface and mutates the UI", async () => {
    boot(ir);
    const client = createClient();
    const env = await client.callTool("add_todo", { text: "buy milk" });
    // MCP-style envelope.
    expect(env.content[0]!.type).toBe("text");
    const result = JSON.parse(env.content[0]!.text);
    expect(result).toMatchObject({ ok: true, value: "buy milk" });
    // The real DOM changed.
    const texts = Array.from(document.querySelectorAll(".todo-item-text")).map((e) => e.textContent);
    expect(texts).toContain("buy milk");
  });

  it("marks failures with isError in the envelope", async () => {
    document.body.innerHTML = "<div>empty</div>";
    boot(ir);
    const client = createClient();
    const env = await client.callTool("add_todo", { text: "x" });
    expect(env.isError).toBe(true);
    expect(JSON.parse(env.content[0]!.text).ok).toBe(false);
  });

  it("client normalizes a native-style stringified executeTool result to an object", async () => {
    // Chrome's native modelContext returns the CallToolResult as a JSON string.
    const envelope = { content: [{ type: "text", text: '{"ok":true}' }], isError: false };
    const nativeish = {
      getTools: async () => [{ name: "t" }],
      executeTool: async () => JSON.stringify(envelope), // string, like native
    } as unknown as ModelContext;
    const client = createClient(nativeish);
    const res = await client.callTool("t");
    expect(res).toEqual(envelope); // parsed back into an object
    expect(res.content[0]!.text).toBe('{"ok":true}');
  });

  it("fires toolchange on registration and honors dynamic unregistration", async () => {
    const api = boot(); // no IR yet
    let changes = 0;
    api.modelContext!.addEventListener("toolchange", () => changes++);

    api.load(ir);
    expect(changes).toBeGreaterThanOrEqual(1); // one per registered tool
    expect((await createClient().listTools()).length).toBe(4);

    // Reloading with an empty tool set unregisters everything (via AbortSignals).
    api.load({ ...ir, tools: [] });
    expect((await createClient().listTools()).length).toBe(0);
  });
});

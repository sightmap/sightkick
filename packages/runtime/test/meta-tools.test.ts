import { describe, it, expect, beforeEach } from "vitest";
import { boot } from "../src/boot.js";
import type { IR, Query } from "../src/ir.js";
import type { ModelContext as MC } from "../src/webmcp.js";

// sites-b573: the always-on fallback surface — exec_actions (run a resumable list
// of fragment refs) and get_fragments (list the refs available here). Driven via
// the native/polyfill modelContext exactly as an agent would.

function q(sel: string): Query {
  return { parts: [{ locators: [sel] }] };
}

const flowIr: IR = {
  version: 1,
  name: "t",
  views: [{ name: "Checkout", route: "/checkout" }],
  tools: [
    {
      name: "flow",
      mode: "live",
      inputSchema: { type: "object", properties: {} },
      steps: [
        { op: "waitFor", query: q("#a"), timeoutMs: 500, target: "A" },
        { op: "waitFor", query: q("#b"), timeoutMs: 40, target: "B" },
        { op: "waitFor", query: q("#a"), timeoutMs: 500, target: "A-again" },
      ],
    },
    {
      name: "checkout_only",
      mode: "live",
      ensureView: { view: "Checkout", route: "/checkout" },
      inputSchema: { type: "object", properties: {} },
      steps: [{ op: "click", query: q("#x"), target: "Submit" }],
    },
  ],
};

async function exec(ctx: MC, name: string, args: Record<string, unknown> = {}) {
  const env = await ctx.executeTool({ name }, args);
  return { isError: !!env.isError, payload: JSON.parse(env.content[0]!.text) };
}

// The meta tools register after an async getTools() dedup pass (skips names
// already present in the shared registry across execution contexts), so their
// registration completes a tick after boot()/load(). Real agents poll getTools()
// well after load; tests flush a macrotask to reach the same steady state.
function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

beforeEach(() => {
  document.body.innerHTML = "";
  delete (document as unknown as { modelContext?: unknown }).modelContext;
});

describe("meta tools (exec_actions / get_fragments)", () => {
  it("registers exec_actions and get_fragments as always-on tools once an IR loads", async () => {
    const api = boot(flowIr, { currentPath: "/" });
    await settle();
    const names = (await (api.modelContext as MC).getTools()).map((t) => t.name);
    expect(names).toContain("exec_actions");
    expect(names).toContain("get_fragments");
    // The view-scoped tool without ensure_view registers on every view too.
    expect(names).toContain("flow");
  });

  it("get_fragments returns the current view's base set (ensure_view filtered)", async () => {
    const api = boot(flowIr, { currentPath: "/" });
    await settle();
    const { payload } = await exec(api.modelContext as MC, "get_fragments");
    const tools = new Set(payload.fragments.map((f: { tool: string }) => f.tool));
    expect(tools.has("flow")).toBe(true);
    // checkout_only is scoped to /checkout, so it's absent on "/".
    expect(tools.has("checkout_only")).toBe(false);
    // Fragments are pluckable refs with semantic labels.
    expect(payload.fragments[0]).toMatchObject({ id: "flow.0", tool: "flow", op: "waitFor", label: "waitFor A" });
  });

  it("get_fragments includes a tool's fragments when on its view", async () => {
    const api = boot(flowIr, { currentPath: "/checkout" });
    await settle();
    const { payload } = await exec(api.modelContext as MC, "get_fragments");
    const tools = new Set(payload.fragments.map((f: { tool: string }) => f.tool));
    expect(tools.has("checkout_only")).toBe(true);
  });

  it("exec_actions runs a fragment-ref list, interrupts cleanly, and resumes on the remaining refs", async () => {
    document.body.innerHTML = '<div id="a"></div>'; // #a present, #b absent
    const api = boot(flowIr, { currentPath: "/" });
    await settle();

    const r1 = await exec(api.modelContext as MC, "exec_actions", { refs: ["flow.0", "flow.1", "flow.2"] });
    // Interrupted at #b: not done, stops instead of hanging, remaining as REFS.
    expect(r1.isError).toBe(true);
    expect(r1.payload.done).toBe(false);
    expect(r1.payload.completedThrough).toBe(1);
    expect(r1.payload.interrupt.at).toBe(1);
    expect(r1.payload.remaining).toEqual(["flow.1", "flow.2"]);
    // The readable tail is semantic (target labels from B-lite), aligned with the ref tail.
    expect(r1.payload.remainingView[0]).toEqual({ op: "waitFor", target: "B" });

    // Handle the interruption (the awaited element appears), then resume.
    document.body.innerHTML = '<div id="a"></div><div id="b"></div>';
    const r2 = await exec(api.modelContext as MC, "exec_actions", { refs: r1.payload.remaining });
    expect(r2.isError).toBe(false);
    expect(r2.payload.done).toBe(true);
    expect(r2.payload.remaining).toEqual([]);
  });

  it("exec_actions rejects unknown fragment refs with a helpful error", async () => {
    const api = boot(flowIr, { currentPath: "/" });
    await settle();
    const { isError, payload } = await exec(api.modelContext as MC, "exec_actions", { refs: ["flow.0", "nope.9"] });
    expect(isError).toBe(true);
    expect(payload.error).toContain("nope.9");
  });
});

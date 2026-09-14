import { describe, it, expect, beforeEach, vi } from "vitest";
// The runtime consumes the generator's real output: this JSON is the golden IR
// the Go generator emits for examples/todo. Importing it here tests across the
// IR firewall end-to-end.
import todoIr from "../../../generator/internal/gen/testdata/todo.ir.json";
import { boot } from "../src/index.js";
import { execActions, projectFragments, runTool } from "../src/executor.js";
import type { IR, Query, Step, Tool } from "../src/ir.js";
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

describe("waitFor step (view: route form)", () => {
  // These use the runtime's live window.location, not opts.currentPath's
  // frozen-per-call snapshot — a route-form waitFor exists specifically to
  // observe a client-side route change (real history.pushState) happening
  // DURING the tool's own execution, which a fixed test override can't
  // simulate. See the search example's "cross-view guided flow" test for the
  // (deliberately different) frozen-per-boot model this doesn't replace.
  beforeEach(() => {
    history.replaceState({}, "", "/");
  });

  it("resolves once the live route matches, without touching the DOM", async () => {
    const waitTool = {
      name: "go",
      steps: [{ op: "waitFor", route: "/other", timeoutMs: 1000 }],
    } as unknown as Tool;

    const done = runTool(waitTool, {}, fast);
    // The route hasn't changed yet — the step should still be polling.
    await new Promise((r) => setTimeout(r, 20));
    history.pushState({}, "", "/other");
    const res = await done;
    expect(res.ok).toBe(true);
  });

  it("times out if the route never changes", async () => {
    const waitTool = {
      name: "go",
      steps: [{ op: "waitFor", route: "/never", timeoutMs: 50 }],
    } as unknown as Tool;
    const res = await runTool(waitTool, {}, fast);
    expect(res.ok).toBe(false);
    expect(res.message).toMatch(/timed out.*route \/never/);
  });
});

describe("keypress step", () => {
  it("dispatches a real key event at document.activeElement", async () => {
    document.body.innerHTML = `<input id="q" />`;
    const input = document.querySelector("#q") as HTMLInputElement;
    input.focus();
    let seenKey = "";
    input.addEventListener("keydown", (e) => {
      seenKey = (e as KeyboardEvent).key;
    });
    const pressTool = { name: "press", steps: [{ op: "keypress", key: "Enter" }] } as unknown as Tool;
    const res = await runTool(pressTool, {}, fast);
    expect(res.ok).toBe(true);
    expect(seenKey).toBe("Enter");
  });

  it("fails with no key given", async () => {
    const pressTool = { name: "press", steps: [{ op: "keypress" }] } as unknown as Tool;
    const res = await runTool(pressTool, {}, fast);
    expect(res.ok).toBe(false);
    expect(res.message).toMatch(/keypress: no key/);
  });
});

describe("L1 resumable executor (execActions)", () => {
  const q = (sel: string): Query => ({ parts: [{ locators: [sel] }] });

  beforeEach(() => {
    history.replaceState({}, "", "/cart");
    document.body.innerHTML = `
      <button id="continue">Continue to checkout</button>
      <button id="second">Second</button>
    `;
  });

  it("stops at the first unresolved action, returns observed state + the untried tail, then resumes", async () => {
    let secondClicks = 0;
    document.querySelector("#second")!.addEventListener("click", () => {
      secondClicks++;
    });

    // click (ok) -> waitFor a route that never comes (a modal is blocking) ->
    // click. Mirrors a18d: the interposed modal wedges the middle wait.
    const actions: Step[] = [
      { op: "click", query: q("#continue") },
      { op: "waitFor", route: "/checkout", timeoutMs: 50 },
      { op: "click", query: q("#second") },
    ];

    const first = await execActions(actions, {}, fast);
    expect(first.done).toBe(false);
    expect(first.completedThrough).toBe(1); // only the first click completed
    expect(first.interrupt?.at).toBe(1);
    expect(first.interrupt?.reason).toMatch(/timed out.*route \/checkout/);
    expect(first.interrupt?.observed.path).toBe("/cart");
    // The tail begins with the interrupted action, so a resume retries it.
    expect(first.remaining).toHaveLength(2);
    expect(first.remaining[0]).toBe(actions[1]);
    expect(secondClicks).toBe(0); // the action after the wait never ran

    // Handle the interrupt (dismiss modal -> navigation), then resume with the tail.
    history.pushState({}, "", "/checkout");
    const resumed = await execActions(first.remaining, {}, fast);
    expect(resumed.done).toBe(true);
    expect(resumed.completedThrough).toBe(2);
    expect(resumed.remaining).toEqual([]);
    expect(secondClicks).toBe(1);
  });

  it("runs a clean list straight through to done", async () => {
    const actions: Step[] = [
      { op: "click", query: q("#continue") },
      { op: "click", query: q("#second") },
    ];
    const res = await execActions(actions, {}, fast);
    expect(res.done).toBe(true);
    expect(res.completedThrough).toBe(2);
    expect(res.interrupt).toBeUndefined();
    expect(res.remaining).toEqual([]);
    expect(res.remainingView).toEqual([]);
  });

  // sites-be76: the tail/interrupt is projected to a readable {op, target} form —
  // the legend that makes the raw compiled tail intelligible without parsing
  // locator JSON. It KEEPS the predicate discriminators (the author's semantic
  // handle), and stays index-aligned with the raw `remaining` used for resume.
  it("projects a readable, predicate-preserving view of the tail, aligned with remaining", async () => {
    const dismiss: Step = {
      op: "click",
      query: {
        parts: [
          { locators: [".jtpsdk-popup-modal"] },
          {
            locators: [".jtpsdk-popup-modal button"],
            preds: [{ property: "label", extractor: { kind: "raw_text" }, op: "*=", value: "flights only", ci: true }],
          },
        ],
      },
    };
    const actions: Step[] = [
      { op: "click", query: q("#continue") },
      { op: "waitFor", query: q("#never-appears"), timeoutMs: 50 }, // no match -> interrupt
      dismiss,
    ];

    const res = await execActions(actions, {}, fast);
    expect(res.done).toBe(false);
    expect(res.interrupt?.at).toBe(1);
    // The interrupt summary is readable, not a locator-array dump.
    expect(res.interrupt?.action).toBe("waitFor #never-appears");
    // remainingView parallels remaining 1:1 and renders predicates compactly.
    expect(res.remainingView).toHaveLength(res.remaining.length);
    expect(res.remainingView[0]).toEqual({ op: "waitFor", target: "#never-appears" });
    expect(res.remainingView[1]).toEqual({
      op: "click",
      target: '.jtpsdk-popup-modal .jtpsdk-popup-modal button[label*="flights only" i]',
    });
  });
});

// sites-7eba: fragment provenance index. Tool steps are a parts bin an agent
// composes execActions from; projectFragments makes each step pluckable by a
// stable id and by the params it uses, without string-matching interpolations.
describe("fragment index (projectFragments / fragments())", () => {
  const contactIr: IR = {
    version: 1,
    name: "t",
    views: [],
    tools: [
      {
        name: "set_contact",
        mode: "live",
        inputSchema: { type: "object", properties: {} },
        steps: [
          {
            op: "fill",
            query: {
              parts: [
                {
                  locators: ["input.first"],
                  preds: [{ property: "label", extractor: { kind: "raw_text" }, op: "=", value: "First name" }],
                },
              ],
            },
            value: "{{firstName}}",
          },
          { op: "click", query: { parts: [{ locators: ["button.save"] }] } },
        ],
      },
    ],
  };

  it("projects each tool step to a pluckable {id, tool, op, label, uses}", () => {
    const frags = projectFragments(contactIr);
    expect(frags).toHaveLength(2);
    // Pluck by the param it fills, not by matching value === '{{firstName}}'.
    expect(frags[0]).toMatchObject({ id: "set_contact.0", tool: "set_contact", index: 0, op: "fill", uses: ["firstName"] });
    // The label keeps the authored predicate discriminator, so it reads clearly.
    expect(frags[0]!.label).toContain('[label="First name"]');
    expect(frags[1]).toMatchObject({ id: "set_contact.1", op: "click", uses: [] });
  });

  it("SightkickGlobal.fragments() is empty before load and populated after", () => {
    const api = boot(undefined, { currentPath: "/" });
    expect(api.fragments()).toEqual([]);
    api.load(contactIr);
    const frags = api.fragments();
    expect(frags).toHaveLength(2);
    expect(frags.every((f) => f.id.startsWith(f.tool + "."))).toBe(true);
  });
});

describe("goto step", () => {
  it("navigates to the interpolated url, deferred until after the tool returns", async () => {
    const assign = vi.spyOn(window.location, "assign").mockImplementation(() => {});
    vi.useFakeTimers();
    try {
      const gotoTool = {
        name: "deep_link",
        steps: [{ op: "goto", url: "https://x.test/r?from={{origin}}&to={{destination}}" }],
      } as unknown as Tool;

      const res = await runTool(gotoTool, { origin: "JFK", destination: "LAX" }, fast);

      expect(res.ok).toBe(true);
      // Terminal navigation is deferred a tick so the result is delivered first.
      expect(assign).not.toHaveBeenCalled();
      vi.runAllTimers();
      expect(assign).toHaveBeenCalledWith("https://x.test/r?from=JFK&to=LAX");
    } finally {
      vi.useRealTimers();
      assign.mockRestore();
    }
  });
});

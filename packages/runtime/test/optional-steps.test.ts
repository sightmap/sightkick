import { describe, it, expect, beforeEach } from "vitest";
import { runTool } from "../src/executor.js";
import type { Tool } from "../src/ir.js";

const fast = { pollMs: 5 };

// A two-field setter: `a` required, `b` optional. Each step interpolates its own
// param, so the `b` step should auto-skip when `b` is omitted.
function fillTool(): Tool {
  return {
    name: "set_pair",
    mode: "live",
    inputSchema: {
      type: "object",
      properties: { a: { type: "string" }, b: { type: "string" } },
      required: ["a"],
    },
    steps: [
      { op: "fill", query: { parts: [{ locators: ["#a"] }] }, value: "{{a}}" },
      { op: "fill", query: { parts: [{ locators: ["#b"] }] }, value: "{{b}}" },
    ],
  };
}

const val = (sel: string) => (document.querySelector(sel) as HTMLInputElement).value;

beforeEach(() => {
  document.body.innerHTML = '<input id="a" /><input id="b" value="pre" />';
});

describe("optional / skippable steps", () => {
  it("auto-skips a step whose interpolated param was omitted", async () => {
    const res = await runTool(fillTool(), { a: "1" }, fast);
    expect(res.ok).toBe(true);
    expect(val("#a")).toBe("1");
    // `b` omitted -> its fill step is skipped, so #b keeps its prior value.
    expect(val("#b")).toBe("pre");
  });

  it("runs the step when the optional param is provided", async () => {
    const res = await runTool(fillTool(), { a: "1", b: "2" }, fast);
    expect(res.ok).toBe(true);
    expect(val("#b")).toBe("2");
  });

  it("treats an explicit empty string as a real value (runs, not skipped)", async () => {
    const res = await runTool(fillTool(), { a: "1", b: "" }, fast);
    expect(res.ok).toBe(true);
    // The step ran and cleared #b (would still read "pre" had it been skipped).
    expect(val("#b")).toBe("");
  });

  it("honors an explicit `when` guard", async () => {
    document.body.innerHTML = '<button id="btn"></button>';
    const clicks: string[] = [];
    document.querySelector("#btn")!.addEventListener("click", () => clicks.push("x"));
    const t: Tool = {
      name: "maybe_click",
      mode: "live",
      inputSchema: { type: "object", properties: { go: { type: "string" } } },
      steps: [{ op: "click", query: { parts: [{ locators: ["#btn"] }] }, when: "{{go}}" }],
    };
    await runTool(t, {}, fast); // go absent -> when interpolates empty -> skip
    expect(clicks.length).toBe(0);
    await runTool(t, { go: "yes" }, fast); // present -> click runs
    expect(clicks.length).toBe(1);
  });
});

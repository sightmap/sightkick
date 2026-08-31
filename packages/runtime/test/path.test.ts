import { describe, it, expect, beforeEach } from "vitest";
import { resolvePath } from "../src/dom.js";
import type { PathPart } from "../src/ir.js";

// Mirrors burrito's OptionGroup > OptionButton, where "none" repeats across groups.
const group = (name: string): PathPart => ({
  locators: [".option-group"],
  preds: [{ property: "name", extractor: { kind: "text", within: ".option-group-label" }, op: "=", value: name }],
});
const button = (value: string, op: "=" | "^=" | "*=" = "=", ci = false): PathPart => ({
  locators: [".option-button"],
  preds: [{ extractor: { kind: "text" }, op, value, ci }],
});

beforeEach(() => {
  document.body.innerHTML = `
    <div class="option-group"><div class="option-group-label">Rice</div>
      <button class="option-button">white</button>
      <button class="option-button">none</button>
    </div>
    <div class="option-group"><div class="option-group-label">Beans</div>
      <button class="option-button">black</button>
      <button class="option-button">none</button>
    </div>`;
});

describe("resolvePath — compquery scoping", () => {
  it("disambiguates a repeated child label by its parent (the 'none' case)", () => {
    const riceNone = resolvePath([group("Rice"), button("none")], {});
    expect(riceNone).toHaveLength(1);
    expect(riceNone[0]!.closest(".option-group")!.querySelector(".option-group-label")!.textContent).toBe("Rice");

    // Unscoped, "none" is ambiguous across both groups.
    expect(resolvePath([button("none")], {})).toHaveLength(2);
  });

  it("interpolates params in predicate values", () => {
    const part: PathPart = { locators: [".option-button"], preds: [{ extractor: { kind: "text" }, op: "=", value: "{{opt}}" }] };
    const el = resolvePath([group("Beans"), part], { opt: "black" });
    expect(el.map((e) => e.textContent)).toEqual(["black"]);
  });

  it("supports prefix/substring + case-insensitive ops", () => {
    expect(resolvePath([button("wh", "^=")], {}).map((e) => e.textContent)).toEqual(["white"]);
    expect(resolvePath([button("LAC", "*=", true)], {}).map((e) => e.textContent)).toEqual(["black"]);
  });
});

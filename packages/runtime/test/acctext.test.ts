import { describe, it, expect, beforeEach } from "vitest";
import { extract } from "../src/dom.js";

// An <input> has no innerText; its accessible name comes from a label. The
// text extractor must follow aria-labelledby and native <label> association so
// a text predicate matches the same value the lib's offline node.Name resolves.
describe("accessibleText — input label association", () => {
  beforeEach(() => {
    document.body.innerHTML = `
      <span id="lbl-depart">Depart</span>
      <input id="depart" aria-labelledby="lbl-depart" value="Tue, Sep 15" />

      <label for="ret">Return</label>
      <input id="ret" value="Tue, Sep 22" />

      <input id="plain" aria-label="Promo code" />`;
  });

  const text = (sel: string) =>
    extract(document.querySelector(sel)!, { kind: "text" });

  it("resolves aria-labelledby to the referenced element's text", () => {
    expect(text("#depart")).toBe("Depart");
  });

  it("resolves a native <label for> association", () => {
    expect(text("#ret")).toBe("Return");
  });

  it("still prefers aria-label when present", () => {
    expect(text("#plain")).toBe("Promo code");
  });
});

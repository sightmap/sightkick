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

// raw_text is the counterpart to text: the node's OWN literal text (direct text
// nodes), the deterministic escape when the accessible name welds in CSS/aria
// text. Mirrors the sightmap lib's offline node.RawText.
describe("raw_text — the node's own literal text", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
  });
  const raw = (el: Element) => extract(el, { kind: "raw_text" });
  const txt = (el: Element) => extract(el, { kind: "text" });

  it("returns only the element's own direct text nodes, excluding descendant text", () => {
    // Analog of the JetBlue fare-tile h3 whose visible badge text welds onto the
    // accessible name (there via a CSS ::after, here via a descendant span).
    const h = document.createElement("h3");
    h.appendChild(document.createTextNode(" Main "));
    const badge = document.createElement("span");
    badge.textContent = "Most popular";
    h.appendChild(badge);
    document.body.appendChild(h);
    expect(raw(h)).toBe("Main"); // own text only, normalized
    expect(txt(h)).toContain("Most popular"); // the name welds the badge in
  });

  it("normalizes whitespace runs and trims the ends", () => {
    const el = document.createElement("div");
    el.appendChild(document.createTextNode("  Main   Base \n "));
    document.body.appendChild(el);
    expect(raw(el)).toBe("Main Base");
  });

  it("omits (empty) when the node has no own text", () => {
    const el = document.createElement("div");
    const child = document.createElement("span");
    child.textContent = "child";
    el.appendChild(child);
    document.body.appendChild(el);
    expect(raw(el)).toBe("");
  });
});

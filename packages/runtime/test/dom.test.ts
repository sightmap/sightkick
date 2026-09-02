import { describe, it, expect, beforeEach } from "vitest";
import { typeInto } from "../src/dom.js";

beforeEach(() => {
  document.body.innerHTML = "";
});

describe("typeInto", () => {
  it("focuses the field and types character by character, ending with the full value", () => {
    const el = document.createElement("input");
    document.body.appendChild(el);
    const keydowns: string[] = [];
    const inputs: string[] = [];
    el.addEventListener("keydown", (e) => keydowns.push((e as KeyboardEvent).key));
    el.addEventListener("input", () => inputs.push(el.value));

    typeInto(el, "JFK");

    expect(document.activeElement).toBe(el);
    expect(el.value).toBe("JFK");
    // one keydown per character; no ArrowDown for a plain (non-combobox) input.
    expect(keydowns).toEqual(["J", "F", "K"]);
    // the leading "" is the clear; then the value builds up one char at a time —
    // this incremental, focused edit sequence is what a controlled combobox needs.
    expect(inputs).toEqual(["", "J", "JF", "JFK"]);
  });

  it("nudges a role=combobox open with a trailing ArrowDown", () => {
    const el = document.createElement("input");
    el.setAttribute("role", "combobox");
    document.body.appendChild(el);
    const keydowns: string[] = [];
    el.addEventListener("keydown", (e) => keydowns.push((e as KeyboardEvent).key));

    typeInto(el, "LAX");

    expect(keydowns).toEqual(["L", "A", "X", "ArrowDown"]);
  });

  it("clears prior content so a repeated fill does not accumulate", () => {
    const el = document.createElement("input");
    el.value = "old";
    document.body.appendChild(el);

    typeInto(el, "new");

    expect(el.value).toBe("new");
  });
});

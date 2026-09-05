import { describe, it, expect, beforeEach } from "vitest";
import { clickElement } from "../src/dom.js";

// Stub document.elementFromPoint for the duration of fn (happy-dom has no layout).
// fn is awaited so the stub stays in place until clickElement's async hit-test
// loop resolves.
async function withHit(hit: Element | null, fn: () => Promise<void>): Promise<void> {
  const orig = (document as unknown as { elementFromPoint?: unknown }).elementFromPoint;
  (document as unknown as { elementFromPoint: () => Element | null }).elementFromPoint = () => hit;
  try {
    await fn();
  } finally {
    (document as unknown as { elementFromPoint?: unknown }).elementFromPoint = orig;
  }
}

beforeEach(() => {
  document.body.innerHTML = "";
});

describe("clickElement", () => {
  it("dispatches at the hit-tested inner element — a wrapper-dispatch would miss its handler", async () => {
    // Mirrors <jb-select-option> (resolved node) > div.body (real click target).
    const outer = document.createElement("div");
    const inner = document.createElement("div");
    outer.appendChild(inner);
    document.body.appendChild(outer);
    const got: string[] = [];
    inner.addEventListener("mousedown", () => got.push("mousedown"));
    inner.addEventListener("click", () => got.push("click"));

    // hit resolves into the target subtree on frame 1 → dispatches immediately.
    await withHit(inner, () => clickElement(outer));

    expect(got).toContain("mousedown");
    expect(got).toContain("click");
  });

  it("falls back to the resolved node when the hit point never resolves into it", async () => {
    const el = document.createElement("button");
    document.body.appendChild(el);
    const got: string[] = [];
    el.addEventListener("click", () => got.push("click"));

    // body is not within el, so the hit-test never agrees; after the retry budget
    // it falls back to dispatching on the node itself.
    await withHit(document.body, () => clickElement(el));

    expect(got).toContain("click");
  });
});

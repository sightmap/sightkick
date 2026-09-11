import { describe, it, expect, beforeEach } from "vitest";
import { clickElement, clickElementAtPoint } from "../src/dom.js";

// Stub document.elementFromPoint for the duration of fn (happy-dom has no layout).
// fn is awaited so the stub stays in place until the async hit-test loop resolves.
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

// The default path: dispatch the press sequence + click DIRECTLY on the element,
// with no coordinate hit-testing. This is what makes it robust for offscreen /
// mid-scroll targets (sites-02d5) — there is no elementFromPoint that can return
// null or a wrong node and drop the click.
describe("clickElement (default — dispatch on the element)", () => {
  it("dispatches the full press sequence then a click, on the element itself", async () => {
    const el = document.createElement("button");
    document.body.appendChild(el);
    const got: string[] = [];
    for (const type of ["pointerdown", "mousedown", "pointerup", "mouseup", "click"]) {
      el.addEventListener(type, () => got.push(type));
    }

    await clickElement(el);

    expect(got).toEqual(["pointerdown", "mousedown", "pointerup", "mouseup", "click"]);
  });

  it("reaches an inner-child handler by subtree descent — outer-node dispatch would miss it", async () => {
    // Mirrors <jb-select-option> (resolved node) > div.body (real click target):
    // the handler is on the inner child, so we must descend to it. This uses each
    // node's own rect, NOT elementFromPoint.
    const outer = document.createElement("div");
    const inner = document.createElement("div");
    outer.appendChild(inner);
    document.body.appendChild(outer);
    const got: string[] = [];
    inner.addEventListener("mousedown", () => got.push("mousedown"));
    inner.addEventListener("click", () => got.push("click"));

    await clickElement(outer);

    expect(got).toContain("mousedown");
    expect(got).toContain("click");
  });

  it("clicks even when elementFromPoint resolves to null (offscreen) — sites-02d5 guard", async () => {
    // The far-scroll failure: an option whose centre hit-tests to null must still
    // get the click. The default path never consults elementFromPoint (it descends
    // by own-rect), so a null hit-test cannot drop it.
    const el = document.createElement("div");
    document.body.appendChild(el);
    const got: string[] = [];
    el.addEventListener("click", () => got.push("click"));

    await withHit(null, () => clickElement(el));

    expect(got).toContain("click");
  });
});

// The precise, coordinate-targeted variant — preserved for inner-child / portal
// cases and opted into deliberately. It DOES hit-test and dispatch on the topmost
// node at the centre point.
describe("clickElementAtPoint (precise — coordinate hit-tested)", () => {
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
    await withHit(inner, () => clickElementAtPoint(outer));

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
    await withHit(document.body, () => clickElementAtPoint(el));

    expect(got).toContain("click");
  });

  it("does not hang when requestAnimationFrame is starved (throttled/wedged renderer)", async () => {
    const el = document.createElement("button");
    document.body.appendChild(el);
    const got: string[] = [];
    el.addEventListener("click", () => got.push("click"));

    // Simulate a renderer that has stopped painting: rAF still EXISTS but never
    // invokes its callback. The settle loop's 300ms deadline is only checked
    // between frames, so without nextFrame's timer fallback the `await nextFrame()`
    // would never resolve and it would hang forever (this test would time out).
    // With the fallback the loop keeps turning, hits its deadline, and falls back
    // to dispatching on the node. (sightkick-a381)
    const origRaf = globalThis.requestAnimationFrame;
    globalThis.requestAnimationFrame = (() => 0) as unknown as typeof requestAnimationFrame;
    try {
      // hit never resolves into el, so the loop must run to its wall-clock deadline.
      await withHit(document.body, () => clickElementAtPoint(el));
    } finally {
      globalThis.requestAnimationFrame = origRaf;
    }

    expect(got).toContain("click");
  });
});

import { describe, it, expect } from "vitest";
import { whenBootable, type ReadyOptions } from "../src/autoboot.js";

// A controllable document/window/clock so the readiness gate can be driven
// deterministically without real timers or a real page lifecycle.
function harness(init: {
  readyState: DocumentReadyState;
  modelContext?: boolean;
}) {
  let readyState = init.readyState;
  let modelContext: unknown = init.modelContext ? {} : undefined;
  let clock = 0;
  const timers: Array<{ at: number; cb: () => void }> = [];
  const listeners: Record<string, Array<() => void>> = {};

  const on = (obj: Record<string, unknown>) => {
    obj.addEventListener = (type: string, cb: () => void) => {
      (listeners[type] ??= []).push(cb);
    };
  };
  const doc = { get readyState() { return readyState; }, get modelContext() { return modelContext; } } as Record<string, unknown>;
  const win = { setTimeout: (cb: () => void, ms: number) => { timers.push({ at: clock + ms, cb }); } } as Record<string, unknown>;
  on(doc);
  on(win);

  const opts: ReadyOptions = {
    doc: doc as unknown as Document,
    win: win as unknown as Window & typeof globalThis,
    now: () => clock,
    settleMs: 800,
    nonNativeGraceMs: 500,
    pollMs: 50,
  };

  return {
    opts,
    setReady(s: DocumentReadyState) { readyState = s; },
    setModelContext(present: boolean) { modelContext = present ? {} : undefined; },
    emit(type: string) { (listeners[type] ?? []).forEach((cb) => cb()); },
    /** Advance the fake clock, firing due timers (which may reschedule polls). */
    advance(ms: number) {
      const target = clock + ms;
      for (;;) {
        const next = timers.filter((t) => t.at <= target).sort((a, b) => a.at - b.at)[0];
        if (!next) break;
        timers.splice(timers.indexOf(next), 1);
        clock = next.at;
        next.cb();
      }
      clock = target;
    },
  };
}

const POLL = 50;

describe("whenBootable readiness gate (sites-67f3)", () => {
  it("boots synchronously when the document is already loaded (live inject / direct install)", () => {
    const h = harness({ readyState: "complete", modelContext: true });
    let booted = 0;
    whenBootable(() => booted++, h.opts);
    expect(booted).toBe(1);
  });

  it("boots synchronously when the document is interactive", () => {
    const h = harness({ readyState: "interactive" });
    let booted = 0;
    whenBootable(() => booted++, h.opts);
    expect(booted).toBe(1);
  });

  it("at document_start on a native page, waits for load AND settle before booting", () => {
    const h = harness({ readyState: "loading", modelContext: true });
    let booted = 0;
    whenBootable(() => booted++, h.opts);
    // Native surface is up, but the document is still loading: must NOT register yet
    // (registering mid-bootstrap crashes the renderer).
    expect(booted).toBe(0);
    h.advance(2000);
    expect(booted).toBe(0);

    // Page finishes loading → the settle window opens; still must not boot until it elapses.
    h.setReady("complete");
    h.emit("load");
    expect(booted).toBe(0);
    h.advance(800 + POLL); // settle + one poll
    expect(booted).toBe(1);
  });

  it("does not begin the settle until the native surface is present (native page)", () => {
    const h = harness({ readyState: "loading" });
    let booted = 0;
    whenBootable(() => booted++, h.opts);
    // Loaded but native surface not up yet, still inside the non-native grace.
    h.setReady("complete");
    h.emit("load");
    h.advance(400);
    expect(booted).toBe(0);
    // Native surface appears within the grace → settle starts now, boots settle later.
    h.setModelContext(true);
    h.advance(POLL);
    expect(booted).toBe(0); // settle only just started
    h.advance(800);
    expect(booted).toBe(1);
  });

  it("on a non-native page, boots on the polyfill after load + grace + settle", () => {
    const h = harness({ readyState: "loading" }); // never gets a native surface
    let booted = 0;
    whenBootable(() => booted++, h.opts);
    h.setReady("complete");
    h.emit("load");
    // Needs grace (500) to conclude non-native, then settle (800).
    h.advance(500);
    expect(booted).toBe(0);
    h.advance(800 + POLL);
    expect(booted).toBe(1);
  });

  it("boots exactly once", () => {
    const h = harness({ readyState: "loading", modelContext: true });
    let booted = 0;
    whenBootable(() => booted++, h.opts);
    h.setReady("complete");
    h.emit("load");
    h.emit("DOMContentLoaded");
    h.advance(5000);
    expect(booted).toBe(1);
  });
});

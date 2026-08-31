import { describe, it, expect, beforeEach } from "vitest";
import { boot } from "../src/boot.js";
import { createClient } from "../src/client.js";
import { installIrChannel, IR_ATTR, HOST_ATTR, IR_EVENT } from "../src/channel.js";
import type { IR } from "../src/ir.js";
import searchIr from "../../../generator/internal/gen/testdata/search.ir.json";

const ir = searchIr as unknown as IR;

beforeEach(() => {
  document.body.innerHTML = "";
  document.documentElement.removeAttribute(IR_ATTR);
  document.documentElement.removeAttribute(HOST_ATTR);
  delete (document as unknown as { modelContext?: unknown }).modelContext;
});

describe("IR DOM channel (document_start injection handover)", () => {
  it("boots with no IR, then loads what the bridge posts via <html> attrs + event", async () => {
    // Runtime came up first (as a document_start content script would): no IR yet.
    const api = boot(undefined, { currentPath: "/" });
    expect(api.ir).toBeNull();
    expect(api.mode).toBe("direct");
    installIrChannel(api);

    // Bridge (isolated world) hands the IR over on the shared DOM.
    document.documentElement.setAttribute(IR_ATTR, JSON.stringify(ir));
    document.documentElement.setAttribute(HOST_ATTR, JSON.stringify({ corpus: "search-demo" }));
    document.dispatchEvent(new Event(IR_EVENT));

    // Loaded synchronously in the event handler: view-scoped tools for "/" appear,
    // and provenance flips the mode to injected.
    expect(api.ir?.name).toBe("search");
    expect(api.mode).toBe("injected");
    const client = createClient(api.modelContext!);
    expect((await client.listTools()).map((t) => t.name)).toEqual(["search"]);

    // Payload is consumed so a second reader can't double-load.
    expect(document.documentElement.getAttribute(IR_ATTR)).toBeNull();
  });

  it("handles the bridge posting BEFORE the runtime installs the channel", async () => {
    document.documentElement.setAttribute(IR_ATTR, JSON.stringify(ir));
    const api = boot(undefined, { currentPath: "/" });
    installIrChannel(api); // initial read picks it up immediately, no event needed
    expect(api.ir?.name).toBe("search");
  });
});

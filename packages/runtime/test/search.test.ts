import { describe, it, expect, beforeEach } from "vitest";
import { boot } from "../src/boot.js";
import { createClient } from "../src/client.js";
import type { IR } from "../src/ir.js";
// The generator's real output for examples/search — exercises view-scoped
// registration, cross-view (after_navigation) guidance, and rich returns.
import searchIr from "../../../generator/internal/gen/testdata/search.ir.json";

const ir = searchIr as unknown as IR;

const pages: Record<string, (nav: (url: string) => void) => void> = {
  "/": (nav) => {
    document.body.innerHTML = `<input id="q" /><button id="go" type="button">Search</button>`;
    document.querySelector("#go")!.addEventListener("click", () => nav("/results"));
  },
  "/results": () => {
    document.body.innerHTML = `
      <button id="sort" type="button">Sort: relevance</button>
      <div class="result" data-id="r1"><span class="result-title">Alpha</span><span class="result-price">$10</span><button class="result-select" data-id="r1" type="button">Select</button></div>
      <div class="result" data-id="r2"><span class="result-title">Beta</span><span class="result-price">$20</span><button class="result-select" data-id="r2" type="button">Select</button></div>
      <div id="sel"></div>`;
    document.querySelectorAll(".result-select").forEach((btn) =>
      btn.addEventListener("click", () => {
        const row = btn.closest(".result")!;
        const id = btn.getAttribute("data-id")!;
        const title = row.querySelector(".result-title")!.textContent;
        document.querySelector("#sel")!.innerHTML =
          `<div class="selection" data-selected="${id}">Selected: ${title}</div><button class="book-button" type="button">Book</button>`;
        document.querySelector(".book-button")!.addEventListener("click", () => {
          const conf = document.createElement("div");
          conf.className = "booking-confirmation";
          conf.setAttribute("data-ref", "BK-" + id.toUpperCase());
          conf.textContent = "Booked! Ref BK-" + id.toUpperCase();
          document.body.appendChild(conf);
        });
      }),
    );
  },
};

let path = "/";
const nav = (url: string) => {
  path = new URL(url, "http://sim.test").pathname;
  document.body.innerHTML = "";
  pages[path]!(nav);
};

function loadPage() {
  delete (document as unknown as { modelContext?: unknown }).modelContext;
  const api = boot(ir, { currentPath: path });
  return { api, client: createClient(api.modelContext!) };
}

beforeEach(() => {
  path = "/";
  document.body.innerHTML = "";
  delete (document as unknown as { modelContext?: unknown }).modelContext;
});

describe("cross-view guided flow (generator -> runtime)", () => {
  it("registers only the current view's tools, and guides across the navigation", async () => {
    nav("/");

    // Search page: view-scoped registration exposes only `search`.
    let page = loadPage();
    expect((await page.client.listTools()).map((t) => t.name)).toEqual(["search"]);

    // Running search fills the box, clicks the real button (which navigates),
    // and its result carries after_navigation guidance toward the results view.
    const env = await page.client.callTool("search", { query: "alpha" });
    const searched = JSON.parse(env.content[0]!.text);
    expect(searched.guidance).toEqual([
      { tool: "list_results", reason: "read the results the search produced", when: "after_navigation", view: "Results" },
    ]);
    expect(path).toBe("/results");

    // Results page (fresh boot): now only the results-view tools are offered.
    page = loadPage();
    expect((await page.client.listTools()).map((t) => t.name).sort()).toEqual([
      "book",
      "list_results",
      "select_flight",
      "set_sort",
    ]);

    // Rich return: machine ids + human-readable fields.
    const listed = JSON.parse((await page.client.callTool("list_results")).content[0]!.text);
    expect(listed.items).toEqual([
      { id: "r1", title: "Alpha", price: "$10" },
      { id: "r2", title: "Beta", price: "$20" },
    ]);
  });
});

describe("SPA route change re-registers view-scoped tools (no reload)", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    delete (document as unknown as { modelContext?: unknown }).modelContext;
    try {
      history.replaceState({}, "", "/");
    } catch {
      /* non-DOM */
    }
  });

  it("swaps the tool set on pushState", async () => {
    const api = boot(ir); // live window.location + nav hook (no fixed currentPath)
    const client = createClient(api.modelContext!);
    expect((await client.listTools()).map((t) => t.name)).toEqual(["search"]);

    // Client-side navigation: the patched pushState fires the nav hook, which
    // re-evaluates view-scoped registration for the new path.
    history.pushState({}, "", "/results");
    expect((await client.listTools()).map((t) => t.name).sort()).toEqual([
      "book",
      "list_results",
      "select_flight",
      "set_sort",
    ]);
  });
});

describe("mutating flow: per-tool idempotency guards skip re-applied effects", () => {
  it("select_flight then book are each idempotent (2nd call is a skipped no-op)", async () => {
    nav("/results");
    const page = loadPage();

    // select_flight r1: no .selection yet -> guard doesn't hold -> runs steps,
    // returns the selected summary.
    const sel1 = JSON.parse((await page.client.callTool("select_flight", { flight_id: "r1" })).content[0]!.text);
    expect(sel1.ok).toBe(true);
    expect(sel1.skipped).toBeUndefined();
    expect(sel1.value).toContain("Alpha");

    // Selecting the same flight again: guard (present .selection[data-selected=r1])
    // holds -> steps skipped, current state returned unchanged.
    const sel2 = JSON.parse((await page.client.callTool("select_flight", { flight_id: "r1" })).content[0]!.text);
    expect(sel2.skipped).toBe(true);
    expect(sel2.value).toContain("Alpha");

    // book: no .booking-confirmation yet -> runs, returns the reference.
    const book1 = JSON.parse((await page.client.callTool("book")).content[0]!.text);
    expect(book1.ok).toBe(true);
    expect(book1.skipped).toBeUndefined();
    expect(book1.value).toBe("BK-R1");

    // Re-book: .booking-confirmation present -> skipped, same reference.
    const book2 = JSON.parse((await page.client.callTool("book")).content[0]!.text);
    expect(book2.skipped).toBe(true);
    expect(book2.value).toBe("BK-R1");
  });
});

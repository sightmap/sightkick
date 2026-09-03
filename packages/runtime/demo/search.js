// A single-document SPA (Search view at "/", Results view at "/results") wired to
// the generator's compiled search IR. Client-side routing via history.pushState;
// the runtime's nav hook re-registers the view-scoped tools on each route change
// (no reload), so the tool set changes as the agent moves between views.
// Import the modules directly (not ../src/index.js) so the entry's auto-boot
// side effect doesn't run — we boot explicitly below.
import { boot } from "../src/boot.js";
import { createClient } from "../src/client.js";
import ir from "../../../generator/internal/gen/testdata/search.ir.json";

const DATA = [
  { id: "f1", title: "Alpha Air — 8:00am nonstop", price: "$211" },
  { id: "f2", title: "Beta Jet — 11:20am 1 stop", price: "$180" },
  { id: "f3", title: "Gamma Wings — 6:45pm nonstop", price: "$264" },
];

let query = sessionStorage.getItem("search.q") ?? "";
let sortAsc = true;
let selectedId = null; // drives .selection[data-selected]
let bookingRef = null; // drives .booking-confirmation[data-ref]

function results() {
  const items = DATA.slice();
  items.sort((a, b) => (sortAsc ? 1 : -1) * (parseInt(a.price.slice(1)) - parseInt(b.price.slice(1))));
  return items;
}

// The selection + booking area (#sel) is re-rendered on its own so a select
// click doesn't rebuild the whole results list, and so it survives a re-sort.
function renderSelection() {
  const sel = document.querySelector("#sel");
  if (!sel) return;
  if (!selectedId) {
    sel.innerHTML = "";
    return;
  }
  const item = DATA.find((d) => d.id === selectedId);
  sel.innerHTML =
    `<div class="selection" data-selected="${selectedId}">Selected: ${item ? item.title : selectedId}</div>` +
    `<button class="book-button" type="button">Book</button>` +
    (bookingRef ? `<div class="booking-confirmation" data-ref="${bookingRef}">Booked! Ref ${bookingRef}</div>` : "");
  sel.querySelector(".book-button").addEventListener("click", () => {
    bookingRef = "BK-" + selectedId.toUpperCase();
    renderSelection();
  });
}

function render() {
  const app = document.querySelector("#app");
  if (location.pathname === "/results") {
    app.innerHTML =
      `<h1>Results for "${query}"</h1>` +
      `<button id="sort" type="button">Sort: price ${sortAsc ? "↑" : "↓"}</button>` +
      results()
        .map(
          (i) =>
            `<div class="result" data-id="${i.id}"><span class="result-title">${i.title}</span><span class="result-price">${i.price}</span><button class="result-select" data-id="${i.id}" type="button">Select</button></div>`,
        )
        .join("") +
      `<div id="sel"></div>`;
    app.querySelector("#sort").addEventListener("click", () => {
      sortAsc = !sortAsc;
      render();
    });
    app.querySelectorAll(".result-select").forEach((btn) =>
      btn.addEventListener("click", () => {
        selectedId = btn.getAttribute("data-id");
        bookingRef = null; // a fresh selection isn't booked yet
        renderSelection();
      }),
    );
    renderSelection();
  } else {
    app.innerHTML = `<h1>Flight search</h1><input id="q" placeholder="e.g. ATL to LHR" value="${query}" /><button id="go" type="button">Search</button>`;
    app.querySelector("#go").addEventListener("click", () => {
      query = app.querySelector("#q").value;
      sessionStorage.setItem("search.q", query);
      history.pushState({}, "", "/results"); // fires the runtime nav hook
      render();
    });
  }
}

// App rendering + client-side routing always run; this is the "site".
window.addEventListener("sightkick:navigate", render);
window.addEventListener("popstate", render);
render();

// ?noboot simulates a third-party page that does NOT self-install sightkick, so
// an injector (`sightmap browser eval`) can load the very same IR + runtime
// instead. It's the same page and paths, which is the point: injected vs. direct
// is identical.
if (location.search.includes("noboot")) {
  console.log("[sightkick] ?noboot — not self-installing; waiting for injection.");
} else {
  boot(ir);
  wirePanel();
}

// --- agent panel (drive tools without an external agent) --------------------
function wirePanel() {
const client = createClient();
const out = document.querySelector("#out");
async function refreshTools() {
  document.querySelector("#tools").textContent = (await client.listTools()).map((t) => t.name).join(", ") || "(none)";
}
// executeTool's return shape can differ between the polyfill and a native
// document.modelContext, so render defensively: MCP text content if present,
// else the raw JSON (which also reveals an unexpected native shape).
async function show(label, promise) {
  out.textContent = `${label} …`;
  try {
    const env = await promise;
    const text = Array.isArray(env?.content)
      ? env.content.map((c) => c.text).join("\n")
      : JSON.stringify(env, null, 2);
    out.textContent = `${label} →\n${text}`;
  } catch (err) {
    out.textContent = `${label} ✗ ${err}`;
  }
  refreshTools();
}
document.querySelector("#p-search").addEventListener("click", () =>
  show("search", client.callTool("search", { query: document.querySelector("#p-q").value || "ATL to LHR" })),
);
document.querySelector("#p-list").addEventListener("click", () => show("list_results", client.callTool("list_results")));
document.querySelector("#p-sort").addEventListener("click", () => show("set_sort", client.callTool("set_sort")));
document.querySelector("#p-select").addEventListener("click", () =>
  show("select_flight", client.callTool("select_flight", { flight_id: document.querySelector("#p-fid").value || "f1" })),
);
document.querySelector("#p-book").addEventListener("click", () => show("book", client.callTool("book")));

window.addEventListener("sightkick:navigate", refreshTools);
window.addEventListener("popstate", refreshTools);
refreshTools();
}

console.log("[sightkick] search SPA ready. Try: (await document.modelContext.getTools()).map(t=>t.name)");

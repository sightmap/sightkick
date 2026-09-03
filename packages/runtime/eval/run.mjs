// Deterministic eval harness. Drives the self-contained search demo
// through the `sightmap browser` CLI the way an agent would — getTools() +
// executeTool() — and asserts the view-scoped tool set, guidance breadcrumbs,
// rich returns, and idempotency guards at each step. No LLM, no Playwright: the
// standalone runtime polyfills document.modelContext, so this is pure
// tools-in / DOM-out verification. Doubles as a reproducible demo.
//
// Usage:  node eval/run.mjs         (spawns serve.mjs if :5174 isn't up)
// Exit 0 on all-pass, 1 on any failure.
import { execFileSync, spawn } from "node:child_process";
import { rmSync } from "node:fs";
import http from "node:http";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const RUNTIME_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const PORT = 5174;
// serve.mjs is plain HTTP; the standalone runtime polyfills modelContext in JS,
// so no TLS is needed.
const BASE = `http://localhost:${PORT}`;
const PROFILE = "/tmp/sk-eval-profile";

// ---- tiny sync helpers ------------------------------------------------------
const sleep = (ms) => Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);

function sm(args, opts = {}) {
  return execFileSync("sightmap", ["browser", ...args], { encoding: "utf8", ...opts });
}

// The CLI prints a value JSON-encoded on the last line; peel that one layer.
function evalRaw(js) {
  const out = sm(["eval", js]).trim().split("\n").filter(Boolean);
  return JSON.parse(out[out.length - 1] ?? '""');
}

// Resolve a stashed global that a dispatched promise fills. `expr` must assign a
// STRING to window[name] ('RUN' sentinel while pending). Returns the settled
// string, or throws on an 'ERR:' payload / timeout.
function poll(name, dispatchJs, { tries = 50, ms = 200 } = {}) {
  evalRaw(`window.${name}='RUN'; ${dispatchJs}; 'go'`);
  for (let i = 0; i < tries; i++) {
    const v = evalRaw(`window.${name}`);
    if (v !== "RUN") {
      if (typeof v === "string" && v.startsWith("ERR:")) throw new Error(v.slice(4));
      return v;
    }
    sleep(ms);
  }
  throw new Error(`poll ${name} timed out`);
}

const path = () => evalRaw("location.pathname");

function toolNames() {
  const s = poll("__tn", "document.modelContext.getTools().then(ts=>{window.__tn=JSON.stringify(ts.map(t=>t.name))}).catch(e=>{window.__tn='ERR:'+e})");
  return JSON.parse(s).sort();
}

function callTool(name, args = {}) {
  const s = poll("__r", `document.modelContext.executeTool({name:${JSON.stringify(name)}},${JSON.stringify(args)}).then(r=>{window.__r=r.content[0].text}).catch(e=>{window.__r='ERR:'+e})`);
  return JSON.parse(s); // the ToolResult JSON the provider put in content[0].text
}

// ---- assertions -------------------------------------------------------------
const checks = [];
function check(label, fn) {
  try {
    fn();
    checks.push({ ok: true, label });
    console.log(`  \u2713 ${label}`);
  } catch (e) {
    checks.push({ ok: false, label, err: e.message });
    console.log(`  \u2717 ${label}\n      ${e.message}`);
  }
}
function eq(a, b, what) {
  const av = JSON.stringify(a), bv = JSON.stringify(b);
  if (av !== bv) throw new Error(`${what ?? "value"}: got ${av}, want ${bv}`);
}
function ok(cond, msg) {
  if (!cond) throw new Error(msg);
}

// ---- lifecycle --------------------------------------------------------------
function portUp(port) {
  return new Promise((res) => {
    const req = http.get(`http://localhost:${port}/`, (r) => { r.resume(); res(true); });
    req.on("error", () => res(false));
    req.setTimeout(500, () => { req.destroy(); res(false); });
  });
}

async function ensureServer() {
  if (await portUp(PORT)) return null; // already running (dev, HTTP): reuse it
  const proc = spawn("node", ["serve.mjs"], { cwd: RUNTIME_DIR, stdio: "ignore" });
  for (let i = 0; i < 60; i++) {
    if (await portUp(PORT)) return proc;
    await new Promise((r) => setTimeout(r, 500));
  }
  proc.kill();
  throw new Error("demo server did not come up on :" + PORT);
}

function startBrowser() {
  try { sm(["stop"], { stdio: "ignore" }); } catch { /* none running */ }
  rmSync(PROFILE, { recursive: true, force: true });
  // `start` is a blocking foreground daemon; --detach backgrounds it, waits
  // until it is serving, and returns (the script-safe form).
  sm(["start", "--detach", "--url", `${BASE}/`, "--profile", PROFILE], { stdio: "ignore" });
  // --detach returns once the daemon is serving, but the content tab can open a
  // beat later; page commands error with "no content tab" in that gap, so wait
  // for the tab (status lists it by URL) before issuing them.
  let tabReady = false;
  for (let i = 0; i < 40; i++) {
    try { if (sm(["status"]).includes(`:${PORT}`)) { tabReady = true; break; } } catch { /* not yet */ }
    sleep(250);
  }
  if (!tabReady) throw new Error("content tab did not open at :" + PORT);
  sm(["wait-for", "--selector", "#go", "--timeout-ms", "15000"], { stdio: "ignore" });
  // modelContext readiness (standalone boot is synchronous but be safe).
  for (let i = 0; i < 20; i++) {
    if (evalRaw("!!(document.modelContext&&document.modelContext.__sightkickPolyfill)") === true) break;
    sleep(300);
  }
}

// ---- the scripted flow ------------------------------------------------------
function run() {
  console.log("\nsightkick eval \u2014 search SPA (deterministic, tools-in/DOM-out)\n");

  console.log("view: / (Search)");
  check("only `search` is registered on the search view", () => eq(toolNames(), ["search"], "tools"));

  console.log("\ntool: search \u2192 navigates to Results");
  const searched = callTool("search", { query: "ATL to LHR" });
  check("search returns after_navigation guidance to list_results", () =>
    eq(searched.guidance, [{ tool: "list_results", reason: "read the results the search produced", when: "after_navigation", view: "Results" }], "guidance"));
  sm(["wait-for", "--selector", ".result", "--timeout-ms", "10000"], { stdio: "ignore" });
  check("client-side nav landed on /results", () => eq(path(), "/results", "path"));

  console.log("\nview: /results (Results)");
  check("results view re-registers its four tools", () =>
    eq(toolNames(), ["book", "list_results", "select_flight", "set_sort"], "tools"));

  console.log("\ntool: list_results \u2192 rich list return");
  const listed = callTool("list_results");
  check("items are price-sorted asc with {id,title,price}", () =>
    eq(listed.items?.map((r) => r.id), ["f2", "f1", "f3"], "ids"));
  check("each row carries the human fields", () => {
    ok(listed.items?.[0]?.title?.includes("Beta Jet"), "f2 title missing");
    ok(listed.items?.[0]?.price === "$180", "f2 price missing");
  });
  check("list_results guides to select_flight (same-view, now)", () =>
    eq(listed.guidance, [{ tool: "select_flight", reason: "pick a flight by its id", when: "now" }], "guidance"));

  console.log("\ntool: set_sort \u2192 folds the read into the mutation");
  const sorted = callTool("set_sort");
  check("re-sorted items come back desc (rich return, no extra call)", () =>
    eq(sorted.items?.map((r) => r.id), ["f3", "f1", "f2"], "ids"));
  check("set_sort emits no guidance (fold-in, not a breadcrumb)", () => ok(!sorted.guidance, "unexpected guidance"));

  console.log("\ntool: select_flight (idempotency guard)");
  const sel1 = callTool("select_flight", { flight_id: "f1" });
  check("first select runs and returns the summary", () => {
    ok(sel1.ok && !sel1.skipped, "should not be skipped");
    ok(sel1.value?.includes("Alpha Air"), `summary missing: ${sel1.value}`);
  });
  const sel2 = callTool("select_flight", { flight_id: "f1" });
  check("re-selecting the same flight is a skipped no-op", () => ok(sel2.skipped === true, "second select should skip"));

  console.log("\ntool: book (idempotency guard)");
  const book1 = callTool("book");
  check("first book runs and returns the reference", () => {
    ok(book1.ok && !book1.skipped, "should not be skipped");
    eq(book1.value, "BK-F1", "ref");
  });
  const book2 = callTool("book");
  check("re-booking is a skipped no-op with the same ref", () => {
    ok(book2.skipped === true, "second book should skip");
    eq(book2.value, "BK-F1", "ref");
  });
}

// ---- main -------------------------------------------------------------------
let server = null;
let failed = false;
try {
  server = await ensureServer();
  startBrowser();
  run();
} catch (e) {
  console.error(`\nharness error: ${e.message}`);
  failed = true;
} finally {
  try { sm(["stop"], { stdio: "ignore" }); } catch { /* ignore */ }
  rmSync(PROFILE, { recursive: true, force: true });
  if (server) server.kill();
}

const passed = checks.filter((c) => c.ok).length;
console.log(`\n${passed}/${checks.length} checks passed`);
if (failed || passed !== checks.length) process.exit(1);

/**
 * sightkick background service worker — the injector.
 *
 * The extension is ONLY a third-party injector: on a site an enabled corpus
 * matches, it injects the generated IR + the runtime bundle into the page's MAIN
 * world. The injected runtime behaves identically to a direct `<script>` install
 * (same boot, same view-scoped registration on document.modelContext, same
 * guidance-in-results). No host executor, no cross-page state; each document
 * boots fresh (which is what makes this resilient to full reloads across areas).
 *
 * Primary path: register a document_start MAIN-world content script (the runtime)
 * plus a document_start ISOLATED bridge (hands over the IR via the DOM channel).
 * Registering at document_start means the tools are on document.modelContext
 * before an agent's first getTools(). We keep chrome.scripting.executeScript for
 * "inject now" (enable on an already-open tab) and as a fallback if
 * registerContentScripts isn't available.
 */
import { pickCorpus, toChromeMatchPattern } from "./match.js";

interface CorpusMeta {
  id: string;
  name: string;
  description: string;
  match: string[];
  irFile: string;
  source: string;
  version: string;
  defaultEnabled?: boolean;
}

interface Provenance {
  source: string;
  corpus: string;
  version: string;
}

const CS_IDS = ["sightkick-bridge", "sightkick-main"];
let contentScriptsSupported = true;
let lastSyncStatus = "pending";

const url = (p: string) => chrome.runtime.getURL(p);

async function loadCorpora(): Promise<CorpusMeta[]> {
  return (await (await fetch(url("corpora/index.json"))).json()) as CorpusMeta[];
}

async function loadIr(meta: CorpusMeta): Promise<unknown> {
  return (await fetch(url("corpora/" + meta.irFile))).json();
}

/** Enabled map, seeded once from each corpus's defaultEnabled. */
async function getEnabled(): Promise<Record<string, boolean>> {
  const stored = (await chrome.storage.local.get("enabled")) as { enabled?: Record<string, boolean> };
  if (stored.enabled) return stored.enabled;
  const seed: Record<string, boolean> = {};
  for (const c of await loadCorpora()) seed[c.id] = !!c.defaultEnabled;
  await chrome.storage.local.set({ enabled: seed });
  return seed;
}

async function setEnabled(id: string, on: boolean): Promise<void> {
  const enabled = await getEnabled();
  enabled[id] = on;
  await chrome.storage.local.set({ enabled });
}

async function corpusForUrl(pageUrl: string): Promise<CorpusMeta | undefined> {
  const [corpora, enabled] = await Promise.all([loadCorpora(), getEnabled()]);
  return pickCorpus(
    corpora.filter((c) => enabled[c.id]),
    pageUrl,
  );
}

/**
 * (Re)register the document_start content scripts. Serialized: onInstalled and
 * onStartup (and popup toggles) can fire close together, and two concurrent
 * register calls interleave into a "Duplicate script ID" throw. Chaining makes
 * each run see a clean slate from the previous unregister.
 */
let syncChain: Promise<void> = Promise.resolve();
function syncContentScripts(): Promise<void> {
  syncChain = syncChain.then(doSyncContentScripts, doSyncContentScripts);
  return syncChain;
}

async function doSyncContentScripts(): Promise<void> {
  try {
    await chrome.storage.local.set({ syncStatus: "running" });
    const [corpora, enabled] = await Promise.all([loadCorpora(), getEnabled()]);
    const matches = [
      ...new Set(
        corpora
          .filter((c) => enabled[c.id])
          .flatMap((c) => c.match)
          .map(toChromeMatchPattern)
          .filter((m): m is string => m !== null),
      ),
    ];
    await chrome.scripting.unregisterContentScripts({ ids: CS_IDS }).catch(() => {});
    if (matches.length === 0) {
      contentScriptsSupported = true;
      lastSyncStatus = "no enabled corpora";
      await chrome.storage.local.set({ syncStatus: lastSyncStatus });
      return;
    }
    await chrome.scripting.registerContentScripts([
      {
        id: "sightkick-bridge",
        js: ["bridge.js"],
        matches,
        runAt: "document_start",
        world: "ISOLATED",
        persistAcrossSessions: true,
      },
      {
        id: "sightkick-main",
        js: ["sightkick-runtime.js"],
        matches,
        runAt: "document_start",
        world: "MAIN",
        persistAcrossSessions: true,
      },
    ]);
    contentScriptsSupported = true;
    lastSyncStatus = `registered: ${matches.join(", ")}`;
    console.info(`[sightkick] content scripts @document_start for: ${matches.join(", ")}`);
  } catch (e) {
    contentScriptsSupported = false;
    lastSyncStatus = `failed: ${String((e as Error)?.message ?? e)}`;
    console.warn(`[sightkick] registerContentScripts failed; falling back to executeScript on complete: ${String(e)}`);
  }
  await chrome.storage.local.set({ syncStatus: lastSyncStatus }); // observability
}

/** Read the tool names the page currently exposes on document.modelContext. */
async function readTools(tabId: number): Promise<string[] | null> {
  try {
    const [r] = await chrome.scripting.executeScript({
      target: { tabId },
      world: "MAIN",
      func: async () => {
        const mc = (globalThis as { document?: { modelContext?: { getTools(): Promise<{ name: string }[]> } } }).document
          ?.modelContext;
        if (!mc) return null;
        return (await mc.getTools()).map((t) => t.name);
      },
    });
    return (r?.result as string[] | null) ?? null;
  } catch {
    return null;
  }
}

/**
 * executeScript injection for "inject now" (an already-open tab) and as the
 * fallback when content scripts aren't available. Idempotent: a re-inject into an
 * already-booted document just reloads the IR (no second boot).
 */
async function inject(tabId: number, meta: CorpusMeta): Promise<{ how: string; tools: string[] | null }> {
  const ir = await loadIr(meta);
  const prov: Provenance = { source: meta.source, corpus: meta.id, version: meta.version };

  const [primed] = await chrome.scripting.executeScript({
    target: { tabId },
    world: "MAIN",
    func: (irArg: unknown, provArg: unknown) => {
      const w = window as unknown as {
        __sightkick_ir?: unknown;
        __sightkick_host?: unknown;
        __sightkick?: { load(ir: unknown): void };
      };
      w.__sightkick_ir = irArg;
      w.__sightkick_host = provArg; // provenance flag -> boot reports mode "injected"
      if (w.__sightkick) {
        w.__sightkick.load(irArg);
        return "reloaded";
      }
      return "primed";
    },
    args: [ir, prov],
  });

  let how = String(primed?.result ?? "primed");
  if (how === "primed") {
    await chrome.scripting.executeScript({ target: { tabId }, world: "MAIN", files: ["sightkick-runtime.js"] });
    how = "injected";
  }
  return { how, tools: await readTools(tabId) };
}

// Reconcile content-script registration on EVERY service-worker start. In MV3 the
// SW is torn down when idle and re-run on wake, and onInstalled/onStartup do NOT
// fire on wake — so a top-level call is what keeps registration live across the
// idle/restart cycle. (onInstalled/onStartup are kept as belt-and-suspenders.)
// Return the promise (don't `void` it): for onInstalled/onStartup, Chrome keeps
// the worker alive until it settles, so the (persisted) registration actually
// completes. A bare top-level async call, by contrast, gets dropped when the SW
// idles — which is why an earlier `void syncContentScripts()` never ran.
chrome.runtime.onInstalled.addListener(() => syncContentScripts());
chrome.runtime.onStartup.addListener(() => syncContentScripts());

// Fallback auto-injection: only when content scripts aren't handling document_start.
chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (contentScriptsSupported) return;
  if (changeInfo.status !== "complete" || !tab.url) return;
  void (async () => {
    const meta = await corpusForUrl(tab.url!);
    if (!meta) return;
    try {
      const { how, tools } = await inject(tabId, meta);
      console.info(`[sightkick] (fallback) ${how} "${meta.id}" -> ${tab.url} — tools: ${tools?.join(", ") ?? "?"}`);
    } catch (e) {
      console.warn(`[sightkick] fallback inject failed for ${tab.url}: ${String(e)}`);
    }
  })();
});

// Popup RPC.
chrome.runtime.onMessage.addListener((msg: { type: string; id?: string; on?: boolean }, _sender, sendResponse) => {
  void (async () => {
    if (msg.type === "getState") {
      const [corpora, enabled] = await Promise.all([loadCorpora(), getEnabled()]);
      const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
      const tools = tab?.id ? await readTools(tab.id) : null;
      sendResponse({ corpora, enabled, tab: tab ? { id: tab.id, url: tab.url } : null, tools, sync: lastSyncStatus });
    } else if (msg.type === "toggle" && msg.id != null) {
      await setEnabled(msg.id, !!msg.on);
      await syncContentScripts(); // future loads of newly enabled sites get injected
      sendResponse({ ok: true });
    } else if (msg.type === "injectNow") {
      const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
      if (!tab?.id || !tab.url) return sendResponse({ ok: false, error: "no active tab" });
      const meta = await corpusForUrl(tab.url);
      if (!meta) return sendResponse({ ok: false, error: "no enabled corpus matches this URL" });
      sendResponse({ ok: true, corpus: meta.id, ...(await inject(tab.id, meta)) });
    }
  })();
  return true; // keep the message channel open for the async sendResponse
});

/**
 * Isolated-world content script, injected at document_start on any URL a
 * registered corpus might match. It runs in the ISOLATED world (so it can use
 * chrome.storage + fetch extension resources) and hands the matching corpus's IR
 * to the MAIN-world runtime over the shared DOM — the only channel both worlds
 * see. This is what makes injection win the race against an agent's first
 * getTools(): the runtime (also at document_start) picks the IR up immediately.
 *
 * The content-script registration match is a coarse, port-stripped filter, so we
 * re-check the precise pattern (port + path) here before doing anything.
 *
 * Channel contract mirrors packages/runtime/src/channel.ts (data attrs on <html>
 * + a bare ready event). Kept in sync by hand, like the IR JSON contract.
 */
import { matchUrl } from "./match.js";

const IR_ATTR = "data-sightkick-ir";
const HOST_ATTR = "data-sightkick-host";
const IR_EVENT = "sightkick:ir";

interface CorpusMeta {
  id: string;
  match: string[];
  irFile: string;
  source: string;
  version: string;
  defaultEnabled?: boolean;
}

const resourceUrl = (p: string) => chrome.runtime.getURL(p);

void (async () => {
  try {
    const corpora = (await (await fetch(resourceUrl("corpora/index.json"))).json()) as CorpusMeta[];
    const stored = (await chrome.storage.local.get("enabled")) as { enabled?: Record<string, boolean> };
    const enabled = stored.enabled ?? {};

    const meta = corpora.find(
      (c) => (enabled[c.id] ?? c.defaultEnabled) && c.match.some((p) => matchUrl(p, location.href)),
    );
    if (!meta) return;

    const ir = await (await fetch(resourceUrl("corpora/" + meta.irFile))).json();
    const de = document.documentElement;
    de.setAttribute(IR_ATTR, JSON.stringify(ir));
    de.setAttribute(HOST_ATTR, JSON.stringify({ corpus: meta.id, source: meta.source, version: meta.version }));
    document.dispatchEvent(new Event(IR_EVENT));
  } catch (e) {
    console.warn("[sightkick] bridge failed:", e);
  }
})();

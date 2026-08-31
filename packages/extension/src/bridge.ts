/**
 * Isolated-world content script, injected at document_start on any URL a
 * registered corpus might match. It runs in the ISOLATED world (so it can use
 * chrome.storage + fetch extension resources) and hands the matching corpus's IR
 * to the MAIN-world runtime over the shared DOM — the only channel both worlds
 * see. This is what makes injection win the race against an agent's first
 * getTools(): the runtime (also at document_start) picks the IR up immediately.
 *
 * Corpus resolution (bundled + local, atlas later) goes through the shared
 * CorpusSource seam, so a locally-added corpus injects with no code change here.
 * The content-script registration match is a coarse, port-stripped filter, so we
 * re-check the precise pattern (port + path) with matchUrl.
 *
 * Channel contract mirrors packages/runtime/src/channel.ts (data attrs on <html>
 * + a bare ready event). Kept in sync by hand, like the IR JSON contract.
 */
import { matchUrl } from "./match.js";
import { listCorpora, getIr } from "./corpus.js";

const IR_ATTR = "data-sightkick-ir";
const HOST_ATTR = "data-sightkick-host";
const IR_EVENT = "sightkick:ir";

void (async () => {
  try {
    const corpora = await listCorpora();
    const stored = (await chrome.storage.local.get("enabled")) as { enabled?: Record<string, boolean> };
    const enabled = stored.enabled ?? {};

    const meta = corpora.find(
      (c) => (enabled[c.id] ?? c.defaultEnabled) && c.match.some((p) => matchUrl(p, location.href)),
    );
    if (!meta) return;

    const ir = await getIr(meta);
    const de = document.documentElement;
    de.setAttribute(IR_ATTR, JSON.stringify(ir));
    de.setAttribute(HOST_ATTR, JSON.stringify({ corpus: meta.id, source: meta.source, version: meta.version }));
    document.dispatchEvent(new Event(IR_EVENT));
  } catch (e) {
    console.warn("[sightkick] bridge failed:", e);
  }
})();

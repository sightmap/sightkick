/**
 * CorpusSource seam. A corpus (its IR + where it applies + provenance) can come
 * from more than one place; today: corpora BUNDLED in the extension, and LOCAL
 * corpora the user adds at runtime (stored in chrome.storage.local) so they can
 * point sightkick at a real site without rebuilding. The atlas is a future
 * source that slots in here behind the same listCorpora()/getIr() interface.
 *
 * Everything that resolves "which corpus applies to this URL and what's its IR"
 * (the background injector AND the document_start bridge) goes through here, so
 * adding a source is a one-file change.
 */
import { toChromeMatchPattern } from "./match.js";

export interface CorpusMeta {
  id: string;
  name: string;
  description: string;
  match: string[];
  source: string; // provenance label, e.g. "generator:examples/search" | "local" | "atlas:<...>"
  version: string;
  origin: "bundled" | "local";
  irFile?: string; // bundled: fetched from packaged resources
  ir?: unknown; // local: inline IR
  defaultEnabled?: boolean;
}

const KEY = "userCorpora";
const resourceUrl = (p: string) => chrome.runtime.getURL(p);

/** Bundled, read-only corpora shipped in the extension package. */
export async function bundledCorpora(): Promise<CorpusMeta[]> {
  const raw = (await (await fetch(resourceUrl("corpora/index.json"))).json()) as Omit<CorpusMeta, "origin">[];
  return raw.map((c) => ({ ...c, origin: "bundled" as const }));
}

/** User-added corpora persisted in chrome.storage.local. */
export async function localCorpora(): Promise<CorpusMeta[]> {
  const stored = (await chrome.storage.local.get(KEY)) as { [KEY]?: CorpusMeta[] };
  return (stored[KEY] ?? []).map((c) => ({ ...c, origin: "local" as const }));
}

/** Merge sources; a local corpus overrides a bundled one with the same id. */
export function mergeCorpora(bundled: CorpusMeta[], local: CorpusMeta[]): CorpusMeta[] {
  const byId = new Map<string, CorpusMeta>();
  for (const c of bundled) byId.set(c.id, c);
  for (const c of local) byId.set(c.id, c);
  return [...byId.values()];
}

export async function listCorpora(): Promise<CorpusMeta[]> {
  const [b, l] = await Promise.all([bundledCorpora(), localCorpora()]);
  return mergeCorpora(b, l);
}

/** Resolve a corpus's IR: inline for local, fetched from the package for bundled. */
export async function getIr(meta: CorpusMeta): Promise<unknown> {
  if (meta.ir !== undefined) return meta.ir;
  if (meta.irFile) return (await fetch(resourceUrl("corpora/" + meta.irFile))).json();
  throw new Error(`corpus "${meta.id}" has no IR`);
}

export async function addLocalCorpus(c: CorpusMeta): Promise<void> {
  const existing = await localCorpora();
  const next = existing.filter((x) => x.id !== c.id);
  next.push(c);
  await chrome.storage.local.set({ [KEY]: next });
}

export async function removeLocalCorpus(id: string): Promise<void> {
  const existing = await localCorpora();
  await chrome.storage.local.set({ [KEY]: existing.filter((x) => x.id !== id) });
}

function slug(s: string): string {
  return (
    s
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "corpus"
  );
}

export interface NewCorpusInput {
  id?: string;
  name: string;
  match: string[];
  ir: unknown;
}

/**
 * Validate user-supplied corpus input (from the popup): a name, at least one
 * usable match pattern, and an IR that at least looks like one (an object with a
 * tools array). Returns a ready-to-store local CorpusMeta or an error message.
 */
export function validateCorpus(input: NewCorpusInput): { ok: true; corpus: CorpusMeta } | { ok: false; error: string } {
  const name = input.name?.trim();
  if (!name) return { ok: false, error: "name is required" };

  const match = (input.match ?? []).map((m) => m.trim()).filter(Boolean);
  if (match.length === 0) return { ok: false, error: "at least one match pattern is required" };
  for (const p of match) {
    if (toChromeMatchPattern(p) === null) return { ok: false, error: `invalid match pattern: ${p}` };
  }

  const ir = input.ir as { name?: string; tools?: unknown[] } | null;
  if (!ir || typeof ir !== "object" || !Array.isArray(ir.tools)) {
    return { ok: false, error: "IR must be an object with a tools array" };
  }

  const id = input.id?.trim() || slug(name);
  const description = `local · ${ir.tools.length} tool(s)${ir.name ? ` · ${ir.name}` : ""}`;
  return {
    ok: true,
    corpus: { id, name, description, match, source: "local", version: "local", origin: "local", ir, defaultEnabled: true },
  };
}

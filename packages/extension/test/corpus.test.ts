import { describe, it, expect } from "vitest";
import { mergeCorpora, validateCorpus, type CorpusMeta } from "../src/corpus.js";

const bundled: CorpusMeta[] = [
  { id: "search-demo", name: "Search", description: "", match: ["https://localhost:5174/*"], source: "generator:examples/search", version: "0.0.1", origin: "bundled", irFile: "search.ir.json" },
];

describe("mergeCorpora", () => {
  it("keeps bundled, appends local, and lets local override by id", () => {
    const local: CorpusMeta[] = [
      { id: "acme", name: "Acme", description: "", match: ["https://acme.test/*"], source: "local", version: "local", origin: "local", ir: { tools: [] } },
      { id: "search-demo", name: "Search (local override)", description: "", match: ["https://localhost:5174/*"], source: "local", version: "local", origin: "local", ir: { tools: [] } },
    ];
    const merged = mergeCorpora(bundled, local);
    expect(merged.map((c) => c.id).sort()).toEqual(["acme", "search-demo"]);
    const search = merged.find((c) => c.id === "search-demo")!;
    expect(search.origin).toBe("local");
    expect(search.name).toBe("Search (local override)");
  });
});

describe("validateCorpus", () => {
  const ir = { name: "acme", tools: [{ name: "t1" }, { name: "t2" }] };

  it("accepts a well-formed corpus and derives id/description", () => {
    const r = validateCorpus({ name: "Acme Store", match: ["https://acme.test/*"], ir });
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.corpus.id).toBe("acme-store");
      expect(r.corpus.origin).toBe("local");
      expect(r.corpus.defaultEnabled).toBe(true);
      expect(r.corpus.description).toContain("2 tool(s)");
    }
  });

  it("rejects missing name, empty match, bad pattern, and non-IR", () => {
    expect(validateCorpus({ name: "", match: ["https://a.test/*"], ir })).toMatchObject({ ok: false });
    expect(validateCorpus({ name: "A", match: [], ir })).toMatchObject({ ok: false });
    expect(validateCorpus({ name: "A", match: ["not a url"], ir })).toMatchObject({ ok: false });
    expect(validateCorpus({ name: "A", match: ["https://a.test/*"], ir: { nope: 1 } })).toMatchObject({ ok: false });
  });
});

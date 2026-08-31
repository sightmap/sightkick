import { describe, it, expect } from "vitest";
import { matchUrl, pickCorpus, toChromeMatchPattern } from "../src/match.js";

describe("matchUrl", () => {
  it("matches host + path wildcards", () => {
    expect(matchUrl("https://localhost:5174/*", "https://localhost:5174/results")).toBe(true);
    expect(matchUrl("https://localhost:5174/*", "https://localhost:5174/?noboot")).toBe(true);
    expect(matchUrl("https://localhost:5174/*", "https://example.com/")).toBe(false);
  });

  it("honors scheme, including the * scheme (http/https only)", () => {
    expect(matchUrl("*://example.com/*", "http://example.com/a")).toBe(true);
    expect(matchUrl("*://example.com/*", "https://example.com/a")).toBe(true);
    expect(matchUrl("https://example.com/*", "http://example.com/a")).toBe(false);
  });

  it("supports *. subdomain (and the bare domain)", () => {
    expect(matchUrl("https://*.example.com/*", "https://app.example.com/x")).toBe(true);
    expect(matchUrl("https://*.example.com/*", "https://example.com/x")).toBe(true);
    expect(matchUrl("https://*.example.com/*", "https://evil.com/x")).toBe(false);
  });

  it("matches a path glob within the URL", () => {
    expect(matchUrl("http://localhost:*/*todo*", "http://localhost:5174/todo.html")).toBe(true);
    expect(matchUrl("http://localhost:*/*todo*", "http://localhost:5174/search")).toBe(false);
  });

  it("toChromeMatchPattern drops ports and widens the path", () => {
    expect(toChromeMatchPattern("https://localhost:5174/*")).toBe("https://localhost/*");
    expect(toChromeMatchPattern("http://localhost:*/*todo*")).toBe("http://localhost/*");
    expect(toChromeMatchPattern("*://example.com/*")).toBe("*://example.com/*");
    expect(toChromeMatchPattern("https://*.example.com/app")).toBe("https://*.example.com/*");
    expect(toChromeMatchPattern("not-a-pattern")).toBeNull();
  });

  it("pickCorpus returns the first matching corpus", () => {
    const corpora = [
      { id: "a", match: ["https://a.test/*"] },
      { id: "b", match: ["https://b.test/*", "https://localhost:5174/*"] },
    ];
    expect(pickCorpus(corpora, "https://localhost:5174/results")?.id).toBe("b");
    expect(pickCorpus(corpora, "https://none.test/")).toBeUndefined();
  });
});

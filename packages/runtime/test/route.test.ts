import { describe, it, expect } from "vitest";
import { routeMatches } from "../src/executor.js";

describe("routeMatches (standalone ensure_view verification)", () => {
  it("matches the root route", () => {
    expect(routeMatches("/", "/")).toBe(true);
    expect(routeMatches("/", "/x")).toBe(false);
  });

  it("ignores a trailing slash and query/hash", () => {
    expect(routeMatches("/", "/?latency=200")).toBe(true);
    expect(routeMatches("/trips", "/trips/")).toBe(true);
    expect(routeMatches("/trips", "/trips#top")).toBe(true);
  });

  it("matches literals and single-segment params/wildcards", () => {
    expect(routeMatches("/trips/:id", "/trips/42")).toBe(true);
    expect(routeMatches("/trips/*", "/trips/42")).toBe(true);
    expect(routeMatches("/trips/:id", "/trips/42/legs")).toBe(false);
    expect(routeMatches("/users/me", "/users/me")).toBe(true);
    expect(routeMatches("/users/me", "/users/42")).toBe(false);
  });

  it("matches globstar with zero or more trailing segments", () => {
    expect(routeMatches("/admin/**", "/admin")).toBe(true);
    expect(routeMatches("/admin/**", "/admin/x")).toBe(true);
    expect(routeMatches("/admin/**", "/admin/x/y")).toBe(true);
    expect(routeMatches("/admin/**", "/other")).toBe(false);
  });
});

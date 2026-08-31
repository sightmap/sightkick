/**
 * Chrome-style URL match patterns, minimal subset. A corpus declares the
 * URLs it applies to; the background injector uses this to decide which
 * enabled corpus (if any) to inject into a given tab.
 *
 * Supported: `<scheme>://<host>/<path>` where `<scheme>` is `*|http|https`,
 * `<host>` may lead with `*.` and/or be `*`, and `*` is a wildcard anywhere
 * in host or path. This is intentionally smaller than the full Chrome grammar
 * (no `file:`/`ftp:` niceties) — enough to target demo + real sites.
 */
export function matchUrl(pattern: string, url: string): boolean {
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return false;
  }
  const m = /^(\*|https?):\/\/([^/]*)(\/.*)?$/.exec(pattern);
  if (!m) return false;
  const [, scheme, host, path = "/*"] = m;

  if (scheme !== "*" && scheme !== u.protocol.replace(/:$/, "")) return false;
  if (scheme === "*" && !/^https?:$/.test(u.protocol)) return false;

  if (host !== "*") {
    const hostRe = "^" + host!.replace(/[.]/g, "\\.").replace(/^\\\.\*/, "").replace(/\*/g, ".*") + "$";
    // A leading "*." should match the bare domain too (Chrome semantics).
    const bare = host!.startsWith("*.") ? host!.slice(2) : null;
    if (!new RegExp(hostRe).test(u.host) && !(bare && u.host === bare)) return false;
  }

  const pathRe = "^" + path.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*") + "$";
  return new RegExp(pathRe).test(u.pathname + u.search);
}

/** First enabled corpus whose any pattern matches, or undefined. */
export function pickCorpus<T extends { match: string[] }>(corpora: T[], url: string): T | undefined {
  return corpora.find((c) => c.match.some((p) => matchUrl(p, url)));
}

/**
 * Broaden one of our patterns into a valid Chrome match pattern for
 * registerContentScripts, which rejects ports (`localhost:5174`, `localhost:*`)
 * and complex path globs. We drop the port and widen the path to `/*`; the
 * isolated bridge then re-checks the precise pattern (port + path) with matchUrl
 * before doing anything, so the coarser registration filter stays correct.
 * Returns null if the pattern can't be parsed.
 */
export function toChromeMatchPattern(pattern: string): string | null {
  const m = /^(\*|https?):\/\/([^/]*)(\/.*)?$/.exec(pattern);
  if (!m) return null;
  const [, scheme, host] = m;
  const hostNoPort = host!.replace(/:.*$/, ""); // strip :port or :*
  if (!hostNoPort) return null;
  return `${scheme}://${hostNoPort}/*`;
}

// Dev server for the manual demos. Rebuilds the demo bundles on change and
// serves the demo/ directory with clean-URL routing so the signup pages live at
// /step1 and /step2 — matching the routes in the compiled signup IR. Uses HTTPS
// when a local cert is present (see certs/README.md), otherwise plain HTTP.
import { context } from "esbuild";
import http from "node:http";
import https from "node:https";
import { readFile } from "node:fs/promises";
import { existsSync, readFileSync } from "node:fs";
import { extname, join, normalize, resolve } from "node:path";

const DEMO = resolve("demo");

// Clean URLs → files. The search demo is an SPA, so both its routes serve the
// same document; client-side routing + the runtime nav hook do the rest.
const routes = {
  "/": "search.html",
  "/results": "search.html",
};

const mime = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json",
  ".css": "text/css; charset=utf-8",
  ".ico": "image/x-icon",
};

const ctx = await context({
  entryPoints: { "demo.bundle": "demo/demo.js", "search.bundle": "demo/search.js" },
  outdir: "demo",
  bundle: true,
  format: "iife",
  platform: "browser",
  target: "es2020",
  loader: { ".json": "json" },
});
await ctx.rebuild();
await ctx.watch();

async function handler(req, res) {
  const { pathname } = new URL(req.url, "http://localhost");
  const rel = routes[pathname] ?? pathname.replace(/^\/+/, "");
  const filePath = join(DEMO, normalize(rel));
  if (!filePath.startsWith(DEMO)) {
    res.writeHead(403).end("forbidden");
    return;
  }
  try {
    const data = await readFile(filePath);
    res.writeHead(200, { "content-type": mime[extname(filePath)] ?? "application/octet-stream" });
    res.end(data);
  } catch {
    res.writeHead(404, { "content-type": "text/plain" }).end("not found");
  }
}

const CERTFILE = "certs/localhost.pem";
const KEYFILE = "certs/localhost-key.pem";
// HTTPS when a local cert is present, EXCEPT when SIGHTKICK_HTTP is set (the eval
// harness forces HTTP: the standalone runtime polyfills modelContext in JS, so
// it needs no HTTPS, and this sidesteps Chrome's self-signed-cert interstitial).
const useHttps = existsSync(CERTFILE) && existsSync(KEYFILE) && !process.env.SIGHTKICK_HTTP;
const server = useHttps
  ? https.createServer({ cert: readFileSync(CERTFILE), key: readFileSync(KEYFILE) }, handler)
  : http.createServer(handler);

const PORT = 5174;
server.listen(PORT, "127.0.0.1", () => {
  const base = `${useHttps ? "https" : "http"}://localhost:${PORT}`;
  console.log(`\n  sightkick search demo (two-view SPA) → ${base}/\n`);
  if (!useHttps) {
    console.log("  (http; Chrome's Ask Gemini needs https — see certs/README.md)\n");
  }
});

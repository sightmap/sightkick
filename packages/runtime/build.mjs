import { build } from "esbuild";

const common = {
  bundle: true,
  platform: "browser",
  target: "es2020",
  logLevel: "info",
};

// 1) The injectable runtime artifact. Attaches window.__sightkick and auto-boots
//    from window.__sightkick_ir. This is what the exporter/extension ships.
await build({
  ...common,
  entryPoints: ["src/index.ts"],
  outfile: "dist/sightkick-runtime.js",
  format: "iife",
});

// 2) The manual demo bundles (IR inlined so the pages work served statically).
await build({
  ...common,
  entryPoints: { "demo.bundle": "demo/demo.js", "search.bundle": "demo/search.js" },
  outdir: "demo",
  format: "iife",
  loader: { ".json": "json" },
});

console.log("built dist/sightkick-runtime.js and the demo bundles");

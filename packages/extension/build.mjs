// Build the unpacked MV3 extension into dist/. Bundles the background service
// worker + popup, then assembles the loadable extension: manifest, popup HTML,
// the runtime bundle (built fresh from ../runtime), and the corpora (IRs copied
// straight from the generator goldens so they can't drift from what we ship).
import { build } from "esbuild";
import { execFileSync } from "node:child_process";
import { cp, mkdir, rm } from "node:fs/promises";

const OUT = "dist";
await rm(OUT, { recursive: true, force: true });
await mkdir(`${OUT}/corpora`, { recursive: true });

// 1) Fresh runtime bundle (the artifact we inject).
execFileSync("node", ["build.mjs"], { cwd: "../runtime", stdio: "inherit" });

// 2) Extension scripts.
const common = { bundle: true, target: "es2020", platform: "browser", logLevel: "info" };
await build({ ...common, entryPoints: ["src/background.ts"], outfile: `${OUT}/background.js`, format: "esm" });
await build({ ...common, entryPoints: ["src/popup.ts"], outfile: `${OUT}/popup.js`, format: "iife" });
// Isolated-world document_start bridge (hands the IR to the MAIN-world runtime).
await build({ ...common, entryPoints: ["src/bridge.ts"], outfile: `${OUT}/bridge.js`, format: "iife" });

// 3) Static assets + injected artifact.
await cp("manifest.json", `${OUT}/manifest.json`);
await cp("popup.html", `${OUT}/popup.html`);
await cp("../runtime/dist/sightkick-runtime.js", `${OUT}/sightkick-runtime.js`);

// 4) Corpora: the index we author + IRs from the generator's goldens.
await cp("corpora/index.json", `${OUT}/corpora/index.json`);
await cp("../../generator/internal/gen/testdata/search.ir.json", `${OUT}/corpora/search.ir.json`);
await cp("../../generator/internal/gen/testdata/todo.ir.json", `${OUT}/corpora/todo.ir.json`);
// burrito IR is a committed snapshot (built from examples/burrito against the
// sibling potemkin repo; regenerate with: go run . build ../examples/burrito).
await cp("corpora/burrito.ir.json", `${OUT}/corpora/burrito.ir.json`);

console.log(`built unpacked extension -> packages/extension/${OUT}/`);

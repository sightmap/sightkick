import { build } from "esbuild";
// Bundle only the shared DOM resolver/extractors into an isolated expression.
// Every operation evaluates it afresh, including after full-document navigation.
const dom = await build({stdin: {contents: 'export { resolveQuery, extract } from "../runtime/src/dom.ts";', resolveDir: process.cwd()}, bundle: true, write: false, format: "iife", globalName: "dom", target: "es2020", minify: true});
await build({entryPoints: ["src/index.ts"], bundle: true, platform: "node", target: "es2020", format: "esm", outfile: "dist/executor.mjs", define: {SIGHTKICK_DOM: JSON.stringify(`(()=>{${dom.outputFiles[0].text};return dom;})()`)} });

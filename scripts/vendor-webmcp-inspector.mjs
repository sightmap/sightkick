// Vendor the WebMCP inspector extension from its upstream source repo into
// vendor/webmcp-tool/unpacked/ — the copy `sightkick browser --webmcp`
// auto-loads (via the embedded copy under generator/webmcpinspector/).
//
// Upstream: https://github.com/beaufortfrancois/model-context-tool-inspector
// (Apache-2.0). The repo does NOT commit the bundled @google/genai payload;
// `npm install` runs a postinstall that esbuild-bundles it into js-genai.js and
// then removes node_modules, leaving a directly-loadable unpacked extension. We
// pin a commit, run that build in a temp dir, strip the store-only manifest
// fields, and sweep the loadable file set into vendor/webmcp-tool/unpacked/.
//
// Usage:
//   node scripts/vendor-webmcp-inspector.mjs [<ref>]
//   INSPECTOR_REF=<ref> node scripts/vendor-webmcp-inspector.mjs
// where <ref> is a commit SHA / tag / branch (default: the pinned SHA below).
//
// After running, regenerate the embedded Go copy and commit both:
//   cd generator && go generate ./webmcpinspector/...
//
// Requires: node >= 20 (global fetch), and `tar` + `npm` on PATH.

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const REPO = "beaufortfrancois/model-context-tool-inspector";
const UPSTREAM_URL = `https://github.com/${REPO}`;
// Pinned upstream commit. Bump this (or pass a ref) to pull a newer inspector.
const DEFAULT_REF = "f164a9aa5c3f6083f5976ccae308257bdf86cb99"; // v1.9.14

// Manifest keys that only make sense for the Web-Store-managed build; strip them
// so the copy loads as a plain local unpacked extension on any Chrome-for-Testing
// (see vendor/webmcp-tool/NOTES.md).
const STRIP_MANIFEST_KEYS = ["key", "update_url", "minimum_chrome_version"];

// Files in the built upstream tree that are NOT part of the loadable extension.
// Everything else (the manifest, its referenced scripts/styles, the bundled
// js-genai.js, plus LICENSE for attribution) is vendored — a denylist so a new
// asset the inspector starts referencing is picked up automatically.
const SKIP = new Set([
  "node_modules",
  "test",
  ".git",
  ".gitignore",
  "package.json",
  "package-lock.json",
  "README.md",
  "PRIVACY.md",
]);

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..");
const destDir = path.join(repoRoot, "vendor", "webmcp-tool", "unpacked");
const provenancePath = path.join(repoRoot, "vendor", "webmcp-tool", ".vendored.json");

const ref = process.env.INSPECTOR_REF || process.argv[2] || DEFAULT_REF;

async function main() {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "webmcp-inspector-"));
  try {
    console.log(`→ vendoring WebMCP inspector from ${REPO}@${ref}`);
    const srcDir = await download(ref, tmp);
    build(srcDir);
    const version = stripManifest(path.join(srcDir, "manifest.json"));
    const files = sweep(srcDir, destDir);
    writeProvenance({ ref, version, files });
    console.log(
      `✓ vendored ${files.length} file(s) (extension v${version}) into ${path.relative(repoRoot, destDir)}`,
    );
    console.log(
      "  next: cd generator && go generate ./webmcpinspector/...  (then commit both)",
    );
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

// download fetches the repo tarball at ref and extracts it, returning the
// extracted source directory.
async function download(ref, tmp) {
  const url = `https://codeload.github.com/${REPO}/tar.gz/${ref}`;
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`download ${url}: HTTP ${res.status}`);
  }
  const tgz = path.join(tmp, "src.tgz");
  fs.writeFileSync(tgz, Buffer.from(await res.arrayBuffer()));
  execFileSync("tar", ["xzf", tgz, "-C", tmp]);
  const dirs = fs
    .readdirSync(tmp, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name);
  if (dirs.length !== 1) {
    throw new Error(`expected one extracted dir, got: ${dirs.join(", ")}`);
  }
  return path.join(tmp, dirs[0]);
}

// build runs the upstream `npm install`, whose postinstall bundles @google/genai
// into js-genai.js and deletes node_modules. Fails loudly if that didn't happen
// (e.g. esbuild's platform binary was unavailable), so we never vendor a broken
// tree.
function build(srcDir) {
  console.log("→ building (npm install → esbuild bundle of @google/genai) …");
  execFileSync("npm", ["install", "--no-audit", "--no-fund"], {
    cwd: srcDir,
    stdio: "inherit",
  });
  if (!fs.existsSync(path.join(srcDir, "js-genai.js"))) {
    throw new Error("build did not produce js-genai.js — upstream build failed");
  }
  if (fs.existsSync(path.join(srcDir, "node_modules"))) {
    throw new Error(
      "node_modules still present — upstream postinstall bundle step did not complete",
    );
  }
}

// stripManifest removes the store-only keys and rewrites the manifest in place,
// returning the extension version.
function stripManifest(manifestPath) {
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  for (const key of STRIP_MANIFEST_KEYS) delete manifest[key];
  fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + "\n");
  return manifest.version;
}

// sweep replaces destDir with the loadable file set from srcDir, returning the
// sorted list of vendored file names.
function sweep(srcDir, destDir) {
  fs.rmSync(destDir, { recursive: true, force: true });
  fs.mkdirSync(destDir, { recursive: true });
  const files = [];
  for (const name of fs.readdirSync(srcDir)) {
    if (SKIP.has(name)) continue;
    const from = path.join(srcDir, name);
    if (!fs.statSync(from).isFile()) continue; // extension is a flat dir
    fs.copyFileSync(from, path.join(destDir, name));
    files.push(name);
  }
  files.sort();
  if (!files.includes("manifest.json")) {
    throw new Error("no manifest.json among vendored files");
  }
  return files;
}

// writeProvenance records exactly what was vendored, mirroring the pinned-source
// marker pattern used elsewhere. Includes a content hash so drift is auditable.
function writeProvenance({ ref, version, files }) {
  const hash = createHash("sha256");
  for (const name of files) {
    hash.update(name);
    hash.update(fs.readFileSync(path.join(destDir, name)));
  }
  const provenance = {
    source: UPSTREAM_URL,
    ref,
    extensionVersion: version,
    license: "Apache-2.0",
    vendoredAt: new Date().toISOString(),
    manifestStripped: STRIP_MANIFEST_KEYS,
    build: "npm install (postinstall esbuild-bundles @google/genai → js-genai.js)",
    files,
    sha256: hash.digest("hex"),
  };
  fs.writeFileSync(provenancePath, JSON.stringify(provenance, null, 2) + "\n");
}

main().catch((err) => {
  console.error(`vendor-webmcp-inspector: ${err.message}`);
  process.exit(1);
});

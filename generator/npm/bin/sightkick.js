#!/usr/bin/env node
"use strict";
// Locate the platform-specific binary shipped by the matching optional
// dependency (@sightmap/sightkick-<os>-<arch>) and exec it. npm installs only
// the one package matching the host's os/cpu, so there is no download and no
// install script — the binary is already on disk after `npm install`.

const path = require("node:path");
const { spawnSync } = require("node:child_process");

const key = `${process.platform}-${process.arch}`;
const pkgName = `@sightmap/sightkick-${key}`;
const binaryName = process.platform === "win32" ? "sightkick.exe" : "sightkick";

function resolveBinary() {
  // Resolve via the package's package.json (always resolvable, unlike an
  // extensionless binary path), then join the binary next to it.
  try {
    const pkgJson = require.resolve(`${pkgName}/package.json`);
    return path.join(path.dirname(pkgJson), binaryName);
  } catch (_) {
    return null;
  }
}

const binary = resolveBinary();
if (!binary) {
  console.error(
    `sightkick: no prebuilt binary found for ${key}.\n` +
      `Expected the optional dependency ${pkgName}. This usually means either:\n` +
      `  - your platform/arch is unsupported — see https://github.com/sightmap/sightkick/releases\n` +
      `  - optional dependencies were skipped (e.g. --no-optional / --omit=optional), or a\n` +
      `    known npm bug left it out. Try: npm install --force @sightmap/sightkick`
  );
  process.exit(1);
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  if (result.error.code === "ENOENT") {
    console.error(`sightkick: binary not found at ${binary}`);
  } else {
    console.error("sightkick:", result.error.message);
  }
  process.exit(1);
}
process.exit(result.status ?? 1);

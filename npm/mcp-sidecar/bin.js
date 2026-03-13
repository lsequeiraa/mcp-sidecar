#!/usr/bin/env node

const { execFileSync } = require("child_process");
const os = require("os");

// Map Node.js platform+arch to the corresponding npm package and binary
// subpath. Windows places the .exe at the package root; Unix-like platforms
// place it under bin/.
const PLATFORMS = {
  "darwin arm64": { pkg: "@mcp-sidecar/darwin-arm64", bin: "bin/mcp-sidecar" },
  "darwin x64":   { pkg: "@mcp-sidecar/darwin-x64",   bin: "bin/mcp-sidecar" },
  "linux arm64":  { pkg: "@mcp-sidecar/linux-arm64",  bin: "bin/mcp-sidecar" },
  "linux x64":    { pkg: "@mcp-sidecar/linux-x64",    bin: "bin/mcp-sidecar" },
  "win32 x64":    { pkg: "@mcp-sidecar/win32-x64",    bin: "mcp-sidecar.exe" },
};

const key = `${process.platform} ${os.arch()}`;
const entry = PLATFORMS[key];

if (!entry) {
  console.error(`mcp-sidecar: unsupported platform ${key}`);
  process.exit(1);
}

let binPath;
try {
  binPath = require.resolve(`${entry.pkg}/${entry.bin}`);
} catch {
  console.error(
    `mcp-sidecar: could not find the binary for ${key}.\n` +
    `The platform package ${entry.pkg} does not appear to be installed.\n` +
    `Try reinstalling: npm install mcp-sidecar`
  );
  process.exit(1);
}

try {
  execFileSync(binPath, process.argv.slice(2), { stdio: "inherit" });
} catch (err) {
  if (err.status !== null) {
    process.exit(err.status);
  }
  console.error(`mcp-sidecar: failed to run binary: ${err.message}`);
  process.exit(1);
}

#!/usr/bin/env node

// Postinstall script for mcp-sidecar.
//
// In the normal case the platform-specific binary is already installed via
// optionalDependencies and this script is a no-op.  When the binary is
// missing (e.g. the user passed --no-optional, or the package manager
// didn't resolve the platform package) we fall back to downloading the
// correct platform package tarball directly from the npm registry.

const https = require("https");
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");

const PLATFORMS = {
  "darwin arm64": { pkg: "@mcp-sidecar/darwin-arm64", bin: "bin/mcp-sidecar" },
  "darwin x64":   { pkg: "@mcp-sidecar/darwin-x64",   bin: "bin/mcp-sidecar" },
  "linux arm64":  { pkg: "@mcp-sidecar/linux-arm64",  bin: "bin/mcp-sidecar" },
  "linux x64":    { pkg: "@mcp-sidecar/linux-x64",    bin: "bin/mcp-sidecar" },
  "win32 x64":    { pkg: "@mcp-sidecar/win32-x64",    bin: "mcp-sidecar.exe" },
};

function getPlatformEntry() {
  const key = `${process.platform} ${os.arch()}`;
  return PLATFORMS[key] || null;
}

function binaryIsInstalled(entry) {
  try {
    require.resolve(`${entry.pkg}/${entry.bin}`);
    return true;
  } catch {
    return false;
  }
}

// ---- Fallback: download from npm registry ----

function fetch(url) {
  return new Promise((resolve, reject) => {
    https
      .get(url, (res) => {
        if (
          res.statusCode >= 300 &&
          res.statusCode < 400 &&
          res.headers.location
        ) {
          return fetch(res.headers.location).then(resolve, reject);
        }
        if (res.statusCode !== 200) {
          return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

// Extract a single file from a .tar.gz buffer.  npm packages are tarballs
// whose entries are prefixed with "package/".
function extractFileFromTarball(tgz, filepath) {
  const tar = zlib.gunzipSync(tgz);
  // Simple tar parser -- enough for npm tarballs.
  let offset = 0;
  while (offset < tar.length) {
    const header = tar.subarray(offset, offset + 512);
    if (header[0] === 0) break; // end of archive

    const name = header.toString("utf8", 0, 100).replace(/\0.*$/, "");
    const sizeOctal = header.toString("utf8", 124, 136).replace(/\0.*$/, "");
    const size = parseInt(sizeOctal, 8) || 0;
    const dataStart = offset + 512;
    const dataEnd = dataStart + size;

    // npm tarballs prefix entries with "package/"
    if (name === `package/${filepath}` || name === filepath) {
      return tar.subarray(dataStart, dataEnd);
    }

    // Advance to the next 512-byte boundary
    offset = dataEnd + (512 - (dataEnd % 512)) % 512;
  }
  return null;
}

async function downloadFromRegistry(entry) {
  const version = require(path.join(__dirname, "package.json")).version;
  const scopedName = entry.pkg; // e.g. "@mcp-sidecar/win32-x64"
  const encodedPkg = scopedName.replace("/", "%2f");
  const url = `https://registry.npmjs.org/${encodedPkg}/-/${scopedName.split("/")[1]}-${version}.tgz`;

  console.log(`mcp-sidecar: downloading ${scopedName}@${version} from npm...`);
  const tgz = await fetch(url);
  const data = extractFileFromTarball(tgz, entry.bin);

  if (!data) {
    throw new Error(`Could not find ${entry.bin} in ${scopedName} tarball`);
  }

  // Write the binary next to where require.resolve would find it.
  // Create a minimal node_modules layout so bin.js can resolve it.
  const pkgDir = path.join(
    __dirname,
    "node_modules",
    ...scopedName.split("/")
  );
  const binDir = path.dirname(path.join(pkgDir, entry.bin));
  fs.mkdirSync(binDir, { recursive: true });
  fs.writeFileSync(path.join(pkgDir, entry.bin), data);

  // Write a minimal package.json so require.resolve works
  fs.writeFileSync(
    path.join(pkgDir, "package.json"),
    JSON.stringify({ name: scopedName, version })
  );

  if (process.platform !== "win32") {
    fs.chmodSync(path.join(pkgDir, entry.bin), 0o755);
  }
}

// ---- Main ----

async function main() {
  const entry = getPlatformEntry();
  if (!entry) {
    // Unsupported platform -- nothing to install, will fail at runtime
    return;
  }

  if (binaryIsInstalled(entry)) {
    // Happy path: optionalDependencies resolved the platform package
    return;
  }

  // Fallback: download from npm registry
  try {
    await downloadFromRegistry(entry);
    console.log("mcp-sidecar: binary installed successfully (fallback).");
  } catch (err) {
    console.error(
      `mcp-sidecar: failed to install binary for ${process.platform} ${os.arch()}.\n` +
      `  ${err.message}\n` +
      `  Try: npm install ${entry.pkg}`
    );
    process.exit(1);
  }
}

main();

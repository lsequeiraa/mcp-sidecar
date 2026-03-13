const https = require("https");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { execSync } = require("child_process");

const REPO = "lsequeiraa/mcp-sidecar";

// Map Node.js platform/arch to GoReleaser naming.
const PLATFORM_MAP = {
  linux: "linux",
  darwin: "darwin",
  win32: "windows",
};

const ARCH_MAP = {
  x64: "amd64",
  arm64: "arm64",
};

function getAssetName() {
  const platform = PLATFORM_MAP[os.platform()];
  const arch = ARCH_MAP[os.arch()];

  if (!platform || !arch) {
    throw new Error(
      `Unsupported platform: ${os.platform()} ${os.arch()}`
    );
  }

  const ext = platform === "windows" ? "zip" : "tar.gz";
  return `mcp-sidecar_${platform}_${arch}.${ext}`;
}

function getVersion() {
  const pkg = require("./package.json");
  return pkg.version;
}

function download(url) {
  return new Promise((resolve, reject) => {
    https
      .get(url, (res) => {
        // Follow redirects (GitHub sends 302 to S3).
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          return download(res.headers.location).then(resolve, reject);
        }
        if (res.statusCode !== 200) {
          return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        }
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

function extractTarGz(buffer, destDir) {
  const archive = path.join(destDir, "archive.tar.gz");
  fs.writeFileSync(archive, buffer);
  execSync(`tar -xzf "${archive}" -C "${destDir}"`, { stdio: "ignore" });
  fs.unlinkSync(archive);
}

function extractZip(buffer, destDir) {
  const archive = path.join(destDir, "archive.zip");
  fs.writeFileSync(archive, buffer);
  // Use system tar which handles zip files on Windows 10+ without
  // depending on PowerShell's Expand-Archive (which fails when the
  // execution policy blocks script-based modules).
  execSync(`tar -xf "${archive}" -C "${destDir}"`, { stdio: "ignore" });
  fs.unlinkSync(archive);
}

async function main() {
  const version = getVersion();
  const asset = getAssetName();
  const binDir = path.join(__dirname, "bin");

  // Skip if binary already exists (e.g. CI cache).
  const ext = os.platform() === "win32" ? ".exe" : "";
  const binaryPath = path.join(binDir, `mcp-sidecar${ext}`);
  if (fs.existsSync(binaryPath)) {
    console.log("mcp-sidecar binary already exists, skipping download.");
    return;
  }

  const url = `https://github.com/${REPO}/releases/download/v${version}/${asset}`;
  console.log(`Downloading mcp-sidecar v${version} for ${os.platform()}/${os.arch()}...`);
  console.log(`  ${url}`);

  const buffer = await download(url);

  fs.mkdirSync(binDir, { recursive: true });

  if (asset.endsWith(".zip")) {
    extractZip(buffer, binDir);
  } else {
    extractTarGz(buffer, binDir);
  }

  // Ensure the binary is executable on Unix.
  if (os.platform() !== "win32") {
    fs.chmodSync(binaryPath, 0o755);
  }

  console.log("mcp-sidecar installed successfully.");
}

main().catch((err) => {
  console.error(`Failed to install mcp-sidecar: ${err.message}`);
  process.exit(1);
});

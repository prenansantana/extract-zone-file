const https = require("https");
const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

const VERSION = "0.1.0";
const REPO = "prenansantana/extract-zone-file";

function getPlatformBinary() {
  const platform = process.platform;
  const arch = process.arch;

  const map = {
    "darwin-arm64": "dzone-darwin-arm64",
    "darwin-x64": "dzone-darwin-amd64",
    "linux-x64": "dzone-linux-amd64",
    "linux-arm64": "dzone-linux-arm64",
    "win32-x64": "dzone-windows-amd64.exe",
  };

  const key = `${platform}-${arch}`;
  const binary = map[key];
  if (!binary) {
    console.error(`Unsupported platform: ${key}`);
    process.exit(1);
  }
  return binary;
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const handleResponse = (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        https.get(res.headers.location, handleResponse).on("error", reject);
        return;
      }
      if (res.statusCode !== 200) {
        reject(new Error(`Download failed: HTTP ${res.statusCode}`));
        return;
      }
      const file = fs.createWriteStream(dest);
      res.pipe(file);
      file.on("finish", () => {
        file.close(resolve);
      });
    };
    https.get(url, handleResponse).on("error", reject);
  });
}

async function main() {
  const binaryName = getPlatformBinary();
  const url = `https://github.com/${REPO}/releases/download/v${VERSION}/${binaryName}`;
  const binDir = path.join(__dirname, "bin");
  const dest = path.join(binDir, process.platform === "win32" ? "dzone.exe" : "dzone");

  fs.mkdirSync(binDir, { recursive: true });

  console.log(`Downloading dzone v${VERSION} for ${process.platform}-${process.arch}...`);

  await download(url, dest);
  fs.chmodSync(dest, 0o755);

  console.log("dzone installed successfully!");
}

main().catch((err) => {
  console.error("Failed to install dzone:", err.message);
  process.exit(1);
});

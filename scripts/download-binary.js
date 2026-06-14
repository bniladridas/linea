#!/usr/bin/env node
const { createWriteStream, chmodSync, existsSync, mkdirSync, unlinkSync } = require("fs");
const { get } = require("https");
const { join } = require("path");
const { spawnSync } = require("child_process");

const arch = process.arch === "arm64" ? "arm64" : "amd64";

if (process.platform !== "darwin") {
  console.error("Unsupported platform:", process.platform);
  process.exit(1);
}

const targetDir = join(__dirname, "..", "bin");
const targetFile = join(targetDir, "linea");

if (existsSync(targetFile)) {
  process.exit(0);
}

const { version } = require("../package.json");
const url = `https://github.com/bniladridas/linea/releases/download/v${version}/linea_v${version}_darwin_${arch}.tar.gz`;
const tmpFile = join(targetDir, "linea.tar.gz");
mkdirSync(targetDir, { recursive: true });
console.log("Downloading linea " + version + " (darwin-" + arch + ")");

function onError(err) {
  try { unlinkSync(tmpFile); } catch (_) {}
  console.error("Download failed:", err.message);
  process.exit(1);
}

const file = createWriteStream(tmpFile);
file.on("error", onError);
file.on("finish", () => {
  file.close();
  const result = spawnSync("tar", ["-xzf", tmpFile, "-C", targetDir], { stdio: "pipe" });
  if (result.status !== 0) {
    console.error("Failed to extract binary");
    process.exit(1);
  }
  unlinkSync(tmpFile);
  chmodSync(targetFile, 0o755);
  console.log("linea installed");
});

function download(url, redirects) {
  if (redirects > 5) {
    onError(new Error("Too many redirects"));
    return;
  }
  get(url, { headers: { "User-Agent": "linea-npm" } }, (r) => {
    if (r.statusCode >= 300 && r.statusCode < 400 && r.headers.location) {
      download(r.headers.location, redirects + 1);
      return;
    }
    if (r.statusCode !== 200) {
      onError(new Error("HTTP " + r.statusCode));
      return;
    }
    r.pipe(file);
    r.on("error", onError);
  }).on("error", onError);
}

download(url, 0);

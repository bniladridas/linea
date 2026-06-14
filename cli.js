#!/usr/bin/env node
const { spawnSync } = require("child_process");
const { join } = require("path");

const binary = join(__dirname, "bin", "linea");
const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
process.exit(result.status ?? 1);

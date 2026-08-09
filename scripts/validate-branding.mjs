import { readdir, readFile } from "node:fs/promises";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const removedIdentifier = ["as", "ter"].join("");
const excluded = new Set([".git", ".pytest_cache", "dist", "node_modules"]);
const violations = [];

async function scan(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    if (excluded.has(entry.name)) continue;
    const path = resolve(directory, entry.name);
    const display = relative(root, path);
    if (entry.name.toLowerCase().includes(removedIdentifier)) {
      violations.push(`${display}: removed identifier in path`);
    }
    if (entry.isDirectory()) {
      await scan(path);
      continue;
    }
    const content = await readFile(path);
    if (content.toString("utf8").toLowerCase().includes(removedIdentifier)) {
      violations.push(`${display}: removed identifier in content`);
    }
  }
}

await scan(root);
if (violations.length > 0) {
  throw new Error(`branding validation failed:\n${violations.join("\n")}`);
}
console.log("Validated Hobot Code branding: no predecessor identifiers");

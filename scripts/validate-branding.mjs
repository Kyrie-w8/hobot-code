import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const removedIdentifier = ["as", "ter"].join("");
const removedPattern = new RegExp(`(^|[^a-z0-9])${removedIdentifier}([^a-z0-9]|$)`, "i");
const violations = [];

const sourceFiles = execFileSync(
  "git",
  ["-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
  { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 },
).split("\0").filter(Boolean);

for (const name of sourceFiles) {
  const path = resolve(root, name);
  const display = relative(root, path);
  let content;
  try {
    content = await readFile(path);
  } catch (error) {
    if (error?.code === "ENOENT") continue;
    throw error;
  }
  if (name.split("/").some((segment) => removedPattern.test(segment))) {
    violations.push(`${display}: removed identifier in path`);
  }
  if (removedPattern.test(content.toString("utf8"))) {
    violations.push(`${display}: removed identifier in content`);
  }
}

if (violations.length > 0) {
  throw new Error(`branding validation failed:\n${violations.join("\n")}`);
}
console.log("Validated Hobot Code branding: no predecessor identifiers");

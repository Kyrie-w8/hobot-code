import { createHash } from "node:crypto";
import { readFile, readdir, realpath, rename, utimes, writeFile } from "node:fs/promises";
import { relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

export async function releaseFiles(rootDirectory) {
  const root = resolve(rootDirectory);
  const files = [];
  async function walk(directory) {
    const entries = await readdir(directory, { withFileTypes: true });
    entries.sort((left, right) => left.name < right.name ? -1 : left.name > right.name ? 1 : 0);
    for (const entry of entries) {
      const path = resolve(directory, entry.name);
      const name = relative(root, path).split(sep).join("/");
      if (name === "MANIFEST.sha256") continue;
      if (entry.isSymbolicLink()) throw new Error(`release packages must not contain symlinks: ${name}`);
      if (entry.isDirectory()) {
        await walk(path);
        continue;
      }
      if (!entry.isFile()) throw new Error(`unsupported release entry: ${name}`);
      if (name.includes("\n") || name.includes("\r") || name.includes("\\")) {
        throw new Error(`release path cannot contain a newline or backslash: ${name}`);
      }
      files.push({ name, path });
    }
  }
  await walk(root);
  return files;
}

async function normalizeTimestamps(directory, timestamp) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) await normalizeTimestamps(path, timestamp);
    await utimes(path, timestamp, timestamp);
  }
  await utimes(directory, timestamp, timestamp);
}

export async function createManifest(rootDirectory, builtAt) {
  const root = resolve(rootDirectory);
  const records = [];
  for (const file of await releaseFiles(root)) {
    const digest = createHash("sha256").update(await readFile(file.path)).digest("hex");
    records.push(`${digest}  ${file.name}`);
  }
  const destination = resolve(root, "MANIFEST.sha256");
  const temporary = `${destination}.new-${process.pid}`;
  await writeFile(temporary, `${records.join("\n")}\n`, { flag: "wx", mode: 0o644 });
  await rename(temporary, destination);
  if (builtAt) {
    const timestamp = new Date(builtAt);
    if (Number.isNaN(timestamp.valueOf())) throw new Error(`Invalid release timestamp: ${builtAt}`);
    await normalizeTimestamps(root, timestamp);
  }
  return records;
}

async function main() {
  const [rootDirectory, builtAt] = process.argv.slice(2);
  if (!rootDirectory) throw new Error("Usage: write-release-manifest.mjs <package-root>");
  const records = await createManifest(rootDirectory, builtAt);
  console.log(`Wrote MANIFEST.sha256 for ${records.length} release files`);
}

if (process.argv[1] && await realpath(process.argv[1]) === await realpath(fileURLToPath(import.meta.url))) await main();

import { access, readFile, readdir } from "node:fs/promises";
import { dirname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const topLevel = ["README.md", "CHANGELOG.md", "CONTRIBUTING.md", "SECURITY.md"];
const documentation = (await readdir(resolve(root, "docs")))
  .filter((name) => name.endsWith(".md"))
  .map((name) => `docs/${name}`);
const markdownFiles = [...topLevel, ...documentation];
const anchorCache = new Map();
const failures = [];

function githubSlug(value) {
  return value
    .replace(/<[^>]*>/gu, "")
    .replace(/[`*_~]/gu, "")
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .replace(/\s+/gu, "-");
}

async function anchorsFor(path) {
  if (anchorCache.has(path)) return anchorCache.get(path);
  const anchors = new Set();
  const counts = new Map();
  const content = await readFile(path, "utf8");
  for (const line of content.split(/\r?\n/u)) {
    const heading = /^#{1,6}\s+(.+?)\s*#*\s*$/u.exec(line)?.[1];
    if (!heading) continue;
    const base = githubSlug(heading);
    const count = counts.get(base) ?? 0;
    counts.set(base, count + 1);
    anchors.add(count === 0 ? base : `${base}-${count}`);
  }
  anchorCache.set(path, anchors);
  return anchors;
}

for (const sourceName of markdownFiles) {
  const sourcePath = resolve(root, sourceName);
  const content = await readFile(sourcePath, "utf8");
  const linkPattern = /!?\[[^\]]*\]\(([^)\s]+)(?:\s+[^)]*)?\)/gu;
  for (const match of content.matchAll(linkPattern)) {
    const destination = match[1].replace(/^<|>$/gu, "");
    if (/^(?:[a-z][a-z0-9+.-]*:|\/\/)/iu.test(destination)) continue;
    const hashIndex = destination.indexOf("#");
    const pathPart = hashIndex < 0 ? destination : destination.slice(0, hashIndex);
    const fragment = hashIndex < 0 ? "" : decodeURIComponent(destination.slice(hashIndex + 1));
    const targetPath = pathPart ? resolve(dirname(sourcePath), decodeURIComponent(pathPart)) : sourcePath;
    if (targetPath !== root && !targetPath.startsWith(`${root}${sep}`)) {
      failures.push(`${sourceName}: link escapes repository: ${destination}`);
      continue;
    }
    try {
      await access(targetPath);
    } catch {
      failures.push(`${sourceName}: missing link target: ${destination}`);
      continue;
    }
    if (fragment && !(await anchorsFor(targetPath)).has(fragment.toLowerCase())) {
      failures.push(`${sourceName}: missing anchor: ${destination}`);
    }
  }
}

if (failures.length > 0) {
  throw new Error(`documentation link validation failed:\n${failures.join("\n")}`);
}

console.log(`Validated local links in ${markdownFiles.length} Markdown files`);

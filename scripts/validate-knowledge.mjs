import { readFile, readdir, stat } from "node:fs/promises";
import { dirname, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = resolve(repository, "knowledge");
const manifest = JSON.parse(await readFile(resolve(root, "manifest.json"), "utf8"));
const allowedBoards = new Set(["all", "x5", "s100", "s600"]);
const officialHosts = new Set([
  "archive.d-robotics.cc",
  "developer.d-robotics.cc",
  "github.com",
  "toolchain.d-robotics.cc",
]);
const ids = new Set();
const files = new Set();

async function walkMarkdown(directory) {
  const found = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) found.push(...await walkMarkdown(path));
    else if (entry.isFile() && entry.name.endsWith(".md")) found.push(path);
  }
  return found;
}

if (manifest.schemaVersion !== 1) throw new Error("knowledge schemaVersion must be 1");
if (!/^\d{4}\.\d{2}\.\d+$/.test(manifest.knowledgeVersion)) {
  throw new Error("knowledgeVersion must use YYYY.MM.N format");
}
if (!/^\d{4}-\d{2}-\d{2}$/.test(manifest.updatedAt)) throw new Error("updatedAt must use YYYY-MM-DD");
if (!Array.isArray(manifest.documents) || manifest.documents.length === 0) {
  throw new Error("knowledge manifest has no documents");
}

for (const document of manifest.documents) {
  if (!document.id || ids.has(document.id)) throw new Error(`duplicate or empty document id: ${document.id}`);
  ids.add(document.id);
  if (!document.file || files.has(document.file)) throw new Error(`duplicate or empty knowledge file: ${document.file}`);
  files.add(document.file);
  if (!Array.isArray(document.boards) || document.boards.some((board) => !allowedBoards.has(board))) {
    throw new Error(`invalid boards for ${document.id}`);
  }
  if (!Array.isArray(document.rdkOs) || document.rdkOs.length === 0) {
    throw new Error(`missing RDK OS applicability for ${document.id}`);
  }
  if (!Array.isArray(document.topics) || document.topics.length === 0) {
    throw new Error(`missing topics for ${document.id}`);
  }
  const path = resolve(root, document.file);
  if (!path.startsWith(`${root}${sep}`)) throw new Error(`document escapes knowledge root: ${document.file}`);
  const body = await readFile(path, "utf8");
  if (!body.startsWith("# ") || body.length < 300) throw new Error(`knowledge document is incomplete: ${document.file}`);
  if (!body.includes(`> 资料核对日期：${manifest.updatedAt}`)) {
    throw new Error(`knowledge document has no current review date: ${document.file}`);
  }
  if (!body.includes("## 官方来源")) throw new Error(`knowledge document has no official source section: ${document.file}`);
  if (/\b(?:sk-[A-Za-z0-9_-]{12,}|docker\s+login\b[^\n]*\s-p\s+['"]?\S+)/iu.test(body)) {
    throw new Error(`knowledge document appears to contain a credential: ${document.file}`);
  }
  await stat(path);
  if (!Array.isArray(document.sources) || document.sources.length < 2) {
    throw new Error(`knowledge document needs at least two official sources: ${document.id}`);
  }
  for (const source of document.sources) {
    if (!source.title || !source.url) throw new Error(`incomplete source metadata in ${document.id}`);
    const url = new URL(source.url);
    if (url.protocol !== "https:") throw new Error(`non-HTTPS source in ${document.id}: ${source.url}`);
    if (!officialHosts.has(url.hostname)) throw new Error(`non-official source in ${document.id}: ${source.url}`);
    if (url.hostname === "developer.d-robotics.cc" && url.pathname.startsWith("/rdk_doc/") && !source.title.includes("归档")) {
      throw new Error(`archived source must be labeled in ${document.id}: ${source.url}`);
    }
    if (url.hostname === "github.com" && url.pathname !== "/D-Robotics" && !url.pathname.startsWith("/D-Robotics/")) {
      throw new Error(`non-D-Robotics GitHub source in ${document.id}: ${source.url}`);
    }
    if (!body.includes(source.url)) throw new Error(`source is not cited in ${document.file}: ${source.url}`);
  }
}

const unlisted = (await walkMarkdown(root))
  .map((path) => relative(root, path).split(sep).join("/"))
  .filter((file) => !files.has(file));
if (unlisted.length > 0) throw new Error(`knowledge markdown is missing from manifest:\n${unlisted.join("\n")}`);

console.log(`Validated RDK knowledge ${manifest.knowledgeVersion}: ${manifest.documents.length} documents`);

import { readFile, stat } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = resolve(repository, "knowledge");
const manifest = JSON.parse(await readFile(resolve(root, "manifest.json"), "utf8"));
const allowedBoards = new Set(["all", "x5", "s100", "s600"]);
const officialHosts = new Set(["developer.d-robotics.cc", "github.com"]);
const ids = new Set();

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
  if (!path.startsWith(`${root}/`)) throw new Error(`document escapes knowledge root: ${document.file}`);
  const body = await readFile(path, "utf8");
  if (!body.startsWith("# ") || body.length < 300) throw new Error(`knowledge document is incomplete: ${document.file}`);
  await stat(path);
  for (const source of document.sources ?? []) {
    const url = new URL(source.url);
    if (!officialHosts.has(url.hostname)) throw new Error(`non-official source in ${document.id}: ${source.url}`);
    if (url.hostname === "github.com" && !url.pathname.startsWith("/D-Robotics/")) {
      throw new Error(`non-D-Robotics GitHub source in ${document.id}: ${source.url}`);
    }
  }
}

console.log(`Validated RDK knowledge ${manifest.knowledgeVersion}: ${manifest.documents.length} documents`);

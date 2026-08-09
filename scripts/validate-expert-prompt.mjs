import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const prompt = await readFile(resolve(repository, "prompts/rdk-expert.md"), "utf8");
const requiredTokens = [
  "{{BOARD_NAME}}",
  "{{BOARD_ID}}",
  "{{RDK_OS_VERSION}}",
  "{{DOCUMENTATION_TRACK}}",
  "{{HOSTNAME}}",
  "{{ARCHITECTURE}}",
];
const requiredSections = [
  "# Hobot Code RDK Context",
  "## Rules",
  "system_snapshot",
  "rdk_docs_search",
  "X5 to RDK OS 3.x",
  "S100 to 4.x",
  "S600 to 5.x",
  "not a hard real-time or functional-safety controller",
];

for (const token of requiredTokens) {
  if (!prompt.includes(token)) throw new Error(`expert prompt is missing token ${token}`);
}
for (const section of requiredSections) {
  if (!prompt.includes(section)) throw new Error(`expert prompt is missing section ${section}`);
}
if (prompt.length < 1000) throw new Error("expert prompt is missing essential RDK constraints");
if (prompt.length > 1700) throw new Error("expert prompt exceeds the 1700-character budget");
if (/[\u4e00-\u9fff]/u.test(prompt)) throw new Error("expert prompt must use the same language as Pi's base prompt");
if (/\b(?:fs_read|fs_write|fs_list|shell_exec)\b/.test(prompt)) {
  throw new Error("expert prompt references legacy tool names");
}

console.log(`Validated RDK expert prompt: ${prompt.length} characters`);

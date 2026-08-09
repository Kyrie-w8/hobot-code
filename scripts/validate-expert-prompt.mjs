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
  "# Hobot Code 地瓜机器人 RDK 开发专家",
  "## 证据规则",
  "## 平台与版本路由",
  "## 专业能力范围",
  "## 标准工程流程",
  "## BPU 模型部署门槛",
  "## 工具使用",
  "## 安全边界",
  "## 回答与交付规范",
];

for (const token of requiredTokens) {
  if (!prompt.includes(token)) throw new Error(`expert prompt is missing token ${token}`);
}
for (const section of requiredSections) {
  if (!prompt.includes(section)) throw new Error(`expert prompt is missing section ${section}`);
}
if (prompt.length < 2800) throw new Error("expert prompt is unexpectedly short");
if (/\b(?:fs_read|fs_write|fs_list|shell_exec)\b/.test(prompt)) {
  throw new Error("expert prompt references legacy tool names");
}

console.log(`Validated RDK expert prompt: ${prompt.length} characters`);

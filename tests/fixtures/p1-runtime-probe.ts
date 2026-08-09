import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { GoalStore } from "../../extensions/rdk/goal-store.ts";
import { runHooks } from "../../extensions/rdk/hook-runner.ts";
import { LspManager } from "../../extensions/rdk/lsp-manager.ts";

export default function p1RuntimeProbe(pi: ExtensionAPI) {
  pi.registerCommand("p1-probe", {
    description: "Run Hobot Code P1 integration probe",
    handler: async (_args, ctx) => {
      const root = process.env.HOBOT_P1_PROBE_ROOT || "/tmp/hobot-p1-runtime-probe";
      const fixtureRoot = fileURLToPath(new URL(".", import.meta.url));
      rmSync(root, { recursive: true, force: true });
      mkdirSync(root, { recursive: true, mode: 0o700 });

      const goalStore = new GoalStore(resolve(root, "goals.db"));
      const created = goalStore.create({ project: root, objective: "probe P1", turnBudget: 2, tokenBudget: 2000, session: "one" });
      goalStore.restore(root, "two");
      const paused = goalStore.consumeTurn(root, 2100, 50);
      if (!paused || paused.status !== "paused" || paused.continuationCount !== 1) throw new Error("goal budget or restore failed");
      goalStore.extend(root, 2, 2000);
      const completed = goalStore.complete({ project: root, outcome: "probe passed", actor: "user", verificationStatus: "passed" });
      if (completed.status !== "completed") throw new Error("goal completion failed");
      goalStore.close();

      const hookConfig = {
        schemaVersion: 1 as const,
        enabled: true,
        failurePolicy: "block" as const,
        timeoutMs: 3000,
        maxOutputChars: 2000,
        allowProjectHooks: false,
        hooks: [
          { name: "pre", event: "PreToolUse" as const, tool: "bash", command: ["/usr/bin/python3", resolve(fixtureRoot, "p1-hook.py")] },
          { name: "post", event: "PostToolUse" as const, tool: "read", command: ["/usr/bin/python3", resolve(fixtureRoot, "p1-hook.py")] },
        ],
      };
      const pre = await runHooks({
        config: hookConfig,
        event: "PreToolUse",
        toolName: "bash",
        toolCallId: "pre-1",
        cwd: root,
        input: { command: "blocked-marker" },
        auditPath: resolve(root, "hooks.jsonl"),
      });
      if (!pre.blocked) throw new Error("PreToolUse hook did not block");
      const post = await runHooks({
        config: hookConfig,
        event: "PostToolUse",
        toolName: "read",
        toolCallId: "post-1",
        cwd: root,
        input: { path: "sample.ts" },
        result: { content: [], details: {}, isError: false },
        auditPath: resolve(root, "hooks.jsonl"),
      });
      if (!post.appendText?.includes("observed")) throw new Error("PostToolUse hook did not modify output");

      writeFileSync(resolve(root, "sample.ts"), "export const value = 1;\n");
      const manager = new LspManager({
        schemaVersion: 1,
        enabled: true,
        maxProcesses: 1,
        maxMemoryMiB: 256,
        idleTimeoutMs: 300,
        requestTimeoutMs: 3000,
        diagnosticsWaitMs: 100,
        servers: [{
          id: "fixture",
          extensions: [".ts"],
          languageId: "typescript",
          command: ["/usr/bin/python3", resolve(fixtureRoot, "fake-lsp.py")],
        }],
      });
      const hover = await manager.query({ action: "hover", path: "sample.ts", root, line: 1, column: 14 });
      const diagnostics = await manager.query({ action: "diagnostics", path: "sample.ts", root });
      if (!JSON.stringify(hover).includes("fixture hover")) throw new Error("LSP hover failed");
      if (!JSON.stringify(diagnostics).includes("fixture diagnostic")) throw new Error("LSP diagnostics failed");
      if ((manager.status().running as unknown[]).length !== 1) throw new Error("LSP process tracking failed");
      await new Promise((resolveWait) => setTimeout(resolveWait, 600));
      if ((manager.status().running as unknown[]).length !== 0) throw new Error("LSP idle shutdown failed");
      ctx.ui.notify(`P1_RUNTIME_PROBE=passed goal=${created.id}`, "info");
    },
  });
}

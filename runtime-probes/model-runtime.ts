import { Type } from "@earendil-works/pi-ai";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const PROBE_NONCE = "hobot-runtime-probe-v1";
const PARALLEL_NONCES = new Set(["parallel-a", "parallel-b"]);
const RECOVERY_NONCE = "repaired-after-error";

function probeResult(text: string, stage: string) {
  return {
    content: [{ type: "text" as const, text }],
    details: { schemaVersion: 1, readOnly: true, stage },
  };
}

export default function runtimeProbe(pi: ExtensionAPI): void {
  if (process.env.HOBOT_CODE_RUNTIME_PROBE !== "1") {
    throw new Error("The Hobot Code runtime probe extension is not available in normal sessions");
  }

  pi.registerTool({
    name: "hobot_runtime_probe",
    label: "Hobot runtime probe",
    description: "Run one deterministic Hobot Code Agent runtime probe step. This tool is read-only. For recovery, invalid-on-purpose returns an expected error and repaired-after-error succeeds.",
    promptSnippet: "Complete the explicit Hobot Code Agent runtime protocol probe",
    parameters: Type.Object({
      stage: Type.Union([
        Type.Literal("basic"),
        Type.Literal("parallel"),
        Type.Literal("recovery"),
        Type.Literal("approval"),
        Type.Literal("interrupt"),
      ]),
      nonce: Type.Union([
        Type.Literal(PROBE_NONCE),
        Type.Literal("parallel-a"),
        Type.Literal("parallel-b"),
        Type.Literal("invalid-on-purpose"),
        Type.Literal(RECOVERY_NONCE),
        Type.Literal("confirm-read-only"),
        Type.Literal("wait-for-termination"),
      ]),
    }, { additionalProperties: false }),
    async execute(_toolCallId, params, signal, _onUpdate, ctx) {
      if (params.stage === "basic" && params.nonce === PROBE_NONCE) {
        return probeResult("HOBOT_RUNTIME_PROBE_OK", params.stage);
      }
      if (params.stage === "parallel" && PARALLEL_NONCES.has(params.nonce)) {
        await new Promise((resolve) => setTimeout(resolve, 25));
        return probeResult(`HOBOT_RUNTIME_PARALLEL_${params.nonce === "parallel-a" ? "A" : "B"}`, params.stage);
      }
      if (params.stage === "recovery" && params.nonce === "invalid-on-purpose") {
        throw new Error("HOBOT_RUNTIME_PROBE_EXPECTED_ARGUMENT_ERROR");
      }
      if (params.stage === "recovery" && params.nonce === RECOVERY_NONCE) {
        return probeResult("HOBOT_RUNTIME_RECOVERY_OK", params.stage);
      }
      if (params.stage === "approval" && params.nonce === "confirm-read-only") {
        const confirmed = await ctx.ui.confirm(
          "Hobot Code runtime probe",
          "Allow this isolated read-only approval probe?",
        );
        if (!confirmed) throw new Error("HOBOT_RUNTIME_PROBE_APPROVAL_DENIED");
        return probeResult("HOBOT_RUNTIME_APPROVAL_OK", params.stage);
      }
      if (params.stage === "interrupt" && params.nonce === "wait-for-termination") {
        await new Promise<never>((_resolve, reject) => {
          if (signal.aborted) {
            reject(new Error("HOBOT_RUNTIME_PROBE_INTERRUPTED"));
            return;
          }
          signal.addEventListener("abort", () => reject(new Error("HOBOT_RUNTIME_PROBE_INTERRUPTED")), { once: true });
        });
      }
      throw new Error("runtime probe stage and nonce do not match");
    },
  });

  pi.on("session_start", () => {
    pi.setActiveTools(["hobot_runtime_probe"]);
  });
}

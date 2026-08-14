#!/bin/sh

if [ "${1:-}" = "--offline" ] && [ "${2:-}" = "--list-models" ]; then
  printf '%s\n' \
    'provider model context max-out thinking images' \
    'drobotics kimi-k3 1M 8K yes yes' \
    'drobotics text-only 1M 8K yes no'
  exit 0
fi

if [ "${HOBOT_CODE_RDK_PROBE:-}" = 1 ]; then
  arguments=" $* "
  for required in '--mode rpc' '--no-session' '--no-builtin-tools' '--no-extensions' '--no-skills' '--no-prompt-templates' '--no-context-files'; do
    case "$arguments" in
      *" $required "*) ;;
      *) printf 'RDK probe omitted %s\n' "$required" >&2; exit 2 ;;
    esac
  done
  while IFS= read -r line; do
    case "$line" in
      *'"id":"rdk-state"'*'"type":"get_state"'*)
        printf '%s\n' '{"type":"response","id":"rdk-state","command":"get_state","success":true,"data":{"model":{"provider":"drobotics","id":"kimi-k3"}}}'
        ;;
      *'"id":"rdk-diagnostic"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"rdk-diagnostic","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"rdk-snapshot-1","toolName":"system_snapshot","args":{"includeProcesses":false}}'
        printf '%s\n' '{"type":"tool_execution_end","toolCallId":"rdk-snapshot-1","toolName":"system_snapshot","result":{"content":[{"type":"text","text":"{\"board\":\"D-Robotics RDK S600\",\"boardId\":\"s600\",\"rdkOsVersion\":\"5.0.0\",\"architecture\":\"arm64\",\"bpuDevices\":[\"/dev/bpu0\"],\"rdkUtilities\":{\"hrt_model_exec\":true}}"}]},"isError":false}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"rdk-knowledge-1","toolName":"rdk_docs_search","args":{"query":"system version logs temperature memory diagnostics","board":"s600","topic":"diagnostics","limit":3}}'
        printf '%s\n' '{"type":"tool_execution_end","toolCallId":"rdk-knowledge-1","toolName":"rdk_docs_search","result":{"content":[{"type":"text","text":"{\"knowledgeVersion\":\"2026.08.3\",\"updatedAt\":\"2026-08-10\",\"detectedBoard\":\"s600\",\"detectedRdkOs\":\"5.0.0\",\"results\":[{\"versionMatch\":true,\"sources\":[{\"url\":\"https://developer.d-robotics.cc/rdk_s_doc/RDK\"}]}]}"}]},"isError":false}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"{\"boardId\":\"s600\",\"rdkOsVersion\":\"5.0.0\",\"architecture\":\"arm64\",\"knowledgeVersion\":\"2026.08.3\",\"sourceUrl\":\"https://developer.d-robotics.cc/rdk_s_doc/RDK\",\"evidenceStatus\":\"complete\",\"signals\":[\"board-identified\",\"bpu-visible\",\"runtime-tool-visible\",\"docs-version-match\"]}"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
    esac
  done
  exit 0
fi

if [ "${HOBOT_CODE_RUNTIME_PROBE:-}" = 1 ]; then
  arguments=" $* "
  for required in '--mode rpc' '--no-builtin-tools' '--no-extensions' '--no-skills' '--no-prompt-templates' '--no-context-files'; do
    case "$arguments" in
      *" $required "*) ;;
      *) printf 'runtime probe omitted %s\n' "$required" >&2; exit 2 ;;
    esac
  done
  probe_phase=${HOBOT_CODE_RUNTIME_PROBE_PHASE:-ephemeral}
  session_dir=
  session_file=
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --session-dir) session_dir=$2; shift 2 ;;
      --session) session_file=$2; shift 2 ;;
      *) shift ;;
    esac
  done
  case "$probe_phase" in
    ephemeral)
      case "$arguments" in *' --no-session '*) ;; *) exit 2 ;; esac
      ;;
    persistent)
      [ -n "$session_dir" ] || exit 2
      session_file="$session_dir/fake-runtime-session.jsonl"
      ;;
    resume)
      [ -n "$session_dir" ] && [ -n "$session_file" ] && [ -f "$session_file" ] || exit 2
      ;;
    *) exit 2 ;;
  esac
  while IFS= read -r line; do
    case "$line" in
      *'"id":"runtime-thinking-level"'*'"type":"set_thinking_level"'*)
        printf '%s\n' '{"type":"response","id":"runtime-thinking-level","command":"set_thinking_level","success":true}'
        ;;
      *'"id":"runtime-thinking-state"'*'"type":"get_state"'*)
        printf '%s\n' '{"type":"response","id":"runtime-thinking-state","command":"get_state","success":true,"data":{"model":{"provider":"drobotics","id":"kimi-k3"},"thinkingLevel":"high"}}'
        ;;
      *'"type":"get_state"'*)
        case "$line" in
          *'"id":"persistent-state"'*)
            printf '{"type":"response","id":"persistent-state","command":"get_state","success":true,"data":{"model":{"provider":"drobotics","id":"kimi-k3"},"sessionFile":"%s","sessionId":"fake-runtime-session"}}\n' "$session_file"
            ;;
          *'"id":"resumed-state"'*)
            printf '{"type":"response","id":"resumed-state","command":"get_state","success":true,"data":{"model":{"provider":"drobotics","id":"kimi-k3"},"sessionFile":"%s","sessionId":"fake-runtime-session"}}\n' "$session_file"
            ;;
          *)
            printf '%s\n' '{"type":"response","command":"get_state","success":true,"data":{"model":{"provider":"drobotics","id":"kimi-k3"}}}'
            ;;
        esac
        ;;
      *'"id":"runtime-basic"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"runtime-basic","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"probe-1","toolName":"hobot_runtime_probe","args":{"stage":"basic","nonce":"hobot-runtime-probe-v1"}}'
        printf '%s\n' '{"type":"tool_execution_end","toolCallId":"probe-1","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_PROBE_OK"}]},"isError":false}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_PROBE_COMPLETE"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"runtime-parallel"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"runtime-parallel","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"parallel-1","toolName":"hobot_runtime_probe","args":{"stage":"parallel","nonce":"parallel-a"}}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"parallel-2","toolName":"hobot_runtime_probe","args":{"stage":"parallel","nonce":"parallel-b"}}'
        printf '%s\n' '{"type":"tool_execution_end","toolCallId":"parallel-2","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_PARALLEL_B"}]},"isError":false}'
        printf '%s\n' '{"type":"tool_execution_end","toolCallId":"parallel-1","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_PARALLEL_A"}]},"isError":false}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_PARALLEL_COMPLETE"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"runtime-recovery"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"runtime-recovery","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"recovery-1","toolName":"hobot_runtime_probe","args":{"stage":"recovery","nonce":"invalid-on-purpose"}}'
        printf '%s\n' '{"type":"tool_execution_end","toolCallId":"recovery-1","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_PROBE_EXPECTED_ARGUMENT_ERROR"}]},"isError":true}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"recovery-2","toolName":"hobot_runtime_probe","args":{"stage":"recovery","nonce":"repaired-after-error"}}'
        printf '%s\n' '{"type":"tool_execution_end","toolCallId":"recovery-2","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_RECOVERY_OK"}]},"isError":false}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_RECOVERY_COMPLETE"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"runtime-thinking"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"runtime-thinking","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}'
        printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"deterministic reasoning"}}'
        printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0,"content":"deterministic reasoning"}}'
        printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"thinking","thinking":"deterministic reasoning"},{"type":"text","text":"HOBOT_RUNTIME_THINKING_COMPLETE"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"runtime-approval"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"runtime-approval","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"approval-1","toolName":"hobot_runtime_probe","args":{"stage":"approval","nonce":"confirm-read-only"}}'
        printf '%s\n' '{"type":"extension_ui_request","id":"runtime-approval-confirm","method":"confirm","title":"Hobot Code runtime probe","message":"Allow this isolated read-only approval probe?"}'
        ;;
      *'"id":"runtime-approval-confirm"'*'"type":"extension_ui_response"'*)
        printf '%s\n' '{"type":"tool_execution_end","toolCallId":"approval-1","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_APPROVAL_OK"}]},"isError":false}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_APPROVAL_COMPLETE"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"runtime-image"'*'"mimeType":"image/png"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"runtime-image","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"red green blue yellow"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"runtime-image"'*)
        printf '%s\n' '{"type":"response","id":"runtime-image","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"persistent-seed"'*'"type":"prompt"'*)
        printf '%s\n' \
          '{"type":"session","version":3,"id":"fake-runtime-session","cwd":"/tmp"}' \
          '{"type":"message","id":"seed-user","parentId":null,"message":{"role":"user","content":[{"type":"text","text":"HOBOT_COMPACTION_CANARY_7F3C9A2D"}]}}' \
          '{"type":"message","id":"seed-assistant","parentId":"seed-user","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_MEMORY_STORED"}]}}' > "$session_file"
        chmod 0600 "$session_file"
        printf '%s\n' '{"type":"response","id":"persistent-seed","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_MEMORY_STORED"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"persistent-filler"'*'"type":"prompt"'*)
        printf '%s\n' \
          '{"type":"message","id":"filler-user","parentId":"seed-assistant","message":{"role":"user","content":[{"type":"text","text":"bounded-context-padding"}]}}' \
          '{"type":"message","id":"filler-assistant","parentId":"filler-user","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_FILLER_STORED"}]}}' >> "$session_file"
        printf '%s\n' '{"type":"response","id":"persistent-filler","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_FILLER_STORED"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"persistent-compact"'*'"type":"compact"'*)
        printf '%s\n' '{"type":"compaction","id":"compact-entry","parentId":"filler-assistant","summary":"The user asked to preserve HOBOT_COMPACTION_CANARY_7F3C9A2D.","firstKeptEntryId":"filler-user","tokensBefore":200}' >> "$session_file"
        printf '%s\n' '{"type":"compaction_start","reason":"manual"}'
        printf '%s\n' '{"type":"compaction_end","reason":"manual","result":{"summary":"The user asked to preserve an opaque identifier.","firstKeptEntryId":"filler-user","tokensBefore":200,"estimatedTokensAfter":90},"aborted":false,"willRetry":false}'
        printf '%s\n' '{"type":"response","id":"persistent-compact","command":"compact","success":true,"data":{"summary":"The user asked to preserve an opaque identifier.","firstKeptEntryId":"filler-user","tokensBefore":200,"estimatedTokensAfter":90}}'
        ;;
      *'"id":"persistent-after-compact"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"persistent-after-compact","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_COMPACTION_CANARY_7F3C9A2D"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
      *'"id":"persistent-interrupt"'*'"type":"prompt"'*)
        printf '%s\n' \
          '{"type":"message","id":"interrupt-user","parentId":"compact-entry","message":{"role":"user","content":[{"type":"text","text":"HOBOT_RECOVERY_CANARY_4B8E1D6A"}]}}' \
          '{"type":"message","id":"interrupt-assistant","parentId":"interrupt-user","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"toolCall","id":"interrupt-call-1","name":"hobot_runtime_probe","arguments":{"stage":"interrupt","nonce":"wait-for-termination"}}]}}' >> "$session_file"
        printf '%s\n' '{"type":"response","id":"persistent-interrupt","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"tool_execution_start","toolCallId":"interrupt-call-1","toolName":"hobot_runtime_probe","args":{"stage":"interrupt","nonce":"wait-for-termination"}}'
        ;;
      *'"id":"resumed-prompt"'*'"type":"prompt"'*)
        printf '%s\n' '{"type":"response","id":"resumed-prompt","command":"prompt","success":true}'
        printf '%s\n' '{"type":"agent_start"}'
        printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RECOVERY_CANARY_4B8E1D6A"}]}}'
        printf '%s\n' '{"type":"agent_settled"}'
        ;;
    esac
  done
  exit 0
fi

session_dir=
session_file=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --session-dir) session_dir=$2; shift 2 ;;
    --session) session_file=$2; shift 2 ;;
    *) shift ;;
  esac
done
if [ -z "$session_file" ]; then
  session_file="$session_dir/fake-session.jsonl"
  : > "$session_file"
  chmod 0600 "$session_file"
fi

while IFS= read -r line; do
  case "$line" in
    *'"type":"get_state"'*)
      printf '{"type":"response","command":"get_state","success":true,"data":{"sessionFile":"%s","sessionId":"fake-session"}}\n' "$session_file"
      ;;
    *'"message":"approval-test"'*)
      printf '%s\n' '{"type":"response","command":"prompt","success":true}'
      printf '%s\n' '{"type":"agent_start"}'
      printf '%s\n' '{"type":"extension_ui_request","id":"approval-1","method":"confirm","title":"Run test?","message":"Confirm execution"}'
      ;;
    *'"message":"sandbox-probe"'*)
      workspace_write=blocked
      if touch .hobot-sandbox-probe-write 2>/dev/null; then
        workspace_write=allowed
        rm -f .hobot-sandbox-probe-write
      fi
      system_write=blocked
      if mkdir /etc/.hobot-sandbox-probe 2>/dev/null; then
        system_write=allowed
        rmdir /etc/.hobot-sandbox-probe
      fi
      policy_write=blocked
      if touch "$(dirname "$HOBOT_CODE_PERMISSION_POLICY")/.probe" 2>/dev/null; then
        policy_write=allowed
        rm -f "$(dirname "$HOBOT_CODE_PERMISSION_POLICY")/.probe"
      fi
      devices=minimal
      if [ -e /dev/bpu ]; then devices=rdk; fi
      capabilities=$(sed -n 's/^CapEff:[[:space:]]*//p' /proc/self/status)
      network=unavailable
      if getent hosts ai-api.d-robotics.cc >/dev/null 2>&1; then network=available; fi
      printf '%s\n' '{"type":"response","command":"prompt","success":true}'
      printf '%s\n' '{"type":"agent_start"}'
      printf '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"mode=%s workspace=%s system=%s policy=%s devices=%s capabilities=%s network=%s"}}\n' \
        "${HOBOT_CODE_SANDBOX_MODE:-unknown}" "$workspace_write" "$system_write" "$policy_write" "$devices" "$capabilities" "$network"
      printf '%s\n' '{"type":"agent_settled"}'
      ;;
    *'"type":"prompt"'*)
      printf '%s\n' '{"type":"response","command":"prompt","success":true}'
      printf '%s\n' '{"type":"agent_start"}'
      printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"background ok"}}'
      printf '%s\n' '{"type":"agent_settled"}'
      ;;
    *'"type":"abort"'*)
      printf '%s\n' '{"type":"response","command":"abort","success":true}'
      ;;
    *'"type":"extension_ui_response"'*)
      printf '%s\n' '{"type":"agent_settled"}'
      ;;
  esac
done

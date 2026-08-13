#!/bin/sh

if [ "${1:-}" = "--offline" ] && [ "${2:-}" = "--list-models" ]; then
  printf '%s\n' \
    'provider model context max-out thinking images' \
    'drobotics kimi-k3 1M 8K yes yes' \
    'drobotics text-only 1M 8K yes no'
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

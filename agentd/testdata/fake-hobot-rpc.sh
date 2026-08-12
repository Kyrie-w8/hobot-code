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

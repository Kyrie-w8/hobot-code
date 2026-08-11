#!/bin/sh

while IFS= read -r line; do
  case "$line" in
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

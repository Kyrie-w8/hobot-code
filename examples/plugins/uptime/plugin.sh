#!/bin/sh
read -r request
uptime_value=$(cut -d ' ' -f 1 /proc/uptime 2>/dev/null || printf unknown)
printf '{"jsonrpc":"2.0","id":1,"result":{"uptime_seconds":"%s"}}\n' "$uptime_value"

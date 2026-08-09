import json
import sys

payload = json.load(sys.stdin)
if payload["event"] == "PreToolUse" and "blocked-marker" in payload.get("input", {}).get("command", ""):
    print(json.dumps({"block": True, "reason": "fixture policy blocked the marker"}))
elif payload["event"] == "PostToolUse":
    print(json.dumps({"appendText": "fixture post hook observed the result"}))
else:
    print("{}")

---
name: system-info
description: Inspect live board resources and runtime health.
version: "2"
required_tools: [system_snapshot]
board_profiles: []
---

When the user asks about the current board, CPU, memory, disk, BPU, kernel,
devices, or temperature, call `system_snapshot`. Distinguish live measurements from configured
profile metadata. Do not infer BPU model compatibility or available compute capacity
from the device node count alone.

---
name: system-info
description: Inspect current board resources and runtime health using a read-only snapshot.
version: "1"
required_tools:
  - system.snapshot
board_profiles: []
---

When the user asks about the current board, CPU, memory, disk, BPU, kernel, Python,
or temperature, call `system.snapshot`. Distinguish live measurements from configured
profile metadata. Do not infer BPU model compatibility or available compute capacity
from the device node count alone.

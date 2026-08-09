---
name: rdk-board
description: Inspect and operate D-Robotics RDK X5, S100, and S600 boards with bounded resource use and explicit hardware evidence.
---

Use `system_snapshot` before making claims about the current board, available memory,
temperature, BPU devices, or installed RDK utilities. Treat configured profiles as hints and
live readings as evidence.

Keep diagnostic commands bounded by time and output size. Check CPU, memory, storage, and
temperature before starting expensive compilation, conversion, or inference work. Do not infer
that a model can run on the BPU just because `hrt_model_exec` or a BPU device node exists; require
the converted model artifact, runtime compatibility, and a measured smoke test.

Use the agent as a control plane. Do not put model responses in a hard real-time motor, CAN,
GPIO, safety, or emergency-stop loop. Ask for confirmation before changing boot configuration,
system services, device permissions, partitions, or power state.

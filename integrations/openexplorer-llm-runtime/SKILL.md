---
name: openexplorer-llm-runtime
description: Validate and use the OpenExplorer LLM 2.x board runtime and samples on an RDK S600 without treating host-side CUDA tooling as board software.
---

Source: the user-provided `OpenExplorer_LLM` delivery package. Runtime version is taken from
`oellm_runtime/include/oellm_runtime_basic/oellm_runtime_version.h`; sample behavior is taken from
the scripts and source files under `oellm_runtime/examples/`.

Use this skill only when `system_snapshot` identifies an S600 and
`HOBOT_CODE_OPENEXPLORER_LLM_ROOT` points to a verified Capability. The package's
`llm_compression/` and `package/host/` directories require Linux x86_64, Python 3.10, NVIDIA CUDA,
and substantial host storage. Never install or run those host tools on the ARM64 board.

Before running a sample:

1. Check current memory, temperature, BPU use, disk space, and existing inference processes.
2. Resolve the runtime as `$HOBOT_CODE_OPENEXPLORER_LLM_ROOT/oellm_runtime` and remain inside it.
3. Use a bounded timeout and a non-conflicting port. Do not stop unrelated board workloads.
4. Do not claim model inference passed unless the referenced HBM, embedding weights, tokenizer,
   and config all exist and the sample returns a real response.

Useful model-free validation:

- Run `examples/serving_demo/run_serving_demo.sh --no-load -p <port>`, query `/v1/models`, then
  stop only that server process.
- Build and run `examples/vla_demo/minimal_policy`; it intentionally loads no model and should
  emit 14 fixed action values.
- Run each packaged `build_demo.sh` to verify headers and shared-library linkage when resources
  permit. A successful build is API/ABI evidence, not model accuracy evidence.

Model-backed samples under `simple_demo`, `chat_demo`, `batch_request_demo`, `image_data_demo`,
`performance_demo`, and `runtime_cicd` require artifacts matching the exact 2.x JSON config.
Report missing or version-mismatched artifacts as blocked rather than reusing an older runtime's
models by filename alone.

# RT-IGEV on RDK X5

This is the acceptance workload for Hobot Code's model deployment loop. RT-IGEV is the real-time stereo model in the official IGEV++ repository; its authors publish PyTorch weights and evaluation scripts, but no ONNX or RDK X5 artifact.

## Pinned sources

- Architecture and evaluation: `gangweix/IGEV-plusplus`, commit `099dec97989780d151700459c682df1cc2c18e88`
  - https://github.com/gangweix/IGEV-plusplus
- RDK X5 conversion target: `march: bayes-e`
  - https://d-robotics.github.io/rdk_doc/rdk_s/FAQ/toolchain/
- Conversion procedure and model checker requirement
  - https://d-robotics.github.io/rdk_doc/Advanced_development/toolchain_development/intermediate/ptq_process/
- RDK X5 operator constraints
  - https://d-robotics.github.io/rdk_doc/Advanced_development/toolchain_development/intermediate/supported_op_list/

## Architecture risk

RT-IGEV is not a single conventional image CNN. It constructs a group-wise correlation volume, runs 3D convolution/deconvolution, regresses disparity, samples the geometry volume and executes an iterative ConvGRU update. A monolithic ONNX export is therefore only the first probe, not the assumed production design.

The deployment task must evaluate these options in order:

1. Export the fixed-shape, fixed-iteration graph and run the X5 model checker.
2. If recurrent or sampling operators fall back to CPU or fail conversion, partition the network into BPU subgraphs and keep orchestration/update logic on the CPU.
3. If 3D cost aggregation is unsupported or misses latency targets, replace only that deployment subgraph with an equivalent X5-friendly implementation and re-run the floating-point comparison. Do not silently switch to another stereo model.

## Reproducible acceptance profile

- Inputs: rectified RGB stereo pair, batch 1, fixed dimensions divisible by 32.
- Initial deployment shape: `1x3x256x320` per image, maximum disparity 192, 8 update iterations. Increase resolution only after the first accepted result.
- Reference: official Scene Flow checkpoint evaluated by the pinned PyTorch code.
- Calibration: a documented, deterministic subset of the same preprocessing domain; calibration and evaluation samples must not overlap.
- Accuracy: evaluate a named Scene Flow subset. Required metrics are absolute EPE and D1 plus the quantized-minus-reference delta. Initial acceptance is EPE increase <= 0.10 px and D1 increase <= 0.50 percentage points. These thresholds compare the X5 result with the exported floating-point reference at the same shape and iteration count, not with the paper's different hardware/runtime number.
- Performance: at least 5 warmups and 20 measured runs. Report model-only and end-to-end p50/p95 plus throughput.
- Resources: sample before, during and after the run. Require peak temperature <= 85 C and available system memory >= 256 MiB; report BPU utilization and ION allocation without substituting CPU values when a metric is unavailable.

## Toolchain topology

The HBDK conversion environment is an x86 Linux build dependency; it is not assumed to exist on the ARM64 RDK X5. Hobot Code should retain the X5 as the bound target while running export/conversion on a declared x86 worker, then transfer the digest-verified artifact into the selected X5 workspace for runtime validation. Conversion logs, toolchain version, configuration and intermediate ONNX must remain in the deployment workspace.

## Definition of done

The deployment is complete only when `deployment.status` accepts a schema v2 report. A generated `.bin`, a model checker pass, a visual disparity image, or a standalone `hrt_model_exec perf` result is insufficient by itself.

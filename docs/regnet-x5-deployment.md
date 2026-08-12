# RegNet-X-400MF on RDK X5

This is Hobot Code's end-to-end acceptance workload for a model that is not supplied by the pinned D-Robotics RDK X5 Model Zoo snapshot. Machine-readable provenance is retained in `examples/regnet-x5/source-lock.json`.

## Scope and pinned sources

- Model and pretrained weights: torchvision `RegNet_X_400MF_Weights.IMAGENET1K_V2`
  - https://docs.pytorch.org/vision/main/models/generated/torchvision.models.regnet_x_400mf.html
  - The official metadata reports 74.864% ImageNet-1K Top-1, 5,495,976 parameters and 0.41 GFLOPS.
- D-Robotics official RDK X5 Model Zoo: branch `rdk_x5`, commit `76a1abc878b52f381aed7fbf62f35307d984cc72`
  - https://github.com/D-Robotics/rdk_model_zoo/tree/76a1abc878b52f381aed7fbf62f35307d984cc72
  - Its complete, non-truncated 1,125-path Git tree contains no `regnet` path. This claim is limited to that pinned official snapshot; it is not a claim about all third-party deployments.
- Dataset: Imagenette2-160 from fast.ai
  - https://s3.amazonaws.com/fast-ai-imageclas/imagenette2-160.tgz
  - Archive SHA-256: `64d0c4859f35a461889e0147755a999a48b49bf38a7e0f9bd27003f10db02fe5`
- X5 PTQ procedure and operator constraints:
  - https://d-robotics.github.io/rdk_doc/Advanced_development/toolchain_development/intermediate/ptq_process/
  - https://d-robotics.github.io/rdk_doc/Advanced_development/toolchain_development/intermediate/supported_op_list/

MobileOne was used during initial plumbing validation but is not the final acceptance model: the same pinned official Model Zoo tree contains `samples/vision/mobileone`. RT-IGEV remains a documented negative feasibility case because its converted monolithic graph did not meet the real-time threshold.

## Frozen acceptance profile

The `regnet-x-400mf-x5` profile is frozen by `agentd` before the persistent deployment task starts:

| Evidence | Requirement |
|---|---:|
| Dataset | Balanced 200-sample Imagenette validation subset, 20 per class, seed 20260812 |
| FP32 Top-1 | >= 0.65 |
| X5 quantized Top-1 | >= 0.63 |
| FP32 minus quantized Top-1 | <= 0.02 |
| Warmup / measured iterations | >= 5 / >= 200 |
| Model P95 | <= 10 ms |
| End-to-end P95 | <= 12 ms |
| Throughput | >= 100 FPS |
| Peak temperature | <= 85 C |
| Minimum available system memory | >= 256 MiB |

The validation also requires a non-zero BPU utilization sample and measured CMA, ION or Hbmem allocation. The source ONNX, final X5 binary and report must all remain inside the bound workspace. Hobot Code verifies the source binding, final artifact SHA-256, report ownership and `0600` mode.

## Reproduction

Export and prepare data on an x86 conversion host:

```bash
python3 export.py --output regnet_x_400mf_224.onnx --metadata export-metadata.json
python3 prepare_imagenette.py \
  --dataset /path/to/imagenette2-160 \
  --onnx regnet_x_400mf_224.onnx \
  --output data_imagenette
./convert_x5.sh /path/to/verified-input /path/to/output
```

Copy the ONNX, generated `regnet_x_400mf_224_rgb_x5.bin`, manifest, evaluation inputs/references, `validate_x5.py` and `examples/x5-classification/run_hb_dnn.cc` into one X5 workspace. Bind the immutable source model to Hobot Code before validation:

```bash
hobot deploy start --profile regnet-x-400mf-x5 --permissions developer regnet_x_400mf_224.onnx
hobot task attach TASK_ID
hobot deploy status TASK_ID
```

Compile the small official `hb_dnn` API runner and execute the automated acceptance using the exact report path returned by `hobot deploy status`:

```bash
g++ -O2 -std=c++17 run_hb_dnn.cc -o run_hb_dnn -ldnn
python3 validate_x5.py \
  --workspace /absolute/workspace \
  --model /absolute/workspace/regnet_x_400mf_224_rgb_x5.bin \
  --reference /absolute/workspace/regnet_x_400mf_224.onnx \
  --data /absolute/workspace/data_imagenette \
  --runner /absolute/workspace/run_hb_dnn \
  --work /absolute/workspace/acceptance \
  --report /absolute/workspace/.hobot/deployments/REPORT_ID.json
hobot deploy status TASK_ID
```

`validate_x5.py` writes a private schema-v2 report atomically. It first replaces any stale report with `partial`; failures are written as `failed`, so a previous passing result cannot survive a broken rerun.

## Verified result

The frozen workload passed on an RDK X5 MD V1.2 running RDK OS 3.4.1 on 2026-08-12. This is a full 200-sample accuracy run followed by 200 measured latency iterations and a 20,000-frame resource stress run, not a single-image smoke test.

| Metric | Measured | Requirement |
|---|---:|---:|
| FP32 Top-1 | 0.685 | >= 0.65 |
| X5 quantized Top-1 | 0.685 | >= 0.63 |
| Top-1 drop | 0.000 | <= 0.02 |
| Model latency p50 / p95 | 2.222 / 2.351 ms | p95 <= 10 ms |
| End-to-end latency p50 / p95 | 2.551 / 2.688 ms | p95 <= 12 ms |
| Throughput | 510.4 FPS | >= 100 FPS |
| Peak BPU utilization | 42% | required and non-zero |
| Peak temperature | 76.168 C | <= 85 C |
| Minimum available system memory | 6,203,916,288 bytes | >= 268,435,456 bytes |
| CMA allocation during stress | 235,491,328 bytes | required and available |

The converted model SHA-256 is `b0bb61719aa9db5f8bca58a057401787cf6d22317afe04e3e64fa4b627db5c8d`. OpenExplorer 1.24.3 reported one BPU subgraph, a compiler estimate of 948 us / 1054.84 FPS, and quantized-output cosine similarity of 0.994431. The compiler estimate is retained as conversion evidence only; the table above contains the authoritative measurements from the physical X5.

Issues found and resolved during this acceptance:

1. The first conversion package lost its executable bit. The conversion launcher now has an explicit shell syntax check and is packaged with executable mode.
2. A stale conversion script embedded an obsolete calibration digest. `source-lock.json` now defines the digest format, and `convert_x5.sh --verify` checks the pinned ONNX, PTQ configuration and sorted calibration file set before conversion.
3. Early background tasks persisted a Pi session path before its private JSONL file existed, so resume could fail. `agentd` now binds a session only after validating the real file, and startup recovery clears stale session and placeholder model metadata.
4. The Agent initially searched another project to guess the report location even though `reportPath` was already bound. The deployment prompt now treats `cwd`, `reportPath`, artifact, board and frozen acceptance profile as authoritative control-plane constraints and forbids cross-project path inference.

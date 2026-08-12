#!/usr/bin/env python3
"""Run RegNet-X-400MF acceptance on X5 and write a Hobot Code schema-v2 report."""

import argparse
import array
import hashlib
import json
import os
import re
import signal
import shutil
import subprocess
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path


DATASET = "Imagenette2-160 validation, balanced 200-sample subset (20 per class), seed 20260812"
MIN_FP32_TOP1 = 0.65
MIN_QUANTIZED_TOP1 = 0.63
MAX_TOP1_DROP = 0.02
MAX_MODEL_P95_MS = 10.0
MAX_END_TO_END_P95_MS = 12.0
MIN_THROUGHPUT_FPS = 100.0
MAX_TEMPERATURE_C = 85.0
MIN_MEMORY_AVAILABLE_BYTES = 256 << 20
INFERENCE_TIMEOUT_SECONDS = 60
BENCHMARK_TIMEOUT_SECONDS = 120
RESOURCE_TIMEOUT_SECONDS = 300


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def require_within(workspace: Path, path: Path, label: str, must_exist: bool = True) -> Path:
    try:
        workspace = workspace.resolve(strict=True)
        resolved = path.resolve(strict=must_exist)
        resolved.relative_to(workspace)
    except (OSError, ValueError) as error:
        raise ValueError(f"{label} must resolve inside workspace: {path}") from error
    if path.is_symlink():
        raise ValueError(f"{label} cannot be a symbolic link: {path}")
    return resolved


def ensure_real_directory(path: Path) -> None:
    if path.is_symlink():
        raise ValueError(f"directory cannot be a symbolic link: {path}")
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    if not path.is_dir():
        raise ValueError(f"path is not a directory: {path}")


def float32_values(path: Path) -> array.array:
    values = array.array("f")
    with path.open("rb") as source:
        values.fromfile(source, path.stat().st_size // values.itemsize)
    if path.stat().st_size % values.itemsize != 0 or not values:
        raise RuntimeError(f"invalid float32 output: {path}")
    return values


def top1(values: array.array) -> int:
    return max(range(len(values)), key=values.__getitem__)


def validate_manifest(data: Path, reference: Path) -> dict:
    manifest = json.loads((data / "manifest.json").read_text(encoding="utf-8"))
    if manifest.get("schema") != 1 or manifest.get("seed") != 20260812:
        raise RuntimeError("dataset manifest schema or seed does not match the frozen profile")
    if manifest.get("sourceModelSha256") != digest(reference):
        raise RuntimeError("dataset references do not match the bound source ONNX")
    samples = manifest.get("evaluation")
    if not isinstance(samples, list) or len(samples) != 200:
        raise RuntimeError("dataset manifest must contain exactly 200 evaluation samples")
    seen = set()
    for sample in samples:
        sample_id = sample.get("id")
        if not isinstance(sample_id, str) or sample_id in seen:
            raise RuntimeError("dataset manifest contains a missing or duplicate sample id")
        seen.add(sample_id)
        for field in ("rgb", "reference"):
            relative = sample.get(field)
            expected = sample.get(f"{field}Sha256")
            if not isinstance(relative, str) or not isinstance(expected, str):
                raise RuntimeError(f"dataset sample {sample_id} is missing {field} integrity metadata")
            path = require_within(data, data / relative, f"sample {sample_id} {field}")
            if digest(path) != expected:
                raise RuntimeError(f"dataset sample {sample_id} {field} SHA-256 does not match")
    return manifest


def timestamp() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def meminfo() -> dict[str, int]:
    values = {}
    for line in Path("/proc/meminfo").read_text(encoding="utf-8").splitlines():
        match = re.match(r"^(MemTotal|MemAvailable|CmaTotal|CmaFree):\s+(\d+) kB$", line)
        if match:
            values[match.group(1)] = int(match.group(2)) * 1024
    required = {"MemTotal", "MemAvailable", "CmaTotal", "CmaFree"}
    if set(values) != required:
        raise RuntimeError("required memory and CMA counters are unavailable")
    return values


def read_float(paths: list[Path], divisor: float = 1.0) -> float:
    values = []
    for path in paths:
        try:
            values.append(float(path.read_text(encoding="utf-8").strip()) / divisor)
        except (OSError, ValueError):
            continue
    if not values:
        raise RuntimeError(f"no readable values among: {paths}")
    return max(values)


def resource_sample() -> dict:
    memory = meminfo()
    bpu_paths = [
        Path("/sys/devices/system/bpu/ratio"),
        Path("/sys/devices/platform/soc/3a000000.bpu/ratio"),
    ]
    thermal_paths = sorted(Path("/sys/class/thermal").glob("thermal_zone*/temp"))
    return {
        "capturedAt": timestamp(),
        "systemMemoryUsedBytes": memory["MemTotal"] - memory["MemAvailable"],
        "systemMemoryAvailableBytes": memory["MemAvailable"],
        "aiAllocationAvailable": True,
        "aiAllocationSource": "cma",
        "aiAllocatedBytes": memory["CmaTotal"] - memory["CmaFree"],
        "bpuUtilizationAvailable": True,
        "bpuUtilizationPercent": read_float(bpu_paths),
        "temperatureAvailable": True,
        "maxTemperatureC": read_float(thermal_paths, 1000.0),
    }


def run_accuracy(model: Path, data: Path, work: Path, manifest: dict) -> dict:
    samples = manifest["evaluation"]
    predictions = work / "predictions"
    dump = work / "dump"
    ensure_real_directory(predictions)
    reference_correct = []
    quantized_correct = []
    for index, sample in enumerate(samples, start=1):
        if dump.is_symlink():
            raise RuntimeError(f"dump directory cannot be a symbolic link: {dump}")
        shutil.rmtree(dump, ignore_errors=True)
        ensure_real_directory(dump)
        command = [
            "hrt_model_exec", "infer", "--model_file", str(model),
            "--input_file", str(data / sample["rgb"]), "--enable_dump=true",
            "--dump_path", str(dump),
        ]
        completed = subprocess.run(
            command, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            timeout=INFERENCE_TIMEOUT_SECONDS,
        )
        if completed.returncode != 0:
            raise RuntimeError(f"accuracy inference {sample['id']} failed:\n{completed.stdout[-4000:]}")
        outputs = sorted(dump.glob("model_infer_output_*.bin"))
        if len(outputs) != 1:
            raise RuntimeError(f"expected one output dump for {sample['id']}, found {len(outputs)}")
        destination = predictions / f"{sample['id']}.bin"
        if destination.is_symlink():
            raise RuntimeError(f"prediction cannot replace a symbolic link: {destination}")
        shutil.copyfile(outputs[0], destination)
        reference = float32_values(data / sample["reference"])
        actual = float32_values(destination)
        if len(reference) != len(actual):
            raise RuntimeError(f"output size mismatch for {sample['id']}: {len(reference)} != {len(actual)}")
        label = int(sample["label"])
        reference_correct.append(top1(reference) == label)
        quantized_correct.append(top1(actual) == label)
        if index % 20 == 0:
            print(f"accuracy {index}/{len(samples)}", flush=True)
    fp32_top1 = sum(reference_correct) / len(reference_correct)
    quantized_top1 = sum(quantized_correct) / len(quantized_correct)
    accuracy_drop = fp32_top1 - quantized_top1
    return {
        "sampleCount": len(samples),
        "fp32Top1": fp32_top1,
        "quantizedTop1": quantized_top1,
        "top1AccuracyDrop": accuracy_drop,
    }


def parse_json_output(value: str) -> dict:
    start, end = value.find("{"), value.rfind("}")
    if start < 0 or end < start:
        raise RuntimeError(f"benchmark emitted no JSON:\n{value[-4000:]}")
    return json.loads(value[start:end + 1])


def run_performance(runner: Path, model: Path, sample: Path, warmup: int, iterations: int) -> dict:
    completed = subprocess.run(
        [str(runner), str(model), str(sample), str(warmup), str(iterations)],
        text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        timeout=BENCHMARK_TIMEOUT_SECONDS,
    )
    if completed.returncode != 0:
        raise RuntimeError(f"hb_dnn benchmark failed:\n{completed.stdout[-4000:]}")
    return parse_json_output(completed.stdout)


def run_resource_stress(model: Path, sample: Path, work: Path, frames: int) -> dict:
    profile = work / "profile"
    ensure_real_directory(profile)
    baseline = resource_sample()
    log = (work / "resource-stress.log").open("w", encoding="utf-8")
    process = subprocess.Popen(
        ["hrt_model_exec", "perf", "--model_file", str(model), "--input_file", str(sample),
         "--frame_count", str(frames), "--profile_path", str(profile)],
        stdout=log, stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    during = []
    try:
        deadline = time.monotonic() + RESOURCE_TIMEOUT_SECONDS
        while process.poll() is None:
            if time.monotonic() >= deadline:
                raise TimeoutError(f"resource stress exceeded {RESOURCE_TIMEOUT_SECONDS} seconds")
            during.append(resource_sample())
            time.sleep(0.25)
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
        log.close()
    if process.returncode != 0:
        raise RuntimeError(f"resource stress failed; see {work / 'resource-stress.log'}")
    final = resource_sample()
    if not during:
        raise RuntimeError("resource stress completed before a sample was captured")
    write_private_json(work / "resource-samples.json", {
        "schema": 1, "baseline": baseline, "during": during, "final": final,
    })
    peak = {
        "capturedAt": max(item["capturedAt"] for item in during),
        "systemMemoryUsedBytes": max(item["systemMemoryUsedBytes"] for item in during),
        "systemMemoryAvailableBytes": min(item["systemMemoryAvailableBytes"] for item in during),
        "aiAllocationAvailable": True,
        "aiAllocationSource": "cma",
        "aiAllocatedBytes": max(item["aiAllocatedBytes"] for item in during),
        "bpuUtilizationAvailable": True,
        "bpuUtilizationPercent": max(item["bpuUtilizationPercent"] for item in during),
        "temperatureAvailable": True,
        "maxTemperatureC": max(item["maxTemperatureC"] for item in during),
    }
    return {"sampleCount": len(during) + 2, "baseline": baseline, "peak": peak, "final": final}


def metric(name: str, value: float, threshold: float, comparator: str) -> dict:
    passed = value <= threshold if comparator == "<=" else value >= threshold
    return {"name": name, "unit": "ratio", "value": value, "threshold": threshold,
            "comparator": comparator, "passed": passed}


def write_private_json(path: Path, value: dict) -> None:
    ensure_real_directory(path.parent)
    if path.is_symlink():
        raise ValueError(f"report cannot be a symbolic link: {path}")
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(value, output, indent=2)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", required=True, type=Path)
    parser.add_argument("--reference", required=True, type=Path)
    parser.add_argument("--data", required=True, type=Path)
    parser.add_argument("--runner", required=True, type=Path)
    parser.add_argument("--workspace", required=True, type=Path)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--work", required=True, type=Path)
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--iterations", type=int, default=200)
    parser.add_argument("--resource-frames", type=int, default=20000)
    args = parser.parse_args()
    workspace = args.workspace.resolve(strict=True)
    if not workspace.is_dir():
        raise SystemExit(f"workspace is not a directory: {workspace}")
    args.model = require_within(workspace, args.model, "model")
    args.reference = require_within(workspace, args.reference, "reference")
    args.data = require_within(workspace, args.data, "data")
    args.runner = require_within(workspace, args.runner, "runner")
    if not (args.data / "manifest.json").is_file():
        raise SystemExit(f"data manifest does not exist: {args.data / 'manifest.json'}")
    report_parent = require_within(workspace, args.report.parent, "report directory")
    args.report = report_parent / args.report.name
    work_parent = require_within(workspace, args.work.parent, "work directory parent")
    args.work = work_parent / args.work.name
    ensure_real_directory(args.work)
    preliminary = {
        "schema": 2,
        "outcome": "partial",
        "boardId": "x5",
        "artifactPath": str(args.model.resolve()),
        "artifactSha256": digest(args.model.resolve()),
        "summary": "RegNet-X-400MF X5 acceptance is running.",
        "correctness": {"passed": False},
        "performance": {},
        "resources": {},
    }
    write_private_json(args.report.resolve(), preliminary)
    try:
        manifest = validate_manifest(args.data, args.reference)
        accuracy = run_accuracy(args.model, args.data, args.work, manifest)
        sample = args.data / manifest["evaluation"][0]["rgb"]
        performance = run_performance(
            args.runner.resolve(), args.model.resolve(), sample, args.warmup, args.iterations
        )
        resources = run_resource_stress(
            args.model.resolve(), sample, args.work, args.resource_frames
        )
    except Exception as error:
        preliminary["outcome"] = "failed"
        preliminary["summary"] = f"RegNet-X-400MF X5 acceptance failed: {error}"[:4096]
        write_private_json(args.report.resolve(), preliminary)
        raise
    metrics = [
        metric("fp32_top1", accuracy["fp32Top1"], MIN_FP32_TOP1, ">="),
        metric("quantized_top1", accuracy["quantizedTop1"], MIN_QUANTIZED_TOP1, ">="),
        metric("top1_accuracy_drop", accuracy["top1AccuracyDrop"], MAX_TOP1_DROP, "<="),
    ]
    passed = all(item["passed"] for item in metrics)
    passed &= performance["modelP95LatencyMs"] <= MAX_MODEL_P95_MS
    passed &= performance["endToEndP95LatencyMs"] <= MAX_END_TO_END_P95_MS
    passed &= performance["throughputFps"] >= MIN_THROUGHPUT_FPS
    passed &= resources["peak"]["maxTemperatureC"] <= MAX_TEMPERATURE_C
    passed &= resources["peak"]["systemMemoryAvailableBytes"] >= MIN_MEMORY_AVAILABLE_BYTES
    report = {
        "schema": 2,
        "outcome": "passed" if passed else "failed",
        "boardId": "x5",
        "artifactPath": str(args.model.resolve()),
        "artifactSha256": digest(args.model.resolve()),
        "summary": (
            f"RegNet-X-400MF X5 acceptance {'passed' if passed else 'failed'}: "
            f"INT8 Top-1 {accuracy['quantizedTop1']:.3f}, model p95 "
            f"{performance['modelP95LatencyMs']:.3f} ms, {performance['throughputFps']:.1f} FPS."
        ),
        "correctness": {
            "passed": all(item["passed"] for item in metrics),
            "method": "X5 INT8 Top-1 versus pinned torchvision FP32 ONNX on identical samples",
            "dataset": DATASET,
            "sampleCount": accuracy["sampleCount"],
            "referenceArtifact": str(args.reference.resolve()),
            "metrics": metrics,
        },
        "performance": {
            "warmupIterations": performance["warmupIterations"],
            "iterations": performance["iterations"],
            "p50LatencyMs": performance["modelP50LatencyMs"],
            "p95LatencyMs": performance["modelP95LatencyMs"],
            "throughput": performance["throughputFps"],
            "endToEndP50Ms": performance["endToEndP50LatencyMs"],
            "endToEndP95Ms": performance["endToEndP95LatencyMs"],
        },
        "resources": {
            **resources,
            "limits": {
                "maxTemperatureC": MAX_TEMPERATURE_C,
                "minSystemMemoryAvailableBytes": MIN_MEMORY_AVAILABLE_BYTES,
            },
        },
    }
    write_private_json(args.report.resolve(), report)
    write_private_json((args.work / "acceptance-report.json").resolve(), report)
    print(json.dumps(report, indent=2))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()

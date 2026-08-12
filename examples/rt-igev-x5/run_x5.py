#!/usr/bin/env python3
"""Run RT-IGEV on RDK X5 and emit prediction/latency evidence."""

from __future__ import annotations

import argparse
import json
import statistics
import time
from pathlib import Path

import numpy as np
from hobot_dnn import pyeasy_dnn as dnn


def percentile(values: list[float], fraction: float) -> float:
    ordered = sorted(values)
    index = (len(ordered) - 1) * fraction
    lower = int(index)
    upper = min(lower + 1, len(ordered) - 1)
    weight = index - lower
    return ordered[lower] * (1 - weight) + ordered[upper] * weight


def output_array(value) -> np.ndarray:
    buffer = np.asarray(value.buffer)
    return np.ascontiguousarray(buffer, dtype=np.float32).reshape(1, 1, 256, 320)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", required=True, type=Path)
    parser.add_argument("--data", required=True, type=Path)
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--iterations", type=int, default=20)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    model = dnn.load(str(args.model.resolve()))[0]
    if len(model.inputs) != 2 or len(model.outputs) != 1:
        raise RuntimeError(f"expected 2 inputs and 1 output, got {len(model.inputs)} and {len(model.outputs)}")
    manifest = json.loads((args.data / "manifest.json").read_text(encoding="utf-8"))
    samples = manifest["evaluation"]

    def load(sample: dict) -> tuple[np.ndarray, np.ndarray]:
        left = np.fromfile(args.data / sample["left"], dtype=np.float32).reshape(1, 3, 256, 320)
        right = np.fromfile(args.data / sample["right"], dtype=np.float32).reshape(1, 3, 256, 320)
        return left, right

    warmup_input = load(samples[0])
    for _ in range(args.warmup):
        model.forward(*warmup_input)

    predictions = args.output / "predictions"
    predictions.mkdir(parents=True, exist_ok=True)
    model_latency = []
    end_to_end_latency = []
    for index in range(args.iterations):
        started = time.perf_counter_ns()
        inputs = load(samples[index % len(samples)])
        model_started = time.perf_counter_ns()
        outputs = model.forward(*inputs)
        model_ended = time.perf_counter_ns()
        prediction = output_array(outputs[0])
        prediction.tofile(predictions / f"{samples[index % len(samples)]['id']}.bin")
        ended = time.perf_counter_ns()
        model_latency.append((model_ended - model_started) / 1_000_000)
        end_to_end_latency.append((ended - started) / 1_000_000)
        print(f"[{index + 1:02d}/{args.iterations}] model={model_latency[-1]:.3f} ms end-to-end={end_to_end_latency[-1]:.3f} ms")

    report = {
        "schema": 1,
        "model": str(args.model.resolve()),
        "warmupIterations": args.warmup,
        "iterations": args.iterations,
        "modelLatencyMs": model_latency,
        "endToEndLatencyMs": end_to_end_latency,
        "modelP50LatencyMs": percentile(model_latency, 0.5),
        "modelP95LatencyMs": percentile(model_latency, 0.95),
        "endToEndP50LatencyMs": percentile(end_to_end_latency, 0.5),
        "endToEndP95LatencyMs": percentile(end_to_end_latency, 0.95),
        "throughputFps": 1000 / statistics.mean(model_latency),
    }
    (args.output / "performance.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()

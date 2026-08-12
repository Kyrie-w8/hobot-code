#!/usr/bin/env python3
"""Compare X5 quantized predictions with pinned FP32 RT-IGEV outputs."""

from __future__ import annotations

import argparse
import json
from pathlib import Path

import numpy as np


def metrics(prediction: np.ndarray, ground_truth: np.ndarray) -> tuple[float, np.ndarray]:
    valid = np.isfinite(ground_truth) & (np.abs(ground_truth) < 192)
    error = np.abs(prediction - ground_truth)
    return float(error[valid].mean()), error[valid] > 3


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--data", required=True, type=Path)
    parser.add_argument("--predictions", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    manifest = json.loads((args.data / "manifest.json").read_text(encoding="utf-8"))
    fp32_epe, x5_epe, fp32_d1, x5_d1 = [], [], [], []
    for sample in manifest["evaluation"]:
        shape = (256, 320)
        reference = np.fromfile(args.data / sample["reference"], dtype=np.float32).reshape(shape)
        prediction = np.fromfile(args.predictions / f"{sample['id']}.bin", dtype=np.float32).reshape(shape)
        ground_truth = np.fromfile(args.data / sample["groundTruth"], dtype=np.float32).reshape(shape)
        reference_epe, reference_outliers = metrics(reference, ground_truth)
        prediction_epe, prediction_outliers = metrics(prediction, ground_truth)
        fp32_epe.append(reference_epe)
        x5_epe.append(prediction_epe)
        fp32_d1.append(reference_outliers)
        x5_d1.append(prediction_outliers)

    reference_epe = float(np.mean(fp32_epe))
    quantized_epe = float(np.mean(x5_epe))
    reference_d1 = float(np.concatenate(fp32_d1).mean() * 100)
    quantized_d1 = float(np.concatenate(x5_d1).mean() * 100)
    epe_delta = quantized_epe - reference_epe
    d1_delta = quantized_d1 - reference_d1
    result = {
        "schema": 1,
        "sampleCount": len(manifest["evaluation"]),
        "reference": {"epePx": reference_epe, "d1Percent": reference_d1},
        "quantized": {"epePx": quantized_epe, "d1Percent": quantized_d1},
        "metrics": [
            {"name": "epe_delta", "unit": "px", "value": epe_delta, "threshold": 0.1, "comparator": "<=", "passed": epe_delta <= 0.1},
            {"name": "d1_delta", "unit": "percentage_points", "value": d1_delta, "threshold": 0.5, "comparator": "<=", "passed": d1_delta <= 0.5},
        ],
    }
    args.output.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()

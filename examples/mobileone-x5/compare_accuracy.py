#!/usr/bin/env python3
"""Compare X5 INT8 logits with the pinned FP32 ONNX reference."""

import argparse
import json
from pathlib import Path

import numpy as np


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--data", required=True, type=Path)
    parser.add_argument("--predictions", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    samples = json.loads((args.data / "manifest.json").read_text())["evaluation"]
    disagreements, cosine_losses, maximum_errors = [], [], []
    reference_correct, actual_correct = [], []
    for sample in samples:
        reference = np.fromfile(args.data / sample["reference"], np.float32)
        actual = np.fromfile(args.predictions / f"{sample['id']}.bin", np.float32)
        disagreements.append(float(reference.argmax() != actual.argmax()))
        label = sample.get("label")
        if label is not None:
            reference_correct.append(float(reference.argmax() == label))
            actual_correct.append(float(actual.argmax() == label))
        denominator = np.linalg.norm(reference) * np.linalg.norm(actual)
        cosine_losses.append(float(1 - np.dot(reference, actual) / denominator))
        maximum_errors.append(float(np.abs(reference - actual).max()))
    disagreement = float(np.mean(disagreements))
    cosine_loss = float(np.mean(cosine_losses))
    metrics = [
        {"name": "top1_disagreement", "unit": "ratio", "value": disagreement, "threshold": 0.05, "comparator": "<=", "passed": disagreement <= 0.05},
        {"name": "cosine_loss", "unit": "ratio", "value": cosine_loss, "threshold": 0.01, "comparator": "<=", "passed": cosine_loss <= 0.01},
    ]
    result = {
        "schema": 1,
        "sampleCount": len(samples),
        "maximumAbsoluteLogitError": max(maximum_errors),
        "metrics": metrics,
    }
    if reference_correct:
        fp32_top1 = float(np.mean(reference_correct))
        quantized_top1 = float(np.mean(actual_correct))
        accuracy_drop = fp32_top1 - quantized_top1
        result.update({"fp32Top1": fp32_top1, "quantizedTop1": quantized_top1})
        metrics.append(
            {"name": "top1_accuracy_drop", "unit": "ratio", "value": accuracy_drop, "threshold": 0.02, "comparator": "<=", "passed": accuracy_drop <= 0.02}
        )
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()

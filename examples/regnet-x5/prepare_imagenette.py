#!/usr/bin/env python3
"""Prepare a fixed, balanced Imagenette subset for RegNet calibration and evaluation."""

import argparse
import hashlib
import json
import random
from pathlib import Path

import numpy as np
import onnxruntime as ort
from PIL import Image


IMAGENET_CLASS_INDEX = {
    "n01440764": 0,
    "n02102040": 217,
    "n02979186": 482,
    "n03000684": 491,
    "n03028079": 497,
    "n03394916": 566,
    "n03417042": 569,
    "n03425413": 571,
    "n03445777": 574,
    "n03888257": 701,
}


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def preprocess(path: Path) -> tuple[np.ndarray, np.ndarray]:
    with Image.open(path) as source:
        image = source.convert("RGB")
        width, height = image.size
        scale = 232.0 / min(width, height)
        resized = image.resize(
            (round(width * scale), round(height * scale)), Image.Resampling.BILINEAR
        )
        left = (resized.width - 224) // 2
        top = (resized.height - 224) // 2
        rgb = np.asarray(resized.crop((left, top, left + 224, top + 224)), dtype=np.uint8)
    nchw = np.ascontiguousarray(rgb.transpose(2, 0, 1)[None].astype(np.float32))
    return rgb, nchw


def balanced_samples(root: Path, count_per_class: int, seed: int) -> list[Path]:
    selected: list[Path] = []
    for offset, synset in enumerate(sorted(IMAGENET_CLASS_INDEX)):
        candidates = sorted(
            path for path in (root / synset).iterdir()
            if path.suffix.lower() in {".jpeg", ".jpg", ".png"}
        )
        random.Random(seed + offset).shuffle(candidates)
        if len(candidates) < count_per_class:
            raise RuntimeError(f"{synset} has only {len(candidates)} images")
        selected.extend(candidates[:count_per_class])
    return selected


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", required=True, type=Path)
    parser.add_argument("--onnx", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--calibration-per-class", type=int, default=10)
    parser.add_argument("--evaluation-per-class", type=int, default=20)
    parser.add_argument("--seed", type=int, default=20260812)
    args = parser.parse_args()

    session = ort.InferenceSession(str(args.onnx.resolve()), providers=["CPUExecutionProvider"])
    manifest = {
        "schema": 1,
        "dataset": "Imagenette2-160 train calibration and validation evaluation",
        "source": "https://s3.amazonaws.com/fast-ai-imageclas/imagenette2-160.tgz",
        "sourceArchiveSha256": "64d0c4859f35a461889e0147755a999a48b49bf38a7e0f9bd27003f10db02fe5",
        "sourceModel": str(args.onnx.resolve()),
        "sourceModelSha256": digest(args.onnx),
        "preprocessing": "RGB; resize shorter edge to 232; center crop 224; model-embedded ImageNet normalization",
        "seed": args.seed,
        "classIndex": IMAGENET_CLASS_INDEX,
        "calibration": [],
        "evaluation": [],
    }
    groups = (
        ("calibration", args.dataset / "train", args.calibration_per_class),
        ("evaluation", args.dataset / "val", args.evaluation_per_class),
    )
    for group, root, count in groups:
        for index, source in enumerate(balanced_samples(root, count, args.seed)):
            sample_id = f"{group[:3]}-{index:03d}"
            rgb, nchw = preprocess(source)
            record = {
                "id": sample_id,
                "source": str(source.relative_to(args.dataset)),
                "sourceSha256": digest(source),
                "synset": source.parent.name,
                "label": IMAGENET_CLASS_INDEX[source.parent.name],
            }
            if group == "calibration":
                feature_path = args.output / group / "featuremap" / f"{sample_id}.bin"
                feature_path.parent.mkdir(parents=True, exist_ok=True)
                nchw.tofile(feature_path)
                record.update({
                    "featuremap": str(feature_path.relative_to(args.output)),
                    "featuremapSha256": digest(feature_path),
                })
            else:
                rgb_path = args.output / group / "rgb" / f"{sample_id}.bin"
                rgb_path.parent.mkdir(parents=True, exist_ok=True)
                rgb.tofile(rgb_path)
                logits = session.run(None, {"image": nchw})[0].astype(np.float32)
                reference = args.output / group / "reference" / f"{sample_id}.bin"
                reference.parent.mkdir(parents=True, exist_ok=True)
                logits.tofile(reference)
                record.update({
                    "rgb": str(rgb_path.relative_to(args.output)),
                    "rgbSha256": digest(rgb_path),
                    "reference": str(reference.relative_to(args.output)),
                    "referenceSha256": digest(reference),
                    "referenceTop1": int(logits.argmax()),
                    "referenceCorrect": bool(int(logits.argmax()) == record["label"]),
                })
            manifest[group].append(record)

    args.output.mkdir(parents=True, exist_ok=True)
    (args.output / "manifest.json").write_text(
        json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps({
        "calibrationSamples": len(manifest["calibration"]),
        "evaluationSamples": len(manifest["evaluation"]),
        "fp32Top1": sum(item["referenceCorrect"] for item in manifest["evaluation"])
        / len(manifest["evaluation"]),
    }, indent=2))


if __name__ == "__main__":
    main()

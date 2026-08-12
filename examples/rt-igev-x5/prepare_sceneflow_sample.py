#!/usr/bin/env python3
"""Prepare deterministic calibration/evaluation data from the official sample pack."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

import numpy as np
from PIL import Image


HEIGHT = 256
WIDTH = 320
CALIBRATION_COUNT = 100
EVALUATION_COUNT = 20


def read_pfm(path: Path) -> np.ndarray:
    with path.open("rb") as stream:
        if stream.readline().rstrip() != b"Pf":
            raise ValueError(f"expected single-channel PFM: {path}")
        dimensions = stream.readline().decode("ascii").strip().split()
        width, height = (int(value) for value in dimensions)
        scale = float(stream.readline().decode("ascii").strip())
        byte_order = "<" if scale < 0 else ">"
        data = np.frombuffer(stream.read(), dtype=byte_order + "f4")
    return np.flipud(data.reshape(height, width)).astype(np.float32, copy=False)


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def crop_positions(count: int, seed: int) -> list[tuple[int, int]]:
    # Source frames are 960x540. The integer lattice and coprime strides make
    # every crop deterministic without relying on a random generator version.
    positions = []
    for index in range(count):
        x = (seed * 37 + index * 83) % (960 - WIDTH + 1)
        y = (seed * 29 + index * 47) % (540 - HEIGHT + 1)
        positions.append((x, y))
    return positions


def load_rgb(path: Path) -> np.ndarray:
    value = np.asarray(Image.open(path).convert("RGB"), dtype=np.float32)
    if value.shape != (540, 960, 3):
        raise ValueError(f"unexpected Scene Flow sample shape {value.shape}: {path}")
    return value


def write_input(path: Path, value: np.ndarray) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    np.ascontiguousarray(value.transpose(2, 0, 1), dtype=np.float32).tofile(path)


def sample_record(
    output: Path,
    split: str,
    sample_id: str,
    left: np.ndarray,
    right: np.ndarray,
    x: int,
    y: int,
    source: str,
    ground_truth: np.ndarray | None = None,
) -> dict:
    directory = output / split
    left_path = directory / "left" / f"{sample_id}.bin"
    right_path = directory / "right" / f"{sample_id}.bin"
    write_input(left_path, left[y : y + HEIGHT, x : x + WIDTH])
    write_input(right_path, right[y : y + HEIGHT, x : x + WIDTH])
    record = {
        "id": sample_id,
        "source": source,
        "crop": {"x": x, "y": y, "width": WIDTH, "height": HEIGHT},
        "left": str(left_path.relative_to(output)),
        "right": str(right_path.relative_to(output)),
        "leftSha256": file_sha256(left_path),
        "rightSha256": file_sha256(right_path),
    }
    if ground_truth is not None:
        gt_path = directory / "ground_truth" / f"{sample_id}.bin"
        gt_path.parent.mkdir(parents=True, exist_ok=True)
        np.ascontiguousarray(
            ground_truth[y : y + HEIGHT, x : x + WIDTH], dtype=np.float32
        ).tofile(gt_path)
        record["groundTruth"] = str(gt_path.relative_to(output))
        record["groundTruthSha256"] = file_sha256(gt_path)
    return record


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--sampler", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--archive-sha256", required=True)
    args = parser.parse_args()

    sampler = args.sampler.resolve()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)

    calibration_sources = []
    for dataset, frames in (("Driving", ("0400", "0401", "0402")), ("Monkaa", ("0048", "0049", "0050"))):
        for frame in frames:
            calibration_sources.append((dataset, frame))
    evaluation_sources = [("FlyingThings3D", frame) for frame in ("0006", "0007", "0008")]

    calibration = []
    positions = crop_positions(CALIBRATION_COUNT, 13)
    cached_rgb = {}
    for index in range(CALIBRATION_COUNT):
        dataset, frame = calibration_sources[index % len(calibration_sources)]
        key = (dataset, frame)
        if key not in cached_rgb:
            base = sampler / dataset / "RGB_cleanpass"
            cached_rgb[key] = (load_rgb(base / "left" / f"{frame}.png"), load_rgb(base / "right" / f"{frame}.png"))
        x, y = positions[index]
        calibration.append(sample_record(output, "calibration", f"cal-{index:03d}", *cached_rgb[key], x, y, f"{dataset}/{frame}"))

    evaluation = []
    positions = crop_positions(EVALUATION_COUNT, 71)
    for index in range(EVALUATION_COUNT):
        dataset, frame = evaluation_sources[index % len(evaluation_sources)]
        key = (dataset, frame)
        if key not in cached_rgb:
            base = sampler / dataset / "RGB_cleanpass"
            cached_rgb[key] = (load_rgb(base / "left" / f"{frame}.png"), load_rgb(base / "right" / f"{frame}.png"))
        disparity = read_pfm(sampler / dataset / "disparity" / f"{frame}.pfm")
        x, y = positions[index]
        evaluation.append(sample_record(output, "evaluation", f"eval-{index:03d}", *cached_rgb[key], x, y, f"{dataset}/{frame}", disparity))

    manifest = {
        "schema": 1,
        "source": "Freiburg Scene Flow official sample pack",
        "sourceUrl": "https://lmb.informatik.uni-freiburg.de/resources/datasets/SceneFlow/assets/Sampler.tar.gz",
        "archiveSha256": args.archive_sha256,
        "input": {"shape": [1, 3, HEIGHT, WIDTH], "layout": "NCHW", "dtype": "float32", "range": [0, 255]},
        "calibration": calibration,
        "evaluation": evaluation,
    }
    manifest_path = output / "manifest.json"
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {len(calibration)} calibration and {len(evaluation)} evaluation samples")
    print(f"manifest sha256: {file_sha256(manifest_path)}")


if __name__ == "__main__":
    main()

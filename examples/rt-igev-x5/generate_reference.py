#!/usr/bin/env python3
"""Generate pinned FP32 RT-IGEV outputs for the evaluation manifest."""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path
from types import SimpleNamespace

import numpy as np
import torch


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--checkpoint", required=True, type=Path)
    parser.add_argument("--data", required=True, type=Path)
    parser.add_argument("--iterations", type=int, default=8)
    args = parser.parse_args()

    source = args.source.resolve()
    checkpoint_path = args.checkpoint.resolve()
    data = args.data.resolve()
    sys.path.insert(0, str(source))
    import timm

    original_create_model = timm.create_model
    timm.create_model = lambda name, *values, **options: original_create_model(
        name, *values, **{**options, "pretrained": False}
    )
    from core_rt.rt_igev_stereo import IGEVStereo

    config = SimpleNamespace(
        hidden_dim=96,
        corr_levels=2,
        corr_radius=4,
        n_downsample=2,
        n_gru_layers=3,
        max_disp=192,
        mixed_precision=False,
        precision_dtype="float32",
    )
    model = IGEVStereo(config)
    checkpoint = torch.load(checkpoint_path, map_location="cpu", weights_only=False)
    model.load_state_dict(
        {name.removeprefix("module."): value for name, value in checkpoint.items()}, strict=True
    )
    model.eval()

    manifest_path = data / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    output = data / "evaluation" / "reference"
    output.mkdir(parents=True, exist_ok=True)
    with torch.no_grad():
        for index, sample in enumerate(manifest["evaluation"]):
            left = np.fromfile(data / sample["left"], dtype=np.float32).reshape(1, 3, 256, 320)
            right = np.fromfile(data / sample["right"], dtype=np.float32).reshape(1, 3, 256, 320)
            prediction = model(
                torch.from_numpy(left), torch.from_numpy(right), iters=args.iterations, test_mode=True
            ).numpy()
            path = output / f"{sample['id']}.bin"
            np.ascontiguousarray(prediction, dtype=np.float32).tofile(path)
            sample["reference"] = str(path.relative_to(data))
            sample["referenceSha256"] = sha256(path)
            print(f"[{index + 1:02d}/{len(manifest['evaluation'])}] {sample['id']}")

    manifest["reference"] = {
        "sourceCommit": "099dec97989780d151700459c682df1cc2c18e88",
        "checkpointSha256": sha256(checkpoint_path),
        "iterations": args.iterations,
        "maxDisparity": 192,
        "framework": f"PyTorch {torch.__version__} FP32",
    }
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(f"manifest sha256: {sha256(manifest_path)}")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Export the official torchvision RegNet-X-400MF V2 weights for RDK X5."""

import argparse
import hashlib
import json
from pathlib import Path
from urllib.parse import urlparse

import torch
import torchvision
from torchvision.models import RegNet_X_400MF_Weights, regnet_x_400mf


class RawRGBRegNet(torch.nn.Module):
    """Accept float RGB values in [0, 255] and embed ImageNet normalization."""

    def __init__(self, model: torch.nn.Module) -> None:
        super().__init__()
        self.model = model
        self.register_buffer("mean", torch.tensor([0.485, 0.456, 0.406]).reshape(1, 3, 1, 1))
        self.register_buffer("std", torch.tensor([0.229, 0.224, 0.225]).reshape(1, 3, 1, 1))

    def forward(self, image: torch.Tensor) -> torch.Tensor:
        return self.model((image / 255.0 - self.mean) / self.std)


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--metadata", required=True, type=Path)
    args = parser.parse_args()

    weights = RegNet_X_400MF_Weights.IMAGENET1K_V2
    model = RawRGBRegNet(regnet_x_400mf(weights=weights).eval()).eval()
    weights_path = Path(torch.hub.get_dir()) / "checkpoints" / Path(urlparse(weights.url).path).name
    if not weights_path.is_file():
        raise RuntimeError(f"downloaded weights are unavailable: {weights_path}")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    torch.onnx.export(
        model,
        torch.zeros(1, 3, 224, 224),
        args.output,
        input_names=["image"],
        output_names=["logits"],
        opset_version=11,
        do_constant_folding=True,
        dynamo=False,
    )
    metadata = {
        "schema": 1,
        "architecture": "torchvision RegNet-X-400MF",
        "weights": "IMAGENET1K_V2",
        "weightsUrl": weights.url,
        "weightsSha256": digest(weights_path),
        "torchVersion": torch.__version__,
        "torchvisionVersion": torchvision.__version__,
        "onnxSha256": digest(args.output),
        "input": {"shape": [1, 3, 224, 224], "layout": "NCHW", "dtype": "float32", "range": [0, 255]},
        "preprocessing": "model-embedded ImageNet normalization",
        "source": "https://docs.pytorch.org/vision/main/models/generated/torchvision.models.regnet_x_400mf.html",
    }
    args.metadata.write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(metadata, indent=2))


if __name__ == "__main__":
    main()

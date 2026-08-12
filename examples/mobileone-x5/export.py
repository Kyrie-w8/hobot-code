#!/usr/bin/env python3
"""Export Apple's fused MobileOne-S0 checkpoint for RDK X5."""

import argparse
import sys
from pathlib import Path

import torch


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--checkpoint", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    sys.path.insert(0, str(args.source.resolve()))
    from mobileone import mobileone

    model = mobileone(variant="s0", inference_mode=True)
    checkpoint = torch.load(args.checkpoint, map_location="cpu", weights_only=False)
    model.load_state_dict(checkpoint, strict=True)
    model.eval()

    class PreprocessedMobileOne(torch.nn.Module):
        def __init__(self, wrapped: torch.nn.Module) -> None:
            super().__init__()
            self.wrapped = wrapped
            self.register_buffer("mean", torch.tensor([0.485, 0.456, 0.406]).reshape(1, 3, 1, 1))
            self.register_buffer("std", torch.tensor([0.229, 0.224, 0.225]).reshape(1, 3, 1, 1))

        def forward(self, image: torch.Tensor) -> torch.Tensor:
            return self.wrapped((image / 255.0 - self.mean) / self.std)

    args.output.parent.mkdir(parents=True, exist_ok=True)
    torch.onnx.export(
        PreprocessedMobileOne(model),
        torch.zeros(1, 3, 224, 224),
        args.output,
        input_names=["image"],
        output_names=["logits"],
        opset_version=11,
        do_constant_folding=True,
        dynamo=False,
    )


if __name__ == "__main__":
    main()

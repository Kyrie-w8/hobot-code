#!/usr/bin/env python3
"""Export the pinned RT-IGEV graph for the RDK X5 OpenExplorer toolchain."""

from __future__ import annotations

import argparse
from pathlib import Path
from types import SimpleNamespace

import torch
import torch.nn.functional as functional


MAX_BPU_BATCH_AXIS = 2560


def stereo_linear_sampler(image: torch.Tensor, coordinates: torch.Tensor) -> torch.Tensor:
    """Equivalent to align-corners GridSample for RT-IGEV's H=1 stereo volume."""
    width = image.shape[-1]
    x = coordinates[..., 0]
    x0 = torch.floor(x)
    x1 = x0 + 1
    source = image.squeeze(2)

    def gather(position: torch.Tensor) -> torch.Tensor:
        valid = (position >= 0) & (position <= width - 1)
        index = position.clamp(0, width - 1).to(torch.int64)
        values = torch.gather(source, 2, index.expand(-1, source.shape[1], -1))
        return values * valid.expand(-1, source.shape[1], -1).to(values.dtype)

    result = gather(x0) * (x1 - x) + gather(x1) * (x - x0)
    return result.unsqueeze(2)


def build_gwc_volume_functional(
    reference: torch.Tensor,
    target: torch.Tensor,
    max_disparity: int,
    groups: int,
) -> torch.Tensor:
    """Build the GWC volume without ONNX ScatterND-producing assignments."""
    batch, channels, height, width = reference.shape
    channels_per_group = channels // groups
    slices = []
    for disparity in range(max_disparity):
        if disparity == 0:
            left = reference
            right = target
        else:
            left = reference[..., disparity:]
            right = target[..., :-disparity]
        correlation = (left * right).reshape(
            batch, groups, channels_per_group, height, width - disparity
        ).mean(dim=2)
        slices.append(functional.pad(correlation, (disparity, 0)).unsqueeze(2))
    return torch.cat(slices, dim=2).contiguous()


class ChunkedGeoEncodingVolume:
    """RT-IGEV geometry lookup with every X5 BPU batch axis below 4096."""

    def __init__(self, geo_volume: torch.Tensor, num_levels: int = 2, radius: int = 4) -> None:
        self.num_levels = num_levels
        self.radius = radius
        self.batch, channels, disparity, self.height, self.width = geo_volume.shape
        if self.batch != 1 or MAX_BPU_BATCH_AXIS % self.width:
            raise ValueError("the X5 export requires batch 1 and row-aligned geometry chunks")
        flat = geo_volume.permute(0, 3, 4, 1, 2).reshape(
            self.batch * self.height * self.width, channels, 1, disparity
        )
        self.pyramids = []
        self.chunk_ranges = []
        for start in range(0, flat.shape[0], MAX_BPU_BATCH_AXIS):
            end = min(start + MAX_BPU_BATCH_AXIS, flat.shape[0])
            chunk = flat[start:end]
            levels = [chunk]
            for _ in range(self.num_levels - 1):
                levels.append(functional.avg_pool2d(levels[-1], (1, 2), stride=(1, 2)))
            self.pyramids.append(levels)
            self.chunk_ranges.append((start, end))

    def __call__(self, disparity: torch.Tensor) -> torch.Tensor:
        offsets = torch.linspace(
            -self.radius, self.radius, 2 * self.radius + 1, device=disparity.device
        ).reshape(1, 1, 2 * self.radius + 1, 1)
        flat_disparity = disparity.reshape(self.batch * self.height * self.width, 1, 1, 1)
        sampled_chunks = []
        for chunk_index, (start, end) in enumerate(self.chunk_ranges):
            disparity_chunk = flat_disparity[start:end]
            levels = []
            for level in range(self.num_levels):
                x = offsets + disparity_chunk / (2**level)
                coordinates = torch.cat((x, torch.zeros_like(x)), dim=-1)
                sampled = stereo_linear_sampler(self.pyramids[chunk_index][level], coordinates)
                levels.append(sampled.reshape(disparity_chunk.shape[0], -1))
            sampled_chunk = torch.cat(levels, dim=1)
            chunk_rows = sampled_chunk.shape[0] // self.width
            sampled_chunks.append(
                sampled_chunk.reshape(self.batch, chunk_rows, self.width, -1)
            )
        sampled = torch.cat(sampled_chunks, dim=1)
        return sampled.permute(0, 3, 1, 2).contiguous().float()


class CompatibleConv3d(torch.nn.Module):
    """Exact fixed-depth Conv3d expressed as one X5-native Conv2d."""

    def __init__(self, source: torch.nn.Conv3d) -> None:
        super().__init__()
        if source.groups != 1:
            raise ValueError("grouped Conv3d is not supported by the fixed-depth export")
        self.source = source

    def forward(self, value: torch.Tensor) -> torch.Tensor:
        batch, input_channels, input_depth, height, width = value.shape
        kernel_depth = self.source.kernel_size[0]
        stride_depth = self.source.stride[0]
        padding_depth = self.source.padding[0]
        dilation_depth = self.source.dilation[0]
        output_depth = (
            input_depth + 2 * padding_depth - dilation_depth * (kernel_depth - 1) - 1
        ) // stride_depth + 1
        zero = torch.zeros_like(self.source.weight[:, :, 0])
        output_blocks = []
        for output_index in range(output_depth):
            input_blocks = []
            for input_index in range(input_depth):
                offset = input_index - output_index * stride_depth + padding_depth
                if offset >= 0 and offset % dilation_depth == 0 and offset // dilation_depth < kernel_depth:
                    input_blocks.append(self.source.weight[:, :, offset // dilation_depth])
                else:
                    input_blocks.append(zero)
            output_blocks.append(torch.cat(input_blocks, dim=1))
        weight = torch.cat(output_blocks, dim=0)
        flattened = value.permute(0, 2, 1, 3, 4).reshape(
            batch, input_depth * input_channels, height, width
        )
        bias = None if self.source.bias is None else self.source.bias.repeat(output_depth)
        result = functional.conv2d(
            flattened,
            weight,
            bias,
            stride=self.source.stride[1:],
            padding=self.source.padding[1:],
            dilation=self.source.dilation[1:],
        )
        return result.reshape(
            batch, output_depth, self.source.out_channels, result.shape[2], result.shape[3]
        ).permute(0, 2, 1, 3, 4)


class CompatibleConvTranspose3d(torch.nn.Module):
    """Exact fixed-depth ConvTranspose3d expressed as one ConvTranspose2d."""

    def __init__(self, source: torch.nn.ConvTranspose3d) -> None:
        super().__init__()
        if source.groups != 1:
            raise ValueError("grouped ConvTranspose3d is not supported")
        self.source = source

    def forward(self, value: torch.Tensor) -> torch.Tensor:
        batch, input_channels, input_depth, height, width = value.shape
        kernel_depth = self.source.kernel_size[0]
        stride_depth = self.source.stride[0]
        padding_depth = self.source.padding[0]
        dilation_depth = self.source.dilation[0]
        output_depth = (
            (input_depth - 1) * stride_depth
            - 2 * padding_depth
            + dilation_depth * (kernel_depth - 1)
            + self.source.output_padding[0]
            + 1
        )
        zero = torch.zeros_like(self.source.weight[:, :, 0])
        input_blocks = []
        for input_index in range(input_depth):
            output_blocks = []
            for output_index in range(output_depth):
                offset = output_index - input_index * stride_depth + padding_depth
                if offset >= 0 and offset % dilation_depth == 0 and offset // dilation_depth < kernel_depth:
                    output_blocks.append(self.source.weight[:, :, offset // dilation_depth])
                else:
                    output_blocks.append(zero)
            input_blocks.append(torch.cat(output_blocks, dim=1))
        weight = torch.cat(input_blocks, dim=0)
        flattened = value.permute(0, 2, 1, 3, 4).reshape(
            batch, input_depth * input_channels, height, width
        )
        bias = None if self.source.bias is None else self.source.bias.repeat(output_depth)
        result = functional.conv_transpose2d(
            flattened,
            weight,
            bias,
            stride=self.source.stride[1:],
            padding=self.source.padding[1:],
            output_padding=self.source.output_padding[1:],
            dilation=self.source.dilation[1:],
        )
        return result.reshape(
            batch, output_depth, self.source.out_channels, result.shape[2], result.shape[3]
        ).permute(0, 2, 1, 3, 4)


def replace_conv3d(module: torch.nn.Module) -> tuple[int, int]:
    convolutions = 0
    transposed = 0
    for name, child in list(module.named_children()):
        if isinstance(child, torch.nn.ConvTranspose3d):
            setattr(module, name, CompatibleConvTranspose3d(child))
            transposed += 1
        elif isinstance(child, torch.nn.Conv3d):
            setattr(module, name, CompatibleConv3d(child))
            convolutions += 1
        else:
            nested_convolutions, nested_transposed = replace_conv3d(child)
            convolutions += nested_convolutions
            transposed += nested_transposed
    return convolutions, transposed


def verify_sampler() -> None:
    torch.manual_seed(7)
    image = torch.randn(3, 8, 1, 48)
    x = torch.empty(3, 1, 81).uniform_(-6, 53)
    coordinates = torch.stack((x, torch.zeros_like(x)), dim=-1)
    grid = coordinates.clone()
    grid[..., 0] = 2 * grid[..., 0] / (image.shape[-1] - 1) - 1
    reference = functional.grid_sample(image, grid, align_corners=True)
    actual = stereo_linear_sampler(image, coordinates)
    torch.testing.assert_close(actual, reference, atol=1e-5, rtol=5e-5)


def verify_gwc_volume(original) -> None:
    torch.manual_seed(11)
    reference = torch.randn(1, 16, 5, 13)
    target = torch.randn(1, 16, 5, 13)
    expected = original(reference, target, 9, 8)
    actual = build_gwc_volume_functional(reference, target, 9, 8)
    torch.testing.assert_close(actual, expected, atol=1e-6, rtol=1e-6)


def verify_conv_transpose3d() -> None:
    torch.manual_seed(17)
    cases = [
        torch.nn.ConvTranspose3d(8, 5, 4, stride=2, padding=1, bias=False),
        torch.nn.ConvTranspose3d(
            8, 5, (1, 4, 4), stride=(1, 2, 2), padding=(0, 1, 1), bias=True
        ),
        torch.nn.ConvTranspose3d(
            8,
            5,
            (3, 3, 3),
            stride=(2, 1, 2),
            padding=(1, 1, 1),
            output_padding=(1, 0, 1),
            dilation=(1, 2, 1),
            bias=True,
        ),
    ]
    for layer in cases:
        value = torch.randn(1, 8, 3, 5, 7)
        expected = layer(value)
        actual = CompatibleConvTranspose3d(layer)(value)
        torch.testing.assert_close(actual, expected, atol=2e-6, rtol=2e-6)


def verify_conv3d() -> None:
    torch.manual_seed(13)
    cases = [
        torch.nn.Conv3d(8, 5, 3, padding=1, bias=False),
        torch.nn.Conv3d(8, 6, (3, 3, 3), stride=(2, 2, 1), padding=(1, 1, 1), dilation=(1, 1, 2), bias=True),
    ]
    for layer in cases:
        value = torch.randn(2, 8, 7, 9, 11)
        expected = layer(value)
        actual = CompatibleConv3d(layer)(value)
        torch.testing.assert_close(actual, expected, atol=2e-5, rtol=2e-5)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--checkpoint", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--height", type=int, default=256)
    parser.add_argument("--width", type=int, default=320)
    parser.add_argument("--iterations", type=int, default=8)
    args = parser.parse_args()

    verify_sampler()
    verify_conv3d()
    verify_conv_transpose3d()
    import sys
    sys.path.insert(0, str(args.source))
    import timm
    create_model = timm.create_model
    timm.create_model = lambda name, *values, **options: create_model(name, *values, **{**options, "pretrained": False})
    import core_rt.geometry as geometry
    import core_rt.rt_igev_stereo as stereo
    import core_rt.submodule as submodule
    from core_rt.rt_igev_stereo import IGEVStereo
    verify_gwc_volume(submodule.build_gwc_volume)
    geometry.bilinear_sampler = stereo_linear_sampler
    geometry.Geo_Encoding_Volume = ChunkedGeoEncodingVolume
    submodule.build_gwc_volume = build_gwc_volume_functional
    stereo.Geo_Encoding_Volume = ChunkedGeoEncodingVolume
    stereo.build_gwc_volume = build_gwc_volume_functional

    config = SimpleNamespace(hidden_dim=96, corr_levels=2, corr_radius=4, n_downsample=2, n_gru_layers=3,
                             max_disp=192, mixed_precision=False, precision_dtype="float32")
    model = IGEVStereo(config)
    checkpoint = torch.load(args.checkpoint, map_location="cpu", weights_only=False)
    model.load_state_dict({name.removeprefix("module."): value for name, value in checkpoint.items()}, strict=True)
    model.eval()
    convolution_count, transposed_count = replace_conv3d(model)
    if convolution_count != 13 or transposed_count != 3:
        raise RuntimeError(
            "unexpected 3D operator count: "
            f"Conv3d={convolution_count}, ConvTranspose3d={transposed_count}"
        )

    class FixedRTIGEV(torch.nn.Module):
        def __init__(self, wrapped: torch.nn.Module) -> None:
            super().__init__()
            self.wrapped = wrapped

        def forward(self, left: torch.Tensor, right: torch.Tensor) -> torch.Tensor:
            return self.wrapped(left, right, iters=args.iterations, test_mode=True)

    inputs = (torch.zeros(1, 3, args.height, args.width), torch.zeros(1, 3, args.height, args.width))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    torch.onnx.export(FixedRTIGEV(model), inputs, args.output, input_names=["left", "right"], output_names=["disparity"],
                      opset_version=11, do_constant_folding=True)


if __name__ == "__main__":
    main()

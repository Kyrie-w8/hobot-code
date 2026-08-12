#!/usr/bin/env python3
"""Unit tests for the X5 acceptance report helpers."""

import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path


SPEC = importlib.util.spec_from_file_location(
    "validate_x5", Path(__file__).with_name("validate_x5.py")
)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ValidationHelpersTest(unittest.TestCase):
    def test_metric_uses_frozen_comparator(self) -> None:
        self.assertTrue(MODULE.metric("top1", 0.65, 0.63, ">=")["passed"])
        self.assertFalse(MODULE.metric("drop", 0.03, 0.02, "<=")["passed"])

    def test_private_json_is_atomic_and_private(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "reports" / "result.json"
            MODULE.write_private_json(path, {"outcome": "passed"})
            self.assertEqual(json.loads(path.read_text()), {"outcome": "passed"})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(list(path.parent.glob(f".{path.name}.*")), [])

    def test_workspace_boundary_rejects_escape_and_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            workspace = root / "workspace"
            workspace.mkdir()
            model = workspace / "model.bin"
            model.write_bytes(b"model")
            self.assertEqual(MODULE.require_within(workspace, model, "model"), model.resolve())
            outside = root / "outside.bin"
            outside.write_bytes(b"outside")
            with self.assertRaises(ValueError):
                MODULE.require_within(workspace, outside, "model")
            link = workspace / "link.bin"
            os.symlink(outside, link)
            with self.assertRaises(ValueError):
                MODULE.require_within(workspace, link, "model")

    def test_float32_top1_rejects_malformed_output(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            valid = Path(temporary) / "valid.bin"
            values = MODULE.array.array("f", [0.1, 0.9, 0.2])
            with valid.open("wb") as output:
                values.tofile(output)
            self.assertEqual(MODULE.top1(MODULE.float32_values(valid)), 1)
            invalid = Path(temporary) / "invalid.bin"
            invalid.write_bytes(b"123")
            with self.assertRaises(RuntimeError):
                MODULE.float32_values(invalid)

    def test_manifest_binds_reference_and_sample_hashes(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            reference = root / "reference.onnx"
            reference.write_bytes(b"onnx")
            data = root / "data"
            data.mkdir()
            rgb = data / "rgb.bin"
            output = data / "reference.bin"
            rgb.write_bytes(b"rgb")
            output.write_bytes(b"output")
            sample = {
                "id": "sample",
                "rgb": rgb.name,
                "rgbSha256": MODULE.digest(rgb),
                "reference": output.name,
                "referenceSha256": MODULE.digest(output),
            }
            manifest = {
                "schema": 1,
                "seed": 20260812,
                "sourceModelSha256": MODULE.digest(reference),
                "evaluation": [{**sample, "id": f"sample-{index:03d}"} for index in range(200)],
            }
            (data / "manifest.json").write_text(json.dumps(manifest))
            self.assertEqual(len(MODULE.validate_manifest(data, reference)["evaluation"]), 200)
            rgb.write_bytes(b"corrupt")
            with self.assertRaisesRegex(RuntimeError, "SHA-256"):
                MODULE.validate_manifest(data, reference)


if __name__ == "__main__":
    unittest.main()

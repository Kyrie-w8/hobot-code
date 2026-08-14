import hashlib
import importlib.util
import json
import os
import stat
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "verify-model-egress-runtime.py"
SPEC = importlib.util.spec_from_file_location("verify_model_egress_runtime", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class ModelEgressHarnessTest(unittest.TestCase):
    def package_fixture(self, root: Path) -> tuple[Path, str]:
        package = root / "package"
        (package / "nested").mkdir(parents=True)
        (package / "VERSION").write_text("0.26.0\n", encoding="utf-8")
        (package / "nested" / "payload.txt").write_text("verified\n", encoding="utf-8")
        entries = []
        for path in sorted((package / name for name in ("VERSION", "nested/payload.txt")), key=str):
            entries.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.relative_to(package).as_posix()}")
        content = "\n".join(entries) + "\n"
        (package / "MANIFEST.sha256").write_text(content, encoding="utf-8")
        return package, hashlib.sha256(content.encode("utf-8")).hexdigest()

    def test_manifest_binds_every_regular_file(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package, digest = self.package_fixture(Path(temporary))
            self.assertEqual(MODULE.verify_manifest(package), digest)
            (package / "nested" / "payload.txt").write_text("changed\n", encoding="utf-8")
            with self.assertRaisesRegex(MODULE.VerificationError, "checksum mismatch"):
                MODULE.verify_manifest(package)

    def test_manifest_rejects_unlisted_files_and_symlinks(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package, _ = self.package_fixture(Path(temporary))
            (package / "extra.txt").write_text("extra\n", encoding="utf-8")
            with self.assertRaisesRegex(MODULE.VerificationError, "missing from the manifest"):
                MODULE.verify_manifest(package)
            (package / "extra.txt").unlink()
            (package / "linked.txt").symlink_to("VERSION")
            with self.assertRaisesRegex(MODULE.VerificationError, "non-regular file"):
                MODULE.verify_manifest(package)

    def test_private_report_is_atomic_private_and_does_not_follow_links(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            report = root / "report.json"
            MODULE.private_report(report, {"schema": "test", "status": "pass"})
            self.assertEqual(stat.S_IMODE(report.stat().st_mode), 0o600)
            self.assertEqual(json.loads(report.read_text(encoding="utf-8"))["status"], "pass")

            target = root / "target.json"
            target.write_text("unchanged\n", encoding="utf-8")
            report.unlink()
            report.symlink_to(target)
            with self.assertRaisesRegex(MODULE.VerificationError, "regular file owned"):
                MODULE.private_report(report, {"status": "replaced"})
            self.assertEqual(target.read_text(encoding="utf-8"), "unchanged\n")

    def test_report_must_be_outside_package(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package = Path(temporary).resolve() / "package"
            package.mkdir()
            with self.assertRaisesRegex(MODULE.VerificationError, "outside"):
                MODULE.validate_report_destination(package, package / "report.json")
            MODULE.validate_report_destination(package, package.parent / "report.json")

    def test_readiness_report_validation_is_strict_and_counted(self) -> None:
        checks = [
            {"name": name, "status": "pass", "summary": "verified"}
            for name in (
                "configuration-current", "model-configuration", "release-integrity",
                "board-target", "task-lifecycle",
            )
        ]
        report = {
            "schemaVersion": 1,
            "capturedAt": "2026-08-14T10:00:00Z",
            "status": "healthy",
            "summary": {"pass": 5, "info": 0, "warn": 0, "fail": 0},
            "checks": checks,
            "findings": [],
            "repairs": [],
        }
        self.assertEqual(MODULE.validate_diagnostic_report(report), {})
        report["capturedAt"] = "2026-08-14T10:00:00.123456789Z"
        self.assertEqual(MODULE.validate_diagnostic_report(report), {})

        report["summary"]["pass"] = 4
        with self.assertRaisesRegex(MODULE.VerificationError, "summary does not match"):
            MODULE.validate_diagnostic_report(report)

        report["summary"]["pass"] = 5
        report["repairs"] = [{
            "id": "private-runtime-permissions", "executor": "agentd", "status": "available",
            "requiresConfirmation": False, "summary": "repair", "reason": "test",
        }]
        with self.assertRaisesRegex(MODULE.VerificationError, "unsafe repair"):
            MODULE.validate_diagnostic_report(report)

    def test_anthropic_mock_drives_tool_and_multiturn_states(self) -> None:
        state = MODULE.GatewayState()
        tool = MODULE.anthropic_response({"messages": [{"role": "user", "content": "HOBOT_RPC_APPROVAL"}]}, state)
        self.assertIn(b'"type":"tool_use"', tool)
        completed = MODULE.anthropic_response({"messages": [{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_hobot_rpc", "content": "HOBOT_RPC_EXECUTED"}]}]}, state)
        self.assertIn(b"HOBOT_RPC_TOOL_OK", completed)
        second = MODULE.anthropic_response({"messages": [
            {"role": "user", "content": "HOBOT_RPC_SECOND"},
            {"role": "assistant", "content": "HOBOT_RPC_SECOND_OK"},
            {"role": "user", "content": "HOBOT_SIDE_FIRST"},
        ]}, state)
        self.assertIn(b"HOBOT_SIDE_FIRST_OK", second)
        with self.assertRaisesRegex(MODULE.VerificationError, "inherit"):
            MODULE.anthropic_response({"messages": [{"role": "user", "content": "HOBOT_SIDE_FIRST"}]}, state)

        parallel = MODULE.anthropic_response({"messages": [{"role": "user", "content": "stage parallel and nonce parallel-a; stage parallel and nonce parallel-b"}]}, state)
        self.assertEqual(parallel.count(b'"type":"tool_use"'), 2)
        parallel_complete = MODULE.anthropic_response({"messages": [
            {"role": "user", "content": "stage basic and nonce hobot-runtime-probe-v1"},
            {"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_runtime_basic", "name": "hobot_runtime_probe", "input": {}}]},
            {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_runtime_basic", "content": "HOBOT_RUNTIME_PROBE_OK"}]},
            {"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_runtime_parallel_a", "name": "hobot_runtime_probe", "input": {}}, {"type": "tool_use", "id": "toolu_runtime_parallel_b", "name": "hobot_runtime_probe", "input": {}}]},
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": "toolu_runtime_parallel_a", "content": "HOBOT_RUNTIME_PARALLEL_A"},
                {"type": "tool_result", "tool_use_id": "toolu_runtime_parallel_b", "content": "HOBOT_RUNTIME_PARALLEL_B"},
            ]},
        ]}, state)
        self.assertIn(b"HOBOT_RUNTIME_PARALLEL_COMPLETE", parallel_complete)
        self.assertNotIn(b"HOBOT_RUNTIME_PROBE_COMPLETE", parallel_complete)
        recovery = MODULE.anthropic_response({"messages": [
            {"role": "user", "content": "stage recovery and nonce invalid-on-purpose"},
            {"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_runtime_recovery_1", "name": "hobot_runtime_probe", "input": {}}]},
            {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_runtime_recovery_1", "content": "HOBOT_RUNTIME_PROBE_EXPECTED_ARGUMENT_ERROR", "is_error": True}]},
        ]}, state)
        self.assertIn(b"repaired-after-error", recovery)
        compacted = MODULE.anthropic_response({"messages": [{
            "role": "user",
            "content": "Preserve exact opaque identifiers and user constraints: HOBOT_RUNTIME_MEMORY_STORED HOBOT_COMPACTION_CANARY_7F3C9A2D",
        }]}, state)
        self.assertIn(b"Preserve HOBOT_COMPACTION_CANARY_7F3C9A2D", compacted)
        self.assertNotIn(b'"text":"HOBOT_RUNTIME_MEMORY_STORED"', compacted)

        holder = MODULE.anthropic_response({"messages": [{"role": "user", "content": "HOBOT_EXTENSION_LEASE_HOLDER"}]}, state)
        self.assertIn(b"toolu_extension_holder", holder)
        holder_complete = MODULE.anthropic_response({"messages": [{
            "role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_extension_holder", "content": ""}],
        }]}, state)
        self.assertIn(b"HOBOT_EXTENSION_HOLDER_OK", holder_complete)
        contender_complete = MODULE.anthropic_response({"messages": [{
            "role": "user", "content": [{
                "type": "tool_result", "tool_use_id": "toolu_extension_contender", "is_error": True,
                "content": "Workspace writes are busy: another Agent owns the lease.",
            }],
        }]}, state)
        self.assertIn(b"HOBOT_EXTENSION_LEASE_BLOCKED", contender_complete)

    def test_anthropic_mock_emits_structured_thinking_and_validates_tui_input(self) -> None:
        state = MODULE.GatewayState()
        thinking = MODULE.anthropic_response({"messages": [{
            "role": "user",
            "content": "Use the structured reasoning channel, then return HOBOT_RUNTIME_THINKING_COMPLETE.",
        }]}, state)
        self.assertIn(b'"type":"thinking"', thinking)
        self.assertIn(b'"type":"thinking_delta"', thinking)
        self.assertIn(b"HOBOT_RUNTIME_THINKING_COMPLETE", thinking)

        chinese = MODULE.anthropic_response({"messages": [{
            "role": "user", "content": "HOBOT_TUI_CHINESE \u4f60\u597d\uff0c\u5730\u74dc\u5f00\u53d1\u677f",
        }]}, state)
        self.assertIn(b"HOBOT_TUI_THINKING_CHINESE", chinese)
        self.assertIn(b"HOBOT_TUI_CHINESE_OK", chinese)

        edited = MODULE.anthropic_response({"messages": [{
            "role": "user", "content": "HOBOT_TUI_EDIT_OK \u4e2d\u6587\u7f16\u8f91",
        }]}, state)
        self.assertIn(b"HOBOT_TUI_EDIT_OK_RESPONSE", edited)
        with self.assertRaisesRegex(MODULE.VerificationError, "deleted text"):
            MODULE.anthropic_response({"messages": [{
                "role": "user", "content": "HOBOT_TUI_EDIT_BAD",
            }]}, state)

    def test_approval_response_matches_exact_dialog_method(self) -> None:
        self.assertEqual(MODULE.approval_allow_once_response({
            "id": "confirm-1", "method": "confirm",
        }), {
            "type": "extension_ui_response", "id": "confirm-1", "confirmed": True,
        })
        self.assertEqual(MODULE.approval_allow_once_response({
            "id": "select-1", "method": "select", "options": ["Allow once", "Deny"],
        }), {
            "type": "extension_ui_response", "id": "select-1", "value": "Allow once",
        })

    def test_approval_response_rejects_ambiguous_or_interactive_dialogs(self) -> None:
        with self.assertRaisesRegex(MODULE.VerificationError, "exactly one"):
            MODULE.approval_allow_once_response({
                "id": "select-1", "method": "select", "options": ["Allow this exact call for this task", "Deny"],
            })
        with self.assertRaisesRegex(MODULE.VerificationError, "cannot be accepted"):
            MODULE.approval_allow_once_response({"id": "input-1", "method": "input"})
        with self.assertRaisesRegex(MODULE.VerificationError, "no ID"):
            MODULE.approval_allow_once_response({"method": "confirm"})

    def test_value_locations_reports_paths_without_payloads(self) -> None:
        payload = "private-image"
        value = {"events": [{"event": {"message": {"content": [{"type": "image", "data": payload}]}}}]}
        self.assertEqual(MODULE.value_locations(value, payload), ["$.events[0].event.message.content[0].data"])
        self.assertEqual(MODULE.value_locations(value, "absent"), [])

    def test_session_recovery_result_requires_exact_partial_evidence(self) -> None:
        checks = [
            {"name": name, "status": "passed", "message": "bounded evidence"}
            for name in sorted(MODULE.RUNTIME_PROBE_CHECKS)
        ]
        result = {
            "schemaVersion": 1, "scope": "agent-runtime-partial", "provider": "anthropic-test", "model": "anthropic-test",
            "status": "partial", "reasoningDeclared": True, "imageInputDeclared": True,
            "checks": checks, "pending": ["rdk-task-suite"],
        }
        self.assertEqual(MODULE.validate_runtime_recovery_result(result), [
            {"name": "context-compaction", "status": "pass"},
            {"name": "interrupted-session-recovery", "status": "pass"},
        ])
        result["checks"] = checks[:-1]
        with self.assertRaisesRegex(MODULE.VerificationError, "incomplete"):
            MODULE.validate_runtime_recovery_result(result)


if __name__ == "__main__":
    unittest.main()

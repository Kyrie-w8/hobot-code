from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import pwd
import stat
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "verify-install-lifecycle.py"
SPEC = importlib.util.spec_from_file_location("verify_install_lifecycle", SCRIPT)
assert SPEC is not None
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class InstallLifecycleVerifierTest(unittest.TestCase):
    def package_fixture(self, root: Path) -> tuple[Path, str]:
        package = root / "package"
        (package / "nested").mkdir(parents=True)
        (package / "VERSION").write_text("0.26.0\n", encoding="utf-8")
        (package / "nested" / "payload.txt").write_text("verified\n", encoding="utf-8")
        lines = []
        for path in sorted((package / name for name in ("VERSION", "nested/payload.txt")), key=str):
            lines.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.relative_to(package).as_posix()}")
        content = "\n".join(lines) + "\n"
        (package / "MANIFEST.sha256").write_text(content, encoding="utf-8")
        return package, hashlib.sha256(content.encode("utf-8")).hexdigest()

    def test_manifest_binds_every_regular_file(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package, digest = self.package_fixture(Path(temporary))
            self.assertEqual(MODULE.verify_manifest(package), digest)
            (package / "nested" / "payload.txt").write_text("changed\n", encoding="utf-8")
            with self.assertRaisesRegex(MODULE.VerificationError, "checksum mismatch"):
                MODULE.verify_manifest(package)

    def test_manifest_rejects_unlisted_files_and_links(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package, _ = self.package_fixture(Path(temporary))
            (package / "extra.txt").write_text("extra\n", encoding="utf-8")
            with self.assertRaisesRegex(MODULE.VerificationError, "missing from the manifest"):
                MODULE.verify_manifest(package)
            (package / "extra.txt").unlink()
            (package / "linked.txt").symlink_to("VERSION")
            with self.assertRaisesRegex(MODULE.VerificationError, "non-regular file"):
                MODULE.verify_manifest(package)

    def test_private_report_is_atomic_private_and_rejects_links(self) -> None:
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

    def test_report_must_be_outside_candidate_package(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package = Path(temporary).resolve() / "package"
            package.mkdir()
            with self.assertRaisesRegex(MODULE.VerificationError, "outside"):
                MODULE.validate_report_destination(package, package / "report.json")
            MODULE.validate_report_destination(package, package.parent / "report.json")

    def test_metadata_fingerprint_changes_without_reading_payload_into_report(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            payload = root / "state.txt"
            payload.write_text("first\n", encoding="utf-8")
            before = MODULE.metadata_fingerprint((root,))
            payload.write_text("second-and-longer\n", encoding="utf-8")
            after = MODULE.metadata_fingerprint((root,))
            self.assertNotEqual(before, after)
            self.assertNotIn("first", before)
            self.assertNotIn("second", after)

    def test_lifecycle_check_set_is_stable_and_complete(self) -> None:
        self.assertEqual(MODULE.CHECKS, (
            "isolated-root",
            "first-install",
            "ordinary-user-launcher",
            "upgrade-preserves-user-data",
            "failed-upgrade-restores-runtime",
            "rollback-restores-runtime",
            "uninstall-preserves-user-data",
        ))
        with self.assertRaisesRegex(MODULE.VerificationError, "does not exist or is root"):
            MODULE.select_test_user("root")

    def test_traversal_check_uses_the_target_account_permissions(self) -> None:
        account = pwd.getpwuid(os.getuid())
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            root.chmod(0o700)
            self.assertEqual(MODULE.account_can_traverse(root.stat(), account), True)
            root.chmod(0o600)
            self.assertEqual(MODULE.account_can_traverse(root.stat(), account), False)


if __name__ == "__main__":
    unittest.main()

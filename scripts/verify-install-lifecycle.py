#!/usr/bin/env python3
"""Verify install, upgrade, recovery, rollback, and uninstall on an RDK board.

The verifier must run as root against an extracted Linux ARM64 release package.
Every write is redirected below a private temporary installation root. Existing
Hobot Code programs, backups, configuration, and task state are fingerprinted
before and after the test and must remain unchanged.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import pwd
import re
import shutil
import stat
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path, PurePosixPath
from typing import Any, Optional


MAX_MANIFEST_BYTES = 4 * 1024 * 1024
MAX_MANIFEST_FILES = 4096
MANIFEST_LINE = re.compile(r"^([0-9a-f]{64})  ([^\x00\r\n]+)$")
STRICT_VERSION = re.compile(
    r"^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)"
    r"(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$"
)
CHECKS = (
    "isolated-root",
    "first-install",
    "ordinary-user-launcher",
    "upgrade-preserves-user-data",
    "failed-upgrade-restores-runtime",
    "rollback-restores-runtime",
    "uninstall-preserves-user-data",
)
PRODUCTION_PATHS = (
    Path("/usr/local/lib/hobot-code"),
    Path("/usr/local/lib/hobot-code-backups"),
    Path("/usr/local/lib/hobot-code.install.lock"),
    Path("/usr/local/bin/hobot"),
    Path("/usr/local/sbin/hobot-rollback"),
    Path("/etc/hobot-code"),
    Path("/var/lib/hobot-code"),
)


class VerificationError(RuntimeError):
    pass


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_manifest(package: Path) -> str:
    manifest_path = package / "MANIFEST.sha256"
    try:
        info = manifest_path.lstat()
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= MAX_MANIFEST_BYTES:
            raise VerificationError("package manifest is not a bounded regular file")
        content = manifest_path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as error:
        raise VerificationError(f"cannot read package manifest: {error}") from error

    expected: dict[str, str] = {}
    lines = content.splitlines()
    if not lines or len(lines) > MAX_MANIFEST_FILES:
        raise VerificationError("package manifest has an invalid file count")
    for line_number, line in enumerate(lines, start=1):
        match = MANIFEST_LINE.fullmatch(line)
        if match is None:
            raise VerificationError(f"package manifest line {line_number} is malformed")
        name = match.group(2)
        relative = PurePosixPath(name)
        if relative.is_absolute() or name != relative.as_posix() or any(part in {"", ".", ".."} for part in relative.parts):
            raise VerificationError(f"package manifest line {line_number} has an unsafe path")
        if name == "MANIFEST.sha256" or name in expected:
            raise VerificationError(f"package manifest line {line_number} is duplicated or self-referential")
        expected[name] = match.group(1)

    actual: set[str] = set()
    for directory, directories, files in os.walk(package, followlinks=False):
        base = Path(directory)
        for name in directories:
            path = base / name
            if path.is_symlink():
                raise VerificationError(f"package contains a symbolic link: {path.relative_to(package).as_posix()}")
        for name in files:
            path = base / name
            relative_name = path.relative_to(package).as_posix()
            file_info = path.lstat()
            if not stat.S_ISREG(file_info.st_mode):
                raise VerificationError(f"package contains a non-regular file: {relative_name}")
            if relative_name == "MANIFEST.sha256":
                continue
            actual.add(relative_name)
            expected_digest = expected.get(relative_name)
            if expected_digest is None:
                raise VerificationError(f"package file is missing from the manifest: {relative_name}")
            if sha256_file(path) != expected_digest:
                raise VerificationError(f"package file checksum mismatch: {relative_name}")
    missing = sorted(set(expected) - actual)
    if missing:
        raise VerificationError(f"package manifest references a missing file: {missing[0]}")
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


def report_destination(path: Path) -> Path:
    requested = path.expanduser()
    return requested.parent.resolve(strict=False) / requested.name


def validate_report_destination(package: Path, path: Path) -> None:
    destination = report_destination(path)
    try:
        destination.relative_to(package)
    except ValueError:
        return
    raise VerificationError("report output must be outside the candidate package root")


def private_report(path: Path, report: dict[str, Any]) -> None:
    destination = report_destination(path)
    destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    try:
        current = destination.lstat()
    except FileNotFoundError:
        current = None
    if current is not None and (
        not stat.S_ISREG(current.st_mode) or stat.S_ISLNK(current.st_mode) or current.st_uid != os.getuid()
    ):
        raise VerificationError("report destination must be a regular file owned by the current user")
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{destination.name}.", dir=destination.parent)
    temporary = Path(temporary_name)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(report, output, ensure_ascii=True, indent=2, sort_keys=True)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, destination)
        destination.chmod(0o600)
    except Exception:
        try:
            os.close(descriptor)
        except OSError:
            pass
        temporary.unlink(missing_ok=True)
        raise


def read_text(path: Path) -> str:
    try:
        return path.read_bytes().replace(b"\x00", b"").decode("utf-8", errors="replace").strip()
    except OSError:
        return ""


def detect_target() -> dict[str, str]:
    architecture = platform.machine().lower()
    if platform.system() != "Linux" or architecture not in {"aarch64", "arm64"}:
        raise VerificationError("install lifecycle acceptance must run on a Linux ARM64 board")
    model = read_text(Path("/sys/firmware/devicetree/base/model")) or read_text(Path("/proc/device-tree/model"))
    lowered = model.lower()
    board_id = next((candidate for candidate in ("s600", "s100", "x5") if candidate in lowered), "")
    if not board_id:
        raise VerificationError("host is not a recognized RDK X5, S100, or S600")
    version = read_text(Path("/etc/version")).lstrip("vV")
    if not STRICT_VERSION.fullmatch(version):
        for line in read_text(Path("/etc/os-release")).splitlines():
            if line.startswith("VERSION_ID="):
                version = line.removeprefix("VERSION_ID=").strip('"').lstrip("vV")
                break
    if not STRICT_VERSION.fullmatch(version):
        raise VerificationError("RDK OS version is missing or is not strict SemVer")
    return {"architecture": architecture, "boardId": board_id, "rdkOsVersion": version}


def select_test_user(requested: Optional[str]) -> pwd.struct_passwd:
    candidates = [requested] if requested else [os.environ.get("SUDO_USER"), "sunrise", "ubuntu", "rdk", "nobody"]
    for candidate in candidates:
        if not candidate or candidate == "root":
            continue
        try:
            account = pwd.getpwnam(candidate)
        except KeyError:
            continue
        if account.pw_uid != 0:
            return account
    if requested:
        raise VerificationError("the selected lifecycle test user does not exist or is root")
    raise VerificationError("no existing non-root account is available; pass --user with an existing account")


def account_can_traverse(info: os.stat_result, account: pwd.struct_passwd) -> bool:
    mode = stat.S_IMODE(info.st_mode)
    if info.st_uid == account.pw_uid:
        return bool(mode & stat.S_IXUSR)
    try:
        groups = os.getgrouplist(account.pw_name, account.pw_gid)
    except OSError:
        groups = [account.pw_gid]
    if info.st_gid in groups:
        return bool(mode & stat.S_IXGRP)
    return bool(mode & stat.S_IXOTH)


def select_temporary_parent(account: pwd.struct_passwd) -> Path:
    candidates = []
    configured = os.environ.get("HOBOT_CODE_LIFECYCLE_TMPDIR", "")
    if configured:
        candidates.append(Path(configured))
    candidates.extend((Path("/var/tmp"), Path(tempfile.gettempdir())))
    seen: set[Path] = set()
    for candidate in candidates:
        if not candidate.is_absolute() or candidate in seen:
            continue
        seen.add(candidate)
        try:
            info = candidate.lstat()
            physical = candidate.resolve(strict=True)
        except OSError:
            continue
        mode = stat.S_IMODE(info.st_mode)
        if (
            physical != candidate
            or not stat.S_ISDIR(info.st_mode)
            or stat.S_ISLNK(info.st_mode)
            or info.st_uid != 0
            or (mode & 0o022 and not mode & stat.S_ISVTX)
            or not account_can_traverse(info, account)
        ):
            continue
        return candidate
    raise VerificationError("no safe root-owned temporary directory is traversable by the lifecycle test user")


def metadata_fingerprint(paths: tuple[Path, ...] = PRODUCTION_PATHS) -> str:
    digest = hashlib.sha256()
    for root in paths:
        try:
            root_info = root.lstat()
        except FileNotFoundError:
            digest.update(f"{root}:absent\n".encode())
            continue
        entries = [root]
        if stat.S_ISDIR(root_info.st_mode) and not stat.S_ISLNK(root_info.st_mode):
            for directory, directories, files in os.walk(root, followlinks=False):
                directories.sort()
                files.sort()
                base = Path(directory)
                entries.extend(base / name for name in directories)
                entries.extend(base / name for name in files)
        for path in entries:
            info = path.lstat()
            kind = "link" if stat.S_ISLNK(info.st_mode) else "dir" if stat.S_ISDIR(info.st_mode) else "file" if stat.S_ISREG(info.st_mode) else "other"
            link = os.readlink(path) if kind == "link" else ""
            record = (
                f"{path}|{kind}|{stat.S_IMODE(info.st_mode):o}|{info.st_uid}|{info.st_gid}|"
                f"{info.st_size}|{info.st_mtime_ns}|{info.st_ctime_ns}|{link}\n"
            )
            digest.update(record.encode("utf-8", errors="surrogateescape"))
    return digest.hexdigest()


def run_checked(command: list[str], environment: dict[str, str], *, expected_failure: bool = False) -> subprocess.CompletedProcess[str]:
    try:
        result = subprocess.run(
            command,
            env=environment,
            text=True,
            capture_output=True,
            check=False,
            timeout=300,
        )
    except subprocess.TimeoutExpired as error:
        raise VerificationError("lifecycle command exceeded the five-minute timeout") from error
    if expected_failure:
        if result.returncode == 0:
            raise VerificationError("the injected upgrade failure unexpectedly succeeded")
        return result
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()[-2000:]
        raise VerificationError(f"lifecycle command failed: {detail or f'exit {result.returncode}'}")
    return result


def assert_file(path: Path, *, executable: bool = False, owner: int | None = None) -> None:
    try:
        info = path.lstat()
    except FileNotFoundError as error:
        raise VerificationError("an expected lifecycle artifact is missing") from error
    if not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode):
        raise VerificationError("a lifecycle artifact is not a regular file")
    if executable and stat.S_IMODE(info.st_mode) & 0o111 == 0:
        raise VerificationError("a lifecycle command is not executable")
    if owner is not None and info.st_uid != owner:
        raise VerificationError("a lifecycle artifact has the wrong owner")


def write_user_sentinel(path: Path, value: str, account: pwd.struct_passwd) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(value, encoding="utf-8")
    os.chown(path, account.pw_uid, account.pw_gid)
    path.chmod(0o600)


def assert_sentinel(path: Path, value: str, owner: int) -> None:
    assert_file(path, owner=owner)
    if path.read_text(encoding="utf-8") != value:
        raise VerificationError("user data changed during the lifecycle transaction")


def run_as_user(account: pwd.struct_passwd, command: list[str], environment: dict[str, str]) -> subprocess.CompletedProcess[str]:
    runuser = shutil.which("runuser")
    if runuser is None:
        raise VerificationError("runuser is required to verify the launcher as an ordinary user")
    assignments = [f"{key}={value}" for key, value in environment.items()]
    return run_checked([runuser, "-u", account.pw_name, "--", "env", "-i", *assignments, *command], environment)


def verify(package: Path, account: pwd.struct_passwd) -> dict[str, Any]:
    if os.getuid() != 0:
        raise VerificationError("install lifecycle acceptance must run as root")
    target = detect_target()
    required = (
        "VERSION", "PI_COMPATIBILITY.json", "MANIFEST.sha256", "agentd", "runtime/hobot",
        "hobot-launcher", "install.sh", "rollback.sh", "uninstall.sh",
    )
    for name in required:
        assert_file(package / name, executable=name in {"agentd", "runtime/hobot", "hobot-launcher", "install.sh", "rollback.sh", "uninstall.sh"})
    manifest_sha256 = verify_manifest(package)
    version = (package / "VERSION").read_text(encoding="utf-8").strip()
    if not STRICT_VERSION.fullmatch(version):
        raise VerificationError("candidate package version is not strict SemVer")
    if shutil.which("bwrap") is None:
        raise VerificationError("bubblewrap must be installed before lifecycle acceptance")

    production_before = metadata_fingerprint()
    temporary_parent = select_temporary_parent(account)
    with tempfile.TemporaryDirectory(prefix="hobot-lifecycle-", dir=temporary_parent) as temporary_name:
        temporary = Path(temporary_name)
        temporary.chmod(0o755)
        install_root = temporary / "root"
        install_root.mkdir(mode=0o755)
        proc_root = temporary / "empty-proc"
        proc_root.mkdir(mode=0o555)
        test_home = install_root / "home" / account.pw_name
        test_home.mkdir(parents=True, mode=0o700)
        os.chown(test_home, account.pw_uid, account.pw_gid)
        temp_root = install_root / "tmp"
        temp_root.mkdir(mode=0o700)

        runtime_root = install_root / "usr/local/lib/hobot-code"
        backup_root = install_root / "usr/local/lib/hobot-code-backups"
        launcher = install_root / "usr/local/bin/hobot"
        rollback = install_root / "usr/local/sbin/hobot-rollback"
        config_sentinel = test_home / ".config/hobot-code/lifecycle-sentinel.txt"
        state_sentinel = test_home / ".local/state/hobot-code/memory/lifecycle-sentinel.txt"
        base_environment = {
            "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
            "LANG": "C.UTF-8",
            "LC_ALL": "C.UTF-8",
            "HOME": str(test_home),
            "TMPDIR": str(temp_root),
            "HOBOT_CODE_TESTING": "1",
            "HOBOT_CODE_TEST_INSTALL_ROOT": str(install_root),
            "HOBOT_CODE_TEST_PROC_ROOT": str(proc_root),
            "HOBOT_CODE_INSTALL_USER": account.pw_name,
            "HOBOT_CODE_INSTALL_HOME": str(test_home),
            "HOBOT_CODE_INSTALL_CHANNEL": "stable",
            "HOBOT_CODE_BACKUP_KEEP": "10",
            "HOBOT_CODE_BACKUP_MAX_MIB": "2048",
        }

        run_checked([str(package / "install.sh")], base_environment)
        if (runtime_root / "VERSION").read_text(encoding="utf-8").strip() != version:
            raise VerificationError("first install has the wrong runtime version")
        for path in (runtime_root / "hobot", runtime_root / "agentd", launcher, rollback):
            assert_file(path, executable=True, owner=0)
        config_root = test_home / ".config/hobot-code"
        state_root = test_home / ".local/state/hobot-code"
        if config_root.stat().st_uid != account.pw_uid or state_root.stat().st_uid != account.pw_uid:
            raise VerificationError("first install did not create user-owned configuration and state")

        user_environment = {
            "PATH": base_environment["PATH"],
            "HOME": str(test_home),
            "HOBOT_CODE_TESTING": "1",
            "HOBOT_CODE_TEST_INSTALL_ROOT": str(install_root),
            "HOBOT_CODE_TEST_PROC_ROOT": str(proc_root),
        }
        launcher_result = run_as_user(account, [str(launcher), "setup", "--help"], user_environment)
        if "hobot setup" not in launcher_result.stdout.lower():
            raise VerificationError("ordinary-user launcher did not reach the setup command")

        config_value = "HOBOT_LIFECYCLE_CONFIG_SENTINEL\n"
        state_value = "HOBOT_LIFECYCLE_STATE_SENTINEL\n"
        write_user_sentinel(config_sentinel, config_value, account)
        write_user_sentinel(state_sentinel, state_value, account)
        first_generation = "first-installed-generation\n"
        (runtime_root / "LIFECYCLE_GENERATION").write_text(first_generation, encoding="utf-8")

        run_checked([str(package / "install.sh")], base_environment)
        if (runtime_root / "LIFECYCLE_GENERATION").exists():
            raise VerificationError("upgrade did not replace the active runtime")
        assert_sentinel(config_sentinel, config_value, account.pw_uid)
        assert_sentinel(state_sentinel, state_value, account.pw_uid)
        backups = sorted(path for path in backup_root.iterdir() if path.is_dir())
        if not backups or not any((path / "runtime-installed/LIFECYCLE_GENERATION").is_file() for path in backups):
            raise VerificationError("upgrade did not retain a rollback-capable runtime backup")

        second_generation = "second-installed-generation\n"
        (runtime_root / "LIFECYCLE_GENERATION").write_text(second_generation, encoding="utf-8")
        failure_environment = dict(base_environment)
        failure_environment["HOBOT_CODE_TEST_FAIL_AFTER_SWAP"] = "1"
        failure = run_checked([str(package / "install.sh")], failure_environment, expected_failure=True)
        if "Injected isolated install failure" not in failure.stderr:
            raise VerificationError("failure injection did not reach the post-swap recovery path")
        if (runtime_root / "LIFECYCLE_GENERATION").read_text(encoding="utf-8") != second_generation:
            raise VerificationError("failed upgrade did not restore the active runtime")
        assert_sentinel(config_sentinel, config_value, account.pw_uid)
        assert_sentinel(state_sentinel, state_value, account.pw_uid)

        rollback_environment = dict(base_environment)
        run_checked([str(rollback)], rollback_environment)
        if (runtime_root / "LIFECYCLE_GENERATION").read_text(encoding="utf-8") != first_generation:
            raise VerificationError("rollback did not restore the previous runtime generation")
        assert_sentinel(config_sentinel, config_value, account.pw_uid)
        assert_sentinel(state_sentinel, state_value, account.pw_uid)

        uninstall_environment = dict(base_environment)
        uninstall_environment.update({
            "HOBOT_CODE_UNINSTALL_USER": account.pw_name,
            "HOBOT_CODE_UNINSTALL_HOME": str(test_home),
        })
        run_checked([str(runtime_root / "uninstall.sh"), "--yes"], uninstall_environment)
        if runtime_root.exists() or launcher.exists() or rollback.exists():
            raise VerificationError("uninstall left an active program or command behind")
        if not backup_root.is_dir():
            raise VerificationError("non-purge uninstall deleted installation backups")
        assert_sentinel(config_sentinel, config_value, account.pw_uid)
        assert_sentinel(state_sentinel, state_value, account.pw_uid)

    if metadata_fingerprint() != production_before:
        raise VerificationError("isolated lifecycle acceptance changed an existing production path")

    return {
        "schema": "hobot.pi-board-compatibility/v1",
        "scenario": "install-lifecycle",
        "status": "pass",
        "capturedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "target": target,
        "build": {
            "version": version,
            "agentdSha256": sha256_file(package / "agentd"),
            "manifestSha256": manifest_sha256,
            "piCompatibilitySha256": sha256_file(package / "PI_COMPATIBILITY.json"),
        },
        "checks": [{"name": name, "status": "pass"} for name in CHECKS],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package-root", required=True, type=Path, help="extracted Hobot Code Linux ARM64 package")
    parser.add_argument("--output", required=True, type=Path, help="private sanitized lifecycle report outside the package")
    parser.add_argument("--user", help="existing non-root account used for launcher and user-data checks")
    args = parser.parse_args()
    try:
        requested_package = args.package_root.expanduser()
        if requested_package.is_symlink() or not requested_package.is_dir():
            raise VerificationError("package root must be a real directory")
        package = requested_package.resolve()
        validate_report_destination(package, args.output)
        account = select_test_user(args.user)
        report = verify(package, account)
        private_report(args.output, report)
        print("PASS install lifecycle: isolated install, upgrade recovery, rollback, and uninstall preservation")
        print(f"WROTE {args.output}")
    except (OSError, VerificationError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

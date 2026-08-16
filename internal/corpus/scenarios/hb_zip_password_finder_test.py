#!/usr/bin/env python3
"""Public differential judge for the ZIP password finder Python port."""

from __future__ import annotations

import py_compile
import re
import subprocess
import sys
from pathlib import Path


ROOT = Path.cwd()
REFERENCE = ROOT / ".hbench" / "zip-password-finder-reference"
EXECUTABLE = ROOT / "executable"
TEST_FILES = ROOT / "test-files"
TIMEOUT_SECONDS = 90

CASES = [
    ("find generated", ["-i", "2.test.txt.zip", "-c", "l", "--maxPasswordLen", "2", "-w", "1"]),
    ("find generated from starting password", ["-i", "3.test.txt.zip", "-c", "l", "--maxPasswordLen", "3", "-s", "abc", "-w", "1"]),
    ("not found", ["-i", "2.test.txt.zip", "-c", "l", "--maxPasswordLen", "1", "-w", "1"]),
    ("dictionary", ["-i", "2.test.txt.zip", "-p", "generated-passwords-lowercase.txt", "-w", "1"]),
    ("mask two lowercase", ["-i", "2.test.txt.zip", "--mask", "?l?l", "-w", "1"]),
    ("mask custom charset", ["-i", "2.test.txt.zip", "--mask", "?1?1", "-1", "ab", "-w", "1"]),
    ("missing input", ["-i", "missing.zip"]),
    ("workers zero", ["-i", "2.test.txt.zip", "-w", "0"]),
    ("minimum length zero", ["-i", "2.test.txt.zip", "--minPasswordLen", "0"]),
    ("maximum before minimum", ["-i", "2.test.txt.zip", "--minPasswordLen", "3", "--maxPasswordLen", "2"]),
    ("file number missing", ["-i", "2.test.txt.zip", "--fileNumber", "99", "-c", "l", "--maxPasswordLen", "2"]),
]

REQUIRED_OPTIONS = {
    "--inputFile",
    "--workers",
    "--passwordDictionary",
    "--charset",
    "--charsetFile",
    "--minPasswordLen",
    "--maxPasswordLen",
    "--fileNumber",
    "--startingPassword",
    "--mask",
    "--customCharset1",
}


def run(argv: list[str], timeout: int = TIMEOUT_SECONDS) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        argv,
        cwd=ROOT,
        text=True,
        encoding="utf-8",
        errors="replace",
        capture_output=True,
        timeout=timeout,
        env={"PATH": "/usr/local/bin:/usr/bin:/bin", "NO_COLOR": "1", "TERM": "dumb"},
    )


def materialize_args(args: list[str]) -> list[str]:
    paths = {"2.test.txt.zip", "3.test.txt.zip", "generated-passwords-lowercase.txt"}
    return [str(TEST_FILES / value) if value in paths else value for value in args]


def normalize(text: str) -> str:
    text = text.replace("\r\n", "\n")
    text = re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", text)
    text = re.sub(r"Time elapsed: [^\n]*", "Time elapsed: <DURATION>", text)
    return " ".join(text.split())


def password(text: str) -> str | None:
    match = re.search(r"(?:password|found)[^A-Za-z0-9]+([A-Za-z0-9_!@#$%^&*.-]+)", text, re.I)
    return match.group(1) if match else None


def same_behavior(actual: subprocess.CompletedProcess[str], expected: subprocess.CompletedProcess[str]) -> bool:
    if actual.returncode != expected.returncode:
        return False
    if expected.returncode != 0:
        return bool(normalize(actual.stderr) or normalize(actual.stdout))
    wanted = password(expected.stdout + "\n" + expected.stderr)
    if wanted:
        return password(actual.stdout + "\n" + actual.stderr) == wanted
    return normalize(actual.stdout) == normalize(expected.stdout)


checks: list[tuple[str, bool, str]] = []


def check(name: str, passed: bool, detail: str = "") -> None:
    checks.append((name, passed, detail))


compile_script = ROOT / "compile.sh"
check("compile.sh exists", compile_script.is_file())

if compile_script.is_file():
    built = run(["sh", str(compile_script)], timeout=180)
    check("compile.sh succeeds", built.returncode == 0, normalize(built.stderr or built.stdout))
else:
    check("compile.sh succeeds", False, "compile.sh is missing")

check("executable exists", EXECUTABLE.is_file())

try:
    py_compile.compile(str(EXECUTABLE), doraise=True)
    check("executable is valid Python", True)
except (OSError, py_compile.PyCompileError) as error:
    check("executable is valid Python", False, str(error))

check("frozen reference binary is available", REFERENCE.is_file())

if EXECUTABLE.is_file():
    help_result = run([sys.executable, str(EXECUTABLE), "--help"], timeout=15)
else:
    help_result = subprocess.CompletedProcess([], 127, "", "missing executable")
help_text = help_result.stdout + "\n" + help_result.stderr
check("root help succeeds", help_result.returncode == 0, normalize(help_text))
missing_options = sorted(option for option in REQUIRED_OPTIONS if option not in help_text)
check("root help covers the frozen CLI", not missing_options, ", ".join(missing_options))

for name, raw_args in CASES:
    args = materialize_args(raw_args)
    try:
        expected = run([str(REFERENCE), *args])
        actual = run([sys.executable, str(EXECUTABLE), *args])
        detail = f"status {actual.returncode}, expected {expected.returncode}"
        check(name, same_behavior(actual, expected), detail)
    except (OSError, subprocess.TimeoutExpired) as error:
        check(name, False, str(error))

for number, (name, passed, detail) in enumerate(checks, 1):
    suffix = f" # {detail}" if detail and not passed else ""
    print(f"{'ok' if passed else 'not ok'} {number} - {name}{suffix}")

print(f"1..{len(checks)}")
raise SystemExit(0 if len(checks) == 18 and all(passed for _, passed, _ in checks) else 1)

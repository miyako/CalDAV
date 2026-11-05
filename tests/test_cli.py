"""Independent stdin protocol checks; network/feature cases also run in Go."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
from build_cli import smoke_test

ICALENDAR = (
    "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalDAV Tests//EN\r\n"
    "BEGIN:VEVENT\r\nUID:stable-test@example.invalid\r\n"
    "DTSTAMP:20261101T000000Z\r\nDTSTART;VALUE=DATE:20261106\r\n"
    "DTEND;VALUE=DATE:20261107\r\nSUMMARY:Calendar test\r\n"
    "END:VEVENT\r\nEND:VCALENDAR\r\n"
)


class CalDAVCLI(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.environ.get("TEST_CLI_LANGUAGE") == "none":
            raise unittest.SkipTest("CLI disabled explicitly")
        cls.scratch = tempfile.TemporaryDirectory(prefix="caldav cli path ")
        cls.addClassCleanup(cls.scratch.cleanup)
        cls.executable = Path(cls.scratch.name) / ("caldav.exe" if os.name == "nt" else "caldav")
        env = os.environ.copy()
        env.update(CGO_ENABLED="0", GOTOOLCHAIN="local")
        env.pop("GOOS", None)
        env.pop("GOARCH", None)
        subprocess.run(["go", "build", "-mod=readonly", "-trimpath", "-o", str(cls.executable), "."],
                       cwd=ROOT / "cli", env=env, check=True, timeout=180)

    def run_cli(self, arguments, value=b""):
        return subprocess.run([str(self.executable), *arguments], input=value,
                              capture_output=True, timeout=30)

    def test_version(self):
        result = self.run_cli(["--version"])
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, b'{"protocol":1,"version":"0.1.1"}\n')
        self.assertEqual(result.stderr, b"")

    def test_contract(self):
        config = json.loads((ROOT / "template.json").read_text())
        smoke_test(self.executable, config["version"], ROOT / "tests/cli-contract.json")

    def test_inspect_stdin(self):
        request = {"protocol": 1, "operation": "inspect", "icalendar": ICALENDAR}
        result = self.run_cli(["request"], json.dumps(request).encode())
        self.assertEqual(result.returncode, 0, result.stderr)
        value = json.loads(result.stdout)
        self.assertEqual(value["protocol"], 1)
        self.assertEqual(value["operation"], "inspect")
        self.assertIsInstance(value["data"], dict)
        self.assertEqual(value["data"]["icalendar"], ICALENDAR)
        self.assertEqual(result.stderr, b"")

    def test_large_multiline_unicode_input_is_not_truncated(self):
        long_text = (" x-" + "\u65e5\u672c\u8a9e" * 7 + "\r\n") * 600
        properties = "DESCRIPTION:large\r\n" + long_text
        for index in range(3):
            properties += f"X-PAYLOAD-{index}:large\r\n" + long_text
        calendar = ICALENDAR.replace("SUMMARY:Calendar test\r\n",
                                    "SUMMARY:\u65e5\u672c\u8a9e\r\n" + properties)
        self.assertGreater(len(calendar.encode("utf-8")), 128 * 1024)
        result = self.run_cli(["request"], json.dumps(
            {"protocol": 1, "operation": "inspect", "icalendar": calendar},
            ensure_ascii=False).encode("utf-8"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["data"]["icalendar"], calendar)

    def test_oversized_input_fails_without_echoing_payload(self):
        result = self.run_cli(["request"], b"x" * (4 * 1024 * 1024))
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, b"")
        self.assertLess(len(result.stderr), 1024)
        self.assertIsInstance(json.loads(result.stderr)["error"]["code"], str)

    def test_malformed_stdin_is_explicit_and_redacted(self):
        for value in (b"", b"\xff", b"[]", b"null", b"{}", b"{}{}",
                      b'{"protocol":true,"operation":"inspect"}',
                      b'{"protocol":1,"operation":"inspect","password":"synthetic-redaction-marker","unknown":1}'):
            with self.subTest(value=value[:32]):
                result = self.run_cli(["request"], value)
                self.assertEqual(result.returncode, 2)
                self.assertEqual(result.stdout, b"")
                error = json.loads(result.stderr)
                self.assertEqual(error["protocol"], 1)
                self.assertIsInstance(error["error"]["code"], str)
                self.assertNotIn(b"synthetic-redaction-marker", result.stderr)

    def test_credentials_not_accepted_as_arguments(self):
        result = self.run_cli(["request", "--password", "synthetic-redaction-marker"])
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, b"")
        self.assertNotIn(b"synthetic-redaction-marker", result.stderr)


class StdinSmokeContract(unittest.TestCase):
    def test_stdin_vectors_are_executed(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            contract = Path(directory) / "contract.json"
            contract.write_text(json.dumps({"schema": 1, "cases": [
                {"args": ["request"], "stdin": "input text", "exitCode": 0,
                 "stdout": "response\n", "stderr": ""},
                {"args": ["request"], "stdin": "", "exitCode": 2,
                 "stdout": "", "stderr": "invalid\n"}]}))
            with patch("build_cli.subprocess.run", side_effect=[
                    subprocess.CompletedProcess([], 0, b"response\n", b""),
                    subprocess.CompletedProcess([], 2, b"", b"invalid\n")]) as run:
                smoke_test(Path(directory) / "caldav", "0.1.0", contract)
                self.assertEqual(run.call_args_list[0].kwargs["input"], b"input text")

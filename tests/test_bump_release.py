"""Exercise the adopted Go feature's bounded release-version registry."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
import bump_release


class BumpReleaseTests(unittest.TestCase):
    def setUp(self):
        scratch = tempfile.TemporaryDirectory(prefix="caldav-version-")
        self.addCleanup(scratch.cleanup)
        self.root = Path(scratch.name)
        for name in ("component", "cli", "tests/host"):
            shutil.copytree(ROOT / name, self.root / name,
                            ignore=shutil.ignore_patterns("bin", "DerivedData", "Libraries"))
        for name in ("template.json", "package.json", "README.md",
                     "tests/test_cli.py", "tests/cli-contract.json", "release-policy.json"):
            destination = self.root / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / name, destination)
        self.version = json.loads((self.root / "template.json").read_text())["version"]
        self.git("init", "-q")
        self.git("config", "user.name", "Release fixture")
        self.git("config", "user.email", "release-fixture@example.invalid")
        self.git("remote", "add", "origin", "https://github.com/miyako/CalDAV.git")
        self.git("add", ".")
        self.git("commit", "-q", "-m", "fixture")
        environment = patch.dict(os.environ, {"GITHUB_REPOSITORY": "miyako/CalDAV"})
        environment.start()
        self.addCleanup(environment.stop)

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.root, check=True,
                              capture_output=True, text=True).stdout.strip()

    def snapshot(self):
        return {str(p.relative_to(self.root)): p.read_bytes()
                for p in self.root.rglob("*") if p.is_file() and ".git" not in p.parts}

    def test_feature_version_surfaces_and_idempotence(self):
        before = self.snapshot()
        result = bump_release.apply(self.root, "1.2.3")
        after = self.snapshot()
        self.assertEqual(result["changed_paths"],
                         sorted(name for name in before if before[name] != after[name]))
        for name in ("template.json", "package.json"):
            self.assertEqual(json.loads((self.root / name).read_text())["version"], "1.2.3")
        self.assertIn('"version":"1.2.3"', (self.root / "tests/test_cli.py").read_text())
        self.assertIn('"version":"1.2.3"',
                      (self.root / "component/CalDAV/Documentation/Protocol.md").read_text())
        self.assertEqual(bump_release.apply(self.root, "1.2.3")["changed_paths"], [])
        self.assertEqual(after, self.snapshot())
        for name in ("cli/go.mod", "cli/go.sum", "release-policy.json"):
            if name in before:
                self.assertEqual(before[name], after[name])

    def test_next_highest_stable_numeric_tag_and_modes(self):
        for tag in ("1.9.9", "1.10.2", "20.0.0-rc.1", "9.0.0+build", "01.0.0",
                    "v99.0.0"):
            self.git("tag", tag)
        for mode, version in (("patch", "1.10.3"), ("minor", "1.11.0"), ("major", "2.0.0")):
            self.assertEqual(bump_release.next_version(self.root, mode),
                             {"version": version, "tag": version})

    def test_no_tag_and_retry(self):
        major, minor, patch_number = map(int, self.version.split("."))
        self.assertEqual(bump_release.next_version(self.root, "patch")["version"],
                         f"{major}.{minor}.{patch_number + 1}")
        self.git("tag", "0.1.0")
        bump_release.apply(self.root, "0.1.1")
        self.assertEqual(bump_release.next_version(self.root, "patch")["version"], "0.1.1")

    def test_invalid_version_and_destination_make_no_changes(self):
        before = self.snapshot()
        for version in ("v1.0.0", "01.2.3", "1.2", "1.2.3\n", "1.2.3-beta", None):
            with self.subTest(version=version), self.assertRaises(ValueError):
                bump_release.apply(self.root, version)
            self.assertEqual(before, self.snapshot())
        for repository in ("miyako/caldav", "other/CalDAV", "miyako/Wrong"):
            with self.subTest(repository=repository), self.assertRaises(ValueError):
                bump_release.apply(self.root, "1.0.0", repository)
        self.git("remote", "set-url", "origin", "https://github.com/miyako/4d-component-template.git")
        with patch.dict(os.environ, {"GITHUB_REPOSITORY": "miyako/4d-component-template"}):
            with self.assertRaisesRegex(ValueError, "original template"):
                bump_release.apply(self.root, "1.0.0")

    def test_inconsistent_surface_fails_before_writes(self):
        for name in ("README.md", "tests/test_cli.py", "tests/host/contract.json"):
            path = self.root / name
            original = path.read_text()
            changed = original.replace(self.version, "0.9.9")
            self.assertNotEqual(original, changed)
            path.write_text(changed)
            before = self.snapshot()
            with self.subTest(name=name), self.assertRaises(ValueError):
                bump_release.apply(self.root, "1.2.3", stage=True)
            self.assertEqual(before, self.snapshot())
            self.assertEqual(self.git("diff", "--cached", "--name-only"), "")
            path.write_text(original)

    def test_stages_only_bounded_version_paths(self):
        (self.root / "unrelated.txt").write_text("leave untracked\n")
        result = bump_release.apply(self.root, "0.2.0", stage=True)
        self.assertEqual(self.git("diff", "--cached", "--name-only").splitlines(),
                         result["changed_paths"])
        self.assertNotIn("unrelated.txt", result["changed_paths"])

    def test_missing_and_symlink_version_surfaces_rejected(self):
        path = self.root / "tests/test_cli.py"
        original = path.read_bytes()
        path.unlink()
        with self.assertRaises(ValueError):
            bump_release.apply(self.root, "1.2.3")
        if os.name == "posix":
            target = self.root / "outside.py"
            target.write_bytes(original)
            path.symlink_to(target)
            with self.assertRaises(ValueError):
                bump_release.apply(self.root, "1.2.3")

    def test_ahead_of_tag_candidate_refuses_downgrade(self):
        bump_release.apply(self.root, "3.4.5")
        before = self.snapshot()
        for mode in ("patch", "minor", "major"):
            with self.subTest(mode=mode), self.assertRaisesRegex(ValueError, "ahead"):
                bump_release.next_version(self.root, mode, tags=["1.2.3"])
        self.assertEqual(before, self.snapshot())

    def test_policy_history_dependencies_and_arbitrary_versions_unchanged(self):
        readme = self.root / "README.md"
        original = readme.read_text()
        history = '\nHistorical response {"protocol":1,"version":"0.1.0"}\n'
        readme.write_text(original + history)
        policy = self.root / "release-policy.json"
        policy.write_text('{"schema":1,"apple_notarized_versions":["0.1.0"]}\n')
        before = self.snapshot()
        bump_release.apply(self.root, "0.2.0")
        self.assertEqual(readme.read_text(),
                         original.replace('"version":"' + self.version + '"',
                                          '"version":"0.2.0"') + history)
        after = self.snapshot()
        for name in ("cli/go.mod", "cli/go.sum", "release-policy.json"):
            if name in before:
                self.assertEqual(before[name], after[name])

    def test_malformed_contract_and_duplicate_metadata_fail_atomically(self):
        for name, value in (
                ("tests/cli-contract.json", "{}"),
                ("tests/host/contract.json", '{"schema":1,"feature":{}}'),
                ("package.json", '{"version":"0.1.0","version":"0.1.0"}')):
            path = self.root / name
            original = path.read_text()
            path.write_text(value)
            before = self.snapshot()
            with self.subTest(name=name), self.assertRaises((ValueError, KeyError)):
                bump_release.apply(self.root, "1.2.3", stage=True)
            self.assertEqual(before, self.snapshot())
            self.assertEqual(self.git("diff", "--cached", "--name-only"), "")
            path.write_text(original)

    def test_retry_revalidates_current_surfaces(self):
        bump_release.apply(self.root, "1.2.4")
        contract = self.root / "tests/host/contract.json"
        contract.write_text(contract.read_text().replace("1.2.4", "1.2.3"))
        before = self.snapshot()
        with self.assertRaisesRegex(ValueError, "inconsistent"):
            bump_release.next_version(self.root, "patch", tags=["1.2.3"])
        self.assertEqual(before, self.snapshot())

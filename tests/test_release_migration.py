"""Historical migration never acts as a general prefixed-tag exception."""

import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
import verify_release_migration as migration


class ReleaseMigrationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.downloads = self.root / "downloads"
        self.downloads.mkdir()
        (self.root / "release-migrations").mkdir()
        self.canonical = self.root / "release-migrations/0.1.2.json"
        self.published = self.downloads / "CalDAV-migration.json"
        self.record = json.loads((ROOT / "release-migrations/0.1.2.json").read_text())
        for asset in self.record["assets"]:
            value = ("synthetic " + asset["name"]).encode()
            (self.downloads / asset["name"]).write_bytes(value)
            asset.update(bytes=len(value), sha256=hashlib.sha256(value).hexdigest())
        self.save()

    def save(self):
        value = json.dumps(self.record).encode()
        self.canonical.write_bytes(value)
        self.published.write_bytes(value)

    def test_exact_record_revalidates_original_then_bare_ref(self):
        with patch.object(migration, "verify_original") as original, \
                patch.object(migration, "verify") as reference:
            result = migration.verify_migration(self.root, self.downloads, "0.1.2")
        original.assert_called_once_with(self.root, self.downloads, self.record)
        reference.assert_called_once_with(self.root, "0.1.2", self.record["commit"])
        self.assertEqual(result["result"], "verified")
        self.assertTrue(result["original_provenance_validated"])
        self.assertFalse(result["rebuilt"])
        self.assertFalse(result["resigned"])

    def test_preparation_does_not_claim_ref_verified(self):
        with patch.object(migration, "verify_original"), \
                patch.object(migration, "verify") as reference:
            result = migration.verify_migration(
                self.root, self.downloads, "0.1.2", check_ref=False)
        reference.assert_not_called()
        self.assertEqual(result["result"], "prepared")
        self.assertFalse(result["ref_verified"])

    def test_missing_unknown_prefixed_and_altered_records_fail_before_validation(self):
        with patch.object(migration, "verify_original") as original:
            for tag in ("v0.1.2", "main", "../0.1.2", "0.1.3"):
                with self.subTest(tag=tag), self.assertRaises(ValueError):
                    migration.verify_migration(self.root, self.downloads, tag)
            self.published.unlink()
            with self.assertRaises(ValueError):
                migration.verify_migration(self.root, self.downloads, "0.1.2")
            self.save()
            self.published.write_bytes(self.published.read_bytes() + b"\n")
            with self.assertRaisesRegex(ValueError, "differs"):
                migration.verify_migration(self.root, self.downloads, "0.1.2")
        original.assert_not_called()

    def test_changed_historical_asset_bytes_never_reach_original_validator(self):
        with patch.object(migration, "verify_original") as original:
            for asset in self.record["assets"]:
                path = self.downloads / asset["name"]
                before = path.read_bytes()
                path.write_bytes(before + b"altered")
                with self.subTest(asset=asset["name"]), self.assertRaisesRegex(ValueError, "bytes changed"):
                    migration.verify_migration(self.root, self.downloads, "0.1.2")
                path.write_bytes(before)
        original.assert_not_called()

    def test_invalid_registered_identity_is_not_an_authorization(self):
        before = copy.deepcopy(self.record)
        for key, value in (
                ("schema", True), ("metadata_only", False), ("rebuilt", True),
                ("resigned", True), ("repository", "other/CalDAV"), ("release_id", True),
                ("original_tag", "v0.1.3"), ("commit", "HEAD"), ("tag", "0.1.3"),
                ("original_publish_run", "https://example.test/")):
            self.record = {**copy.deepcopy(before), key: value}
            self.save()
            with self.subTest(key=key), self.assertRaises(ValueError):
                migration.registered(self.root, self.downloads, "0.1.2")
        self.record = before
        self.record["assets"][0]["name"] = "../CalDAV.zip"
        self.save()
        with self.assertRaises(ValueError):
            migration.registered(self.root, self.downloads, "0.1.2")

    def test_original_provenance_failure_cannot_be_bypassed_by_new_ref(self):
        with patch.object(migration, "verify_original", side_effect=subprocess.CalledProcessError(1, "validator")), \
                patch.object(migration, "verify") as reference:
            with self.assertRaises(subprocess.CalledProcessError):
                migration.verify_migration(self.root, self.downloads, "0.1.2")
        reference.assert_not_called()

    def test_new_ref_must_point_to_original_accepted_commit(self):
        with patch.object(migration, "verify_original"), \
                patch.object(migration, "verify", side_effect=ValueError("release tag moved")):
            with self.assertRaisesRegex(ValueError, "moved"):
                migration.verify_migration(self.root, self.downloads, "0.1.2")

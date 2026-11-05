#!/usr/bin/env python3
"""Verify an explicitly registered metadata-only migration, never a new release."""

import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

from build_provenance import sha256
from bump_release import STABLE
from ci_config import ROOT
from release_ref import verify
from test_4d import _read_json


def regular(path):
    if not path.is_file() or any(parent.is_symlink() for parent in (path, *path.parents)):
        raise ValueError("migration evidence must use regular non-symlink files")
    return path


def registered(root, downloads, tag):
    """Only exact checked-in migration records authorize historical interpretation."""
    if not isinstance(tag, str) or not re.fullmatch(STABLE, tag, re.ASCII):
        raise ValueError("migration tag must be a bare stable version")
    canonical = regular(root / "release-migrations" / (tag + ".json"))
    record = _read_json(canonical)
    if (not isinstance(record, dict) or set(record) != {
            "schema", "kind", "repository", "release_id", "version", "original_tag",
            "tag", "commit", "original_publish_run", "metadata_only", "rebuilt",
            "resigned", "assets"}
            or type(record["schema"]) is not int or record["schema"] != 1
            or record["kind"] != "release-tag-migration"
            or record["repository"] != "miyako/CalDAV"
            or record["version"] != tag or record["tag"] != tag
            or record["original_tag"] != "v" + tag
            or type(record["release_id"]) is not int or record["release_id"] <= 0
            or record["metadata_only"] is not True
            or record["rebuilt"] is not False or record["resigned"] is not False
            or not isinstance(record["commit"], str)
            or not re.fullmatch(r"[0-9a-f]{40}", record["commit"])
            or not isinstance(record["original_publish_run"], str)
            or not re.fullmatch(
                r"https://github\.com/miyako/CalDAV/actions/runs/[1-9][0-9]*",
                record["original_publish_run"])):
        raise ValueError("invalid registered migration identity")
    published = regular(downloads / "CalDAV-migration.json")
    if published.read_bytes() != canonical.read_bytes():
        raise ValueError("downloaded migration record differs from the registered record")
    assets = record["assets"]
    if (not isinstance(assets, list) or len(assets) != 4
            or not all(isinstance(asset, dict) for asset in assets)
            or [asset.get("name") for asset in assets] != [
                "CalDAV.zip", "CalDAV.dmg", "CalDAV-signing.json", "CalDAV.acceptance.json"]):
        raise ValueError("migration must preserve exactly the four original assets")
    for asset in assets:
        if (set(asset) != {"name", "bytes", "sha256"} or type(asset["bytes"]) is not int
                or asset["bytes"] <= 0 or not isinstance(asset["sha256"], str)
                or not re.fullmatch(r"[0-9a-f]{64}", asset["sha256"])):
            raise ValueError("invalid registered asset identity")
        path = regular(downloads / asset["name"])
        if path.stat().st_size != asset["bytes"] or sha256(path) != asset["sha256"]:
            raise ValueError("historical asset bytes changed: " + asset["name"])
    return record


def verify_original(root, downloads, record):
    """Run the unchanged original validator in an isolated historical checkout."""
    subprocess.run(
        ["git", "merge-base", "--is-ancestor", record["commit"], "HEAD"],
        cwd=root, check=True, capture_output=True)
    build = root / "build"
    build.mkdir(exist_ok=True)
    if any(path.is_symlink() for path in (build, *build.parents)):
        raise ValueError("migration staging must not traverse symlinks")
    with tempfile.TemporaryDirectory(prefix="release-migration-", dir=build) as temporary:
        snapshot = Path(temporary) / "original"
        subprocess.run(
            ["git", "clone", "--no-hardlinks", "--no-checkout", "--quiet",
             str(root), str(snapshot)], check=True, capture_output=True)
        subprocess.run(["git", "checkout", "--quiet", "--detach", record["commit"]],
                       cwd=snapshot, check=True, capture_output=True)
        (snapshot / "dist").mkdir()
        for asset in record["assets"]:
            shutil.copy2(downloads / asset["name"], snapshot / "dist" / asset["name"])
        code = (
            "import pathlib,sys;sys.path.insert(0,'scripts');"
            "from release_assets import verify_assets;"
            "verify_assets(pathlib.Path.cwd(),sys.argv[1],sys.argv[2],sys.argv[3],True)"
        )
        subprocess.run(
            [sys.executable, "-c", code, record["repository"], record["original_tag"],
             record["commit"]], cwd=snapshot, check=True, capture_output=True)


def verify_migration(root, downloads, tag, *, check_ref=True):
    root, downloads = Path(root).resolve(), Path(downloads).absolute()
    record = registered(root, downloads, tag)
    verify_original(root, downloads, record)
    if check_ref:
        verify(root, tag, record["commit"])
    return {
        "result": "verified" if check_ref else "prepared",
        "tag": tag, "commit": record["commit"], "release_id": record["release_id"],
        "original_tag": record["original_tag"], "original_assets_preserved": True,
        "original_provenance_validated": True, "ref_verified": check_ref,
        "migration_sha256": sha256(downloads / "CalDAV-migration.json"),
        "rebuilt": False, "resigned": False,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--downloads", type=Path, required=True)
    parser.add_argument("--tag", required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(verify_migration(args.root, args.downloads, args.tag), indent=2))
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(2, "migration verification failed: " + type(error).__name__ + "\n")


if __name__ == "__main__":
    main()

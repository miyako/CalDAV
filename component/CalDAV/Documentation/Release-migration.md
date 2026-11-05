# Bare release tags and initial-release migration

CalDAV release tags equal the component's SemVer without a prefix:
[`0.1.2`](https://github.com/miyako/CalDAV/releases/tag/0.1.2), not `v0.1.2`.
Normal version selection, tag creation, packaging, signing and publication
validators require this convention. They do not accept a legacy signing tag
as an alias for a newly published tag.

## Historical `0.1.2` correction

The original [Publish run](https://github.com/miyako/CalDAV/actions/runs/42128674737)
actually used `v0.1.2` at accepted commit
`aaa06f1da641bd2d21604a278a4ec8c05513f66c`. The user subsequently authorized
a **metadata-only** migration to `0.1.2` at that exact commit, retaining GitHub
release ID `435874038`. No source revision, version, signed payload or original
acceptance claim is changed. This is not a new build or signing run.

The following four published assets remain byte-identical:

| Asset | Bytes | SHA256 |
| --- | --- | --- |
| `CalDAV.zip` | 16230090 | `93bcc0934da60fefc5eeace4df05eee6be984c744f2083c541ac3f1489988fab` |
| `CalDAV.dmg` | 18227867 | `cd84a0e0ebc79a37d88063de3c076491c3cb292acfc4177a7973718fa6a3a5b1` |
| `CalDAV-signing.json` | 995 | `2a3174ffc6c10e610bd1a5af880096dd53a167bee5796177a64490f0defd1141` |
| `CalDAV.acceptance.json` | 1477410 | `ed89188e7e2bb8c8322c5c2c7e254944943d3470d6d6824bfd81c4cbf99344e2` |

The original proof still truthfully contains `releaseTag:"v0.1.2"`. It and the
original acceptance JSON are **historical build evidence**, not freshly issued
bare-tag evidence. Do not edit them or reinterpret the old pipeline as having
run under a different tag.

An additional `CalDAV-migration.json` is required to interpret this one migrated
release. Its exact bytes are registered in
[`release-migrations/0.1.2.json`](../../../release-migrations/0.1.2.json):
repository, retained release ID, old/new tags, accepted SHA, original run and
the four original asset hashes/sizes. No other release gets a prefix exception.

Download all five release assets, then run:

```sh
git fetch origin tag 0.1.2
python3 scripts/verify_release_migration.py --root . \
  --downloads /path/to/downloaded-assets --tag 0.1.2
```

The verifier requires a byte-identical registered migration record and all four
unchanged assets, verifies that the accepted commit is an ancestor, and runs the
**unchanged original asset validator** in a disposable isolated checkout of that
commit. That validator checks the original tag-bound proof, source digests,
native manifests and source/unsigned/final host contracts under their actual
historical rules. Finally, the new bare ref must point to the exact accepted SHA.
It never creates or deletes a tag, uploads an asset or publishes a release.

OS signature/stapling and actual 4D loading remain separate runtime checks.
The original native Intel/Windows 4D and Dependency manager UI `not_run` limits
are not upgraded by this metadata correction.

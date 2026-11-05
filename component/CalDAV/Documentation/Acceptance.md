# Dated acceptance evidence

These are executed observations, not a guarantee of every provider, timezone,
recurrence pattern or native 4D platform. Credentials, account identifiers,
resource URLs, UIDs and event content are deliberately excluded.

## November 6, 2026: implementation revision

Revision: `1284802e7ea6206fc4f037a4339bc1946cdcf872`, version `0.1.0`.

| Check | Result and scope |
| --- | --- |
| Native macOS arm64 CLI | passed, Go 1.26.10 |
| Native macOS x86_64 CLI | passed, Go 1.26.10; not translated execution |
| Native Windows x86_64 CLI | passed, Go 1.26.10 |
| Source 4D host, compiler and exact unpacked compiled ZIP host | passed, macOS arm64, tool4d 20.7 build 20.101905 |
| Native Intel 4D host | not_run |
| Native Windows 4D host | not_run |

Hosted runs: [exact-head push](https://github.com/miyako/CalDAV/actions/runs/42122418748)
and [PR test merge](https://github.com/miyako/CalDAV/actions/runs/42122467189).
The PR run uses its own synthetic merge revision; its artifact is not the
exact-head push artifact.

The exact-head unsigned hosted ZIP is 16,131,979 bytes, SHA256
`af0e1a5a025c585cb34b620ef7235dd61928bc79a7c715721d663332199b47df`.
Its adjacent acceptance JSON binds native manifests, source/compiler/final-host
reports and asset bytes. This dated artifact is not the future signed release.

## Authorized live iCloud observations

Only the uniquely exact dedicated calendar display name `test` was selected.
The bounded query interval was November 3, 2026 00:00 through November 10,
2026 00:00 JST, end-exclusive. Other calendars were not queried for event data.
Credentials came from the user's login Keychain through private in-memory
subprocess pipes, then CLI stdin; none were saved or emitted in evidence.

| Live check | Result |
| --- | --- |
| Authenticated principal/home/calendar discovery | passed; one exact calendar match |
| Initial bounded query | passed; zero resources, so event fields/ETags were not exercised by that initial query |
| Basic timed event with explicit numeric offset | passed: conditional create, GET with strong ETag, UTC-normalized values, query, conditional update and readback |
| All-day recurring series | passed: master, RRULE, EXDATE and detached RECURRENCE-ID exception returned as a complete resource |
| Full-resource preservation during series update | passed: detached exception, all-day dates, EXDATE and custom property retained |
| Stale ETag update | passed for both fixtures: HTTP 412 conflict |
| Conditional deletion and absence | passed for both fixtures: subsequent GET returned HTTP 404 |
| Cleanup/ownership boundary | two temporary resources created, two deleted, zero remaining; no preexisting resources mutated, no attendees added |
| Live compiled 4D facade discovery/query | passed, macOS arm64, actual exact-head hosted ZIP, compiled mode true; zero resources after cleanup |

The live mutation process used the same-revision native Go 1.25.1 arm64 CLI,
SHA256 `1530654ca3f1e54331142699ce2ca24ee5a521677b6413303a6876d38ab127a6`.
The 4D live read-only check used the separately identified hosted Go 1.26.10
artifact. Neither observation substitutes for exact-version release rebuilding.

No local occurrence expansion, invitation scheduling, sharing, background
synchronization or custom VTIMEZONE interpretation is claimed. The live
recurrence check verifies these stored fixtures and full-resource preservation,
not exhaustive occurrence matching or live DST/custom-zone behavior.

## Publication boundary

The published initial release is
[`0.1.2`](https://github.com/miyako/CalDAV/releases/tag/0.1.2). Its original
signed pipeline used `v0.1.2`; the subsequent explicitly authorized
[metadata-only migration](Release-migration.md) preserves the accepted commit
and all four original assets, with a separate required migration record.
Original build proof/evidence are unchanged, not rewritten to claim a new run.

The first [Publish attempt](https://github.com/miyako/CalDAV/actions/runs/42126792241)
created the `0.1.1` metadata commit
`d35c3e2fad16d58f30a1a5ee315057d2625d55f9`, then stopped at three version-test
fixture assertions hardcoded to `0.1.0`. It never reached signing, tagging or
publication: **`0.1.1` was not published**. The assertions now derive the copied
fixture's configured version without changing production version selection.

With no existing stable tag, the existing workflow uses the current configured
version as its bump base. The user explicitly selected the next patch,
**`0.1.2`**, rather than rewriting history or adding retry infrastructure.
The version policy requires Apple signing for both candidates:
missing credentials, failed signing/notarization or failed final-host checks
must stop publication, not fall back to unsigned.

Publish must rebuild and accept its own version commit. It requires all three
native CLI manifests, assembly, actual source host, both compiler targets,
package integrity and exact final signed ZIP host acceptance. Signing also
requires accepted notarization, stapled DMG and validated signing proof.
Downloaded bytes/provenance must match before tag/release creation.

Native Intel/Windows 4D and published Dependency manager installation are
separate evidence rows, not mandatory checks in the existing publication
validator. Hosted acceptance intentionally remains `result:"partial"` and
`release_ready:false`; publication must not be presented as complete
cross-platform acceptance. Released-asset installation and signing outcomes
must be established for the actual release, not inferred from this document.

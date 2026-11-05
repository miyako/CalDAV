# CalDAV

CLI-backed **4D component** for remote iCloud and standards-based CalDAV calendars.
Repository/component/package: `CalDAV`; independent namespace: `CalDAV`;
public class: `Calendar`; bundled Go executable: `caldav`.
This is not EventKit, a local-only macOS calendar interface, or a native 4D plug-in.
No host-installed plug-in is required.

The component discovers principals, calendar home sets and calendars, queries
date ranges, retrieves complete event resources, and creates, replaces or deletes
resources with HTTP preconditions. Offline event building and inspection make
basic creation possible without hand-writing iCalendar.

**Range results are matching complete calendar resources/series, not an expanded
occurrence list.** A server evaluates the RFC 4791 VEVENT time-range filter,
including recurring instances and exceptions whose master DTSTART lies outside
the range. CalDAV returns the original resource with its recurrence rules,
detached exceptions, unknown properties and VTIMEZONE components. This client
does not locally expand recurrence or implement occurrence-level mutation.

## 4D API

```4d
var $calendar : cs.CalDAV.Calendar
var $result; $request : Object
var $target : Text
$target:="macOS-universal"
If (Is Windows)
    $target:="Windows-x64"
End if
$calendar:=cs.CalDAV.Calendar.new($target)
// Obtain these values from your host application's secure storage, not literals.
$request:=New object("url"; "https://caldav.icloud.com/"; \
    "username"; $accountEmail; "password"; $appSpecificPassword)
$result:=$calendar.discover($request)
If (Not($result.ok))
    ALERT($result.error.code+": "+$result.error.message)
End if
```

See [Calendar methods, creation examples and result shapes](component/CalDAV/Documentation/Classes/Calendar.md)
and the [versioned request/response protocol](component/CalDAV/Documentation/Protocol.md).
The generic [_CLI](component/CalDAV/Documentation/Classes/_CLI.md) resolver and
[_Process](component/CalDAV/Documentation/Classes/_Process.md) adapter are
implementation details by convention, not an access-control boundary.

### Editing and concurrency

Retrieve the resource and its ETag first. Preserve the full `icalendar` text,
including all VEVENT exceptions, unknown properties and VTIMEZONE components;
modify only the intended fields, then replace the **whole resource** with its
original ETag. Raw iCalendar is transported unchanged rather than parse/re-encoded
for storage. A replacement is not a merge, and a detached exception must not be
uploaded as if it were the entire series. Deleting a resource deletes the series
and its exceptions, not one occurrence.

Create uses `If-None-Match: *`; update/delete require a strong supplied ETag and
use `If-Match`. Conflicts are explicit errors; there are no unconditional
overwrite fallbacks, hidden retries or automatic merges. Retain UIDs when editing.
If a mutation times out, the server may have committed it: retrieve before retrying.
Calls are independent synchronous subprocesses, no shared session/cache.
The host process deadline is 30 seconds; the CLI network deadline is shorter.
No asynchronous cancellation handle, background synchronization or persistence
is provided. A terminated write cannot be assumed rolled back.

## Authentication and transport

For iCloud, enable two-factor authentication on your Apple Account, then visit
[account.apple.com](https://account.apple.com/), **Sign-In and Security >
App-Specific Passwords > Generate an app-specific password**. Use your Apple
Account email and that generated password, not your normal account password.
Apple may require primary-account confirmation. Revoke the app-specific
password there when no longer needed. See Apple's
[app-specific password instructions](https://support.apple.com/102654).
This release does not claim eligibility for Apple's supported-third-party OAuth.

Credentials belong in **stdin JSON only**, never command-line arguments,
environment variables, logs, source or fixtures. Supply them per request from
the host application's secure storage; this component provides no plaintext
credential store and never asks for passwords in chat. Request objects and
process memory still contain sensitive data: do not log them. There is no
guarantee of memory zeroization or protection against a compromised host.

TLS certificates are verified. Credentials cannot follow arbitrary redirects or
cross-origin DAV hrefs. iCloud service discovery permits only the explicitly
validated provider service-host pattern documented in the protocol, not arbitrary
`*.icloud.com` hosts. Local mock tests use an explicit loopback-only HTTP opt-in;
there is no general insecure TLS mode. Input, response and resource counts are
bounded; malformed UTF-8/JSON/XML/iCalendar and multistatus errors fail explicitly.

Excluded: contacts/CardDAV, invitation/attendee scheduling workflows, sharing
management, occurrence editing, local recurrence expansion, and automatic
background two-way synchronization.

## CLI protocol

UTF-8 JSON, one request object on stdin, exactly one JSON response plus LF.

| Invocation | Result |
| --- | --- |
| `caldav request` | Read bounded stdin JSON; return a protocol-1 operation/data envelope or explicit stderr error |
| `caldav --version` | `{"protocol":1,"version":"0.1.1"}` + LF |

Never put credentials in the command line. The documented protocol includes
offline `inspect` and `build` operations for local preparation/testing; network
requests use `discover`, `query`, `get`, `create`, `update` and `delete`.
The path diagnostic exists only to verify native argument quoting, not as a
credential transport. See [Protocol](component/CalDAV/Documentation/Protocol.md)
for exact fields, limits, statuses, time representations and error codes.

## Installation

After an approved release, add GitHub dependency `miyako/CalDAV` in the host's
Dependency manager and select its version; the asset must be `CalDAV.zip`.
Alternatively unpack a locally accepted ZIP and put `CalDAV.4dbase` in the
host's `Components/` folder. This is separate-process CLI execution, not a
plug-in loader. Signing is optional by version policy; unsigned downloads may
be blocked by Gatekeeper/quarantine, and no trust bypass is provided.

Target CLI platforms: macOS universal arm64/x86_64 and Windows x86_64.
Windows callers must select `Windows-x64`; native Windows ARM64 is unsupported.
Respect your 4D runtime's platform requirements. Go CI is pinned in the workflow;
actual local tool versions are recorded in acceptance evidence.

## Build and acceptance

Read [AGENTS.md](AGENTS.md) for the canonical build/package/acceptance playbook.
The configured repository is already initialized; do not reinitialize it.
Every headless 4D project includes `Project/Sources/dependencies.json`.

```sh
python3 scripts/doctor.py --root .
cd cli && go test ./... && cd ..
python3 -m unittest discover -s tests -v
python3 scripts/generate_icons.py --check
python3 scripts/ci_config.py download-tool4d --root .
python3 scripts/accept_local.py --root . --repository miyako/CalDAV \
  --tool4d .tools/tool4d.app/Contents/MacOS/tool4d --cross
```

`--cross` is Go/macOS-only artifact production, not foreign native execution.
Acceptance builds same-input manifests, assembles both macOS slices and Windows
x64, runs source host calls, performs real 4D compilation, packs source-stripped
`CalDAV.zip`, then loads **the exact ZIP's unpacked compiled component**.
Compiler-generated completion metadata must contain `Calendar`, not the old
starter class. Neither CLI/Python tests nor compilation alone proves loading.

Stable outputs: `dist/CalDAV.zip` and `dist/CalDAV.acceptance.json` with SHA256,
revision/input provenance and per-check statuses. Latest-attempt diagnostics
are in `build/acceptance.json`, `build/source-host-status.json`,
`build/compile-status.json` and `build/compiled-host-status.json`.
`passed`, `failed` and `not_run` are distinct. Local Apple Silicon acceptance
remains **partial / release_ready:false**; native Intel/Windows CLI and 4D,
Dependency manager downloads, and live iCloud are separate gates.
Live iCloud tests require explicit authorization for a dedicated test
account/calendar; none is assumed by automated tests. On November 6, 2026,
an explicitly authorized dedicated calendar passed controlled live event and
all-day recurring-series round trips, conditional conflicts and cleanup.
The actual compiled macOS arm64 4D facade also passed live discovery/query.
See [dated acceptance evidence and remaining limits](component/CalDAV/Documentation/Acceptance.md).
Synthetic loopback server tests never write to personal calendars.

Exactly two workflows are retained: **CI** validates selected-language native
artifacts; optional hosted 4D inspection (`FOURD_ENABLED=true`) retains compiled
ZIP/evidence without publication. **Publish** is manual, main-only, accepts
patch/minor/major, commits synchronized versions and accepts the exact revision
before creating its tag/release. Do not dispatch it without further approval.
Unsigned is default for unlisted versions; [release-policy.json](release-policy.json) requires
Apple signing/notarization for the initial published `0.1.2` release through the
existing isolated Apple signing/notarized-DMG path, followed by final-ZIP host
retesting. No signing credentials are needed for local unsigned acceptance.

## Licenses

The template/component is [MIT](LICENSE); retain its copyright.
Go runtime and selected library notices ship under
`component/CalDAV/Resources/licenses/`. Dependency pins and checksums are in
`cli/go.mod` and `cli/go.sum`; the protocol documents library evaluation.
Vendor tool4d is separately licensed and is not included in the component.

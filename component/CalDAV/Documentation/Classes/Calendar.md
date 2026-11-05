# Calendar

`cs.CalDAV.Calendar.new({target : Text})` is the synchronous CalDAV facade.
Within the component the class is `cs.Calendar`. Omit `target` on macOS, or
use `"macOS-universal"`; Windows requires `"Windows-x64"` explicitly.
Other targets fail with `unsupported_architecture`.

| Method | Input and operation |
| --- | --- |
| `execute(request : Object) : Object` | Dispatch an explicit `operation` |
| `build(event : Object) : Object` | Build a basic iCalendar event locally, without network access |
| `inspect(icalendar : Text) : Object` | Inspect an iCalendar document locally, without network access |
| `discover(request : Object) : Object` | Discover calendar collections from `url` |
| `query(request : Object) : Object` | Query collection `url` using required UTC `start` and `end` bounds |
| `get(request : Object) : Object` | Fetch the event resource at `url` |
| `create(request : Object) : Object` | Create event resource `url` from `icalendar` without overwriting |
| `update(request : Object) : Object` | Replace event resource `url` from `icalendar`, conditional on `etag` |
| `delete(request : Object) : Object` | Delete event resource `url`, conditional on `etag` |
| `version() : Object` | Decode `{"protocol":1,"version":"0.1.2"}` |

Remote request fields are `url`, `username`, `password`, `icalendar`, `etag`,
`start`, `end` and `event`, as required by the operation. Supply credentials for each call;
the instance never stores credentials or request Objects. Named remote wrappers
copy the request before setting `operation`; `execute` copies it before setting
`protocol:1`, so caller Objects are not modified. A Null request or unsupported
operation fails before launching the executable. Detailed field validation,
iCalendar parsing, HTTP/TLS handling and CalDAV semantics belong to the CLI.
Do not pass credentials embedded in URLs.
Updates replace the **complete resource**, not selected properties or PATCH
operations; update/delete require a strong `etag` obtained from the server.
Queries return full raw calendar resources in `data.resources`; they do not
expand recurrence instances locally. Mutations address whole resources/series,
not individual occurrences; occurrence-mutation requests are rejected.
Query bounds must be whole-second RFC 3339 UTC timestamps ending in `Z`,
with `end > start`; the interval is start-inclusive and end-exclusive.
See the canonical [CLI protocol](../Protocol.md) for field requirements,
network policy, limits and redacted error codes.

For basic creation, `event` contains `summary`, `start` and `end`, with optional
`uid`, `description`, `location`, `timezone` and Boolean `allDay`. Omitting `uid`
generates a cryptographically random UUIDv4 URN, returned in `events[0].uid`;
keep that UID when updating the resulting resource. Timed values use explicit,
whole-second RFC 3339 timestamps ending in `Z` or an explicit numeric offset.
Numeric offsets are normalized to UTC; ambiguous local wall-clock Text and
named local zones are rejected. `timezone` must be omitted,
empty or `"UTC"`; it does not synthesize timezone rules. With
`allDay:true`, use `YYYY-MM-DD` dates and an **exclusive** end date.
`build(event)` returns iCalendar in `result.value.data.icalendar`; `create`
can accept the same `event` Object instead of hand-authored `icalendar`.
Do not send both representations. `build` is a basic-event builder, not a
recurrence editor; use complete, valid iCalendar when preserving an existing
series with timezone definitions or recurrence exceptions.
`create({event:…})` returns `data:{url,etag,uid,icalendar}`, so retain the generated
UID and complete resource text along with its URL/ETag. Raw `create({icalendar:…})`
returns `data:{url,etag}`.

Requests go as UTF-8 JSON on **stdin** to the bundled executable with the single
argument `request`. Credentials and calendar bodies never become process
arguments or instance properties. They exist transiently in the caller's request,
serialized input and child process memory; this is not secure memory erasure.
Protect caller logging and storage independently.

Success is `{ok:true,stdout:Text,stderr:Text,exitCode:0,value:Object}` with
`value:{protocol:1,operation:Text,data:Object}`. The facade requires numeric
protocol 1, the requested operation and operation-specific data fields. Calendar
summaries require nonempty raw Text, a numeric count matching a nonempty events
Collection, and typed event fields. Discovery requires principal/home URLs and
typed calendar entries; query requires a Collection of complete resources;
get requires its URL, Text ETag and calendar summary. Create/update require a
nonempty URL and Text ETag (which may be empty when the server omits it); delete
requires a nonempty URL and Boolean `deleted:true`. Empty discovery/query
Collections are valid; missing, Null or wrongly typed Collections are not.
The facade does not invent server results or reinterpret iCalendar
recurrence/timezone semantics.
`version()` instead requires numeric protocol 1 and a Text `version`.
Keep returned raw iCalendar as the authoritative round-trip representation:
summaries are not unique identifiers, recurrence overrides share the master's
UID, all-day `DATE` values are not UTC instants, and `VTIMEZONE`, `RRULE`,
`EXDATE` and unknown properties must not be reconstructed from a summary alone.
Inspection is not local recurrence expansion.

`inspect` returns `data:{icalendar,eventCount,events}`. `icalendar` is the original
Text, including CRLF, folding, timezone definitions and unknown properties.
Each event exposes `uid`, `summary`, `description`, `location`, `start`, `end`,
`recurrenceId`, `rrule`, `allDay` and `timezone`, with `startUTC`/`endUTC` only
when the backend can resolve an instant. `start`, `end` and `recurrenceId` retain
their iCalendar value spelling; use `allDay` and `timezone` rather than guessing
their type from a host locale. A recurrence override is a separate event entry,
not a new UID or a locally expanded occurrence.

Failures preserve raw `stdout`, `stderr` and `exitCode` whenever available;
prelaunch failures do not invent an exit code. Nonzero CLI responses retain
`error.code:"cli_error"`. Valid protocol-1 JSON diagnostics from stderr populate
`error.message` and `error.details:{code,message{,status}}`; malformed diagnostics
never cause a secondary parsing/type error or become success. Unknown diagnostic
fields are not copied. Callers must still treat raw server/calendar diagnostics
and data as sensitive.

Generic errors include `missing_executable`, `unsupported_platform`,
`unsupported_architecture`, `invalid_argument`, `launch_failed`, `process_error`,
`process_terminated` and `cli_error`. A successful process with malformed JSON,
wrong protocol/type, missing data or mismatched operation becomes
`invalid_protocol` while retaining its streams/status. Parsing uses a restored
local error handler, not a persistent host-global handler. Dynamic JSON-parser
diagnostics are not added to returned errors; raw stdout/stderr remain private
caller data as described above.

```4d
var $calendar : cs.CalDAV.Calendar
var $result; $event; $preview; $created : Object
var $target : Text
$target:="macOS-universal"
If (Is Windows)
  $target:="Windows-x64"
End if
$calendar:=cs.CalDAV.Calendar.new($target)
$event:=New object("uid"; Generate UUID+"@example.test"; "summary"; "Meeting 日本語"; "start"; "2026-11-06T09:00:00+09:00"; "end"; "2026-11-06T10:00:00+09:00")
$preview:=$calendar.build($event)
If ($preview.ok)
  $result:=$calendar.create(New object("url"; "https://calendar.example.test/dav/work/new-event.ics"; "username"; $username; "password"; $password; "event"; $event))
Else
  $result:=$preview
End if
If ($result.ok)
  $created:=$result.value.data
Else
  ALERT($result.error.code+": "+$result.error.message)
End if
```

Choose a new resource URL for each creation; the backend prevents overwriting an
existing resource. Keep the returned resource URL and strong ETag for a later
full-resource update/delete. Do not retry an uncertain create blindly with a
different UID: fetch/check the intended resource first.

An all-day preview uses date-only boundaries:

```4d
$preview:=$calendar.build(New object("uid"; Generate UUID+"@example.test"; "summary"; "Holiday"; "allDay"; True; "start"; "2026-11-06"; "end"; "2026-11-07"))
```

Calls close stdin and wait synchronously with a finite 30-second process timeout.
The CLI has a 25-second request deadline, a 2 MiB stdin request limit, an 8 MiB
response limit and a 4096-event resource limit. JSON encoding contributes to the
stdin size, so the calendar Text alone may not consume the whole request limit.
There is no shared session, persistent credential cache, asynchronous queue,
automatic account setup, PATH fallback or host-installed plug-in requirement.
The host fixtures inspect real calendar data offline; without a supplied account
they do not establish remote-server interoperability.
They exercise non-ASCII/multiline stdin, explicit empty stdin, ordinary no-input
`--version` calls and a synthetic folded calendar exceeding 128 KiB to detect
partial pipe writes. The large fixture keeps each unfolded property below the
parser's line bound and contains no credentials.

Underscore-prefixed helpers are implementation details by convention,
not access-control boundaries.

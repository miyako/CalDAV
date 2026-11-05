# CalDAV CLI protocol 1

The Go `caldav` executable is a separate process, not a 4D plug-in. It accepts
`--version` or `request` as its sole argument for the feature API. An offline
`path --value TEXT` diagnostic is also available for real host argument-quoting
tests. Never put credentials, account URLs, event text or request parameters in
argv; the diagnostic is for nonsensitive fixture path text only.

`--version` emits exactly `{"protocol":1,"version":"0.1.1"}` followed by LF,
with empty stderr and exit status 0.

`path --value TEXT` emits exactly `{"protocol":1,"path":TEXT}` plus LF,
where TEXT is JSON-encoded, with empty stderr and exit status 0. It accepts
empty/Unicode/quote/backslash/control-character text except embedded NUL or
malformed UTF-8. It performs no filesystem lookup, file read/write, network
access or path normalization. It proves literal native argv roundtrips only,
not any CalDAV operation.

## Requests and envelopes

`request` reads one UTF-8 JSON object from stdin, ending at EOF. The caller must
write the entire request and close stdin. Unknown/duplicate keys, null fields,
wrong scalar types, NUL, malformed UTF-8, trailing documents and input exceeding
2 MiB (2,097,152 bytes, including JSON encoding) fail before networking.

```json
{
  "protocol": 1,
  "operation": "query",
  "url": "https://calendar.example.test/calendars/user/work/",
  "username": "account",
  "password": "app-specific-password",
  "start": "2026-11-05T00:00:00Z",
  "end": "2026-11-06T00:00:00Z"
}
```

Only `protocol` and `operation` are common required fields. Operation-specific
fields are described below. `username` and `password` may both be omitted for
anonymous access, otherwise both must be nonempty; username cannot contain `:`.
Authentication is HTTP Basic over verified TLS. iCloud generally needs an
Apple Account app-specific password; this CLI does not implement account login,
OAuth, cookie persistence, credential storage, password discovery or MFA.

Success: stdout is exactly one object plus LF, stderr is empty, exit status 0:

```json
{"protocol":1,"operation":"inspect","data":{"icalendar":"...","eventCount":1,"events":[{"uid":"example","summary":"Meeting","description":"","location":"","start":"20261105T090000Z","end":"20261105T100000Z","recurrenceId":"","rrule":"","allDay":false,"timezone":"","startUTC":"2026-11-05T09:00:00Z","endUTC":"2026-11-05T10:00:00Z"}]}}
```

The `...` above abbreviates a complete VCALENDAR string, not a valid input
calendar. JSON property order is not part of the decoded contract.

Failure: stdout is empty; stderr is one JSON object plus LF:

```json
{"protocol":1,"error":{"code":"conflict","message":"server rejected request","status":412}}
```

Exit 2 means invalid request/calendar. Exit 1 means network, server, unsafe
reference/redirect or response failure. Diagnostics are constants; server
bodies, dynamic parser/transport messages, URLs, credentials and event text
are never interpolated into them. An HTTP `status` is present only when known.
There is no partial-success envelope; a failed item fails the entire query.
Success data intentionally contains calendar text and resource URLs: callers
must treat stdout as private data too.

## Operations

| Operation | Required operation fields | Successful `data` |
| --- | --- | --- |
| `inspect` | `icalendar` | Calendar summary below; strictly offline |
| `build` | `event` | Calendar summary containing a newly built basic event; strictly offline |
| `discover` | `url` | `{principalURL,homeURL,calendars:[{url,name,description,components:[]}]}` |
| `query` | Calendar collection `url`, `start`, `end` | `{resources:[Resource,...]}` |
| `get` | Calendar object `url` | `Resource` |
| `create` | New calendar object `url`, exactly one of `icalendar` or `event` | `{url,etag}`; structured input also returns `uid,icalendar` |
| `update` | Existing calendar object `url`, `icalendar`, `etag` | `{url,etag}` |
| `delete` | Existing calendar object `url`, `etag` | `{url,deleted:true}` |

`Resource` is `{url,etag,icalendar,eventCount,events}`: the same calendar summary
as `inspect`, with the resolved object URL and server ETag added.
Empty collections/resources are `[]`, never null. Missing display metadata
becomes empty strings or an empty components array. An absent PUT/GET ETag is
an empty string; a missing query ETag property is a failed response. ETags,
including their quotes, are returned verbatim. Missing/weak ETags cannot be
used for conditional writes; fetch a usable strong ETag before updating.

`inspect` rejects `event`, nonempty network/auth/ETag/time-range fields and
`allowInsecureLoopback:true`. Other operations reject nonempty irrelevant
`icalendar`, `etag`, `start` and `end` fields. `event` is accepted only by
`build` and `create`. `build` rejects raw calendar/network/auth/ETag/time-range
fields and `allowInsecureLoopback:true`. `url` must be absolute; object
operations reject paths ending in `/` to reduce accidental collection writes.
Callers must still supply an actual calendar-object URL, not a collection
alias without a trailing slash.

### Basic event builder

The offline builder gives 4D callers a small safe creation API:

```json
{"protocol":1,"operation":"build","event":{"summary":"Planning","start":"2026-11-05T09:00:00Z","end":"2026-11-05T10:00:00Z","location":"Office","description":"Discuss release"}}
```

`event` is a strict Object with only these fields:

| Field | Contract |
| --- | --- |
| `uid` | Optional nonempty identity; omitted/empty generates a cryptographic UUIDv4 URN |
| `summary` | Required nonempty TEXT |
| `start`, `end` | Required RFC3339 timestamps with `Z` or an explicit numeric offset, at whole-second precision |
| `allDay` | Optional Boolean, default false; true changes start/end to `YYYY-MM-DD` dates |
| `description`, `location` | Optional TEXT |
| `timezone` | Optional, either empty or exactly `UTC`; other values rejected |

End must be strictly after start, including for all-day events; all-day end
is exclusive. Timed input must identify an instant with `Z` or an explicit
offset, and is normalized to UTC DTSTART/DTEND. Civil all-day dates remain dates:
there are no guessed local-zone offsets, ambiguous/gap-time conversions,
server-local/floating defaults or generated VTIMEZONE definitions. To create
a deliberate named-zone/recurring event, supply complete raw iCalendar instead.

The builder creates one master VEVENT with VERSION:2.0, a CalDAV PRODID,
current UTC DTSTAMP, DTSTART/DTEND, UID, summary and supplied optional text.
TEXT is escaped and content lines are UTF-8-safely folded to at most 75 octets
with CRLF framing. It does not create organizers, attendees, alarms, scheduling
messages, recurrence rules or exceptions. Unknown/duplicate/case-mismatched
event keys and null/wrong-type fields fail with `invalid_request`.

The output uses the same summary schema as inspect, including the complete
generated `data.icalendar` and `data.events[0].uid`. A generated UID remains
the identity of that resource when this returned text is reused; separate
builds generate independent IDs, not deterministic IDs from content. UUID
randomness is cryptographic, not a mathematical uniqueness guarantee.
Supplying `event` to `create` invokes this same builder once before its
conditional PUT and additionally returns the generated `uid` and `icalendar`.
Do not rebuild with an omitted UID to retry an uncertain create; first refetch
and reconcile. Entropy failure is a redacted `generation_failed` exit-1 error.

### Discovery

The supplied URL is a known CalDAV endpoint, or an explicitly supplied
`https://HOST/.well-known/caldav` URL. Discovery performs PROPFIND Depth 0 for
`DAV:current-user-principal`, then PROPFIND Depth 0 on that principal for
`CALDAV:calendar-home-set`, then PROPFIND Depth 1 on that home for
`resourcetype`, display name, description and supported component set.
Only resources with the CalDAV calendar resource type are returned.
Relative/absolute hrefs are resolved against the actual response URL, subject
to the origin policy below. The first release requires one principal/home-set;
it does not guess account paths, follow DNS SRV records, search arbitrary
services, auto-probe a failed URL or support multiple home sets.

### Recurrence-aware query

`start` and `end` must be UTC RFC3339 timestamps ending in `Z`, at whole-second
precision, with `end > start`. The window is start-inclusive, end-exclusive.
The backend sends a Depth 1 CalDAV `calendar-query` REPORT with a nested
VCALENDAR/VEVENT `comp-filter` containing `time-range`, and requests complete
`calendar-data` plus `getetag`. It does **not** request `expand`, `limit-recurrence-set`,
property projection or a server transformation.

The CalDAV server evaluates recurrence/time-zone overlap per RFC 4791,
including RRULE/RDATE/EXDATE and detached RECURRENCE-ID exceptions; the CLI does
not pretend that comparing the master's DTSTART implements this operation.
Results are full original calendar **resources**, not generated occurrences.
One returned resource can contain a master and many overrides whose original
DTSTARTs are outside the query window. Summaries enumerate stored VEVENTs,
not the number of occurrences in the window. No local recurrence expansion,
occurrence sorting, sync-token incremental sync or pagination is implemented.
Results rely on server CalDAV compliance. XML decoding necessarily normalizes
XML line endings, but no iCalendar re-encoding or property filtering is done.

### Safe writes and lossless text

`create` sends PUT with `If-None-Match: *`; it does not overwrite existing data.
`update` and `delete` require exactly one strong quoted ETag and send
`If-Match: ETAG`. Wildcards, weak validators, lists and header control
characters are rejected. 409/412 are conflicts, never retried unconditionally.
No automatic request retry is used. A network failure can leave an uncertain
write outcome; callers must refetch and reconcile, not assume a rollback.

Updates replace the **entire resource**. Start from the latest `get` raw
`icalendar`, preserve every master, exception, VTIMEZONE, VALARM, organizer,
attendee and unknown property, edit deliberately, then supply the fetched ETag.
PUT sends the exact supplied UTF-8 calendar bytes; there is no decode/re-encode
step, server-side merge, field patch or silently synthesized timestamps/UIDs.
Exception-only resources are rejected: a complete master VEVENT must be
present. Structured `event` input is not accepted by update, and there is no
recurrence-ID mutation parameter. This prevents a convenience event update
from accidentally replacing a stored series. Deliberately removing exceptions
from a master-containing full resource is still a caller edit, not a merge.
Changing only a detached occurrence requires editing that VEVENT inside the
complete resource, preserving its RECURRENCE-ID and all siblings. No `patch`
operation is exposed in protocol 1.

### Calendar inspection and validation

The calendar summary is:

* `icalendar`: exact supplied input for `inspect` and PUT validation; exact
  received text for GET, or XML-decoded `calendar-data` for REPORT.
* `eventCount`: number of stored VEVENT components, including exceptions.
* `events`: input-order array. Each event has required `uid`, `summary`,
  `description`, `location`, `start`, `end`, `recurrenceId`, `rrule` strings;
  `allDay` Boolean; `timezone` string; optional `startUTC`, `endUTC` strings.
* Summary/description/location are unescaped TEXT. Start/end/recurrenceId/RRULE
  retain their unfolded iCalendar value syntax (not RFC3339). Missing optional
  text/end/recurrence/rule/timezone values become `""`.
* `allDay` means DTSTART has `VALUE=DATE`. DATE values have no UTC conversion;
  DTEND remains exclusive. Floating times also have no UTC conversion.
* UTC DATE-TIME yields RFC3339 UTC. Known IANA TZIDs without an overriding
  VTIMEZONE are resolved with Go's zone database when unambiguous. UTC fields
  are omitted for DST folds/gaps rather than guessing an occurrence; raw wall
  times remain intact for server interpretation. `time/tzdata` is embedded for
  systems without installed IANA data, including Windows. A supplied
  VTIMEZONE is preserved as authoritative but not interpreted locally, so its
  TZID produces no guessed UTC fields. Unknown TZIDs require a matching
  VTIMEZONE. No local instance expansion or custom VTIMEZONE evaluator exists.

Validation checks a single VCALENDAR with VERSION:2.0 and nonempty PRODID;
one or more VEVENTs sharing one nonempty UID, including exactly one master;
unique raw recurrence-ID values;
mandatory UTC DTSTAMP and DTSTART; singleton identity/time/summary fields;
valid date syntax, compatible start/end date types and increasing comparable
DTEND; positive duration mutually exclusive with DTEND; recurrence-rule
syntax; and balanced, bounded component structure. Scheduling METHOD and
top-level VTODO/VJOURNAL/VFREEBUSY components are outside this event-resource API.
This is not a full RFC 5545 conformance validator: preserved extension fields,
arbitrary VTIMEZONE definitions, recurrence list relationships and scheduling
semantics are not all semantically interpreted. Never infer that a successful
inspect proves an iCalendar server will accept it.

## Transport, credentials and resource limits

* HTTPS only, certificate and hostname verification enabled, TLS 1.2 minimum.
  No insecure-TLS option, shell, PATH access, external command, browser cookie
  jar, environment credential lookup or environmental proxy is used.
* Explicit `allowInsecureLoopback:true` permits HTTP only to literal loopback
  IPs (`127.0.0.1`, `[::1]`, etc.) for local fixtures. It does not permit
  `localhost`, remote HTTP, TLS downgrade, a different origin, or TLS bypass.
* Credentials/requests may move only within the original scheme/host/effective
  port. One exact exception: an initial `https://caldav.icloud.com:443`
  origin may move to `https://pNN-caldav.icloud.com:443` or
  `https://pNNN-caldav.icloud.com:443`, with **exactly two or three decimal
  digits** (`^p[0-9]{2,3}-caldav\.icloud\.com$`). The first contacted shard
  is pinned for that request.
  Arbitrary iCloud subdomains, suffix lookalikes, other ports, cross-shard
  pivots and generic suffix matching are not allowed. Starting directly at
  a shard permits only that original origin. The exception applies equally
  to redirect locations and discovered resource hrefs.
* URLs containing userinfo, query strings, fragments, controls, backslashes
  or trailing-dot hosts are rejected. No credential-bearing unsafe target is
  contacted merely to determine whether it is safe.
* GET/PROPFIND/REPORT retain their method/body through 301/302/307/308 redirects.
  PUT/DELETE reject **all** redirects, including 307/308: a server may have
  already applied the mutation, and the CLI never automatically replays it
  at another resource. Discover/refetch a canonical URL before a deliberate
  new conditional write. At most five redirects per read-only HTTP request;
  303 and other statuses are failures.
* Whole operation network context: **25 seconds**, shared across discovery
  requests and redirects. Connect/TLS handshake: 5 seconds; response-header
  wait: 10 seconds; response headers: 64 KiB. No retry loop.
* At most **8 MiB** (8,388,608 bytes) of successful response bodies in aggregate
  per operation, read through a bound. Compressed bodies are not requested or
  accepted. Error bodies are not read into diagnostics.
* At most **4,096 resources** in one multistatus and **4,096 events** per
  calendar/query aggregate; 100,000 XML elements/content lines, XML depth 24,
  iCalendar component depth 16, unfolded content line 64 KiB.
* Multistatus must be namespaced DAV XML with status 207. Per-resource errors,
  missing required properties, malformed calendar data and non-success property
  statuses fail the operation. Only 404 for optional collection metadata is
  tolerated. There are no silently dropped failures or partial query results.
* Calls are independent, synchronous subprocesses. No shared cache, persisted
  credentials, concurrency coordinator or automatic edits are used. ETags
  provide optimistic concurrency, subject to correct server enforcement.

## Stable errors

| Code | Constant message | Exit |
| --- | --- | --- |
| `invalid_request` | `invalid request` | 2 |
| `invalid_calendar` | `invalid iCalendar resource` | 2 |
| `generation_failed` | `event identity generation failed` | 1 |
| `network_error` | `request failed` | 1 |
| `unsafe_url` | `unsafe server URL` | 1 |
| `unsafe_redirect` | `redirect rejected` | 1 |
| `response_limit` | `server response limit exceeded` | 1 |
| `invalid_response` | `invalid server response` or `invalid calendar response` | 1 |
| `multistatus_error` | `server reported resource failure` or `server reported property failure` | 1 |
| `authentication_failed` | `server rejected request` (401/403) | 1 |
| `not_found` | `server rejected request` (404) | 1 |
| `conflict` | `server rejected request` (409/412) | 1 |
| `rate_limited` | `server rejected request` (429) | 1 |
| `server_error` | `server rejected request` (other non-2xx) | 1 |

## Dependencies and verification

Source and upstream MIT licenses were inspected directly at immutable revisions:

* `emersion/go-ical` commit
  `439c63cef608771fe1e93407b1b861e8c9a441ae`
  (`v0.0.0-20250609112844-439c63cef608`) is used for parsing/unescaping and
  recurrence syntax. It depends on `teambition/rrule-go v1.8.2`; both are pinned
  in go.mod/go.sum and both MIT notices ship in Resources/licenses. The parser
  has recursive decoding and can panic on truncated parameter input; a bounded
  structural precheck and narrowly scoped recovery convert this into a
  constant invalid-calendar failure.
* `emersion/go-webdav` commit
  `c16f8a9a132cc666c5fe387a3d7946d54f8e15aa` was evaluated but is **not linked**.
  Its CalDAV query decoder returns parsed objects rather than preserving the
  original calendar-data text; its generic href resolution retains endpoint
  authority rather than enforcing this origin policy, and upstream dynamic
  error diagnostics must not be surfaced here. A small stdlib HTTP/XML DAV
  implementation keeps the required raw-resource and redaction boundaries
  explicit. Its MIT code was not copied and no go-webdav notice is necessary.
* The existing Go BSD notice covers bundled Go runtime/standard-library code.
  IANA timezone data is public domain; the adjacent IANA notice records the
  reviewed terms. Embedded timezone data follows the actual Go toolchain,
  rather than an independently downloaded mutable database. tool4d is not
  bundled by this backend.

Run `cd cli && go test ./...` for offline tests using local HTTP/TLS servers:
principal/home/calendar discovery, scoped redirects and credentials, invalid
certificates, CRUD conditional headers/conflicts, multistatus failures, bounded
responses, timeout, recurrence/exception preservation, all-day/floating/IANA
times across DST, and strict JSON/parser handling. No live CalDAV or iCloud
endpoint is contacted by tests. Passing mock tests does not establish real
account/provider interoperability or any 4D source/compiled-host acceptance.

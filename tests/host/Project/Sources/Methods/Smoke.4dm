//%attributes = {"invisible":true}
var $ignored : Integer
var $i; $j : Integer
var $parameter; $name; $target; $icalendar; $summary; $line : Text
var $chunk; $largeProperties; $largeCalendar : Text
var $folderPath : Text
var $request; $result; $report; $payload; $resource : Object
var $tool : cs.CalDAV.Calendar
var $requestFile; $statusFile : 4D.File
var $renamedExecutable : 4D.File
var $cases; $protocolPayloads : Collection
var $invalid : Variant
var HOST_StatusFile : 4D.File
var HOST_Phase : Text
var HOST_PreviousHandler : Text
$ignored:=Get database parameter(User param value; $parameter)
If (Is Windows)
	$requestFile:=File($parameter; fk platform path)
Else
	$requestFile:=File($parameter; fk posix path)
End if
$request:=JSON Parse($requestFile.getText())
If (Is Windows)
	$statusFile:=File($request.status; fk platform path)
Else
	$statusFile:=File($request.status; fk posix path)
End if
$statusFile.setText("{\"success\":false,\"phase\":\"starting\"}")
HOST_StatusFile:=$statusFile
HOST_Phase:="constructor"
HOST_PreviousHandler:=Method called on error(ek local)
ON ERR CALL("Host_error"; ek local)
$target:="macOS-universal"
If (Is Windows)
	$target:="Windows-x64"
End if
$tool:=cs.CalDAV.Calendar.new($target)
$cases:=New collection("world"; "path with spaces"; "quote \" and \\ slash"; "line\nbreak"; "日本語"; "$() ; & % !")
$report:=New object("success"; True; "finished"; False; "cases"; New collection)
$report.compiled:=$tool._cli.compiled
HOST_Phase:="renamed executable"
If (Is Windows)
	$renamedExecutable:=File($request.executable; fk platform path)
Else
	$renamedExecutable:=File($request.executable; fk posix path)
End if
$report.renamedExecutable:=cs.CalDAV._Process.new().execute($renamedExecutable; New collection("--version"))
If (Not($report.renamedExecutable.ok))
	$report.success:=False
End if
HOST_Phase:="feature cases"
If ($request.expectCompiled#$report.compiled)
	$report.success:=False
End if
$report.calendarPreserved:=True
$report.literalArguments:=New collection
For each ($name; $cases)
	$result:=$tool._cli.execute(New collection("path"; "--value"; $name))
	$report.literalArguments.push($result)
	If (Not($result.ok))
		$report.success:=False
	Else
		$result.value:=JSON Parse($result.stdout)
		If ($result.value.path#$name)
			$report.success:=False
		End if
	End if
	$line:=Char(13)+Char(10)
	$summary:=Replace string($name; "\\"; "\\\\")
	$summary:=Replace string($summary; Char(10); "\\n")
	$summary:=Replace string($summary; ";"; "\\;")
	$summary:=Replace string($summary; ","; "\\,")
	$icalendar:="BEGIN:VCALENDAR"+$line+"VERSION:2.0"+$line+"PRODID:-//CalDAV host tests//EN"+$line
	$icalendar+="BEGIN:VTIMEZONE"+$line+"TZID:Asia/Tokyo"+$line+"BEGIN:STANDARD"+$line+"DTSTART:19700101T000000"+$line+"TZOFFSETFROM:+0900"+$line+"TZOFFSETTO:+0900"+$line+"END:STANDARD"+$line+"END:VTIMEZONE"+$line
	$icalendar+="BEGIN:VEVENT"+$line+"UID:host-stable@example.test"+$line+"DTSTAMP:20261101T000000Z"+$line+"DTSTART;VALUE=DATE:20261101"+$line+"DTEND;VALUE=DATE:20261102"+$line
	$icalendar+="RRULE:FREQ=DAILY;COUNT=3"+$line+"EXDATE;VALUE=DATE:20261102"+$line+"SUMMARY:"+$summary+$line+"X-HOST-PRESERVE:opaque-value"+$line+"END:VEVENT"+$line
	$icalendar+="BEGIN:VEVENT"+$line+"UID:host-stable@example.test"+$line+"DTSTAMP:20261101T000000Z"+$line+"RECURRENCE-ID;VALUE=DATE:20261103"+$line+"DTSTART;VALUE=DATE:20261104"+$line+"DTEND;VALUE=DATE:20261105"+$line+"SUMMARY:Exception 日本語"+$line+"END:VEVENT"+$line+"END:VCALENDAR"+$line
	$result:=$tool.inspect($icalendar)
	$report.cases.push($result)
	If (Not($result.ok))
		$report.success:=False
	Else
		If ($result.value.operation#"inspect")
			$report.success:=False
		End if
		If (Value type($result.value.data.icalendar)#Is text)
			$report.calendarPreserved:=False
			$report.success:=False
		Else
			If ((Length($result.value.data.icalendar)#Length($icalendar)) | (Position($icalendar; $result.value.data.icalendar; *)#1))
				$report.calendarPreserved:=False
				$report.success:=False
			End if
		End if
	End if
End for each
HOST_Phase:="large UTF-8 stdin"
$chunk:="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
$largeProperties:="DESCRIPTION:日本語 "
For ($i; 1; 768)
	$largeProperties+=$line+" "+$chunk
End for
$largeProperties+=$line
For ($i; 1; 3)
	$largeProperties+="X-HOST-PAYLOAD-"+String($i)+":"
	For ($j; 1; 512)
		$largeProperties+=$line+" "+$chunk
	End for
	$largeProperties+=$line
End for
$largeCalendar:="BEGIN:VCALENDAR"+$line+"VERSION:2.0"+$line+"PRODID:-//CalDAV large stdin test//EN"+$line
$largeCalendar+="BEGIN:VEVENT"+$line+"UID:host-large@example.test"+$line+"DTSTAMP:20261101T000000Z"+$line+"DTSTART:20261106T000000Z"+$line+"DTEND:20261106T010000Z"+$line+"SUMMARY:Large 日本語 calendar"+$line
$largeCalendar+=$largeProperties+"END:VEVENT"+$line+"END:VCALENDAR"+$line
$report.largeInputLength:=Length($largeCalendar)
$report.largeInputVerified:=($report.largeInputLength>=131072)
$report.largeInspection:=$tool.inspect($largeCalendar)
If (Not($report.largeInspection.ok))
	$report.success:=False
Else
	If (Value type($report.largeInspection.value.data.icalendar)#Is text)
		$report.largeInputVerified:=False
	Else
		If ((Length($report.largeInspection.value.data.icalendar)#Length($largeCalendar)) | (Position($largeCalendar; $report.largeInspection.value.data.icalendar; *)#1))
			$report.largeInputVerified:=False
		End if
	End if
End if
If (Not($report.largeInputVerified))
	$report.success:=False
End if
HOST_Phase:="empty stdin"
$report.emptyInput:=$tool._cli.execute(New collection("request"); "")
If ($report.emptyInput.ok | ($report.emptyInput.exitCode#2))
	$report.success:=False
End if
$result:=$tool.inspect("")
$report.invalid:=$result
If ($result.ok | ($result.exitCode#2))
	$report.success:=False
End if
HOST_Phase:="basic event builder"
$report.build:=$tool.build(New object("uid"; "host-built@example.test"; "summary"; "Meeting 日本語 \"quoted\""; "start"; "2026-11-06T09:00:00+09:00"; "end"; "2026-11-06T10:00:00+09:00"; "description"; "Line one\nLine two"; "location"; "Tokyo, Japan"))
If (Not($report.build.ok))
	$report.success:=False
Else
	If (Value type($report.build.value.data.icalendar)#Is text)
		$report.success:=False
	Else
		$report.builtInspection:=$tool.inspect($report.build.value.data.icalendar)
		If (Not($report.builtInspection.ok))
			$report.success:=False
		End if
	End if
End if
$report.buildAllDay:=$tool.build(New object("uid"; "host-all-day@example.test"; "summary"; "Holiday"; "allDay"; True; "start"; "2026-11-06"; "end"; "2026-11-07"))
If (Not($report.buildAllDay.ok))
	$report.success:=False
Else
	If (Value type($report.buildAllDay.value.data.icalendar)#Is text)
		$report.success:=False
	Else
		$report.builtAllDayInspection:=$tool.inspect($report.buildAllDay.value.data.icalendar)
		If (Not($report.builtAllDayInspection.ok))
			$report.success:=False
		End if
	End if
End if
$report.nullEvent:=$tool.build(Null)
$report.ambiguousEvent:=$tool.build(New object("uid"; "host-invalid@example.test"; "summary"; "Ambiguous"; "start"; "2026-11-06T09:00:00"; "end"; "2026-11-06T10:00:00"))
If ($report.ambiguousEvent.ok | ($report.ambiguousEvent.exitCode#2))
	$report.success:=False
End if
$result:=$tool.version()
$report.version:=$result
If (Not($result.ok))
	$report.success:=False
Else
	If ($result.value.version#"0.1.2")
		$report.success:=False
	End if
End if
HOST_Phase:="missing executable"
$result:=cs.CalDAV._CLI.new("missing-cli"; $target).execute(New collection("--version"))
$report.missing:=$result
If ($result.error.code#"missing_executable")
	$report.success:=False
End if
$result:=cs.CalDAV.Calendar.new("unsupported-arm-target").version()
$report.unsupported:=$result
If ($result.error.code#"unsupported_architecture")
	$report.success:=False
End if
HOST_Phase:="launch failure"
$result:=cs.CalDAV._Process.new().execute($requestFile; New collection)
$report.launch:=$result
If ($result.error.code#"launch_failed")
	$report.success:=False
End if
HOST_Phase:="malformed protocol"
$result:=$tool._decode(New object("ok"; True; "stdout"; "not json"; "stderr"; ""; "exitCode"; 0); "inspect")
$report.protocol:=$result
If ($result.error.code#"invalid_protocol")
	$report.success:=False
End if
$report.protocolTypes:=New collection
For each ($name; New collection("{\"protocol\":true,\"message\":\"invalid\"}"; "{\"protocol\":false,\"message\":\"invalid\"}"))
	$result:=$tool._decode(New object("ok"; True; "stdout"; $name; "stderr"; ""; "exitCode"; 0); "inspect")
	$report.protocolTypes.push($result)
	If ($result.ok | ($result.error.code#"invalid_protocol"))
		$report.success:=False
	End if
End for each
$result:=$tool._decode(New object("ok"; True; "stdout"; "{\"protocol\":null,\"message\":\"invalid\"}"; "stderr"; ""; "exitCode"; 0); "inspect")
$report.protocolNull:=$result
If ($result.ok | ($result.error.code#"invalid_protocol"))
	$report.success:=False
End if
$report.protocolScalars:=New collection
For each ($name; New collection("true"; "false"; "null"; "1"; "[]"))
	$result:=$tool._decode(New object("ok"; True; "stdout"; $name; "stderr"; ""; "exitCode"; 0); "inspect")
	$report.protocolScalars.push($result)
	If ($result.ok | ($result.error.code#"invalid_protocol"))
		$report.success:=False
	End if
End for each
$report.protocolShapes:=New collection
For each ($name; New collection("{\"protocol\":2,\"operation\":\"inspect\",\"data\":{}}"; "{\"protocol\":1,\"operation\":\"query\",\"data\":{}}"; "{\"protocol\":1,\"operation\":\"inspect\",\"data\":null}"; "{\"protocol\":1,\"operation\":true,\"data\":{}}"; "{\"protocol\":1,\"operation\":\"inspect\",\"data\":[]}"; "{\"protocol\":1,\"version\":true}"))
	$result:=$tool._decode(New object("ok"; True; "stdout"; $name; "stderr"; ""; "exitCode"; 0); "inspect")
	$report.protocolShapes.push($result)
	If ($result.ok | ($result.error.code#"invalid_protocol"))
		$report.success:=False
	End if
End for each
$report.protocolData:=New collection
For each ($name; New collection("inspect"; "build"; "discover"; "query"; "get"; "create"; "update"; "delete"))
	$payload:=New object("protocol"; 1; "operation"; $name; "data"; New object)
	$result:=$tool._decode(New object("ok"; True; "stdout"; JSON Stringify($payload); "stderr"; ""; "exitCode"; 0); $name)
	$report.protocolData.push($result)
	If ($result.ok | ($result.error.code#"invalid_protocol"))
		$report.success:=False
	End if
End for each
$protocolPayloads:=New collection
$protocolPayloads.push(New object("operation"; "inspect"; "data"; New object("icalendar"; "x"; "eventCount"; True; "events"; New collection)))
$protocolPayloads.push(New object("operation"; "inspect"; "data"; New object("icalendar"; "x"; "eventCount"; 1; "events"; True)))
$protocolPayloads.push(New object("operation"; "inspect"; "data"; New object("icalendar"; "x"; "eventCount"; 2; "events"; New collection)))
$protocolPayloads.push(New object("operation"; "inspect"; "data"; New object("icalendar"; "x"; "eventCount"; 1; "events"; New collection(True))))
$protocolPayloads.push(New object("operation"; "inspect"; "data"; New object("icalendar"; "x"; "eventCount"; 1; "events"; New collection(New object))))
$protocolPayloads.push(New object("operation"; "discover"; "data"; New object("principalURL"; True; "homeURL"; "h"; "calendars"; New collection)))
$protocolPayloads.push(New object("operation"; "discover"; "data"; New object("principalURL"; "p"; "homeURL"; "h"; "calendars"; New collection(True))))
$protocolPayloads.push(New object("operation"; "discover"; "data"; New object("principalURL"; "p"; "homeURL"; "h"; "calendars"; New collection(New object("url"; "u"; "name"; ""; "description"; ""; "components"; New collection(True))))))
$protocolPayloads.push(New object("operation"; "query"; "data"; New object("resources"; New object)))
$protocolPayloads.push(New object("operation"; "query"; "data"; New object("resources"; New collection(True))))
$protocolPayloads.push(New object("operation"; "query"; "data"; New object("resources"; New collection(New object("url"; "u"; "etag"; "")))))
$protocolPayloads.push(New object("operation"; "get"; "data"; New object("url"; True; "etag"; ""; "icalendar"; "x")))
$protocolPayloads.push(New object("operation"; "create"; "data"; New object("url"; "u"; "etag"; True)))
$protocolPayloads.push(New object("operation"; "update"; "data"; New object("url"; ""; "etag"; "")))
$protocolPayloads.push(New object("operation"; "delete"; "data"; New object("url"; "u"; "deleted"; 1)))
$protocolPayloads.push(New object("operation"; "delete"; "data"; New object("url"; "u"; "deleted"; False)))
$protocolPayloads.push(New object("operation"; "build"; "data"; New object("icalendar"; "x"; "eventCount"; 1; "events"; Null)))
For each ($payload; $protocolPayloads)
	$payload.protocol:=1
	$result:=$tool._decode(New object("ok"; True; "stdout"; JSON Stringify($payload); "stderr"; ""; "exitCode"; 0); $payload.operation)
	$report.protocolData.push($result)
	If ($result.ok | ($result.error.code#"invalid_protocol"))
		$report.success:=False
	End if
End for each
$report.protocolAccepted:=New collection
If ($report.cases[0].ok)
	$resource:=OB Copy($report.cases[0].value.data)
	$resource.url:="https://calendar.example.test/event.ics"
	$resource.etag:=""
	$protocolPayloads:=New collection
	$protocolPayloads.push(New object("operation"; "get"; "data"; $resource))
	$protocolPayloads.push(New object("operation"; "query"; "data"; New object("resources"; New collection($resource))))
	$protocolPayloads.push(New object("operation"; "query"; "data"; New object("resources"; New collection)))
	$protocolPayloads.push(New object("operation"; "discover"; "data"; New object("principalURL"; "https://calendar.example.test/principal/"; "homeURL"; "https://calendar.example.test/home/"; "calendars"; New collection(New object("url"; "https://calendar.example.test/calendar/"; "name"; ""; "description"; ""; "components"; New collection("VEVENT"))))))
	$protocolPayloads.push(New object("operation"; "discover"; "data"; New object("principalURL"; "https://calendar.example.test/principal/"; "homeURL"; "https://calendar.example.test/home/"; "calendars"; New collection)))
	$protocolPayloads.push(New object("operation"; "create"; "data"; New object("url"; $resource.url; "etag"; "")))
	$protocolPayloads.push(New object("operation"; "update"; "data"; New object("url"; $resource.url; "etag"; "\"etag-1\"")))
	$protocolPayloads.push(New object("operation"; "delete"; "data"; New object("url"; $resource.url; "deleted"; True)))
	For each ($payload; $protocolPayloads)
		$payload.protocol:=1
		$result:=$tool._decode(New object("ok"; True; "stdout"; JSON Stringify($payload); "stderr"; ""; "exitCode"; 0); $payload.operation)
		$report.protocolAccepted.push($result)
		If (Not($result.ok))
			$report.success:=False
		End if
	End for each
End if
$report.invalidVersion:=$tool._decode(New object("ok"; True; "stdout"; "{\"protocol\":1,\"version\":true}"; "stderr"; ""; "exitCode"; 0); "version")
$report.structuredError:=$tool._decode(New object("ok"; False; "stdout"; ""; "stderr"; "{\"protocol\":1,\"error\":{\"code\":\"precondition_failed\",\"message\":\"Event changed\",\"status\":412}}"; "exitCode"; 1; "error"; New object("code"; "cli_error"; "message"; "raw diagnostic")); "update")
$report.malformedErrors:=New collection
For each ($name; New collection("not json"; "[]"; "{\"protocol\":true,\"error\":{}}"; "{\"protocol\":1,\"error\":true}"; "{\"protocol\":1,\"error\":{\"code\":1,\"message\":true}}"))
	$result:=$tool._decode(New object("ok"; False; "stdout"; ""; "stderr"; $name; "exitCode"; 1; "error"; New object("code"; "cli_error"; "message"; "raw diagnostic")); "inspect")
	$report.malformedErrors.push($result)
	If ($result.ok | ($result.error.code#"cli_error"))
		$report.success:=False
	End if
End for each
$report.nullRequest:=$tool.execute(Null)
$report.nullWrapper:=$tool.discover(Null)
$report.invalidOperation:=$tool.execute(New object("operation"; "unknown"))
HOST_Phase:="request copy"
$result:=New object("operation"; "inspect"; "icalendar"; $icalendar)
$report.copiedRequest:=$tool.execute($result)
$report.requestUnchanged:=($result.operation="inspect") & (Value type($result.protocol)=Is undefined)
If (Not($report.copiedRequest.ok))
	$report.success:=False
End if
HOST_Phase:="File argument"
$result:=cs.CalDAV._CLI.new("caldav"; $target).execute(New collection("path"; "--value"; $requestFile))
$report.fileArgument:=$result
If (Not($result.ok))
	$report.success:=False
Else
	If (JSON Parse($result.stdout).path#$parameter)
		$report.success:=False
	End if
End if
$report.rejectedArguments:=New collection
HOST_Phase:="scalar arguments"
For each ($invalid; New collection(123; 1.25; True; False; Null; Current date; Current time; New collection; New object))
	$result:=cs.CalDAV._Process.new().execute($requestFile; New collection($invalid))
	$report.rejectedArguments.push(New object("type"; Value type($invalid); "result"; $result))
	If ($result.ok | ($result.error.code#"invalid_argument"))
		$report.success:=False
	End if
End for each
$report.rejectedNul:=New collection
HOST_Phase:="NUL arguments"
For each ($name; New collection(Char(0)+"prefix"; "middle"+Char(0)+"suffix"; "trailing"+Char(0)))
	$result:=cs.CalDAV._Process.new().execute($requestFile; New collection($name))
	$report.rejectedNul.push($result)
	If ($result.ok | ($result.error.code#"invalid_argument"))
		$report.success:=False
	End if
End for each
$report.missingFile:=cs.CalDAV._Process.new().execute(Null; New collection)
$report.missingArguments:=cs.CalDAV._Process.new().execute($requestFile; Null)
If ($report.missingFile.ok | ($report.missingFile.error.code#"invalid_argument"))
	$report.success:=False
End if
If ($report.missingArguments.ok | ($report.missingArguments.error.code#"invalid_argument"))
	$report.success:=False
End if
HOST_Phase:="Folder argument"
$result:=cs.CalDAV._CLI.new("caldav"; $target).execute(New collection("path"; "--value"; $requestFile.parent))
$report.folderArgument:=$result
$folderPath:=$requestFile.parent.path
If (Is Windows)
	$folderPath:=$requestFile.parent.platformPath
End if
If (Not($result.ok))
	$report.success:=False
Else
	If (JSON Parse($result.stdout).path#$folderPath)
		$report.success:=False
	End if
End if
$report.finished:=True
ON ERR CALL(HOST_PreviousHandler; ek local)
$statusFile.setText(JSON Stringify($report))
HOST_StatusFile:=Null
HOST_Phase:=""
HOST_PreviousHandler:=""
QUIT 4D

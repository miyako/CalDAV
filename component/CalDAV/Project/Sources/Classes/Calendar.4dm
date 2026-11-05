property _cli : cs._CLI

Class constructor($target : Text)
	This._cli:=cs._CLI.new("caldav"; $target)

Function execute($request : Object)->$result : Object
	var $input : Object
	If ($request=Null)
		return New object("ok"; False; "error"; New object("code"; "invalid_argument"; "message"; "A request Object is required"))
	End if
	If (Value type($request.operation)#Is text)
		return New object("ok"; False; "error"; New object("code"; "invalid_argument"; "message"; "A supported operation is required"))
	End if
	If (New collection("build"; "inspect"; "discover"; "query"; "get"; "create"; "update"; "delete").indexOf($request.operation)<0)
		return New object("ok"; False; "error"; New object("code"; "invalid_argument"; "message"; "A supported operation is required"))
	End if
	$input:=OB Copy($request)
	$input.protocol:=1
	$result:=This._decode(This._cli.execute(New collection("request"); JSON Stringify($input)); $input.operation)

Function inspect($icalendar : Text)->$result : Object
	$result:=This.execute(New object("operation"; "inspect"; "icalendar"; $icalendar))

Function build($event : Object)->$result : Object
	If ($event=Null)
		return New object("ok"; False; "error"; New object("code"; "invalid_argument"; "message"; "An event Object is required"))
	End if
	$result:=This.execute(New object("operation"; "build"; "event"; $event))

Function discover($request : Object)->$result : Object
	$result:=This._operation("discover"; $request)

Function query($request : Object)->$result : Object
	$result:=This._operation("query"; $request)

Function get($request : Object)->$result : Object
	$result:=This._operation("get"; $request)

Function create($request : Object)->$result : Object
	$result:=This._operation("create"; $request)

Function update($request : Object)->$result : Object
	$result:=This._operation("update"; $request)

Function delete($request : Object)->$result : Object
	$result:=This._operation("delete"; $request)

Function version()->$result : Object
	$result:=This._decode(This._cli.execute(New collection("--version")); "version")

Function _operation($operation : Text; $request : Object)->$result : Object
	var $input : Object
	If ($request=Null)
		return New object("ok"; False; "error"; New object("code"; "invalid_argument"; "message"; "A request Object is required"))
	End if
	$input:=OB Copy($request)
	$input.operation:=$operation
	$result:=This.execute($input)

Function _decode($result : Object; $operation : Text)->$decoded : Object
	var $payload : Variant
	var $previous; $stream : Text
	var $parseErrors : Collection
	var CLI_LastErrors : Collection
	If (Not($result.ok))
		If ($result.error.code#"cli_error")
			return $result
		End if
		$stream:=$result.stderr
	Else
		$stream:=$result.stdout
	End if
	$previous:=Method called on error(ek local)
	CLI_LastErrors:=Null
	ON ERR CALL("_CaptureError"; ek local)
	$payload:=JSON Parse($stream)
	ON ERR CALL($previous; ek local)
	$parseErrors:=CLI_LastErrors
	CLI_LastErrors:=Null
	If (($parseErrors#Null) | (Value type($payload)#Is object))
		If (Not($result.ok))
			return $result
		End if
		$result.ok:=False
		$result.error:=New object("code"; "invalid_protocol"; "message"; "CLI response must be a JSON object")
		return $result
	End if
	If ((Value type($payload.protocol)#Is real) & (Value type($payload.protocol)#Is longint))
		If (Not($result.ok))
			return $result
		End if
		$result.ok:=False
		$result.error:=New object("code"; "invalid_protocol"; "message"; "Unsupported CLI protocol version")
		return $result
	End if
	If ($payload.protocol#1)
		If (Not($result.ok))
			return $result
		End if
		$result.ok:=False
		$result.error:=New object("code"; "invalid_protocol"; "message"; "Unsupported CLI protocol version")
		return $result
	End if
	If (Not($result.ok))
		If (Value type($payload.error)#Is object)
			return $result
		End if
		If ((Value type($payload.error.code)#Is text) | (Value type($payload.error.message)#Is text))
			return $result
		End if
		$result.error.message:=$payload.error.message
		$result.error.details:=New object("code"; $payload.error.code; "message"; $payload.error.message)
		If ((Value type($payload.error.status)=Is real) | (Value type($payload.error.status)=Is longint))
			$result.error.details.status:=$payload.error.status
		End if
		return $result
	End if
	If ($operation="version")
		If (Not(This._text($payload.version; True)))
			$result.ok:=False
		End if
	Else
		If ((Value type($payload.operation)#Is text) | (Value type($payload.data)#Is object))
			$result.ok:=False
		Else
			If ($payload.operation#$operation)
				$result.ok:=False
			Else
				If (Not(This._validData($operation; $payload.data)))
					$result.ok:=False
				End if
			End if
		End if
	End if
	If (Not($result.ok))
		$result.error:=New object("code"; "invalid_protocol"; "message"; "CLI response does not match the requested operation")
		return $result
	End if
	$result.value:=$payload
	$decoded:=$result

Function _text($value : Variant; $nonempty : Boolean)->$valid : Boolean
	If (Value type($value)#Is text)
		return False
	End if
	If ($nonempty)
		return Length($value)>0
	End if
	$valid:=True

Function _validCalendar($data : Object)->$valid : Boolean
	var $event : Variant
	var $field : Text
	If (Not(This._text($data.icalendar; True)))
		return False
	End if
	If ((Value type($data.eventCount)#Is real) & (Value type($data.eventCount)#Is longint))
		return False
	End if
	If (Value type($data.events)#Is collection)
		return False
	End if
	If (($data.events.length=0) | ($data.eventCount#$data.events.length))
		return False
	End if
	For each ($event; $data.events)
		If (Value type($event)#Is object)
			return False
		End if
		If (Not(This._text($event.uid; True)))
			return False
		End if
		If (Not(This._text($event.start; True)))
			return False
		End if
		For each ($field; New collection("summary"; "description"; "location"; "end"; "recurrenceId"; "rrule"; "timezone"))
			If (Not(This._text($event[$field]; False)))
				return False
			End if
		End for each
		If (Value type($event.allDay)#Is Boolean)
			return False
		End if
		For each ($field; New collection("startUTC"; "endUTC"))
			If (Value type($event[$field])#Is undefined)
				If (Not(This._text($event[$field]; True)))
					return False
				End if
			End if
		End for each
	End for each
	$valid:=True

Function _validData($operation : Text; $data : Object)->$valid : Boolean
	var $item; $component : Variant
	Case of
		: (($operation="inspect") | ($operation="build"))
			return This._validCalendar($data)
		: (($operation="get") | ($operation="create") | ($operation="update"))
			If (Not(This._text($data.url; True)))
				return False
			End if
			If (Not(This._text($data.etag; False)))
				return False
			End if
			If ($operation="get")
				return This._validCalendar($data)
			End if
		: ($operation="delete")
			If (Not(This._text($data.url; True)))
				return False
			End if
			If (Value type($data.deleted)#Is Boolean)
				return False
			End if
			return $data.deleted
		: ($operation="discover")
			If (Not(This._text($data.principalURL; True)))
				return False
			End if
			If (Not(This._text($data.homeURL; True)))
				return False
			End if
			If (Value type($data.calendars)#Is collection)
				return False
			End if
			For each ($item; $data.calendars)
				If (Value type($item)#Is object)
					return False
				End if
				If (Not(This._text($item.url; True)))
					return False
				End if
				If ((Not(This._text($item.name; False))) | (Not(This._text($item.description; False))))
					return False
				End if
				If (Value type($item.components)#Is collection)
					return False
				End if
				For each ($component; $item.components)
					If (Not(This._text($component; False)))
						return False
					End if
				End for each
			End for each
		: ($operation="query")
			If (Value type($data.resources)#Is collection)
				return False
			End if
			For each ($item; $data.resources)
				If (Value type($item)#Is object)
					return False
				End if
				If (Not(This._validData("get"; $item)))
					return False
				End if
			End for each
		Else
			return False
	End case
	$valid:=True

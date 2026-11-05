package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func basicEvent() *eventInput {
	return &eventInput{Summary: "Meeting", Start: "2026-11-05T09:00:00Z", End: "2026-11-05T10:00:00Z"}
}

func TestBuildEvent(t *testing.T) {
	e := basicEvent()
	e.Summary = "世界, semicolon; backslash\\ " + strings.Repeat("会議", 25)
	e.Description = "line1\r\nline2\nline3"
	e.Location = "Office, room; 1"
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	s, f := buildCalendarWith(e, now, bytes.NewReader(make([]byte, 16)))
	if f != nil {
		t.Fatal(f)
	}
	uid := "urn:uuid:00000000-0000-4000-8000-000000000000"
	event := s.Events[0]
	if event.UID != uid || event.Summary != e.Summary || event.Description != "line1\nline2\nline3" || event.Location != e.Location || event.StartUTC != e.Start || event.EndUTC != e.End {
		t.Fatal(event)
	}
	if !strings.Contains(s.ICalendar, "DTSTAMP:20261105T000000Z\r\n") || !strings.Contains(s.ICalendar, "\r\n ") {
		t.Fatal(s.ICalendar)
	}
	for _, line := range strings.Split(s.ICalendar, "\r\n") {
		if len(line) > 75 || !utf8.ValidString(line) {
			t.Fatal("bad folding", line)
		}
	}
	e.UID = "stable-calendar-identity"
	s, f = buildCalendarWith(e, now, bytes.NewReader(nil))
	if f != nil || s.Events[0].UID != e.UID {
		t.Fatal(s, f)
	}
}

func TestBuildOfflineProtocolAndUID(t *testing.T) {
	code, out, err := call(t, request{Protocol: 1, Operation: "build", Event: basicEvent()})
	if code != 0 || err != "" {
		t.Fatal(code, out, err)
	}
	var envelope struct {
		Protocol  int
		Operation string
		Data      calendarSummary
	}
	if json.Unmarshal([]byte(out), &envelope) != nil || envelope.Protocol != 1 || envelope.Operation != "build" || envelope.Data.EventCount != 1 {
		t.Fatal(out)
	}
	uid := envelope.Data.Events[0].UID
	if !regexp.MustCompile(`^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal(uid)
	}
	if !strings.Contains(envelope.Data.ICalendar, "UID:"+uid+"\r\n") {
		t.Fatal("generated identity not retained")
	}
	e := basicEvent()
	e.AllDay = true
	e.Start = "2026-03-08"
	e.End = "2026-03-09"
	e.Timezone = "UTC"
	code, out, err = call(t, request{Protocol: 1, Operation: "build", Event: e})
	envelope = struct {
		Protocol  int
		Operation string
		Data      calendarSummary
	}{}
	json.Unmarshal([]byte(out), &envelope)
	if code != 0 || err != "" || !envelope.Data.Events[0].AllDay || envelope.Data.Events[0].Start != "20260308" || envelope.Data.Events[0].End != "20260309" || envelope.Data.Events[0].StartUTC != "" {
		t.Fatal(code, out, err)
	}
}

func TestBuildExplicitOffsetsNormalizeUTC(t *testing.T) {
	e := basicEvent()
	e.Start = "2026-11-05T09:00:00+09:00"
	e.End = "2026-11-05T10:00:00+09:00"
	s, f := buildCalendar(e)
	if f != nil || s.Events[0].Start != "20261105T000000Z" || s.Events[0].End != "20261105T010000Z" || s.Events[0].StartUTC != "2026-11-05T00:00:00Z" || s.Events[0].EndUTC != "2026-11-05T01:00:00Z" {
		t.Fatal(s, f)
	}
	e.Start = "2026-11-05T09:00:00-05:00"
	e.End = "2026-11-05T10:00:00-05:00"
	s, f = buildCalendar(e)
	if f != nil || s.Events[0].StartUTC != "2026-11-05T14:00:00Z" || s.Events[0].EndUTC != "2026-11-05T15:00:00Z" {
		t.Fatal(s, f)
	}
}

func TestBuildRejectsUnsafeInputAndEntropyFailure(t *testing.T) {
	for name, modify := range map[string]func(*eventInput){
		"missing summary":      func(e *eventInput) { e.Summary = "" },
		"missing start":        func(e *eventInput) { e.Start = "" },
		"end equal start":      func(e *eventInput) { e.End = e.Start },
		"fraction":             func(e *eventInput) { e.Start = "2026-11-05T09:00:00.1Z" },
		"floating input":       func(e *eventInput) { e.Start = "2026-11-05T09:00:00" },
		"floating gap":         func(e *eventInput) { e.Start = "2026-03-08T02:30:00"; e.Timezone = "America/New_York" },
		"floating fold":        func(e *eventInput) { e.Start = "2026-11-01T01:30:00"; e.Timezone = "America/New_York" },
		"timezone unsupported": func(e *eventInput) { e.Timezone = "Asia/Tokyo" },
		"invalid date":         func(e *eventInput) { e.AllDay = true; e.Start = "2026-02-30"; e.End = "2026-03-01" },
		"UID injection":        func(e *eventInput) { e.UID = "one\r\nATTENDEE:bad" },
		"NUL":                  func(e *eventInput) { e.Description = "one\x00two" },
	} {
		t.Run(name, func(t *testing.T) {
			e := basicEvent()
			modify(e)
			if _, f := buildCalendar(e); f == nil || f.exit != 2 {
				t.Fatal(f)
			}
		})
	}
	if _, f := buildCalendarWith(basicEvent(), time.Now(), bytes.NewReader(nil)); f == nil || f.Code != "generation_failed" || f.exit != 1 {
		t.Fatal(f)
	}
	for _, raw := range []string{
		`{"protocol":1,"operation":"build","event":null}`,
		`{"protocol":1,"operation":"build","event":{"summary":null}}`,
		`{"protocol":1,"operation":"build","event":{"Summary":"bad"}}`,
		`{"protocol":1,"operation":"build","event":{"summary":"one","summary":"two"}}`,
		`{"protocol":1,"operation":"build","event":{"summary":"one","recurrenceId":"20260101"}}`,
		`{"protocol":1,"operation":"build","event":{"summary":"one","allDay":"true"}}`,
	} {
		var out, diagnostic bytes.Buffer
		if code := run([]string{"request"}, strings.NewReader(raw), &out, &diagnostic); code != 2 || out.Len() != 0 {
			t.Fatal(code, out.String(), diagnostic.String())
		}
	}
}

func TestCreateStructuredEventAndRejectOccurrenceMutation(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "PUT" || r.Header.Get("If-None-Match") != "*" {
			t.Error("wrong conditional create")
		}
		body, _ := io.ReadAll(r.Body)
		cal, f := parseCalendar(string(body))
		if f != nil || cal.EventCount != 1 || cal.Events[0].UID != "persistent-id" || cal.Events[0].Summary != "Meeting" {
			t.Error(cal, f)
		}
		w.Header().Set("ETag", `"created"`)
		w.WriteHeader(201)
	}))
	defer s.Close()
	e := basicEvent()
	e.UID = "persistent-id"
	r := mockRequest(s, "create")
	r.Event = e
	code, out, err := call(t, r)
	if code != 0 || err != "" || calls != 1 || !strings.Contains(out, `"etag":"\"created\""`) {
		t.Fatal(code, out, err)
	}
	r.ICalendar = sample
	if code, _, _ := call(t, r); code != 2 || calls != 1 {
		t.Fatal("ambiguous create input accepted")
	}
	r.ICalendar = ""
	r.Operation = "update"
	r.ETag = `"rev"`
	if code, _, _ := call(t, r); code != 2 || calls != 1 {
		t.Fatal("structured update could replace series")
	}
	exceptionOnly := strings.Replace(sample, "UID:test-1", "UID:test-1\r\nRECURRENCE-ID:20261105T090000Z", 1)
	r.Event = nil
	r.ICalendar = exceptionOnly
	if code, _, _ := call(t, r); code != 2 || calls != 1 {
		t.Fatal("exception-only replacement accepted")
	}
}

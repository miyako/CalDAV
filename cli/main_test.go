package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const sample = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalDAV Tests//EN\r\nBEGIN:VEVENT\r\nUID:test-1\r\nDTSTAMP:20261105T000000Z\r\nDTSTART:20261105T090000Z\r\nDTEND:20261105T100000Z\r\nSUMMARY:Hello 世界\r\nX-UNKNOWN;X-PARAM=\"a:b\":keep me\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

const recurrence = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalDAV Tests//EN\r\nBEGIN:VTIMEZONE\r\nTZID:Custom/Office\r\nX-UNKNOWN:untouched\r\nBEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0900\r\nTZOFFSETTO:+0900\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\nBEGIN:VEVENT\r\nUID:series\r\nDTSTAMP:20261105T000000Z\r\nDTSTART;TZID=Custom/Office:20261101T090000\r\nDTEND;TZID=Custom/Office:20261101T100000\r\nRRULE:FREQ=DAILY;COUNT=10\r\nEXDATE;TZID=Custom/Office:20261103T090000\r\nSUMMARY:Master\r\nX-MASTER:preserve\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:series\r\nDTSTAMP:20261105T000000Z\r\nRECURRENCE-ID;TZID=Custom/Office:20261105T090000\r\nDTSTART;TZID=Custom/Office:20261105T110000\r\nDTEND;TZID=Custom/Office:20261105T120000\r\nSUMMARY:Exception\r\nDESCRIPTION:line one\\nline two\r\nX-EXCEPTION:preserve\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func call(t *testing.T, r request) (int, string, string) {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	code := run([]string{"request"}, bytes.NewReader(raw), &out, &diagnostic)
	return code, out.String(), diagnostic.String()
}

func TestVersionAndInspect(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"--version"}, strings.NewReader(""), &out, &err); code != 0 || out.String() != "{\"protocol\":1,\"version\":\"0.1.0\"}\n" || err.Len() != 0 {
		t.Fatal(code, out.String(), err.String())
	}
	code, s, diag := call(t, request{Protocol: 1, Operation: "inspect", ICalendar: sample})
	if code != 0 || diag != "" {
		t.Fatal(code, diag)
	}
	var envelope struct {
		Protocol  int
		Operation string
		Data      calendarSummary
	}
	if json.Unmarshal([]byte(s), &envelope) != nil || envelope.Protocol != 1 || envelope.Operation != "inspect" {
		t.Fatal(s)
	}
	data := envelope.Data
	if data.ICalendar != sample || data.EventCount != 1 || len(data.Events) != 1 {
		t.Fatal(s)
	}
	e := data.Events[0]
	if e.UID != "test-1" || e.Summary != "Hello 世界" || e.StartUTC != "2026-11-05T09:00:00Z" || e.EndUTC != "2026-11-05T10:00:00Z" || e.AllDay {
		t.Fatal(e)
	}
}

func TestOfflinePathRoundTrip(t *testing.T) {
	for _, value := range []string{"", "/a/世界 café 'quote' \"double\"\\trailing\\", "C:\\Unicode 世界\\Folder\\", "one\nline\tliteral", "$(not-a-command);&|<>"} {
		var out, diagnostic bytes.Buffer
		status := run([]string{"path", "--value", value}, strings.NewReader(""), &out, &diagnostic)
		var decoded struct {
			Protocol int
			Path     string
		}
		if status != 0 || diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &decoded) != nil || decoded.Protocol != 1 || decoded.Path != value {
			t.Fatal(status, out.String(), diagnostic.String())
		}
	}
	for _, value := range []string{"nul\x00value", "invalid\xff"} {
		var out, diagnostic bytes.Buffer
		if status := run([]string{"path", "--value", value}, strings.NewReader(""), &out, &diagnostic); status != 2 || out.Len() != 0 || !strings.Contains(diagnostic.String(), `"invalid_request"`) {
			t.Fatal(status, out.String(), diagnostic.String())
		}
	}
}

func TestStrictRequests(t *testing.T) {
	inputs := []string{
		`null`, `[]`, `{}`, `{"protocol":true,"operation":"inspect"}`, `{"protocol":null,"operation":"inspect"}`,
		`{"protocol":1,"operation":"inspect","icalendar":null}`,
		`{"protocol":1,"operation":"inspect","bogus":true}`,
		`{"Protocol":1,"operation":"inspect"}`,
		`{"protocol":1,"Protocol":1,"operation":"inspect"}`,
		`{"protocol":1,"protocol":1,"operation":"inspect"}`,
		`{"protocol":1,"operation":"inspect"} {}`,
		`{"protocol":1,"operation":"get","url":"https://example.test/a","password":"one-secret"}`,
		"{\"protocol\":1,\"operation\":\"inspect\",\"icalendar\":\"\xff\"}",
		strings.Repeat("x", maxRequest+1),
	}
	for i, raw := range inputs {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var out, err bytes.Buffer
			if code := run([]string{"request"}, strings.NewReader(raw), &out, &err); code != 2 || out.Len() != 0 || !strings.Contains(err.String(), `"invalid_request"`) {
				t.Fatal(code, out.String(), err.String())
			}
			if strings.Contains(err.String(), "one-secret") {
				t.Fatal("secret leaked")
			}
		})
	}
	for _, args := range [][]string{nil, {"request", "secret"}, {"greet", "--name", "x"}, {"--password=secret"}} {
		var out, err bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &err); code != 2 || out.Len() != 0 || strings.Contains(err.String(), "secret") {
			t.Fatal(code, out.String(), err.String())
		}
	}
}

func TestCalendarValidation(t *testing.T) {
	tests := map[string]string{
		"empty":               "",
		"trailing calendar":   sample + sample,
		"trailing junk":       sample + "junk\r\n",
		"missing version":     strings.Replace(sample, "VERSION:2.0\r\n", "", 1),
		"missing uid":         strings.Replace(sample, "UID:test-1\r\n", "", 1),
		"missing stamp":       strings.Replace(sample, "DTSTAMP:20261105T000000Z\r\n", "", 1),
		"missing start":       strings.Replace(sample, "DTSTART:20261105T090000Z\r\n", "", 1),
		"bad date":            strings.Replace(sample, "20261105T090000Z", "20260230T090000Z", 1),
		"bad duration":        strings.Replace(sample, "DTEND:20261105T100000Z", "DURATION:-PT1H", 1),
		"bad rrule":           strings.Replace(sample, "SUMMARY:", "RRULE:FREQ=IMPOSSIBLE\r\nSUMMARY:", 1),
		"bad rrule bound":     strings.Replace(sample, "SUMMARY:", "RRULE:FREQ=DAILY;BYHOUR=99\r\nSUMMARY:", 1),
		"zero interval":       strings.Replace(sample, "SUMMARY:", "RRULE:FREQ=DAILY;INTERVAL=0\r\nSUMMARY:", 1),
		"duplicate rule part": strings.Replace(sample, "SUMMARY:", "RRULE:FREQ=DAILY;COUNT=1;COUNT=2\r\nSUMMARY:", 1),
		"null":                strings.Replace(sample, "Hello", "Hello\x00", 1),
		"parser panic":        strings.Replace(sample, "SUMMARY:Hello 世界", "SUMMARY;X=", 1),
		"duplicate uid":       strings.Replace(sample, "UID:test-1", "UID:test-1\r\nUID:test-1", 1),
		"method":              strings.Replace(sample, "VERSION:2.0", "VERSION:2.0\r\nMETHOD:REQUEST", 1),
		"deep":                strings.Replace(sample, "SUMMARY:Hello 世界", strings.Repeat("BEGIN:VALARM\r\n", 20), 1),
		"unknown timezone":    strings.Replace(sample, "DTSTART:20261105T090000Z", "DTSTART;TZID=Unknown/Zone:20261105T090000", 1),
		"reversed range":      strings.Replace(sample, "DTEND:20261105T100000Z", "DTEND:20261105T080000Z", 1),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, f := parseCalendar(raw); f == nil || f.Code != "invalid_calendar" {
				t.Fatal("expected invalid calendar", f)
			}
		})
	}
	if s, f := parseCalendar(recurrence); f != nil || s.ICalendar != recurrence || s.EventCount != 2 || s.Events[1].RecurrenceID != "20261105T090000" || s.Events[1].Description != "line one\nline two" {
		t.Fatal(s, f)
	}
	folded := strings.Replace(sample, "SUMMARY:Hello 世界", "SUMMARY:Hello \r\n 世界", 1)
	if s, f := parseCalendar(folded); f != nil || s.ICalendar != folded || s.Events[0].Summary != "Hello 世界" {
		t.Fatal(s, f)
	}
}

func TestAllDayFloatingDST(t *testing.T) {
	tests := []struct {
		start, end, utc string
		allDay          bool
	}{
		{"DTSTART;VALUE=DATE:20260308", "DTEND;VALUE=DATE:20260309", "", true},
		{"DTSTART:20260308T033000", "DTEND:20260308T043000", "", false},
		{"DTSTART;TZID=America/New_York:20260308T033000", "DTEND;TZID=America/New_York:20260308T043000", "2026-03-08T07:30:00Z", false},
		{"DTSTART;TZID=America/New_York:20260308T023000", "DTEND;TZID=America/New_York:20260308T043000", "", false},
		{"DTSTART;TZID=America/New_York:20261101T013000", "DTEND;TZID=America/New_York:20261101T043000", "", false},
		{"DTSTART;TZID=America/New_York:20261101T033000", "DTEND;TZID=America/New_York:20261101T043000", "2026-11-01T08:30:00Z", false},
		{"DTSTART;TZID=Asia/Tokyo:20261105T090000", "DTEND;TZID=Asia/Tokyo:20261105T100000", "2026-11-05T00:00:00Z", false},
	}
	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			raw := strings.ReplaceAll(strings.ReplaceAll(sample, "DTSTART:20261105T090000Z", tt.start), "DTEND:20261105T100000Z", tt.end)
			s, f := parseCalendar(raw)
			if f != nil || s.Events[0].AllDay != tt.allDay || s.Events[0].StartUTC != tt.utc {
				t.Fatal(s, f)
			}
		})
	}
}

func mockRequest(s *httptest.Server, operation string) request {
	return request{Protocol: 1, Operation: operation, URL: s.URL + "/calendar/event.ics", Username: "test-user", Password: "private-password", AllowInsecureLoopback: true}
}

func TestCRUDAndConditions(t *testing.T) {
	var methods []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "test-user" || p != "private-password" {
			t.Error("missing scoped auth")
		}
		methods = append(methods, r.Method)
		switch r.Method {
		case "GET":
			w.Header().Set("ETag", `"v1"`)
			io.WriteString(w, recurrence)
		case "PUT":
			raw, _ := io.ReadAll(r.Body)
			if string(raw) != recurrence {
				t.Error("calendar bytes changed")
			}
			if r.Header.Get("If-None-Match") != "*" && r.Header.Get("If-Match") != `"v1"` {
				t.Error("missing precondition")
			}
			w.Header().Set("ETag", `"v2"`)
			w.WriteHeader(201)
		case "DELETE":
			if r.Header.Get("If-Match") != `"v1"` {
				t.Error("missing delete precondition")
			}
			w.WriteHeader(204)
		}
	}))
	defer s.Close()
	for _, op := range []string{"get", "create", "update", "delete"} {
		r := mockRequest(s, op)
		if op == "update" || op == "delete" {
			r.ETag = `"v1"`
		}
		if op == "create" || op == "update" {
			r.ICalendar = recurrence
		}
		code, out, err := call(t, r)
		if code != 0 || err != "" {
			t.Fatal(op, code, out, err)
		}
		if op == "get" && !strings.Contains(out, `"eventCount":2`) {
			t.Fatal(out)
		}
	}
	if strings.Join(methods, ",") != "GET,PUT,PUT,DELETE" {
		t.Fatal(methods)
	}
	for _, etag := range []string{"", "*", "W/\"weak\"", "bare", "\"bad\r\nheader\"", "\"a\", \"b\""} {
		r := mockRequest(s, "delete")
		r.ETag = etag
		code, out, _ := call(t, r)
		if code != 2 || out != "" {
			t.Fatal(etag, code, out)
		}
	}
}

func TestServerFailuresAreRedacted(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 412, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				io.WriteString(w, "private-password test-user "+r.URL.String())
			}))
			defer s.Close()
			code, out, err := call(t, mockRequest(s, "get"))
			if code != 1 || out != "" || strings.Contains(err, "private-password") || strings.Contains(err, "test-user") || strings.Contains(err, s.URL) {
				t.Fatal(code, out, err)
			}
			if !strings.Contains(err, fmt.Sprintf(`"status":%d`, status)) {
				t.Fatal(err)
			}
		})
	}
}

func xmlText(s string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(s)); return b.String() }
func multiResponse(href, props, status string) string {
	return `<d:response><d:href>` + xmlText(href) + `</d:href><d:propstat><d:prop>` + props + `</d:prop><d:status>HTTP/1.1 ` + status + `</d:status></d:propstat></d:response>`
}
func multistatusXML(inner string) string {
	return `<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` + inner + `</d:multistatus>`
}

func TestDiscoveryRequiresResourceType(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(207)
		switch r.URL.Path {
		case "/":
			io.WriteString(w, multistatusXML(multiResponse("/", `<d:current-user-principal><d:href>/principal/</d:href></d:current-user-principal>`, "200 OK")))
		case "/principal/":
			io.WriteString(w, multistatusXML(multiResponse("/principal/", `<c:calendar-home-set><d:href>/home/</d:href></c:calendar-home-set>`, "200 OK")))
		case "/home/":
			io.WriteString(w, multistatusXML(multiResponse("/home/work/", `<d:displayname>Missing type</d:displayname>`, "200 OK")))
		default:
			t.Error("unexpected discovery request")
		}
	}))
	defer s.Close()
	r := mockRequest(s, "discover")
	r.URL = s.URL + "/"
	code, out, diagnostic := call(t, r)
	if code != 1 || out != "" || !strings.Contains(diagnostic, `"code":"invalid_response"`) {
		t.Fatal(code, out, diagnostic)
	}
}

func TestDiscoveryAndRecurrenceQuery(t *testing.T) {
	var calls []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/start":
			w.Header().Set("Location", "/dav/")
			w.WriteHeader(301)
			return
		case "/dav/":
			if r.Method != "PROPFIND" || r.Header.Get("Depth") != "0" {
				t.Error(r.Method, r.Header)
			}
			w.WriteHeader(207)
			io.WriteString(w, multistatusXML(multiResponse("/dav/", `<d:current-user-principal><d:href>/principal/</d:href></d:current-user-principal>`, "200 OK")))
		case "/principal/":
			w.WriteHeader(207)
			io.WriteString(w, multistatusXML(multiResponse("/principal/", `<c:calendar-home-set><d:href>/home/</d:href></c:calendar-home-set>`, "200 OK")))
		case "/home/":
			if r.Header.Get("Depth") != "1" {
				t.Error("wrong list depth")
			}
			w.WriteHeader(207)
			io.WriteString(w, multistatusXML(multiResponse("/home/work/", `<d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Work 世界</d:displayname><c:calendar-description>Office</c:calendar-description><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set>`, "200 OK")))
		case "/home/work/":
			if r.Method != "REPORT" || r.Header.Get("Depth") != "1" || strings.Contains(string(body), "expand") ||
				!strings.Contains(string(body), `<c:time-range start="20261105T000000Z" end="20261106T000000Z"/>`) ||
				!strings.Contains(string(body), `<c:comp-filter name="VEVENT">`) {
				t.Error("incorrect recurrence-aware REPORT", string(body))
			}
			w.WriteHeader(207)
			io.WriteString(w, multistatusXML(multiResponse("/home/work/series.ics", `<d:getetag>"etag"</d:getetag><c:calendar-data>`+xmlText(recurrence)+`</c:calendar-data>`, "200 OK")))
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	r := mockRequest(s, "discover")
	r.URL = s.URL + "/start"
	code, out, err := call(t, r)
	if code != 0 || err != "" || !strings.Contains(out, `"name":"Work 世界"`) || !strings.Contains(out, `"components":["VEVENT"]`) {
		t.Fatal(code, out, err)
	}
	r.Operation = "query"
	r.URL = s.URL + "/home/work/"
	r.Start = "2026-11-05T00:00:00Z"
	r.End = "2026-11-06T00:00:00Z"
	code, out, err = call(t, r)
	var envelope struct {
		Data struct {
			Resources []struct {
				ICalendar string
				Events    []eventSummary
			}
		}
	}
	if e := json.Unmarshal([]byte(out), &envelope); e != nil {
		t.Fatal(e, out, err)
	}
	if code != 0 || err != "" || len(envelope.Data.Resources) != 1 || envelope.Data.Resources[0].ICalendar != recurrence {
		t.Fatal(code, out, err)
	}
	stored := envelope.Data.Resources[0].Events
	if len(stored) != 2 || stored[0].Start != "20261101T090000" || stored[1].RecurrenceID != "20261105T090000" {
		t.Fatal("recurrence query lost master outside window or detached exception", stored)
	}
	if strings.Join(calls, ",") != "PROPFIND /start,PROPFIND /dav/,PROPFIND /principal/,PROPFIND /home/,REPORT /home/work/" {
		t.Fatal(calls)
	}
}

func TestMultiStatusFailures(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
	}{
		{"property failure", multistatusXML(multiResponse("/a", `<c:calendar-data/>`, "403 Forbidden")), 207},
		{"resource failure", multistatusXML(`<d:response><d:href>/a</d:href><d:status>HTTP/1.1 404 Not Found</d:status></d:response>`), 207},
		{"missing properties", multistatusXML(multiResponse("/a", `<d:getetag>"x"</d:getetag>`, "200 OK")), 207},
		{"malformed status", multistatusXML(multiResponse("/a", `<d:getetag/>`, "bad")), 207},
		{"wrong namespace", `<multistatus><response/></multistatus>`, 207},
		{"wrong status", multistatusXML(""), 200},
		{"DTD", `<!DOCTYPE dav [<!ENTITY x SYSTEM "file:///etc/passwd">]>` + multistatusXML(""), 207},
		{"unsafe href", multistatusXML(multiResponse("https://evil.example/secret", `<d:getetag>"x"</d:getetag><c:calendar-data>`+xmlText(sample)+`</c:calendar-data>`, "200 OK")), 207},
		{"invalid calendar", multistatusXML(multiResponse("/a", `<d:getetag>"x"</d:getetag><c:calendar-data>secret invalid input</c:calendar-data>`, "200 OK")), 207},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); io.WriteString(w, tt.body) }))
			defer s.Close()
			r := mockRequest(s, "query")
			r.Start = "2026-11-05T00:00:00Z"
			r.End = "2026-11-06T00:00:00Z"
			code, out, err := call(t, r)
			if code != 1 || out != "" || strings.Contains(err, "secret") {
				t.Fatal(code, out, err)
			}
		})
	}
}

func TestURLSecurity(t *testing.T) {
	invalidURLs := []string{
		"http://example.com/", "http://127.0.0.1/", "https://u:p@example.com/", "https://example.com/#fragment", "https://example.com/?token=secret",
		"https://example.com./", "https://example.com:0/", "https://example.com:70000/", "https://example.com/%00", "https://example.com/%5c", "https://example.com\\@evil.com/", "file:///etc/passwd",
	}
	for _, raw := range invalidURLs {
		if _, f := checkedURL(raw, false); f == nil {
			t.Fatal("accepted", raw)
		}
	}
	b, f := newBackend(request{URL: "https://caldav.icloud.com/", Username: "u", Password: "p"})
	if f != nil {
		t.Fatal(f)
	}
	defer b.client.CloseIdleConnections()
	for _, raw := range []string{"https://p01-caldav.icloud.com/a", "https://p99-caldav.icloud.com/a", "https://p123-caldav.icloud.com/a", "https://caldav.icloud.com/a"} {
		u, _ := url.Parse(raw)
		if !b.allowed(u) {
			t.Fatal("rejected", raw)
		}
	}
	for _, raw := range []string{
		"https://p1-caldav.icloud.com/", "https://p0001-caldav.icloud.com/", "https://evil.icloud.com/", "https://p01-caldav.icloud.com.evil.example/", "https://p123-caldav.icloud.com.evil.example/",
		"http://p01-caldav.icloud.com/", "https://p01-caldav.icloud.com:8443/", "https://caldav.icloud.com.evil.example/", "https://evilcaldav.icloud.com/",
	} {
		u, _ := url.Parse(raw)
		if b.allowed(u) {
			t.Fatal("accepted", raw)
		}
	}
	b.shard = "p01-caldav.icloud.com"
	u, _ := url.Parse("https://p02-caldav.icloud.com/")
	if b.allowed(u) {
		t.Fatal("cross-shard auth permitted")
	}
	b.shard = "p123-caldav.icloud.com"
	u, _ = url.Parse("https://p124-caldav.icloud.com/")
	if b.allowed(u) {
		t.Fatal("three-digit cross-shard auth permitted")
	}
}

func TestRedirectNoCredentialLeak(t *testing.T) {
	var received atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		t.Error("cross-origin request received")
	}))
	defer evil.Close()
	for _, status := range []int{301, 302, 307, 308} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", evil.URL+"/secret")
			w.WriteHeader(status)
		}))
		code, out, err := call(t, mockRequest(s, "get"))
		s.Close()
		if code != 1 || out != "" || !strings.Contains(err, `"unsafe_url"`) || received.Load() != 0 {
			t.Fatal(code, out, err)
		}
	}
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Location", "/again"); w.WriteHeader(307) }))
	defer loop.Close()
	code, out, err := call(t, mockRequest(loop, "get"))
	if code != 1 || out != "" || !strings.Contains(err, `"unsafe_redirect"`) {
		t.Fatal(code, out, err)
	}
}

func TestHTTPSCertificateAndDowngrade(t *testing.T) {
	var received atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
	defer plain.Close()
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", plain.URL)
		w.WriteHeader(307)
	}))
	defer tlsServer.Close()
	r := request{Protocol: 1, Operation: "get", URL: tlsServer.URL + "/event.ics", Username: "u", Password: "p", AllowInsecureLoopback: true}
	code, out, err := call(t, r)
	if code != 1 || out != "" || !strings.Contains(err, `"network_error"`) {
		t.Fatal(code, out, err)
	}
	b, f := newBackend(r)
	if f != nil {
		t.Fatal(f)
	}
	pool := x509.NewCertPool()
	pool.AddCert(tlsServer.Certificate())
	b.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	defer b.client.CloseIdleConnections()
	_, f = b.do(context.Background(), "GET", b.origin, "", nil)
	if f == nil || f.Code != "unsafe_url" || received.Load() != 0 {
		t.Fatal("TLS downgrade", f, received.Load())
	}
}

func TestLimitsAndTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("a", maxResponse+1)) }))
	code, out, err := call(t, mockRequest(s, "get"))
	s.Close()
	if code != 1 || out != "" || !strings.Contains(err, `"response_limit"`) {
		t.Fatal(code, out, err)
	}
	s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	b, f := newBackend(mockRequest(s, "get"))
	if f != nil {
		t.Fatal(f)
	}
	defer b.client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, f = b.do(ctx, "GET", b.origin, "", nil)
	if f == nil || f.Code != "network_error" {
		t.Fatal(f)
	}
}

func TestQueryValidation(t *testing.T) {
	for _, tt := range []struct{ start, end string }{
		{"", ""}, {"2026-11-05T00:00:00+00:00", "2026-11-06T00:00:00Z"},
		{"2026-11-05T00:00:00Z", "2026-11-05T00:00:00Z"}, {"2026-11-05T00:00:00.1Z", "2026-11-06T00:00:00Z"},
	} {
		r := request{Protocol: 1, Operation: "query", URL: "https://example.invalid/", Start: tt.start, End: tt.end}
		code, out, err := call(t, r)
		if code != 2 || out != "" || !strings.Contains(err, `"invalid_request"`) {
			t.Fatal(code, out, err)
		}
	}
}

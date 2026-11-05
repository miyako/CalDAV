package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestICloudScopedAuthWithoutNetwork(t *testing.T) {
	for _, destination := range []string{"https://p01-caldav.icloud.com/events/a.ics", "https://p123-caldav.icloud.com/events/a.ics", "https://p01-caldav.icloud.com.evil.example/a", "https://p123-caldav.icloud.com.evil.example/a", "https://unrelated.icloud.com/a", "http://p01-caldav.icloud.com/a"} {
		t.Run(destination, func(t *testing.T) {
			b, f := newBackend(request{URL: "https://caldav.icloud.com/events/a.ics", Username: "user", Password: "private"})
			if f != nil {
				t.Fatal(f)
			}
			defer b.client.CloseIdleConnections()
			calls := 0
			b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				u, p, ok := r.BasicAuth()
				if !ok || u != "user" || p != "private" {
					t.Fatal("missing auth")
				}
				if calls == 1 {
					return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{destination}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if r.URL.Host != "p01-caldav.icloud.com" && r.URL.Host != "p123-caldav.icloud.com" {
					t.Fatal("unsafe request sent", r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(sample)), Request: r}, nil
			})
			_, f = b.do(context.Background(), "GET", b.origin, "", nil)
			if destination == "https://p01-caldav.icloud.com/events/a.ics" || destination == "https://p123-caldav.icloud.com/events/a.ics" {
				if f != nil || calls != 2 || "https://"+b.shard+"/events/a.ics" != destination {
					t.Fatal(calls, f, b.shard)
				}
			} else if f == nil || f.Code != "unsafe_url" || calls != 1 {
				t.Fatal(calls, f)
			}
		})
	}
}

func TestMutationRedirectRulesAndConflicts(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		for _, status := range []int{302, 307, 308, 412} {
			t.Run(fmt.Sprintf("%s/%d", operation, status), func(t *testing.T) {
				var calls atomic.Int32
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if operation == "create" && r.Header.Get("If-None-Match") != "*" {
						t.Error("lost create condition")
					}
					if operation != "create" && r.Header.Get("If-Match") != `"rev"` {
						t.Error("lost match condition")
					}
					if r.URL.Path == "/event.ics" {
						w.Header().Set("Location", "/moved.ics")
						w.WriteHeader(status)
						return
					}
					if r.URL.Path != "/moved.ics" {
						t.Error("unexpected target")
					}
					w.WriteHeader(204)
				}))
				defer s.Close()
				r := mockRequest(s, operation)
				r.URL = s.URL + "/event.ics"
				if operation != "delete" {
					r.ICalendar = sample
				}
				if operation != "create" {
					r.ETag = `"rev"`
				}
				code, out, err := call(t, r)
				if code != 1 || out != "" || calls.Load() != 1 {
					t.Fatal(code, out, err, calls.Load())
				}
				if status == 412 && !strings.Contains(err, `"conflict"`) {
					t.Fatal(err)
				}
				if status != 412 && !strings.Contains(err, `"unsafe_redirect"`) {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestDAVRedirectPreservesMethodXMLAndScopedAuth(t *testing.T) {
	const body = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:getetag/></d:prop></d:propfind>`
	for _, method := range []string{"PROPFIND", "REPORT"} {
		for _, status := range []int{301, 302, 307, 308} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				calls := 0
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					raw, _ := io.ReadAll(r.Body)
					if r.Method != method || string(raw) != body || r.Header.Get("Depth") != "1" || r.Header.Get("Content-Type") != "application/xml" {
						t.Error("DAV redirect changed method/XML/headers", r.Method, string(raw), r.Header)
					}
					username, password, ok := r.BasicAuth()
					if !ok || username != "test-user" || password != "private-password" {
						t.Error("lost same-origin auth")
					}
					if r.URL.Path == "/calendar/event.ics" {
						w.Header().Set("Location", "/moved")
						w.WriteHeader(status)
						return
					}
					if r.URL.Path != "/moved" {
						t.Error("unexpected path")
					}
					w.WriteHeader(207)
					io.WriteString(w, multistatusXML(""))
				}))
				defer s.Close()
				b, f := newBackend(mockRequest(s, "query"))
				if f != nil {
					t.Fatal(f)
				}
				defer b.client.CloseIdleConnections()
				resp, f := b.do(context.Background(), method, b.origin, body, map[string]string{"Depth": "1", "Content-Type": "application/xml"})
				if f != nil || resp == nil || resp.status != 207 || calls != 2 {
					t.Fatal(f, resp, calls)
				}
			})
		}
	}
}

func TestXMLStrictnessAndOptionalMetadata(t *testing.T) {
	good := multistatusXML(multiResponse("/a", `<d:resourcetype><d:collection/><c:calendar/></d:resourcetype>`, "200 OK"))
	for _, raw := range []string{good + good, good + "junk", strings.Replace(good, "</d:multistatus>", "", 1), multistatusXML(multiResponse("/a", "<d:resourcetype/>", "404 Not Found")), multistatusXML(multiResponse("/a", "", "404 Not Found"))} {
		if _, f := parseMulti(&response{status: 207, body: []byte(raw)}); f == nil {
			t.Fatal("invalid XML response accepted", raw)
		}
	}
	optional := multistatusXML(`<d:response><d:href>/a</d:href><d:propstat><d:prop><d:resourcetype><c:calendar/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat><d:propstat><d:prop><d:displayname/><c:calendar-description/><c:supported-calendar-component-set/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response>`)
	if _, f := parseMulti(&response{status: 207, body: []byte(optional)}); f != nil {
		t.Fatal(f)
	}
}

func TestCountsAndUnfoldedLineLimit(t *testing.T) {
	event := "BEGIN:VEVENT\r\nUID:test\r\nDTSTAMP:20261105T000000Z\r\nDTSTART:20261105T090000Z\r\nEND:VEVENT\r\n"
	raw := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:tests\r\n" + strings.Repeat(event, maxEvents+1) + "END:VCALENDAR\r\n"
	if _, f := parseCalendar(raw); f == nil {
		t.Fatal("too many events accepted")
	}
	raw = strings.Replace(sample, "SUMMARY:Hello 世界", "SUMMARY:"+strings.Repeat("a", 65537), 1)
	if _, f := parseCalendar(raw); f == nil {
		t.Fatal("large content line accepted")
	}
	raw = strings.Replace(sample, "SUMMARY:Hello 世界", "SUMMARY:"+strings.Repeat("a", 32768)+"\r\n "+strings.Repeat("b", 32768), 1)
	if _, f := parseCalendar(raw); f == nil {
		t.Fatal("large unfolded content line accepted")
	}
	raw = multistatusXML(strings.Repeat(multiResponse("/a", `<d:displayname>n</d:displayname>`, "200 OK"), maxResources+1))
	if _, f := parseMulti(&response{status: 207, body: []byte(raw)}); f == nil {
		t.Fatal("too many resources accepted")
	}
}

func TestDiscoveryHrefScopeAndSafeResourceInput(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(207)
		io.WriteString(w, multistatusXML(multiResponse("/a", `<d:current-user-principal><d:href>https://unrelated.example/principal</d:href></d:current-user-principal>`, "200 OK")))
	}))
	defer s.Close()
	r := mockRequest(s, "discover")
	code, out, err := call(t, r)
	if code != 1 || out != "" || !strings.Contains(err, `"unsafe_url"`) {
		t.Fatal(code, out, err)
	}
	r = mockRequest(s, "delete")
	r.URL = s.URL + "/calendar/"
	r.ETag = `"rev"`
	code, out, err = call(t, r)
	if code != 2 || out != "" || !strings.Contains(err, `"invalid_request"`) {
		t.Fatal(code, out, err)
	}
	r = mockRequest(s, "get")
	r.AllowInsecureLoopback = false
	code, out, err = call(t, r)
	if code != 2 || out != "" {
		t.Fatal(code, out, err)
	}
}

func TestEmptyQueryAndUnsupportedEncoding(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if compressed {
				w.Header().Set("Content-Encoding", "gzip")
			}
			w.WriteHeader(207)
			io.WriteString(w, multistatusXML(""))
		}))
		r := mockRequest(s, "query")
		r.Start = "2026-11-05T00:00:00Z"
		r.End = "2026-11-06T00:00:00Z"
		code, out, err := call(t, r)
		s.Close()
		if !compressed {
			if code != 0 || out != "{\"protocol\":1,\"operation\":\"query\",\"data\":{\"resources\":[]}}\n" || err != "" {
				t.Fatal(code, out, err)
			}
		} else if code != 1 || out != "" || !strings.Contains(err, `"invalid_response"`) {
			t.Fatal(code, out, err)
		}
	}
}

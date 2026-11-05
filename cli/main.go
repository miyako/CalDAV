package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const version = "0.1.2"

const (
	maxRequest     = 2 << 20
	maxResponse    = 8 << 20
	maxEvents      = 4096
	maxResources   = 4096
	requestTimeout = 25 * time.Second
)

type request struct {
	Protocol              int         `json:"protocol"`
	Operation             string      `json:"operation"`
	URL                   string      `json:"url,omitempty"`
	Username              string      `json:"username,omitempty"`
	Password              string      `json:"password,omitempty"`
	ICalendar             string      `json:"icalendar,omitempty"`
	Event                 *eventInput `json:"event,omitempty"`
	ETag                  string      `json:"etag,omitempty"`
	Start                 string      `json:"start,omitempty"`
	End                   string      `json:"end,omitempty"`
	AllowInsecureLoopback bool        `json:"allowInsecureLoopback,omitempty"`
}

type fault struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status,omitempty"`
	exit    int
}

func (f *fault) Error() string { return f.Message }
func invalid() *fault          { return &fault{"invalid_request", "invalid request", 0, 2} }
func invalidCalendar() *fault  { return &fault{"invalid_calendar", "invalid iCalendar resource", 0, 2} }
func remoteCalendar() *fault   { return &fault{"invalid_response", "invalid calendar response", 0, 1} }
func networkError() *fault     { return &fault{"network_error", "request failed", 0, 1} }

func run(args []string, input io.Reader, stdout, stderr io.Writer) int {
	fail := func(f *fault) int {
		json.NewEncoder(stderr).Encode(struct {
			Protocol int    `json:"protocol"`
			Error    *fault `json:"error"`
		}{1, f})
		return f.exit
	}
	if len(args) == 3 && args[0] == "path" && args[1] == "--value" {
		if !utf8.ValidString(args[2]) || strings.ContainsRune(args[2], 0) {
			return fail(invalid())
		}
		out, err := json.Marshal(struct {
			Protocol int    `json:"protocol"`
			Path     string `json:"path"`
		}{1, args[2]})
		if err != nil {
			return fail(invalid())
		}
		if _, err = stdout.Write(append(out, '\n')); err != nil {
			return 1
		}
		return 0
	}
	if len(args) != 1 {
		return fail(invalid())
	}
	if args[0] == "--version" {
		io.WriteString(stdout, "{\"protocol\":1,\"version\":\""+version+"\"}\n")
		return 0
	}
	if args[0] != "request" {
		return fail(invalid())
	}
	raw, err := io.ReadAll(io.LimitReader(input, maxRequest+1))
	if err != nil || len(raw) > maxRequest || !utf8.Valid(raw) || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return fail(invalid())
	}
	var req request
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return fail(invalid())
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || req.Protocol != 1 {
		return fail(invalid())
	}
	// Reject duplicate keys and null scalar fields: neither has a canonical request meaning.
	check := json.NewDecoder(bytes.NewReader(raw))
	check.Token()
	seen := map[string]bool{}
	for check.More() {
		key, _ := check.Token()
		name, ok := key.(string)
		if !ok || seen[name] {
			return fail(invalid())
		}
		switch name {
		case "protocol", "operation", "url", "username", "password", "icalendar", "event", "etag", "start", "end", "allowInsecureLoopback":
		default:
			return fail(invalid())
		}
		seen[name] = true
		var value json.RawMessage
		if check.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return fail(invalid())
		}
		if name == "event" && !validEventKeys(value) {
			return fail(invalid())
		}
	}
	for _, s := range []string{req.URL, req.Username, req.Password, req.ICalendar, req.ETag, req.Start, req.End} {
		if strings.ContainsRune(s, 0) {
			return fail(invalid())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	data, f := execute(ctx, req)
	if f != nil {
		return fail(f)
	}
	// Buffer before emitting, so a failed operation never leaves partial stdout.
	out, err := json.Marshal(struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
		Data      any    `json:"data"`
	}{1, req.Operation, data})
	if err != nil {
		return fail(networkError())
	}
	out = append(out, '\n')
	if _, err = stdout.Write(out); err != nil {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

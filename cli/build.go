package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

type eventInput struct {
	UID         string `json:"uid,omitempty"`
	Summary     string `json:"summary"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	AllDay      bool   `json:"allDay,omitempty"`
}

func validEventKeys(raw []byte) bool {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.Token()
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return false
		}
		seen[name] = true
		switch name {
		case "uid", "summary", "start", "end", "description", "location", "timezone", "allDay":
		default:
			return false
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return false
		}
	}
	return true
}

func buildCalendar(e *eventInput) (*calendarSummary, *fault) {
	return buildCalendarWith(e, time.Now().UTC(), rand.Reader)
}

func buildCalendarWith(e *eventInput, now time.Time, entropy io.Reader) (*calendarSummary, *fault) {
	if e == nil || e.Summary == "" || (e.Timezone != "" && e.Timezone != "UTC") {
		return nil, invalid()
	}
	for _, s := range []string{e.UID, e.Summary, e.Start, e.End, e.Description, e.Location, e.Timezone} {
		if !utf8.ValidString(s) {
			return nil, invalid()
		}
		for _, c := range s {
			if c < 32 && c != '\r' && c != '\n' && c != '\t' {
				return nil, invalid()
			}
		}
	}
	if strings.ContainsAny(e.UID, "\r\n\t") {
		return nil, invalid()
	}
	layout := time.RFC3339
	if e.AllDay {
		layout = "2006-01-02"
	}
	start, err := time.Parse(layout, e.Start)
	if err != nil {
		return nil, invalid()
	}
	end, err := time.Parse(layout, e.End)
	if err != nil || !end.After(start) {
		return nil, invalid()
	}
	if !e.AllDay && (start.Nanosecond() != 0 || end.Nanosecond() != 0) {
		return nil, invalid()
	}
	uid := e.UID
	if uid == "" {
		var id [16]byte
		if _, err := io.ReadFull(entropy, id[:]); err != nil {
			return nil, &fault{"generation_failed", "event identity generation failed", 0, 1}
		}
		id[6] = (id[6] & 0x0f) | 0x40
		id[8] = (id[8] & 0x3f) | 0x80
		uid = fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	}
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//CalDAV//Calendar 1//EN", "BEGIN:VEVENT",
		"UID:" + escapeText(uid), "DTSTAMP:" + now.UTC().Format("20060102T150405Z"),
	}
	if e.AllDay {
		lines = append(lines, "DTSTART;VALUE=DATE:"+start.Format("20060102"), "DTEND;VALUE=DATE:"+end.Format("20060102"))
	} else {
		lines = append(lines, "DTSTART:"+start.UTC().Format("20060102T150405Z"), "DTEND:"+end.UTC().Format("20060102T150405Z"))
	}
	lines = append(lines, "SUMMARY:"+escapeText(e.Summary))
	if e.Description != "" {
		lines = append(lines, "DESCRIPTION:"+escapeText(e.Description))
	}
	if e.Location != "" {
		lines = append(lines, "LOCATION:"+escapeText(e.Location))
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	var raw strings.Builder
	for _, line := range lines {
		raw.WriteString(foldLine(line))
		raw.WriteString("\r\n")
	}
	if raw.Len() > maxRequest {
		return nil, invalid()
	}
	return parseCalendar(raw.String())
}

func escapeText(s string) string {
	return strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\r\n", "\\n", "\r", "\\n", "\n", "\\n").Replace(s)
}

func foldLine(line string) string {
	var out strings.Builder
	width := 0
	for _, c := range line {
		size := utf8.RuneLen(c)
		if width+size > 75 {
			out.WriteString("\r\n ")
			width = 1
		}
		out.WriteRune(c)
		width += size
	}
	return out.String()
}

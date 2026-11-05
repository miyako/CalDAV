package main

import (
	"io"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	ical "github.com/emersion/go-ical"
)

type eventSummary struct {
	UID          string `json:"uid"`
	Summary      string `json:"summary"`
	Description  string `json:"description"`
	Location     string `json:"location"`
	Start        string `json:"start"`
	End          string `json:"end"`
	RecurrenceID string `json:"recurrenceId"`
	RRule        string `json:"rrule"`
	AllDay       bool   `json:"allDay"`
	Timezone     string `json:"timezone"`
	StartUTC     string `json:"startUTC,omitempty"`
	EndUTC       string `json:"endUTC,omitempty"`
}

type calendarSummary struct {
	ICalendar  string         `json:"icalendar"`
	EventCount int            `json:"eventCount"`
	Events     []eventSummary `json:"events"`
}

func property(c *ical.Component, name string) string {
	p := c.Props.Get(name)
	if p == nil {
		return ""
	}
	return p.Value
}

// Bound nesting before entering the upstream recursive decoder. Keep the original
// bytes separately: decoding and re-encoding would change unknown/folded fields.
func calendarStructure(raw string) bool {
	if len(raw) == 0 || len(raw) > maxResponse || !utf8.ValidString(raw) {
		return false
	}
	for _, r := range raw {
		if r < 32 && r != '\r' && r != '\n' && r != '\t' {
			return false
		}
	}
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if strings.ContainsRune(normalized, '\r') || strings.Count(normalized, "\n") > 100000 {
		return false
	}
	var stack []string
	finished := false
	lines := strings.Split(normalized, "\n")
	var unfolded []string
	for _, line := range lines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(unfolded) == 0 {
				return false
			}
			unfolded[len(unfolded)-1] += line[1:]
			if len(unfolded[len(unfolded)-1]) > 65536 {
				return false
			}
		} else if line != "" {
			if len(line) > 65536 {
				return false
			}
			unfolded = append(unfolded, line)
		}
	}
	if len(unfolded) > 100000 {
		return false
	}
	for _, line := range unfolded {
		if finished {
			return false
		}
		if strings.HasPrefix(strings.ToUpper(line), "BEGIN:") {
			name := strings.ToUpper(line[6:])
			if len(stack) == 0 && name != "VCALENDAR" {
				return false
			}
			stack = append(stack, name)
			if len(stack) > 16 {
				return false
			}
		} else if strings.HasPrefix(strings.ToUpper(line), "END:") {
			if len(stack) == 0 || stack[len(stack)-1] != strings.ToUpper(line[4:]) {
				return false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				finished = true
			}
		} else if len(stack) == 0 {
			return false
		}
	}
	return finished
}

func parseCalendar(raw string) (result *calendarSummary, f *fault) {
	// Upstream v439c63c panics on a truncated parameter. Convert only parser
	// panics into invalid input; never expose its dynamic diagnostic.
	defer func() {
		if recover() != nil {
			result = nil
			f = invalidCalendar()
		}
	}()
	if !calendarStructure(raw) {
		return nil, invalidCalendar()
	}
	decoder := ical.NewDecoder(strings.NewReader(raw))
	cal, err := decoder.Decode()
	if err != nil {
		return nil, invalidCalendar()
	}
	if _, err = decoder.Decode(); err != io.EOF {
		return nil, invalidCalendar()
	}
	if property(cal.Component, "VERSION") != "2.0" || property(cal.Component, "PRODID") == "" || property(cal.Component, "METHOD") != "" {
		return nil, invalidCalendar()
	}
	zones := map[string]bool{}
	for _, child := range cal.Children {
		if child.Name == "VTIMEZONE" {
			id := property(child, "TZID")
			if id == "" || zones[id] {
				return nil, invalidCalendar()
			}
			zones[id] = true
		} else if child.Name != "VEVENT" {
			return nil, invalidCalendar()
		}
	}
	events := cal.Events()
	if len(events) == 0 || len(events) > maxEvents {
		return nil, invalidCalendar()
	}
	result = &calendarSummary{ICalendar: raw, EventCount: len(events), Events: make([]eventSummary, 0, len(events))}
	uid := ""
	identities := map[string]bool{}
	for _, event := range events {
		c := event.Component
		for _, name := range []string{"UID", "DTSTAMP", "DTSTART", "DTEND", "DURATION", "RECURRENCE-ID", "RRULE", "SUMMARY", "DESCRIPTION", "LOCATION"} {
			if len(c.Props[name]) > 1 {
				return nil, invalidCalendar()
			}
		}
		id, textErr := c.Props.Text("UID")
		if textErr != nil || id == "" || (uid != "" && id != uid) {
			return nil, invalidCalendar()
		}
		uid = id
		stamp := c.Props.Get("DTSTAMP")
		if stamp == nil || !strings.HasSuffix(stamp.Value, "Z") {
			return nil, invalidCalendar()
		}
		if _, _, _, e := readDate(stamp, zones); e != nil {
			return nil, e
		}
		start := c.Props.Get("DTSTART")
		if start == nil {
			return nil, invalidCalendar()
		}
		s := eventSummary{UID: id, Start: start.Value, End: property(c, "DTEND"), RecurrenceID: property(c, "RECURRENCE-ID"), RRule: property(c, "RRULE")}
		for name, dest := range map[string]*string{"SUMMARY": &s.Summary, "DESCRIPTION": &s.Description, "LOCATION": &s.Location} {
			text, err := c.Props.Text(name)
			if err != nil {
				return nil, invalidCalendar()
			}
			*dest = text
		}
		st, utc, allDay, e := readDate(start, zones)
		if e != nil {
			return nil, e
		}
		s.StartUTC = utc
		s.AllDay = allDay
		s.Timezone = start.Params.Get("TZID")
		if p := c.Props.Get("DTEND"); p != nil {
			end, endUTC, endDay, e := readDate(p, zones)
			if e != nil || endDay != allDay || c.Props.Get("DURATION") != nil {
				return nil, invalidCalendar()
			}
			if !st.IsZero() && !end.IsZero() && !end.After(st) {
				return nil, invalidCalendar()
			}
			s.EndUTC = endUTC
		}
		if p := c.Props.Get("DURATION"); p != nil {
			d, err := p.Duration()
			if err != nil || d <= 0 {
				return nil, invalidCalendar()
			}
		}
		if p := c.Props.Get("RECURRENCE-ID"); p != nil {
			_, _, day, e := readDate(p, zones)
			if e != nil || day != allDay {
				return nil, invalidCalendar()
			}
		}
		key := s.RecurrenceID
		if identities[key] {
			return nil, invalidCalendar()
		}
		identities[key] = true
		if s.RRule != "" {
			if !validRule(c) {
				return nil, invalidCalendar()
			}
		}
		result.Events = append(result.Events, s)
	}
	if !identities[""] {
		return nil, invalidCalendar()
	}
	return result, nil
}

func validRule(c *ical.Component) bool {
	option, err := c.Props.RecurrenceRule()
	if err != nil || option == nil {
		return false
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(property(c, "RRULE"), ";") {
		pair := strings.SplitN(part, "=", 2)
		if len(pair) != 2 || seen[pair[0]] {
			return false
		}
		seen[pair[0]] = true
		if pair[0] == "COUNT" || pair[0] == "INTERVAL" {
			n, err := strconv.Atoi(pair[1])
			if err != nil || n <= 0 {
				return false
			}
		}
	}
	if !seen["FREQ"] || (seen["COUNT"] && seen["UNTIL"]) {
		return false
	}
	for _, bound := range []struct {
		values    []int
		low, high int
		negative  bool
	}{
		{option.Bysecond, 0, 60, false}, {option.Byminute, 0, 59, false}, {option.Byhour, 0, 23, false},
		{option.Bymonth, 1, 12, false}, {option.Bymonthday, 1, 31, true}, {option.Byyearday, 1, 366, true},
		{option.Byweekno, 1, 53, true}, {option.Bysetpos, 1, 366, true},
	} {
		for _, n := range bound.values {
			if n >= bound.low && n <= bound.high {
				continue
			}
			if bound.negative && n <= -bound.low && n >= -bound.high {
				continue
			}
			return false
		}
	}
	return true
}

// Dates remain lossless strings. UTC is supplied only when resolution is
// unambiguous: UTC input or a known IANA TZID without overriding VTIMEZONE.
func readDate(p *ical.Prop, zones map[string]bool) (time.Time, string, bool, *fault) {
	value := p.Value
	day := p.ValueType() == ical.ValueDate
	tz := p.Params.Get("TZID")
	if day {
		t, e := time.Parse("20060102", value)
		if e != nil || tz != "" {
			return time.Time{}, "", false, invalidCalendar()
		}
		return t, "", true, nil
	}
	if p.ValueType() != ical.ValueDateTime {
		return time.Time{}, "", false, invalidCalendar()
	}
	if strings.HasSuffix(value, "Z") {
		t, e := time.Parse("20060102T150405Z", value)
		if e != nil || tz != "" {
			return time.Time{}, "", false, invalidCalendar()
		}
		return t, t.Format(time.RFC3339), false, nil
	}
	t, e := time.Parse("20060102T150405", value)
	if e != nil {
		return time.Time{}, "", false, invalidCalendar()
	}
	if tz == "" {
		return t, "", false, nil
	}
	if zones[tz] {
		return time.Time{}, "", false, nil
	}
	loc, e := time.LoadLocation(tz)
	if e != nil || tz == "Local" {
		return time.Time{}, "", false, invalidCalendar()
	}
	t, e = time.ParseInLocation("20060102T150405", value, loc)
	if e != nil {
		return time.Time{}, "", false, invalidCalendar()
	}
	// Go chooses an unspecified side of a DST fold/gap. Preserve the wall time
	// without asserting a UTC instant when that choice would be ambiguous.
	if t.In(loc).Format("20060102T150405") != value {
		return time.Time{}, "", false, nil
	}
	_, currentOffset := t.Zone()
	for _, nearby := range []time.Time{t.Add(-48 * time.Hour), t.Add(48 * time.Hour)} {
		_, offset := nearby.In(loc).Zone()
		if offset != currentOffset && t.Add(time.Duration(currentOffset-offset)*time.Second).In(loc).Format("20060102T150405") == value {
			return time.Time{}, "", false, nil
		}
	}
	return t, t.UTC().Format(time.RFC3339), false, nil
}

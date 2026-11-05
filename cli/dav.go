package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const dav = "DAV:"
const caldav = "urn:ietf:params:xml:ns:caldav"

type davProps struct {
	Principal *struct {
		Href string `xml:"DAV: href"`
	} `xml:"DAV: current-user-principal"`
	Home *struct {
		Href string `xml:"DAV: href"`
	} `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
	ResourceType *struct {
		Names []struct{ XMLName xml.Name } `xml:",any"`
	} `xml:"DAV: resourcetype"`
	Name        *string `xml:"DAV: displayname"`
	Description *string `xml:"urn:ietf:params:xml:ns:caldav calendar-description"`
	Components  *struct {
		Comp []struct {
			Name string `xml:"name,attr"`
		} `xml:"urn:ietf:params:xml:ns:caldav comp"`
	} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
	ETag *string                      `xml:"DAV: getetag"`
	Data *string                      `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
	All  []struct{ XMLName xml.Name } `xml:",any"`
}
type propstat struct {
	Prop   davProps `xml:"DAV: prop"`
	Status string   `xml:"DAV: status"`
}
type davResponse struct {
	Href      string     `xml:"DAV: href"`
	Status    string     `xml:"DAV: status"`
	Propstats []propstat `xml:"DAV: propstat"`
}
type multistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

func invalidResponse() *fault { return &fault{"invalid_response", "invalid server response", 0, 1} }

func parseStatus(s string) int {
	parts := strings.Fields(s)
	if len(parts) < 2 || (parts[0] != "HTTP/1.1" && parts[0] != "HTTP/1.0" && parts[0] != "HTTP/2") {
		return 0
	}
	n, e := strconv.Atoi(parts[1])
	if e != nil || n < 100 || n > 599 {
		return 0
	}
	return n
}

func parseMulti(resp *response) ([]davResponse, *fault) {
	if resp.status != 207 || !utf8.Valid(resp.body) {
		return nil, invalidResponse()
	}
	// No DTD/directives, excessive nesting or unbounded element counts.
	dec := xml.NewDecoder(bytes.NewReader(resp.body))
	depth, count := 0, 0
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, invalidResponse()
		}
		switch t := token.(type) {
		case xml.StartElement:
			if depth == 0 && count != 0 {
				return nil, invalidResponse()
			}
			depth++
			count++
			if depth > 24 || count > 100000 {
				return nil, invalidResponse()
			}
			if count == 1 && t.Name != (xml.Name{Space: dav, Local: "multistatus"}) {
				return nil, invalidResponse()
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return nil, invalidResponse()
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(t)) != 0 {
				return nil, invalidResponse()
			}
		}
	}
	var ms multistatus
	if xml.Unmarshal(resp.body, &ms) != nil || len(ms.Responses) > maxResources {
		return nil, invalidResponse()
	}
	for _, item := range ms.Responses {
		if item.Href == "" {
			return nil, invalidResponse()
		}
		if item.Status != "" {
			n := parseStatus(item.Status)
			if n == 0 {
				return nil, invalidResponse()
			}
			if n/100 != 2 {
				return nil, &fault{"multistatus_error", "server reported resource failure", n, 1}
			}
		}
		if len(item.Propstats) == 0 {
			return nil, invalidResponse()
		}
		for _, p := range item.Propstats {
			n := parseStatus(p.Status)
			if n == 0 {
				return nil, invalidResponse()
			}
			if n/100 != 2 {
				// Servers commonly return 404 for optional collection metadata.
				optional := p.Prop.Principal == nil && p.Prop.Home == nil && p.Prop.ETag == nil && p.Prop.Data == nil &&
					p.Prop.ResourceType == nil && len(p.Prop.All) == 0 &&
					(p.Prop.Name != nil || p.Prop.Description != nil || p.Prop.Components != nil)
				if n != 404 || !optional {
					return nil, &fault{"multistatus_error", "server reported property failure", n, 1}
				}
			}
		}
	}
	return ms.Responses, nil
}

func successProps(item davResponse) []davProps {
	result := []davProps{}
	for _, p := range item.Propstats {
		if parseStatus(p.Status)/100 == 2 {
			result = append(result, p.Prop)
		}
	}
	return result
}

func strongETag(s string) bool {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return false
	}
	for _, c := range []byte(s[1 : len(s)-1]) {
		if c < 0x21 || c == 0x22 || c == 0x7f {
			return false
		}
	}
	return true
}

func execute(ctx context.Context, r request) (any, *fault) {
	builtUID, builtCalendar := "", ""
	switch r.Operation {
	case "build":
		if r.URL != "" || r.Username != "" || r.Password != "" || r.ICalendar != "" || r.ETag != "" || r.Start != "" || r.End != "" || r.AllowInsecureLoopback {
			return nil, invalid()
		}
		return buildCalendar(r.Event)
	case "inspect":
		if r.Event != nil || r.URL != "" || r.Username != "" || r.Password != "" || r.ETag != "" || r.Start != "" || r.End != "" || r.AllowInsecureLoopback {
			return nil, invalid()
		}
		return parseCalendar(r.ICalendar)
	case "discover", "query", "get", "create", "update", "delete":
	default:
		return nil, invalid()
	}
	if r.Event != nil {
		if r.Operation != "create" || r.ICalendar != "" {
			return nil, invalid()
		}
		built, f := buildCalendar(r.Event)
		if f != nil {
			return nil, f
		}
		r.ICalendar = built.ICalendar
		builtUID = built.Events[0].UID
		builtCalendar = built.ICalendar
	}
	if r.Operation == "create" || r.Operation == "update" {
		if _, f := parseCalendar(r.ICalendar); f != nil {
			return nil, f
		}
	} else if r.ICalendar != "" {
		return nil, invalid()
	}
	if r.Operation == "update" || r.Operation == "delete" {
		if !strongETag(r.ETag) {
			return nil, invalid()
		}
	} else if r.ETag != "" {
		return nil, invalid()
	}
	var start, end time.Time
	if r.Operation == "query" {
		var err error
		start, err = time.Parse(time.RFC3339, r.Start)
		if err != nil || !strings.HasSuffix(r.Start, "Z") || start.Nanosecond() != 0 {
			return nil, invalid()
		}
		end, err = time.Parse(time.RFC3339, r.End)
		if err != nil || !strings.HasSuffix(r.End, "Z") || end.Nanosecond() != 0 || !end.After(start) {
			return nil, invalid()
		}
	} else if r.Start != "" || r.End != "" {
		return nil, invalid()
	}
	b, f := newBackend(r)
	if f != nil {
		return nil, f
	}
	defer b.client.CloseIdleConnections()
	if (r.Operation == "get" || r.Operation == "create" || r.Operation == "update" || r.Operation == "delete") && strings.HasSuffix(b.origin.Path, "/") {
		return nil, invalid()
	}
	switch r.Operation {
	case "discover":
		return b.discover(ctx)
	case "query":
		return b.query(ctx, start, end)
	case "get":
		resp, f := b.do(ctx, "GET", b.origin, "", map[string]string{"Accept": "text/calendar"})
		if f != nil {
			return nil, f
		}
		if resp.status != 200 {
			return nil, invalidResponse()
		}
		return resourceFrom(resp.url, resp.header.Get("ETag"), string(resp.body))
	case "create", "update":
		headers := map[string]string{"Content-Type": "text/calendar; charset=utf-8"}
		if r.Operation == "create" {
			headers["If-None-Match"] = "*"
		} else {
			headers["If-Match"] = r.ETag
		}
		resp, f := b.do(ctx, "PUT", b.origin, r.ICalendar, headers)
		if f != nil {
			return nil, f
		}
		if resp.status != 200 && resp.status != 201 && resp.status != 204 {
			return nil, invalidResponse()
		}
		return struct {
			URL       string `json:"url"`
			ETag      string `json:"etag"`
			UID       string `json:"uid,omitempty"`
			ICalendar string `json:"icalendar,omitempty"`
		}{resp.url.String(), resp.header.Get("ETag"), builtUID, builtCalendar}, nil
	case "delete":
		resp, f := b.do(ctx, "DELETE", b.origin, "", map[string]string{"If-Match": r.ETag})
		if f != nil {
			return nil, f
		}
		if resp.status != 200 && resp.status != 204 {
			return nil, invalidResponse()
		}
		return struct {
			URL     string `json:"url"`
			Deleted bool   `json:"deleted"`
		}{resp.url.String(), true}, nil
	}
	return nil, invalid()
}

func (b *backend) propfind(ctx context.Context, u *url.URL, depth, props string) (*response, []davResponse, *fault) {
	body := `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop>` + props + `</d:prop></d:propfind>`
	resp, f := b.do(ctx, "PROPFIND", u, body, map[string]string{"Content-Type": "application/xml; charset=utf-8", "Depth": depth})
	if f != nil {
		return nil, nil, f
	}
	items, f := parseMulti(resp)
	return resp, items, f
}

type collection struct {
	URL         string   `json:"url"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Components  []string `json:"components"`
}

func (b *backend) discover(ctx context.Context) (any, *fault) {
	resp, items, f := b.propfind(ctx, b.origin, "0", `<d:current-user-principal/>`)
	if f != nil {
		return nil, f
	}
	principal := ""
	if len(items) != 1 {
		return nil, invalidResponse()
	}
	for _, p := range successProps(items[0]) {
		if p.Principal != nil {
			principal = p.Principal.Href
		}
	}
	u, f := b.resolve(resp.url, principal)
	if f != nil {
		return nil, f
	}
	principalURL := u.String()
	resp, items, f = b.propfind(ctx, u, "0", `<c:calendar-home-set/>`)
	if f != nil {
		return nil, f
	}
	home := ""
	if len(items) != 1 {
		return nil, invalidResponse()
	}
	for _, p := range successProps(items[0]) {
		if p.Home != nil {
			home = p.Home.Href
		}
	}
	u, f = b.resolve(resp.url, home)
	if f != nil {
		return nil, f
	}
	homeURL := u.String()
	resp, items, f = b.propfind(ctx, u, "1", `<d:resourcetype/><d:displayname/><c:calendar-description/><c:supported-calendar-component-set/>`)
	if f != nil {
		return nil, f
	}
	calendars := []collection{}
	seen := map[string]bool{}
	for _, item := range items {
		c := collection{Components: []string{}}
		isCalendar := false
		hasResourceType := false
		for _, p := range successProps(item) {
			if p.ResourceType != nil {
				if hasResourceType {
					return nil, invalidResponse()
				}
				hasResourceType = true
				for _, name := range p.ResourceType.Names {
					if name.XMLName == (xml.Name{Space: caldav, Local: "calendar"}) {
						isCalendar = true
					}
				}
			}
			if p.Name != nil {
				c.Name = *p.Name
			}
			if p.Description != nil {
				c.Description = *p.Description
			}
			if p.Components != nil {
				for _, comp := range p.Components.Comp {
					c.Components = append(c.Components, comp.Name)
				}
			}
		}
		if !hasResourceType {
			return nil, invalidResponse()
		}
		target, f := b.resolve(resp.url, item.Href)
		if f != nil {
			return nil, f
		}
		if !isCalendar {
			continue
		}
		c.URL = target.String()
		if seen[c.URL] {
			return nil, invalidResponse()
		}
		seen[c.URL] = true
		calendars = append(calendars, c)
	}
	return struct {
		PrincipalURL string       `json:"principalURL"`
		HomeURL      string       `json:"homeURL"`
		Calendars    []collection `json:"calendars"`
	}{principalURL, homeURL, calendars}, nil
}

type resource struct {
	URL  string `json:"url"`
	ETag string `json:"etag"`
	*calendarSummary
}

func resourceFrom(u *url.URL, etag, raw string) (*resource, *fault) {
	summary, f := parseCalendar(raw)
	if f != nil {
		return nil, remoteCalendar()
	}
	return &resource{u.String(), etag, summary}, nil
}

func (b *backend) query(ctx context.Context, start, end time.Time) (any, *fault) {
	body := `<?xml version="1.0" encoding="utf-8"?><c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/><c:calendar-data/></d:prop><c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="` +
		start.Format("20060102T150405Z") + `" end="` + end.Format("20060102T150405Z") + `"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	resp, f := b.do(ctx, "REPORT", b.origin, body, map[string]string{"Content-Type": "application/xml; charset=utf-8", "Depth": "1"})
	if f != nil {
		return nil, f
	}
	items, f := parseMulti(resp)
	if f != nil {
		return nil, f
	}
	resources := []*resource{}
	seen := map[string]bool{}
	totalEvents := 0
	for _, item := range items {
		var data, etag *string
		for _, p := range successProps(item) {
			if p.Data != nil {
				if data != nil {
					return nil, invalidResponse()
				}
				data = p.Data
			}
			if p.ETag != nil {
				if etag != nil {
					return nil, invalidResponse()
				}
				etag = p.ETag
			}
		}
		if data == nil || etag == nil {
			return nil, invalidResponse()
		}
		u, f := b.resolve(resp.url, item.Href)
		if f != nil {
			return nil, f
		}
		if seen[u.String()] {
			return nil, invalidResponse()
		}
		seen[u.String()] = true
		res, f := resourceFrom(u, *etag, *data)
		if f != nil {
			return nil, f
		}
		totalEvents += res.EventCount
		if totalEvents > maxEvents {
			return nil, &fault{"response_limit", "server response limit exceeded", 0, 1}
		}
		resources = append(resources, res)
	}
	return struct {
		Resources []*resource `json:"resources"`
	}{resources}, nil
}

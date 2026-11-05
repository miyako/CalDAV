package main

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var icloudShard = regexp.MustCompile(`^p[0-9]{2,3}-caldav\.icloud\.com$`)

type backend struct {
	client             *http.Client
	origin             *url.URL
	username, password string
	loopback           bool
	shard              string
	budget             int64
}

func checkedURL(raw string, loopback bool) (*url.URL, *fault) {
	if strings.ContainsAny(raw, "\\") || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return nil, invalid()
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
		return nil, invalid()
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !loopback || ip == nil || !ip.IsLoopback() {
			return nil, invalid()
		}
	}
	if u.Hostname() == "" || strings.HasSuffix(u.Hostname(), ".") {
		return nil, invalid()
	}
	if p := u.Port(); p != "" {
		n, e := net.LookupPort("tcp", p)
		if e != nil || n < 1 || n > 65535 {
			return nil, invalid()
		}
	}
	if strings.IndexFunc(u.Path, unicode.IsControl) >= 0 || strings.Contains(u.Path, "\\") {
		return nil, invalid()
	}
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
func sameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func newBackend(r request) (*backend, *fault) {
	u, f := checkedURL(r.URL, r.AllowInsecureLoopback)
	if f != nil {
		return nil, f
	}
	if strings.Contains(r.Username, ":") || ((r.Username == "") != (r.Password == "")) {
		return nil, invalid()
	}
	tr := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}).DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		DisableCompression:     true,
	}
	return &backend{
		client: &http.Client{Transport: tr, Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		origin: u, username: r.Username, password: r.Password, loopback: r.AllowInsecureLoopback, budget: maxResponse,
	}, nil
}

func (b *backend) allowed(u *url.URL) bool {
	if sameOrigin(b.origin, u) {
		return true
	}
	if b.origin.Scheme == "https" && b.origin.Hostname() == "caldav.icloud.com" && effectivePort(b.origin) == "443" &&
		u.Scheme == "https" && effectivePort(u) == "443" && icloudShard.MatchString(u.Hostname()) {
		return b.shard == "" || b.shard == u.Hostname()
	}
	return false
}

func (b *backend) resolve(base *url.URL, href string) (*url.URL, *fault) {
	href = strings.TrimSpace(href)
	ref, err := url.Parse(href)
	if err != nil || href == "" || ref.User != nil || ref.Fragment != "" || strings.Contains(href, "\\") || strings.IndexFunc(href, unicode.IsControl) >= 0 {
		return nil, &fault{"unsafe_url", "unsafe server URL", 0, 1}
	}
	u, f := checkedURL(base.ResolveReference(ref).String(), b.loopback)
	if f != nil || !b.allowed(u) {
		return nil, &fault{"unsafe_url", "unsafe server URL", 0, 1}
	}
	return u, nil
}

type response struct {
	url    *url.URL
	status int
	header http.Header
	body   []byte
}

func statusFault(status int) *fault {
	code := "server_error"
	switch status {
	case 401, 403:
		code = "authentication_failed"
	case 404:
		code = "not_found"
	case 409, 412:
		code = "conflict"
	case 429:
		code = "rate_limited"
	}
	return &fault{code, "server rejected request", status, 1}
}

func (b *backend) do(ctx context.Context, method string, u *url.URL, body string, headers map[string]string) (*response, *fault) {
	for redirects := 0; ; redirects++ {
		if !b.allowed(u) {
			return nil, &fault{"unsafe_url", "unsafe server URL", 0, 1}
		}
		if u.Hostname() != b.origin.Hostname() && icloudShard.MatchString(u.Hostname()) {
			b.shard = u.Hostname()
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(body))
		if err != nil {
			return nil, networkError()
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("User-Agent", "CalDAV/"+version)
		if b.username != "" {
			req.SetBasicAuth(b.username, b.password)
		}
		resp, err := b.client.Do(req)
		if err != nil {
			return nil, networkError()
		}
		if resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 307 || resp.StatusCode == 308 {
			resp.Body.Close()
			if redirects >= 5 || method == "PUT" || method == "DELETE" {
				return nil, &fault{"unsafe_redirect", "redirect rejected", 0, 1}
			}
			next, f := b.resolve(u, resp.Header.Get("Location"))
			if f != nil {
				return nil, f
			}
			u = next
			continue
		}
		if resp.StatusCode/100 != 2 {
			resp.Body.Close()
			return nil, statusFault(resp.StatusCode)
		}
		if resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
			resp.Body.Close()
			return nil, &fault{"invalid_response", "invalid server response", 0, 1}
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, b.budget+1))
		resp.Body.Close()
		if err != nil {
			return nil, networkError()
		}
		b.budget -= int64(len(data))
		if b.budget < 0 {
			return nil, &fault{"response_limit", "server response limit exceeded", 0, 1}
		}
		return &response{u, resp.StatusCode, resp.Header, data}, nil
	}
}

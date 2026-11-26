package primitive

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// maxBody caps how much of a response is read.
const maxBody = 1 << 20

type httpRequest struct{}

func (httpRequest) Spec() Spec {
	return Spec{
		Name: "http.request",
		Summary: "Makes an HTTP request from this computer and checks the status and body. " +
			"Only loopback addresses are allowed, so use a port the node publishes on 127.0.0.1. " +
			"A request that changes state is not safe to repeat.",
		Timeout: 15 * time.Second,
		Changes: true,
		Args: []Arg{
			{Name: "method", Type: String, Default: "GET", Help: "GET, HEAD, POST, PUT, PATCH or DELETE."},
			{Name: "url", Type: String, Required: true, Help: "The address. The host must be localhost, 127.0.0.1 or ::1."},
			{Name: "headers", Type: Map, Help: "Request headers."},
			{Name: "body", Type: String, Help: "The request body."},
			{Name: "expectStatus", Type: Int, Default: 200, Help: "The status code that counts as a pass."},
			{Name: "contains", Type: String, Help: "If set, the response body must contain this text."},
			argSave,
		},
	}
}

func (httpRequest) Run(ctx context.Context, _ Env, a Args) (Result, error) {
	method := strings.ToUpper(a.String("method"))
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE":
	default:
		return Result{}, fmt.Errorf("method %q is not one of GET, HEAD, POST, PUT, PATCH, DELETE", a.String("method"))
	}
	target, err := loopbackURL(a.String("url"))
	if err != nil {
		return Result{}, err
	}

	var body io.Reader
	if b := a.String("body"); b != "" {
		body = strings.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	headerNames := make([]string, 0)
	for k := range a.Map("headers") {
		headerNames = append(headerNames, k)
	}
	sort.Strings(headerNames)
	for _, k := range headerNames {
		req.Header.Set(k, fmt.Sprint(a.Map("headers")[k]))
	}

	client := &http.Client{CheckRedirect: func(next *http.Request, _ []*http.Request) error {
		if _, err := loopbackURL(next.URL.String()); err != nil {
			return http.ErrUseLastResponse // never follow a redirect off this computer
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%s %s: %w", method, target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Result{}, fmt.Errorf("read the response from %s: %w", target, err)
	}

	res := Result{Evidence: truncate(fmt.Sprintf("%s\n\n%s", resp.Status, data))}
	if name := a.String("save"); name != "" {
		res.Outputs = map[string]string{name: strings.TrimSpace(string(data))}
	}
	switch want := a.Int("expectStatus"); {
	case resp.StatusCode != want:
		res.Status, res.Message = Fail, fmt.Sprintf("%s returned %d, expected %d", target, resp.StatusCode, want)
	case a.String("contains") != "" && !strings.Contains(string(data), a.String("contains")):
		res.Status, res.Message = Fail, fmt.Sprintf("the response from %s does not contain %q", target, a.String("contains"))
	default:
		res.Status, res.Message = Pass, fmt.Sprintf("%s returned %d", target, resp.StatusCode)
	}
	return res, nil
}

// loopbackURL parses a URL and refuses any host that is not this computer. A
// scenario file may be written by a user, so it must not reach other machines
// (R-QUAL-4, R-APP-3).
func loopbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("url %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url %q must start with http:// or https://", raw)
	}
	host := u.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("url %q: only loopback addresses (localhost, 127.0.0.1, ::1) are allowed", raw)
		}
	}
	return u, nil
}

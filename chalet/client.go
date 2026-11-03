// Package chalet speaks to the Chalet JSON API: the same URLs as the pages,
// asked for JSON, with a personal access token as the bearer header.
package chalet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is one account in one Chalet. Paths are account-relative ("/my");
// the client puts the account prefix in front, as the browser's URLs carry it.
type Client struct {
	BaseURL   string
	Account   string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

// Response is an answer as it came: the status, the headers and the body.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// OK reports a 2xx.
func (r *Response) OK() bool {
	return r.Status >= 200 && r.Status < 300
}

// Error is the app's own sentence for a refusal — every refusal carries a
// JSON body — or the status text when there is none.
func (r *Response) Error() string {
	var refusal struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(r.Body, &refusal) == nil && refusal.Error != "" {
		return fmt.Sprintf("%d %s", r.Status, refusal.Error)
	}
	return fmt.Sprintf("%d %s", r.Status, http.StatusText(r.Status))
}

// CheckBaseURL accepts https anywhere and http only on this machine: a token
// never travels in the clear over a network.
func CheckBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not a URL like https://chalet.example", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if host := u.Hostname(); host == "localhost" || net.ParseIP(host).IsLoopback() {
			return nil
		}
		return fmt.Errorf("%s: http is only for localhost; use https", raw)
	default:
		return fmt.Errorf("%s: use an https URL", raw)
	}
}

// Do sends one request and never sends it again: a write retried after an
// unclear failure can happen twice (api-spec §14.3).
//
// The body is a map, never an `any`: a nil map inside an `any` is not nil,
// and went out as a JSON null on every call without fields — a key the app
// refuses, so every read failed.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body map[string]any, header http.Header) (*Response, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path, query), payload)
	if err != nil {
		return nil, err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	return c.send(c.httpClient(), req)
}

// Fetch reads a file the app links to: a preview_url or a download_url from
// its answers. The token goes to this Chalet and nowhere else — the app
// answers with a redirect to the file in storage, which is followed without
// it.
func (c *Client) Fetch(ctx context.Context, link string) (*Response, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	target, err := base.Parse(link)
	if err != nil || target.Scheme != base.Scheme || target.Host != base.Host {
		return nil, fmt.Errorf("%q is not a link into Chalet at %s", link, c.BaseURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	client := *c.httpClient()
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		next.Header.Del("Authorization")
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return c.send(&client, req)
}

func (c *Client) send(client *http.Client, req *http.Request) (*Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Chalet at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: data}, nil
}

func (c *Client) url(path string, query url.Values) string {
	u := strings.TrimRight(c.BaseURL, "/")
	if c.Account != "" {
		u += "/" + c.Account
	}
	u += path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// No redirects: a JSON answer never redirects, and following one could carry
// the token somewhere it was not meant to go.
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

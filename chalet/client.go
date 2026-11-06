// Package chalet speaks to the Chalet JSON API: the same URLs as the pages,
// asked for JSON, with a token as the bearer header — a personal access
// token, the short-lived one a server trades a person's token for, or an
// agent's, which its connection buys an hour at a time.
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

	"github.com/basecamp/mcp/auth/upstream"
	"golang.org/x/oauth2"
)

// Client is one account in one Chalet. Paths are account-relative ("/my");
// the client puts the account prefix in front, as the browser's URLs carry it.
// The token is Token, or, when Tokens is set, the one Tokens holds at each
// request: an agent's connection buys a token of an hour, and buys the next
// before the hour ends.
type Client struct {
	BaseURL   string
	Account   string
	Token     string
	Tokens    oauth2.TokenSource
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
// JSON body — or the status text when there is none. The sentence is scrubbed
// (upstream.ScrubDescription): an answer that is not the app's own could quote
// the request's token back, and this text reaches the agent.
func (r *Response) Error() string {
	var refusal struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(r.Body, &refusal) == nil && refusal.Error != "" {
		return fmt.Sprintf("%d %s", r.Status, upstream.ScrubDescription(refusal.Error))
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
	token, refused, err := c.bearer()
	if refused != nil || err != nil {
		return refused, err
	}

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
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.send(c.httpClient(), req)
	if err != nil {
		return nil, err
	}
	resp.Body = withoutToken(resp.Body, token)
	return resp, nil
}

// bearer is the token for the next request. When Tokens cannot buy one, the
// token endpoint's no is the request's answer, so that a connection that is
// gone reads as any refused sign-in does: a 401, whatever 4xx the endpoint
// gave (OAuth answers a secret sent in the body with a 400), save its 429.
// Its words are the error code alone, scrubbed: an answer that is not the
// app's own could quote the secret back.
func (c *Client) bearer() (string, *Response, error) {
	if c.Tokens == nil {
		return c.Token, nil, nil
	}
	token, err := c.Tokens.Token()
	var refused *oauth2.RetrieveError
	switch {
	case errors.As(err, &refused):
		status := refused.Response.StatusCode
		if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
			status = http.StatusUnauthorized
		}
		body, _ := json.Marshal(map[string]string{"error": upstream.ScrubDescription(refused.ErrorCode)})
		return "", &Response{Status: status, Body: body}, nil
	case err != nil:
		return "", nil, unanswered{"could not get a token from Chalet at " + c.BaseURL, err}
	}
	return token.AccessToken, nil, nil
}

// withoutToken cuts the request's own token out of an answer before anything
// reads it: a 422 that echoes the request, or any answer that is not the app's
// own, would otherwise hand it on in a hint or a confirm sentence. Only this
// token is cut, by its exact bytes, so the app's own words stay whole — a
// confirm link or an sgid, which a scrub by shape would break.
func withoutToken(body []byte, token string) []byte {
	if token == "" {
		return body
	}
	return bytes.ReplaceAll(body, []byte(token), []byte("[redacted]"))
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

	token, refused, err := c.bearer()
	if refused != nil || err != nil {
		return refused, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
		return nil, unanswered{"could not reach Chalet at " + c.BaseURL, err}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, unanswered{"could not read the answer of Chalet at " + c.BaseURL, err}
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: data}, nil
}

// unanswered is a request with no answer it could read. A transport's or a
// body's error can quote what the other end sent — a status or header line, a
// trailer — and with it the request's own token, so its text is scrubbed and
// bounded (upstream.ScrubDescription) before any sink sees it, the agent
// included. errors.As and errors.Is still reach the cause.
type unanswered struct {
	what  string
	cause error
}

func (e unanswered) Error() string {
	return e.what + ": " + upstream.ScrubDescription(e.cause.Error())
}

func (e unanswered) Unwrap() error { return e.cause }

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

package chalet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/basecamp/mcp/auth/upstream/upstreamtest"
)

// An answer that is not the app's own can quote the request's token back — in
// a status or header line, a trailer, a refusal's sentence — and the text the
// client hands on reaches the agent: none of it may carry the token.
func TestAHostileAnswerNeverHandsTheTokenOn(t *testing.T) {
	token := "chalet_dt_" + strings.Repeat("7kQ2mVx9Rt", 5)[:44]
	for _, kind := range upstreamtest.Kinds {
		t.Run(string(kind), func(t *testing.T) {
			server := httptest.NewServer(upstreamtest.Handler(kind))
			defer server.Close()

			resp, err := (&Client{BaseURL: server.URL, Token: token}).Do(context.Background(), "GET", "/my", nil, nil, nil)
			text := ""
			if err != nil {
				text = err.Error()
			} else {
				text = resp.Error()
			}
			upstreamtest.AssertClean(t, string(kind), text, token, upstreamtest.Planted)
		})
	}
}

// A body goes on whole, as a refusal's hint or a confirm sentence, so an
// answer that echoes the request — a 422, a 428 — loses the token, and only
// it: a confirm link's long signature stays as the app wrote it.
func TestAnAnswerNeverHandsTheTokenOnInItsBody(t *testing.T) {
	token := "chalet_dt_" + strings.Repeat("7kQ2mVx9Rt", 5)[:44]
	link := "https://chalet.example/1/agent_confirmations/new?confirmation=eyJfcmFpbHMiOnsiZGF0YSI6IjAxOGNm--3f2a9c0b"
	for _, status := range []int{http.StatusUnprocessableEntity, http.StatusPreconditionRequired} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":"refused","echo":%q,"confirm_url":%q}`, r.Header.Get("Authorization"), link)
		}))
		resp, err := (&Client{BaseURL: server.URL, Token: token}).Do(context.Background(), "POST", "/todos", nil, nil, nil)
		server.Close()
		if err != nil {
			t.Fatalf("%d: %v", status, err)
		}
		body := string(resp.Body)
		if strings.Contains(body, token) {
			t.Errorf("%d: the token reached the body: %s", status, body)
		}
		if !strings.Contains(body, "Bearer [redacted]") || !strings.Contains(body, link) {
			t.Errorf("%d: the body lost more than the token: %s", status, body)
		}
	}
}

func TestHTTPOnlyForThisMachine(t *testing.T) {
	for _, ok := range []string{"https://chalet.example", "http://localhost:3007", "http://127.0.0.1:3007"} {
		if err := CheckBaseURL(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, refused := range []string{"http://chalet.example", "ftp://chalet.example", "chalet.example"} {
		if CheckBaseURL(refused) == nil {
			t.Errorf("%s must be refused", refused)
		}
	}
}

func TestTheAccountPrefixAndTheBearer(t *testing.T) {
	var got *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL + "/", Account: "1", Token: "chalet_pat_x"}
	if _, err := client.Do(context.Background(), "POST", "/todos/a/completion", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if got.URL.Path != "/1/todos/a/completion" || got.Header.Get("Authorization") != "Bearer chalet_pat_x" || got.Header.Get("Accept") != "application/json" {
		t.Fatalf("sent %s %s %v", got.Method, got.URL.Path, got.Header)
	}
}

func TestARedirectIsNotFollowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
	}))
	defer server.Close()

	resp, err := (&Client{BaseURL: server.URL, Token: "chalet_pat_x"}).Do(context.Background(), "GET", "/my", nil, nil, nil)
	if err != nil || resp.Status != http.StatusFound {
		t.Fatalf("got %v, %v", resp, err)
	}
}

// The identity is asked without the account in the path, and a refusal keeps
// the app's answer, so that a caller can tell one refusal from another.
func TestTheIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path != "/my/identity":
			w.WriteHeader(http.StatusNotFound)
		case r.Header.Get("Authorization") != "Bearer chalet_pat_x":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":"AI connections are off for this account"}`)
		default:
			io.WriteString(w, `{"email_address":"anna@example.com","accounts":[{"id":2,"name":"submissio","url":"https://chalet.test/2/"}]}`)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Account: "2", Token: "chalet_pat_x"}
	if who, err := client.Identity(context.Background()); err != nil || who.EmailAddress != "anna@example.com" || who.Accounts[0].Name != "submissio" {
		t.Fatalf("identity = %+v, %v", who, err)
	}

	client.Token = "chalet_pat_y"
	var refused Refusal
	if _, err := client.Identity(context.Background()); !errors.As(err, &refused) || refused.Status != http.StatusForbidden || !strings.Contains(err.Error(), "AI connections are off") {
		t.Fatalf("a refusal = %v", err)
	}
}

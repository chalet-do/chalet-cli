package chalet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

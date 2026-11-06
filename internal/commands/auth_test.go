package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/chalet-do/chalet-cli/internal/config"
)

// An agent signs in with its connection's client ID and secret from one pipe,
// and is named by its name and its sponsor's, never its hidden address. Each
// process buys a token of an hour and keeps it, and buys the next before the
// hour ends. A runtime passes the connection in the environment instead.
func TestAnAgentSignsIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CHALET_NO_KEYRING", "1")

	mints, expiresIn, secret := 0, 3600, "chalet_cs_secret"
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			if r.FormValue("grant_type") != "client_credentials" || r.FormValue("client_id") != "client-1" ||
				r.FormValue("client_secret") != secret || r.Header.Get("Authorization") != "" {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"error":"invalid_client","error_description":"Client authentication failed"}`)
				return
			}
			mints++
			fmt.Fprintf(w, `{"access_token":"chalet_dt_%d","token_type":"Bearer","expires_in":%d,"scope":"read write"}`, mints, expiresIn)
		case r.Header.Get("Authorization") != fmt.Sprintf("Bearer chalet_dt_%d", mints):
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"Unauthorized"}`)
		case r.URL.Path == "/my/identity":
			io.WriteString(w, `{"email_address":"0f3c@agents.invalid","accounts":[{"id":2,"name":"Acme","url":"https://chalet.test/2/",`+
				`"user":{"id":"u1","name":"Claude","role":"agent","sponsor":{"id":"u2","name":"Nina"}}}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer app.Close()

	out, err := run(t, loginCommand(), "client-1\n"+secret+"\n", "--agent", "--url", app.URL)
	if err != nil || !strings.Contains(out, "Signed in to Acme as Claude (agent). Profile: default.") {
		t.Fatalf("login = %q, %v", out, err)
	}

	out, err = run(t, statusCommand(), "")
	if err != nil || !strings.Contains(out, "account 2, as Claude, agent of Nina.") || strings.Contains(out, "agents.invalid") {
		t.Fatalf("status = %q, %v", out, err)
	}

	// Two calls in one process, then two with less than five minutes left.
	for _, c := range []struct{ expiresIn, mints int }{{3600, 1}, {299, 2}} {
		expiresIn, mints = c.expiresIn, 0
		_, client, err := client(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if _, err := client.Identity(context.Background()); err != nil {
				t.Fatalf("expires in %d: %v", c.expiresIn, err)
			}
		}
		if mints != c.mints {
			t.Errorf("a token that expires in %ds was bought %d times for two calls", c.expiresIn, mints)
		}
	}

	// The same connection, from the environment, in the agent's own account.
	defer func(url string) { config.DefaultURL = url }(config.DefaultURL)
	config.DefaultURL = app.URL
	t.Setenv("CHALET_CLIENT_ID", "client-1")
	t.Setenv("CHALET_CLIENT_SECRET", secret)
	if p, _, err := client(context.Background()); err != nil || p.Name != "environment" || p.Account != "2" {
		t.Fatalf("from the environment: %+v, %v", p, err)
	}

	// Disconnected, the secret buys nothing, and it is never shown.
	secret = "chalet_cs_new"
	t.Setenv("CHALET_CLIENT_ID", "")
	out, err = run(t, statusCommand(), "")
	if err == nil || !strings.Contains(err.Error(), "401 invalid_client") || strings.Contains(out+err.Error(), "chalet_cs_") {
		t.Fatalf("disconnected: %q, %v", out, err)
	}
}

func run(t *testing.T, cmd *cobra.Command, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

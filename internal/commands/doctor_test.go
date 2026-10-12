package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chalet-do/chalet-cli/internal/config"
)

func TestDoctor(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CHALET_NO_KEYRING", "1")

	tokenWorks := true
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !tokenWorks || r.Header.Get("Authorization") != "Bearer chalet_pat_test" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"Unauthorized"}`)
			return
		}
		switch r.URL.Path {
		case "/my/identity":
			io.WriteString(w, `{"email_address":"anna@example.com","accounts":[]}`)
		case "/1/agent_catalog":
			io.WriteString(w, `{"version":"1.0","operations":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer app.Close()

	githubUp := true
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !githubUp {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"tag_name":"v1.2.3"}`)
	}))
	defer github.Close()
	latestReleaseURL = github.URL

	defer func(v string) { Version = v }(Version)
	Version = "1.2.3"

	if err := config.Save(&config.Profile{Name: "default", BaseURL: app.URL, Account: "1"}, "chalet_pat_test"); err != nil {
		t.Fatal(err)
	}

	out, err := doctor(t)
	if err != nil {
		t.Fatalf("all well, yet: %v\n%s", err, out)
	}
	for _, line := range []string{
		"ok       profile  default: " + app.URL + ", account 1",
		"ok       token    signed in as anna@example.com",
		"ok       catalog  version 1.0, 0 operations",
		"ok       keychain the token is kept in a file",
		"ok       version  chalet 1.2.3 is the latest release",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("no %q in\n%s", line, out)
		}
	}

	// A refused token is a problem; GitHub away is only a warning. Neither
	// prints the token.
	tokenWorks, githubUp = false, false
	out, err = doctor(t, "--json")
	if err == nil {
		t.Fatalf("a refused token must fail doctor:\n%s", out)
	}
	var checks []check
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		t.Fatalf("--json printed %q: %v", out, err)
	}
	status := map[string]string{}
	for _, c := range checks {
		status[c.Name] = c.Status
	}
	if status["token"] != "problem" || status["catalog"] != "problem" || status["version"] != "warning" || status["profile"] != "ok" {
		t.Errorf("statuses = %v", status)
	}
	if strings.Contains(out, "chalet_pat_test") {
		t.Error("doctor printed the token")
	}
}

func doctor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := doctorCommand()
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

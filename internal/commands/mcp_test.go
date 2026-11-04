package commands

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chalet-do/chalet-cli/chalet"
)

func TestAccount(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/my/identity" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		io.WriteString(w, `{"email_address":"anna@example.com","accounts":[`+
			`{"id":1,"name":"Personal","url":"https://chalet.test/1/"},`+
			`{"id":2,"name":"submissio","url":"https://chalet.test/2/"}]}`)
	}))
	defer app.Close()

	client := &chalet.Client{BaseURL: app.URL, Account: "2", Token: "chalet_pat_test"}
	if a, err := account(context.Background(), client); err != nil || a.Name != "submissio" || a.URL != "https://chalet.test/2/" {
		t.Fatalf("account = %+v, %v", a, err)
	}

	client.Account = "3"
	if _, err := account(context.Background(), client); err == nil {
		t.Error("an account the token does not reach has no name to give")
	}
}

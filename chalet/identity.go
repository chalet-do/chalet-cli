package chalet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Identity is who a token belongs to and the accounts it reaches, as GET
// /my/identity answers: every account a personal access token reaches, or
// the one account a grant was given for.
type Identity struct {
	EmailAddress string    `json:"email_address"`
	Accounts     []Account `json:"accounts"`
}

// Account is one account a token reaches.
type Account struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	User struct {
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"user"`
}

// Refusal is the app's no, with its answer kept, so that a caller can tell
// one refusal from another.
type Refusal struct{ *Response }

func (r Refusal) Error() string { return "Chalet refused the token: " + r.Response.Error() }

// Identity asks who the token belongs to, and where. It asks without the
// account in the path: a token is the identity's, not one account's.
func (c *Client) Identity(ctx context.Context) (*Identity, error) {
	unscoped := *c
	unscoped.Account = ""
	resp, err := unscoped.Do(ctx, http.MethodGet, "/my/identity", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if !resp.OK() {
		return nil, Refusal{resp}
	}
	var who Identity
	if err := json.Unmarshal(resp.Body, &who); err != nil {
		return nil, fmt.Errorf("unexpected answer from %s: %w", c.BaseURL, err)
	}
	return &who, nil
}

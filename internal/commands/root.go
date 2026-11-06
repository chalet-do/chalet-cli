// Package commands is chalet's command tree.
package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/chalet-do/chalet-cli/chalet"
	"github.com/chalet-do/chalet-cli/internal/config"
)

// Version is set at build time; `go install` leaves it at dev.
var Version = "dev"

var profileFlag string

// Execute runs the command line and returns the exit code.
func Execute() int {
	root := &cobra.Command{
		Use:           "chalet",
		Short:         "Chalet from the command line, and for Claude",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&profileFlag, "profile", "", "which Chalet to use (default: the profile you signed in with first)")
	root.AddCommand(authCommand(), mcpCommand(), doctorCommand())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "chalet:", err)
		return 1
	}
	return 0
}

// client signs in as the resolved profile, or as the agent whose connection
// the environment gives (config.FromEnvironment). The URL is checked on every
// use, not only at login: an edited profile must not carry the token in the
// clear.
func client(ctx context.Context) (*config.Profile, *chalet.Client, error) {
	p, creds, fromEnvironment := config.FromEnvironment()
	if !fromEnvironment {
		var err error
		if p, err = config.Resolve(profileFlag); err != nil {
			return nil, nil, err
		}
		if creds, err = p.Credentials(); err != nil {
			return nil, nil, err
		}
	}
	if err := chalet.CheckBaseURL(p.BaseURL); err != nil {
		return nil, nil, fmt.Errorf("profile %s: %w", p.Name, err)
	}

	c := signedIn(ctx, p.BaseURL, p.Account, creds)
	if fromEnvironment {
		// No profile names the account: an agent is in one, its identity's.
		who, err := c.Identity(ctx)
		if err == nil && len(who.Accounts) == 0 {
			err = errors.New("the connection reaches no account")
		}
		if err != nil {
			return nil, nil, fmt.Errorf("CHALET_CLIENT_ID and CHALET_CLIENT_SECRET: %w", err)
		}
		p.Account = fmt.Sprint(who.Accounts[0].ID)
		c.Account = p.Account
	}
	return p, c, nil
}

// signedIn is a client with these credentials: a personal token as it is, or
// an agent's connection, whose client ID and secret buy a token of an hour at
// Chalet's token endpoint (RFC 6749 §4.4, in the body, as the connection is
// registered), and the next five minutes before the hour ends, so that
// `chalet mcp` serves for days. The trade follows no redirect: one could
// carry the secret elsewhere.
func signedIn(ctx context.Context, baseURL, account string, creds config.Credentials) *chalet.Client {
	c := &chalet.Client{BaseURL: baseURL, Account: account, Token: creds.Token, UserAgent: "chalet-cli/" + Version}
	if creds.ClientID != "" {
		trade := &clientcredentials.Config{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			TokenURL:     strings.TrimRight(baseURL, "/") + "/oauth/token",
			AuthStyle:    oauth2.AuthStyleInParams,
		}
		ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		})
		c.Tokens = oauth2.ReuseTokenSourceWithExpiry(nil, trade.TokenSource(ctx), 5*time.Minute)
	}
	return c
}

// An agent's connection buys its own tokens; a person's token is given.
func isAgent(c *chalet.Client) bool {
	return c.Tokens != nil
}

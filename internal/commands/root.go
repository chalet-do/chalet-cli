// Package commands is chalet's command tree.
package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sschuez/chalet-cli/internal/chalet"
	"github.com/sschuez/chalet-cli/internal/config"
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
	root.AddCommand(authCommand(), mcpCommand())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "chalet:", err)
		return 1
	}
	return 0
}

// client signs in as the resolved profile. The URL is checked on every use,
// not only at login: an edited profile must not carry the token in the clear.
func client() (*config.Profile, *chalet.Client, error) {
	p, err := config.Resolve(profileFlag)
	if err != nil {
		return nil, nil, err
	}
	if err := chalet.CheckBaseURL(p.BaseURL); err != nil {
		return nil, nil, fmt.Errorf("profile %s: %w", p.Name, err)
	}
	token, err := p.Token()
	if err != nil {
		return nil, nil, err
	}
	return p, &chalet.Client{BaseURL: p.BaseURL, Account: p.Account, Token: token, UserAgent: "chalet-cli/" + Version}, nil
}

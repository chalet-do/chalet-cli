package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/chalet-do/chalet-cli/internal/chalet"
	"github.com/chalet-do/chalet-cli/internal/config"
)

func authCommand() *cobra.Command {
	auth := &cobra.Command{Use: "auth", Short: "Sign in to Chalet with a personal access token"}
	auth.AddCommand(loginCommand(), statusCommand(), logoutCommand())
	return auth
}

// identity is GET /my/identity.json: who a token belongs to, and where.
type identity struct {
	EmailAddress string `json:"email_address"`
	Accounts     []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		User struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"user"`
	} `json:"accounts"`
}

func loginCommand() *cobra.Command {
	var baseURL string
	var account int

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with a token made in Chalet's settings",
		Long: "Paste a token from Settings › Access tokens in Chalet. It is read from\n" +
			"standard input, never from an argument, and kept in your system's keychain.\n\n" +
			"  chalet auth login",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name := profileFlag
			if name == "" {
				name = "default"
			}
			if err := chalet.CheckBaseURL(baseURL); err != nil {
				return err
			}

			token, err := readToken(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			who, err := whoIs(cmd.Context(), &chalet.Client{BaseURL: baseURL, Token: token, UserAgent: "chalet-cli/" + Version})
			if err != nil {
				return err
			}

			chosen := -1
			for i, a := range who.Accounts {
				if a.ID == account || (account == 0 && len(who.Accounts) == 1) {
					chosen = i
				}
			}
			if chosen < 0 {
				var list []string
				for _, a := range who.Accounts {
					list = append(list, fmt.Sprintf("  --account %d   %s", a.ID, a.Name))
				}
				return fmt.Errorf("pick an account:\n%s", strings.Join(list, "\n"))
			}

			picked := who.Accounts[chosen]
			if err := config.Save(&config.Profile{Name: name, BaseURL: baseURL, Account: fmt.Sprint(picked.ID)}, token); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Signed in to %s as %s (%s). Profile: %s.\n", picked.Name, picked.User.Name, picked.User.Role, name)
			warnAboutKeychain(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&baseURL, "url", "https://chalet.do", "where Chalet runs")
	cmd.Flags().IntVar(&account, "account", 0, "the account's number, as in /1/ in Chalet's URLs (needed when the token reaches several)")
	return cmd
}

func statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show who chalet is signed in as",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := client()
			if err != nil {
				return err
			}
			client.Account = ""
			who, err := whoIs(cmd.Context(), client)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Profile %s: %s, account %s, as %s.\n", p.Name, p.BaseURL, p.Account, who.EmailAddress)
			warnAboutKeychain(cmd)
			return nil
		},
	}
}

func logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the token (revoke it in Chalet's settings as well)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := config.Resolve(profileFlag)
			if err != nil {
				return err
			}
			if err := p.Forget(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Forgot the token for profile %s. Revoke it in Chalet too: Settings › Access tokens.\n", p.Name)
			return nil
		},
	}
}

func warnAboutKeychain(cmd *cobra.Command) {
	if warning := config.KeychainWarning(); warning != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", warning)
	}
}

func whoIs(ctx context.Context, client *chalet.Client) (*identity, error) {
	resp, err := client.Do(ctx, "GET", "/my/identity", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if !resp.OK() {
		return nil, fmt.Errorf("Chalet refused the token: %s", resp.Error())
	}
	var who identity
	if err := json.Unmarshal(resp.Body, &who); err != nil {
		return nil, fmt.Errorf("unexpected answer from %s: %w", client.BaseURL, err)
	}
	return &who, nil
}

// From a terminal the token is typed blind; from a pipe it is the first line.
func readToken(in io.Reader, prompt io.Writer) (string, error) {
	var token string
	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprint(prompt, "Paste your Chalet token: ")
		data, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		token = string(data)
	} else {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		token = line
	}

	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "chalet_pat_") {
		return "", fmt.Errorf("that is not a Chalet token: they start with chalet_pat_")
	}
	return token, nil
}

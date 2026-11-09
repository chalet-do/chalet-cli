package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/chalet-do/chalet-cli/chalet"
	"github.com/chalet-do/chalet-cli/internal/config"
)

func authCommand() *cobra.Command {
	auth := &cobra.Command{Use: "auth", Short: "Sign in to Chalet with a personal access token, or as an agent"}
	auth.AddCommand(loginCommand(), statusCommand(), logoutCommand())
	return auth
}

func loginCommand() *cobra.Command {
	var baseURL string
	var account int
	var agent bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with a token made in Chalet's settings, or as an agent",
		Long: "Paste a token from Settings › Access tokens in Chalet. It is read from\n" +
			"standard input, never from an argument, and kept in your system's keychain.\n\n" +
			"  chalet auth login\n\n" +
			"With --agent, sign in as an agent with a connection from its page in\n" +
			"Settings › AI agents: the client ID, then the secret, each on its own\n" +
			"line. chalet trades them for a token of an hour, and for the next before\n" +
			"the hour ends. A runtime with no keychain sets CHALET_CLIENT_ID and\n" +
			"CHALET_CLIENT_SECRET instead, and signs in to " + config.DefaultURL + ".\n\n" +
			"  chalet auth login --agent --profile claude",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name := profileFlag
			if name == "" {
				name = "default"
			}
			if err := chalet.CheckBaseURL(baseURL); err != nil {
				return err
			}

			creds, err := readCredentials(cmd.InOrStdin(), cmd.ErrOrStderr(), agent)
			if err != nil {
				return err
			}

			who, err := signedIn(cmd.Context(), baseURL, "", creds).Identity(cmd.Context())
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
			if err := config.Save(&config.Profile{Name: name, BaseURL: baseURL, Account: fmt.Sprint(picked.ID)}, creds); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Signed in to %s as %s (%s). Profile: %s.\n", picked.Name, picked.User.Name, picked.User.Role, name)
			warnAboutKeychain(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&baseURL, "url", config.DefaultURL, "where Chalet runs")
	cmd.Flags().IntVar(&account, "account", 0, "the account's number, as in /1/ in Chalet's URLs (needed when the token reaches several)")
	cmd.Flags().BoolVar(&agent, "agent", false, "sign in as an agent, with its connection's client ID and secret")
	return cmd
}

func statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show who chalet is signed in as",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := client(cmd.Context())
			if err != nil {
				return err
			}
			who, err := client.Identity(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Profile %s: %s, account %s, as %s.\n", p.Name, p.BaseURL, p.Account, signedInAs(who))
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
			creds, _ := p.Credentials()
			if err := p.Forget(); err != nil {
				return err
			}
			if creds.ClientID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Forgot the connection for profile %s. Disconnect it in Chalet too, on the agent's page in Settings › AI agents.\n", p.Name)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Forgot the token for profile %s. Revoke it in Chalet too: Settings › Access tokens.\n", p.Name)
			}
			return nil
		},
	}
}

// signedInAs names whom the token signs in as: a person by their address; an
// agent, whose address is a hidden one, by its name and its sponsor's.
func signedInAs(who *chalet.Identity) string {
	for _, a := range who.Accounts {
		if a.User.Role == "agent" {
			return a.User.Name + ", agent of " + a.User.Sponsor.Name
		}
	}
	return who.EmailAddress
}

func warnAboutKeychain(cmd *cobra.Command) {
	if warning := config.KeychainWarning(); warning != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", warning)
	}
}

// readCredentials reads a token, or for an agent its connection's client ID
// and then its secret.
func readCredentials(in io.Reader, prompt io.Writer, agent bool) (config.Credentials, error) {
	read := lines(in, prompt)
	if !agent {
		token, err := read("Paste your Chalet token: ")
		if err == nil && !strings.HasPrefix(token, "chalet_pat_") {
			err = errors.New("that is not a Chalet token: they start with chalet_pat_")
		}
		return config.Credentials{Token: token}, err
	}

	id, err := read("Client ID: ")
	if err != nil {
		return config.Credentials{}, err
	}
	secret, err := read("Client secret: ")
	if err == nil && (id == "" || !strings.HasPrefix(secret, "chalet_cs_")) {
		err = errors.New("give the connection's client ID, then its secret, which starts with chalet_cs_")
	}
	return config.Credentials{ClientID: id, ClientSecret: secret}, err
}

// lines reads one line at a time: from a terminal after its prompt, typed
// blind — the ID too, since a paste of the ID and the secret together would
// show the secret; from a pipe, the next line, every one through the one
// buffered reader, which may already hold the next.
func lines(in io.Reader, prompt io.Writer) func(label string) (string, error) {
	buffered := bufio.NewReader(in)
	file, _ := in.(*os.File)
	terminal := file != nil && term.IsTerminal(int(file.Fd()))

	return func(label string) (string, error) {
		if terminal {
			fmt.Fprint(prompt, label)
			data, err := term.ReadPassword(int(file.Fd()))
			fmt.Fprintln(prompt)
			return strings.TrimSpace(string(data)), err
		}
		line, err := buffered.ReadString('\n')
		if err == io.EOF {
			err = nil
		}
		return strings.TrimSpace(line), err
	}
}

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chalet-do/chalet-cli/internal/config"
)

// Where the latest release is asked for. A variable so tests can stand in.
var latestReleaseURL = "https://api.github.com/repos/chalet-do/chalet-cli/releases/latest"

// check is one line of `chalet doctor`: what it looked at, how it stands (ok,
// warning or problem), and, when it is not ok, what to do.
type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func doctorCommand() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that chalet can reach Chalet, and say what to do when it cannot",
		Long: "Check the profile, the token, Chalet's catalog of tools, where the token is\n" +
			"kept, and whether a newer chalet is out. It exits non-zero when a check finds\n" +
			"a problem. A warning alone does not.\n\n" +
			"  chalet doctor",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := diagnose(cmd.Context())

			out := cmd.OutOrStdout()
			if asJSON {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(checks); err != nil {
					return err
				}
			} else {
				for _, c := range checks {
					fmt.Fprintf(out, "%-8s %-8s %s\n", c.Status, c.Name, c.Detail)
					if c.Fix != "" {
						fmt.Fprintf(out, "%-17s %s\n", "", c.Fix)
					}
				}
			}

			for _, c := range checks {
				if c.Status == "problem" {
					return errors.New("doctor found a problem")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the checks as JSON")
	return cmd
}

// diagnose checks the shapes every call needs — never a Chalet feature. The
// token and the catalog are only checked once the profile is sound.
func diagnose(ctx context.Context) []check {
	var checks []check
	add := func(name, status, detail, fix string) {
		checks = append(checks, check{Name: name, Status: status, Detail: detail, Fix: fix})
	}

	if p, client, err := client(); err != nil {
		add("profile", "problem", err.Error(), "Run `chalet auth login`.")
	} else {
		add("profile", "ok", fmt.Sprintf("%s: %s, account %s", p.Name, p.BaseURL, p.Account), "")

		anyAccount := *client
		anyAccount.Account = ""
		if who, err := whoIs(ctx, &anyAccount); err != nil {
			add("token", "problem", err.Error(), "Make a new token in Chalet's Settings › Access tokens, and run `chalet auth login`.")
		} else {
			add("token", "ok", "signed in as "+who.EmailAddress, "")
		}

		if catalog, err := loadCatalog(ctx, client); err != nil {
			add("catalog", "problem", err.Error(), "Upgrade chalet if it says so; otherwise check that Chalet runs at "+p.BaseURL+".")
		} else {
			add("catalog", "ok", fmt.Sprintf("version %s, %d operations", catalog.Version, len(catalog.Operations)), "")
		}
	}

	switch warning := config.KeychainWarning(); {
	case config.UsingKeychain():
		add("keychain", "ok", "the token is kept in the system keychain", "")
	case warning != "":
		add("keychain", "warning", warning, "Unlock or set up the system keychain, then run `chalet auth login` again.")
	default:
		add("keychain", "ok", "the token is kept in a file in "+config.Dir()+", as CHALET_NO_KEYRING asks", "")
	}

	switch latest, err := latestRelease(ctx); {
	case err != nil:
		add("version", "warning", fmt.Sprintf("chalet %s; could not ask GitHub for the latest release: %v", Version, err), "")
	case latest == Version:
		add("version", "ok", "chalet "+Version+" is the latest release", "")
	default:
		add("version", "warning", fmt.Sprintf("this is chalet %s; the latest release is %s", Version, latest), "Update chalet the way you installed it, e.g. `brew upgrade chalet`.")
	}

	return checks
}

// latestRelease asks GitHub once, without a token and briefly: an answer here
// is a courtesy, never a reason for doctor to fail.
func latestRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "chalet-cli/"+Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New(resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil || release.TagName == "" {
		return "", errors.New("no tag in GitHub's answer")
	}
	return strings.TrimPrefix(release.TagName, "v"), nil
}

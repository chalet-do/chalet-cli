// Package config keeps where chalet points and the token it signs in with:
// named profiles in ~/.config/chalet/config.json (basecamp/cli/profile), and
// the token in the system keychain (basecamp/cli/credstore), which falls back
// to a 0600 file only when the keychain cannot be reached.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/basecamp/cli/credstore"
	"github.com/basecamp/cli/profile"
)

// Profile is one Chalet to talk to: where it is, and which account in it.
type Profile struct {
	Name    string
	BaseURL string
	// Account is the account's URL prefix, e.g. "1" for /1/my.
	Account string
}

// ErrNoProfile means nobody has signed in yet.
var ErrNoProfile = errors.New("not signed in: run `chalet auth login`")

// Dir is ~/.config/chalet, or $XDG_CONFIG_HOME/chalet.
func Dir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "chalet")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "chalet")
}

// Resolve picks the profile: the --profile flag, then CHALET_PROFILE, then
// the default, then the only one there is.
func Resolve(flag string) (*Profile, error) {
	all, fallback, err := profiles().List()
	if err != nil {
		return nil, err
	}
	name, err := profile.Resolve(profile.ResolveOptions{
		FlagValue:      flag,
		EnvVar:         os.Getenv("CHALET_PROFILE"),
		DefaultProfile: fallback,
		Profiles:       all,
	})
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, ErrNoProfile
	}
	return fromStored(all[name])
}

// Save writes the profile and its token, replacing an earlier one of the
// same name. The first profile saved becomes the default.
func Save(p *Profile, token string) error {
	store := profiles()
	if _, err := store.Get(p.Name); err == nil {
		if err := store.Delete(p.Name); err != nil {
			return err
		}
	}
	account, err := json.Marshal(p.Account)
	if err != nil {
		return err
	}
	if err := store.Create(&profile.Profile{Name: p.Name, BaseURL: p.BaseURL, Extra: map[string]json.RawMessage{"account": account}}); err != nil {
		return err
	}
	if _, fallback, err := store.List(); err == nil && fallback == "" {
		if err := store.SetDefault(p.Name); err != nil {
			return err
		}
	}

	data, err := json.Marshal(credentials{Token: token})
	if err != nil {
		return err
	}
	return keychain().Save(p.credentialKey(), data)
}

// Token reads the profile's token. It never leaves this process except as
// the bearer header.
func (p *Profile) Token() (string, error) {
	data, err := keychain().Load(p.credentialKey())
	if err != nil {
		return "", fmt.Errorf("no token for profile %q: run `chalet auth login --profile %s` (%w)", p.Name, p.Name, err)
	}
	var stored credentials
	if err := json.Unmarshal(data, &stored); err != nil {
		return "", fmt.Errorf("unreadable token for profile %q: run `chalet auth login --profile %s`", p.Name, p.Name)
	}
	return stored.Token, nil
}

// Forget removes the profile's token; the profile itself stays.
func (p *Profile) Forget() error {
	return keychain().Delete(p.credentialKey())
}

type credentials struct {
	Token string `json:"token"`
}

func (p *Profile) credentialKey() string {
	return profile.CredentialKey(p.Name, p.BaseURL)
}

func fromStored(stored *profile.Profile) (*Profile, error) {
	p := &Profile{Name: stored.Name, BaseURL: stored.BaseURL}
	if raw, ok := stored.Extra["account"]; ok {
		if err := json.Unmarshal(raw, &p.Account); err != nil {
			return nil, fmt.Errorf("profile %q: unreadable account: %w", stored.Name, err)
		}
	}
	return p, nil
}

func profiles() *profile.Store {
	return profile.NewStore(filepath.Join(Dir(), "config.json"))
}

// KeychainWarning says when the token is kept in a file because the keychain
// could not be reached — never silently (api-spec §15.2). Empty otherwise.
func KeychainWarning() string {
	return keychain().FallbackWarning()
}

// Probed once per process, and bounded: a keychain that hangs must not hang
// an MCP server's start.
var keychain = sync.OnceValue(func() *credstore.Store {
	return credstore.NewStore(credstore.StoreOptions{
		ServiceName:   "chalet",
		DisableEnvVar: "CHALET_NO_KEYRING",
		ProbeTimeout:  5 * time.Second,
		FallbackDir:   Dir(),
	})
})

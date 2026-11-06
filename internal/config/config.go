// Package config keeps where chalet points and what it signs in with: named
// profiles in ~/.config/chalet/config.json (basecamp/cli/profile), and the
// token, or an agent's client ID and secret, in the system keychain
// (basecamp/cli/credstore), which falls back to a 0600 file only when the
// keychain cannot be reached.
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

// Credentials are what a profile signs in with: a personal access token, or
// an agent's connection, whose client ID and secret buy a token an hour at a
// time. A keychain entry from before agents holds the token alone.
type Credentials struct {
	Token        string `json:"token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// ErrNoProfile means nobody has signed in yet.
var ErrNoProfile = errors.New("not signed in: run `chalet auth login`")

// DefaultURL is where Chalet runs. A variable so tests can stand in.
var DefaultURL = "https://chalet.do"

// FromEnvironment is the connection a runtime with no keychain passes in
// CHALET_CLIENT_ID and CHALET_CLIENT_SECRET, instead of `chalet auth login
// --agent`. When both are set it takes the place of every profile: Chalet at
// DefaultURL, in the account the agent is in, which its identity gives.
func FromEnvironment() (*Profile, Credentials, bool) {
	id, secret := os.Getenv("CHALET_CLIENT_ID"), os.Getenv("CHALET_CLIENT_SECRET")
	if id == "" || secret == "" {
		return nil, Credentials{}, false
	}
	return &Profile{Name: "environment", BaseURL: DefaultURL}, Credentials{ClientID: id, ClientSecret: secret}, true
}

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

// Save writes the profile and what it signs in with, replacing an earlier one
// of the same name. The first profile saved becomes the default.
func Save(p *Profile, creds Credentials) error {
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

	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	return keychain().Save(p.credentialKey(), data)
}

// Credentials reads what the profile signs in with. A token leaves this
// process only as the bearer header, and a secret only in the body of its
// trade at Chalet's token endpoint.
func (p *Profile) Credentials() (Credentials, error) {
	data, err := keychain().Load(p.credentialKey())
	if err != nil {
		return Credentials{}, fmt.Errorf("no token for profile %q: run `chalet auth login --profile %s` (%w)", p.Name, p.Name, err)
	}
	var stored Credentials
	if err := json.Unmarshal(data, &stored); err != nil {
		return Credentials{}, fmt.Errorf("unreadable token for profile %q: run `chalet auth login --profile %s`", p.Name, p.Name)
	}
	return stored, nil
}

// Forget removes what the profile signs in with; the profile itself stays.
func (p *Profile) Forget() error {
	return keychain().Delete(p.credentialKey())
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

// UsingKeychain reports whether the token is kept in the system keychain
// rather than in a file.
func UsingKeychain() bool {
	return keychain().UsingKeyring()
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

// Package config loads and stores the ibkr-cli configuration file.
//
// The file is JSON at ~/.config/ibkr-cli/config.json (override with the
// IBKR_CLI_CONFIG environment variable or the global --config flag)
// and must be mode 0600: it can hold OAuth token material in mode O.
// Loading refuses a file that group/other can read — the same secrets
// posture as the sibling CLIs, enforced rather than warned about.
//
// No credential is ever accepted from a command-line flag (shell
// history) and no value from this file is ever printed or logged.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mode selects the authentication provider (docs/SPEC.md §3).
type Mode string

const (
	// ModeGateway talks to a local Client Portal Gateway run by IBeam.
	ModeGateway Mode = "gateway"
	// ModeOAuth signs requests directly with IBKR's OAuth 1.0a variant.
	ModeOAuth Mode = "oauth"
)

// DefaultGatewayURL is the Client Portal Gateway's local base URL
// (IBeam's default port mapping).
const DefaultGatewayURL = "https://localhost:5000/v1/api"

// OAuth holds the mode-O material issued through IBKR's self-service
// OAuth portal. AccessTokenSecret is the base64 blob IBKR issues; the
// plaintext is recovered only in memory during the live-session-token
// exchange (docs/SPEC.md §3). DHPrime is the hex prime from the
// dhparam.pem generated during onboarding.
type OAuth struct {
	ConsumerKey       string `json:"consumer_key,omitempty"`
	AccessToken       string `json:"access_token,omitempty"`
	AccessTokenSecret string `json:"access_token_secret,omitempty"`
	DHPrime           string `json:"dh_prime,omitempty"`
	DHGenerator       int    `json:"dh_generator,omitempty"` // default 2
	Realm             string `json:"realm,omitempty"`        // default "limited_poa"
	EncryptionKeyPath string `json:"encryption_key_path,omitempty"`
	SignatureKeyPath  string `json:"signature_key_path,omitempty"`

	// LiveSessionToken caches the most recent LST (base64) and its
	// expiry (unix milliseconds, as IBKR reports it) so one-shot CLI
	// invocations do not repeat the DH exchange on every command.
	LiveSessionToken        string `json:"live_session_token,omitempty"`
	LiveSessionTokenExpires int64  `json:"live_session_token_expires,omitempty"`
}

// Config is the on-disk configuration.
type Config struct {
	Mode           Mode   `json:"mode,omitempty"` // default gateway
	GatewayURL     string `json:"gateway_url,omitempty"`
	DefaultAccount string `json:"default_account_id,omitempty"`
	OAuth          OAuth  `json:"oauth,omitempty"`
	path           string // file this config was loaded from / will be saved to
}

// Path resolves the config file path: explicit flag path first, then
// IBKR_CLI_CONFIG, then the XDG-style default.
func Path(flagPath string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	if p := os.Getenv("IBKR_CLI_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ibkr-cli", "config.json"), nil
}

// AuditLogPath is next to the config file.
func AuditLogPath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "audit.log")
}

// CachePath is the conid-resolution cache next to the config file.
func CachePath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "conid-cache.json")
}

// Load reads the config file. A missing file is not an error: it
// yields defaults (gateway mode, default URL) so read-only plumbing
// like --help and `auth oauth-status` works before `auth init`.
func Load(flagPath string) (*Config, error) {
	path, err := Path(flagPath)
	if err != nil {
		return nil, err
	}
	cfg := &Config{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("config file %s has mode %04o: it must be 0600 "+
			"(it can hold OAuth token material); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}
	cfg.path = path
	return cfg, nil
}

// Save writes the config back to its file with mode 0600, creating the
// directory 0700 when needed.
func (c *Config) Save() error {
	if c.path == "" {
		return fmt.Errorf("config has no path")
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// FilePath reports where this config lives.
func (c *Config) FilePath() string { return c.path }

// AuthMode returns the configured mode, defaulting to gateway.
func (c *Config) AuthMode() Mode {
	if c.Mode == ModeOAuth {
		return ModeOAuth
	}
	return ModeGateway
}

// GatewayBaseURL resolves the gateway base URL: IBKR_GATEWAY_URL env,
// then the config value, then the IBeam default.
func (c *Config) GatewayBaseURL() string {
	if u := os.Getenv("IBKR_GATEWAY_URL"); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	if c.GatewayURL != "" {
		return strings.TrimSuffix(c.GatewayURL, "/")
	}
	return DefaultGatewayURL
}

// IsPaperAccount reports whether an account id is a paper (DU…)
// account. Paper vs live is an account property, not a mode.
func IsPaperAccount(accountID string) bool {
	return strings.HasPrefix(strings.ToUpper(accountID), "DU")
}

// AccountTag renders the PAPER/LIVE tag mutating commands must print.
func AccountTag(accountID string) string {
	if IsPaperAccount(accountID) {
		return "PAPER"
	}
	return "LIVE"
}

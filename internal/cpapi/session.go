package cpapi

import (
	"context"
	"net/url"
)

// AuthStatus is the decoded POST /iserver/auth/status response.
type AuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Connected     bool   `json:"connected"`
	Competing     bool   `json:"competing"`
	Fail          string `json:"fail"`
	Message       string `json:"message"`
	ServerInfo    struct {
		ServerName    string `json:"serverName"`
		ServerVersion string `json:"serverVersion"`
	} `json:"serverInfo"`
}

// BrokerageLive reports whether trading endpoints will work: the
// session is both connected and authenticated (SPEC §3 two tiers).
func (s *AuthStatus) BrokerageLive() bool { return s.Authenticated && s.Connected }

// AuthStatus calls POST /iserver/auth/status. (The endpoint is POST
// in the official reference and in IBind; the SPEC table said GET —
// corrected at build time, see docs/SPEC.md errata in README.md.)
func (c *Client) AuthStatus(ctx context.Context) (*AuthStatus, *Response, error) {
	resp, err := c.Post(ctx, "/iserver/auth/status", nil)
	if err != nil {
		return nil, resp, err
	}
	var st AuthStatus
	if err := resp.JSON(&st); err != nil {
		return nil, resp, err
	}
	return &st, resp, nil
}

// Tickle calls POST /tickle, the session keepalive.
func (c *Client) Tickle(ctx context.Context) (*Response, error) {
	return c.Post(ctx, "/tickle", nil)
}

// Reauthenticate calls POST /iserver/reauthenticate (gateway mode:
// re-initialise the brokerage session on a live gateway session).
func (c *Client) Reauthenticate(ctx context.Context) (*Response, error) {
	return c.Post(ctx, "/iserver/reauthenticate", nil)
}

// SSODHInit calls POST /iserver/auth/ssodh/init, which establishes
// the brokerage session after (OAuth) authentication.
func (c *Client) SSODHInit(ctx context.Context, compete bool) (*Response, error) {
	body := map[string]any{"publish": true, "compete": compete}
	return c.Post(ctx, "/iserver/auth/ssodh/init", body)
}

// Validate calls GET /sso/validate (outer read-only session check).
func (c *Client) Validate(ctx context.Context) (*Response, error) {
	return c.Get(ctx, "/sso/validate", url.Values{})
}

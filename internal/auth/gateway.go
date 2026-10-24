package auth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/config"
)

// GatewayProvider implements Mode G: the session belongs to the
// Client Portal Gateway run by IBeam. The provider can check and
// nudge the session (tickle, reauthenticate) but never logs in —
// when the brokerage session is down it fails loudly and names
// IBeam as the session owner (SPEC §3/§7.3).
type GatewayProvider struct {
	base string
	cfg  *config.Config
}

// NewGatewayProvider builds a Mode G provider from config.
func NewGatewayProvider(cfg *config.Config) *GatewayProvider {
	return &GatewayProvider{base: cfg.GatewayBaseURL(), cfg: cfg}
}

// BaseURL returns the gateway base URL.
func (p *GatewayProvider) BaseURL() string { return p.base }

// SetBaseURL overrides the gateway base URL (testing).
func (p *GatewayProvider) SetBaseURL(u string) { p.base = u }

// Transport returns a transport that skips TLS verification only for
// loopback hosts (the gateway's self-signed certificate, SPEC §7.4);
// any other host gets a verifying transport.
func (p *GatewayProvider) Transport() http.RoundTripper {
	u, err := url.Parse(p.base)
	host := p.base
	if err == nil {
		host = u.Hostname()
	}
	if isLoopbackHost(host) {
		return &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // loopback-only, SPEC §7.4
		}
	}
	return http.DefaultTransport
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (p *GatewayProvider) httpClient() *http.Client {
	return &http.Client{Transport: p.Transport(), Timeout: 15 * time.Second}
}

// statusResponse mirrors POST /iserver/auth/status.
type statusResponse struct {
	Authenticated bool   `json:"authenticated"`
	Connected     bool   `json:"connected"`
	Competing     bool   `json:"competing"`
	Fail          string `json:"fail"`
	ServerInfo    struct {
		ServerName string `json:"serverName"`
	} `json:"serverInfo"`
}

// Status queries the gateway's auth status.
func (p *GatewayProvider) Status(ctx context.Context) (SessionState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.base+"/iserver/auth/status", nil)
	if err != nil {
		return SessionState{}, err
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return SessionState{}, &apierr.SessionError{Msg: fmt.Sprintf(
			"gateway unreachable at %s (is IBeam running?): %v", p.base, err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return SessionState{}, &apierr.SessionError{Msg: "gateway session not authenticated; IBeam owns login — check the IBeam container"}
	}
	if resp.StatusCode >= 400 {
		return SessionState{}, &apierr.APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var sr statusResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return SessionState{}, &apierr.APIError{Msg: "unparseable auth status: " + string(body)}
	}
	st := SessionState{
		Authenticated: sr.Authenticated,
		BrokerageLive: sr.Authenticated && sr.Connected,
		Competing:     sr.Competing,
		ServerName:    sr.ServerInfo.ServerName,
	}
	if sr.Fail != "" {
		st.Detail = sr.Fail
	}
	return st, nil
}

// EnsureSession tickles a live session, or reauthenticates once and
// re-checks. Beyond that it refuses: IBeam owns (re)login.
func (p *GatewayProvider) EnsureSession(ctx context.Context) error {
	st, err := p.Status(ctx)
	if err != nil {
		return err
	}
	if st.BrokerageLive {
		// Best-effort keepalive; failure is non-fatal here because
		// the API call itself will surface a dead session.
		_ = p.post(ctx, "/tickle")
		return nil
	}
	// One reauthenticate attempt (valid while the gateway's SSO
	// session lives), then a final verdict.
	if err := p.post(ctx, "/iserver/reauthenticate"); err == nil {
		st, err = p.Status(ctx)
		if err == nil && st.BrokerageLive {
			return nil
		}
	}
	detail := ""
	if st.Detail != "" {
		detail = " (" + st.Detail + ")"
	}
	return &apierr.SessionError{Msg: "no live brokerage session at the gateway" + detail +
		"; IBeam owns login and session maintenance — check IBeam, then retry"}
}

func (p *GatewayProvider) post(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return &apierr.APIError{StatusCode: resp.StatusCode}
	}
	return nil
}

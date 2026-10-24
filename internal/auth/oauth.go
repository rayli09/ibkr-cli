package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"crypto/rsa"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/config"
)

// OAuthBaseURL is the direct-access base URL, verified against
// IBind's defaults (IBIND_OAUTH1A_REST_URL) and IBKR's OAuth
// documentation (SPEC §3 build-time verification item, resolved).
const OAuthBaseURL = "https://api.ibkr.com/v1/api"

// liveSessionTokenPath is the LST endpoint relative to the base URL.
const liveSessionTokenPath = "/oauth/live_session_token"

// OAuthProvider implements Mode O: direct OAuth 1.0a signing, no
// gateway. It lazily acquires a live session token (DH exchange),
// caches it in the 0600 config, and signs each request HMAC-SHA256
// with it via a RoundTripper.
type OAuthProvider struct {
	cfg  *config.Config
	base string

	mu      sync.Mutex
	lst     string
	lstExp  int64 // unix ms
	encKey  *rsa.PrivateKey
	sigKey  *rsa.PrivateKey
	httpCli *http.Client
}

// NewOAuthProvider builds a Mode O provider from config.
func NewOAuthProvider(cfg *config.Config) *OAuthProvider {
	return &OAuthProvider{
		cfg:     cfg,
		base:    OAuthBaseURL,
		lst:     cfg.OAuth.LiveSessionToken,
		lstExp:  cfg.OAuth.LiveSessionTokenExpires,
		httpCli: &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseURL returns the direct-access base URL.
func (p *OAuthProvider) BaseURL() string { return p.base }

// SetBaseURL overrides the base URL (testing).
func (p *OAuthProvider) SetBaseURL(u string) { p.base = strings.TrimSuffix(u, "/") }

// Transport returns the signing transport.
func (p *OAuthProvider) Transport() http.RoundTripper {
	return &signingTransport{provider: p, base: http.DefaultTransport}
}

func (p *OAuthProvider) oauthCfgErr() error {
	o := p.cfg.OAuth
	missing := []string{}
	if o.ConsumerKey == "" {
		missing = append(missing, "consumer_key")
	}
	if o.AccessToken == "" {
		missing = append(missing, "access_token")
	}
	if o.AccessTokenSecret == "" {
		missing = append(missing, "access_token_secret")
	}
	if o.DHPrime == "" {
		missing = append(missing, "dh_prime")
	}
	if o.EncryptionKeyPath == "" {
		missing = append(missing, "encryption_key_path")
	}
	if o.SignatureKeyPath == "" {
		missing = append(missing, "signature_key_path")
	}
	if len(missing) > 0 {
		return &apierr.SessionError{Msg: "OAuth mode is missing config values: " +
			strings.Join(missing, ", ") + " (set them via `ibkr-cli auth init --mode oauth`)"}
	}
	return nil
}

func (p *OAuthProvider) loadKeys() error {
	if p.encKey != nil && p.sigKey != nil {
		return nil
	}
	enc, err := os.ReadFile(p.cfg.OAuth.EncryptionKeyPath)
	if err != nil {
		return &apierr.SessionError{Msg: "reading encryption key: " + err.Error()}
	}
	p.encKey, err = ParseRSAPrivateKey(enc)
	if err != nil {
		return &apierr.SessionError{Msg: "parsing encryption key: " + err.Error()}
	}
	sig, err := os.ReadFile(p.cfg.OAuth.SignatureKeyPath)
	if err != nil {
		return &apierr.SessionError{Msg: "reading signature key: " + err.Error()}
	}
	p.sigKey, err = ParseRSAPrivateKey(sig)
	if err != nil {
		return &apierr.SessionError{Msg: "parsing signature key: " + err.Error()}
	}
	return nil
}

func (p *OAuthProvider) realm() string {
	if p.cfg.OAuth.Realm != "" {
		return p.cfg.OAuth.Realm
	}
	return "limited_poa"
}

func (p *OAuthProvider) generator() int64 {
	if p.cfg.OAuth.DHGenerator > 0 {
		return int64(p.cfg.OAuth.DHGenerator)
	}
	return 2
}

// lstValid reports whether the cached LST is still usable
// (60 s safety margin before the IBKR-reported expiry).
func (p *OAuthProvider) lstValid() bool {
	return p.lst != "" && time.Now().UnixMilli() < p.lstExp-60_000
}

// EnsureLST returns a valid live session token, performing the DH
// exchange when the cached one is missing or expired.
func (p *OAuthProvider) EnsureLST(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lstValid() {
		return p.lst, nil
	}
	if err := p.oauthCfgErr(); err != nil {
		return "", err
	}
	if err := p.loadKeys(); err != nil {
		return "", err
	}
	o := p.cfg.OAuth
	prepend, err := Prepend(o.AccessTokenSecret, p.encKey)
	if err != nil {
		return "", err
	}
	dhRandom, err := GenerateDHRandom()
	if err != nil {
		return "", err
	}
	challenge, err := DHChallenge(o.DHPrime, dhRandom, p.generator())
	if err != nil {
		return "", err
	}
	params, err := OAuthParams(o.ConsumerKey, o.AccessToken, "RSA-SHA256")
	if err != nil {
		return "", err
	}
	params["diffie_hellman_challenge"] = challenge
	reqURL := p.base + liveSessionTokenPath
	base := BaseString(http.MethodPost, reqURL, params, prepend)
	sig, err := RSASignSHA256(base, p.sigKey)
	if err != nil {
		return "", err
	}
	params["oauth_signature"] = sig

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", AuthorizationHeader(p.realm(), params))
	req.Header.Set("User-Agent", "ibkr-cli")
	resp, err := p.httpCli.Do(req)
	if err != nil {
		return "", &apierr.SessionError{Msg: "live session token request failed: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", &apierr.SessionError{Msg: fmt.Sprintf(
			"live session token request rejected (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
	var out struct {
		DHResponse    string `json:"diffie_hellman_response"`
		LSTExpiration int64  `json:"live_session_token_expiration"`
		LSTSignature  string `json:"live_session_token_signature"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", &apierr.SessionError{Msg: "unparseable live session token response"}
	}
	lst, err := ComputeLiveSessionToken(o.DHPrime, dhRandom, out.DHResponse, prepend)
	if err != nil {
		return "", err
	}
	if !ValidateLiveSessionToken(lst, out.LSTSignature, o.ConsumerKey) {
		return "", &apierr.SessionError{Msg: "live session token failed signature validation"}
	}
	p.lst, p.lstExp = lst, out.LSTExpiration
	// Cache for later one-shot invocations (0600 config).
	p.cfg.OAuth.LiveSessionToken = lst
	p.cfg.OAuth.LiveSessionTokenExpires = out.LSTExpiration
	_ = p.cfg.Save() // best effort: a failed save only costs a re-exchange
	return lst, nil
}

// Status in OAuth mode = LST availability plus a brokerage-session
// probe (auth/status through the signing transport). Before any LST
// exists it reports the local state without network traffic.
func (p *OAuthProvider) Status(ctx context.Context) (SessionState, error) {
	if err := p.oauthCfgErr(); err != nil {
		return SessionState{Detail: err.Error()}, nil
	}
	if !p.lstValid() {
		return SessionState{Detail: "no live session token cached; run any command or `auth reauth` to acquire one"}, nil
	}
	st, err := p.probeStatus(ctx)
	if err != nil {
		return SessionState{Authenticated: true, Detail: err.Error()}, nil
	}
	return st, nil
}

func (p *OAuthProvider) probeStatus(ctx context.Context) (SessionState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/iserver/auth/status", nil)
	if err != nil {
		return SessionState{}, err
	}
	client := &http.Client{Transport: p.Transport(), Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return SessionState{}, &apierr.SessionError{Msg: "auth status probe failed: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return SessionState{}, &apierr.SessionError{Msg: fmt.Sprintf(
			"auth status probe rejected (HTTP %d)", resp.StatusCode)}
	}
	var sr statusResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return SessionState{}, &apierr.APIError{Msg: "unparseable auth status"}
	}
	return SessionState{
		Authenticated: sr.Authenticated,
		BrokerageLive: sr.Authenticated && sr.Connected,
		Competing:     sr.Competing,
		ServerName:    sr.ServerInfo.ServerName,
	}, nil
}

// EnsureSession acquires an LST (if needed) and initialises the
// brokerage session via ssodh/init.
func (p *OAuthProvider) EnsureSession(ctx context.Context) error {
	if _, err := p.EnsureLST(ctx); err != nil {
		return err
	}
	// If a brokerage session is already live, do not compete with it.
	if st, err := p.probeStatus(ctx); err == nil && st.BrokerageLive {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.base+"/iserver/auth/ssodh/init",
		strings.NewReader(`{"publish":true,"compete":true}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: p.Transport(), Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &apierr.SessionError{Msg: "ssodh/init failed: " + err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return &apierr.SessionError{Msg: fmt.Sprintf("ssodh/init rejected (HTTP %d)", resp.StatusCode)}
	}
	return nil
}

// signingTransport signs every request with HMAC-SHA256 keyed by the
// live session token, IBind-style: the base string covers the OAuth
// parameters plus query parameters (JSON bodies do not participate).
type signingTransport struct {
	provider *OAuthProvider
	base     http.RoundTripper
}

func (t *signingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	lst, err := t.provider.EnsureLST(req.Context())
	if err != nil {
		return nil, err
	}
	o := t.provider.cfg.OAuth
	params, err := OAuthParams(o.ConsumerKey, o.AccessToken, "HMAC-SHA256")
	if err != nil {
		return nil, err
	}
	// Merge query parameters into the signed set.
	q := req.URL.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		params[k] = q.Get(k)
	}
	baseURL := req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
	base := BaseString(req.Method, baseURL, params, "")
	sig, err := HMACSignSHA256(base, lst)
	if err != nil {
		return nil, err
	}
	params["oauth_signature"] = sig
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", AuthorizationHeader(t.provider.realm(), params))
	return t.base.RoundTrip(clone)
}

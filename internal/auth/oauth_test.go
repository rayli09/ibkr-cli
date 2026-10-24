package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rayli09/ibkr-cli/internal/config"
)

// writeKey writes an RSA private key PEM to a temp file.
func writeKey(t *testing.T, dir, name string, key *rsa.PrivateKey) string {
	t.Helper()
	der := x509.MarshalPKCS1PrivateKey(key)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOAuthProviderEndToEnd runs the full Mode O flow against a stub
// that plays IBKR's side of the live-session-token exchange with
// real DH math: RSA-signed LST request in, DH response + signed LST
// out; then signed (HMAC-SHA256) brokerage-session calls. This
// verifies the provider wiring, not just the crypto primitives.
func TestOAuthProviderEndToEnd(t *testing.T) {
	dir := t.TempDir()
	encKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sigKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encPath := writeKey(t, dir, "enc.pem", encKey)
	sigPath := writeKey(t, dir, "sig.pem", sigKey)

	// The "IBKR-issued" access token secret: base64 RSA encryption
	// of a known hex prepend under the client encryption key.
	prepend := "00112233445566778899aabbccddeeff"
	prependBytes, _ := hex.DecodeString(prepend)
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, &encKey.PublicKey, prependBytes)
	if err != nil {
		t.Fatal(err)
	}
	tokenSecret := base64.StdEncoding.EncodeToString(ciphertext)

	consumerKey := "TESTCONSUMER"
	prime, _ := new(big.Int).SetString(testPrime, 16)

	var sawSignedRequest, sawSSODH bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth/live_session_token"):
			if r.Method != http.MethodPost || !strings.Contains(authz, `oauth_signature_method="RSA-SHA256"`) {
				t.Errorf("LST request malformed: %s %s", r.Method, authz)
			}
			challengeHex := oauthHeaderValue(authz, "diffie_hellman_challenge")
			challenge, ok := new(big.Int).SetString(challengeHex, 16)
			if !ok {
				t.Errorf("bad challenge %q", challengeHex)
			}
			// Server side: pick a secret, compute the response and
			// the shared secret K = challenge^secret mod p.
			secret := big.NewInt(424242)
			response := new(big.Int).Exp(big.NewInt(2), secret, prime)
			shared := new(big.Int).Exp(challenge, secret, prime)
			keyBytes := bigToBytesJavaStyle(shared)
			mac := hmac.New(sha1.New, keyBytes)
			mac.Write(prependBytes)
			lst := base64.StdEncoding.EncodeToString(mac.Sum(nil))
			lstBytes, _ := base64.StdEncoding.DecodeString(lst)
			sigMac := hmac.New(sha1.New, lstBytes)
			sigMac.Write([]byte(consumerKey))
			lstSig := hex.EncodeToString(sigMac.Sum(nil))
			exp := time.Now().Add(time.Hour).UnixMilli()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"diffie_hellman_response":"` +
				response.Text(16) + `","live_session_token_expiration":` +
				strconv.FormatInt(exp, 10) + `,"live_session_token_signature":"` + lstSig + `"}`))
		case strings.HasSuffix(r.URL.Path, "/iserver/auth/status"):
			if !strings.Contains(authz, `oauth_signature_method="HMAC-SHA256"`) {
				t.Errorf("status probe not HMAC-signed: %s", authz)
			}
			sawSignedRequest = true
			_, _ = w.Write([]byte(`{"authenticated":false,"connected":false}`))
		case strings.HasSuffix(r.URL.Path, "/iserver/auth/ssodh/init"):
			sawSSODH = true
			_, _ = w.Write([]byte(`{"authenticated":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{Mode: config.ModeOAuth}
	cfg.OAuth = config.OAuth{
		ConsumerKey:       consumerKey,
		AccessToken:       "TESTTOKEN",
		AccessTokenSecret: tokenSecret,
		DHPrime:           testPrime,
		DHGenerator:       2,
		Realm:             "limited_poa",
		EncryptionKeyPath: encPath,
		SignatureKeyPath:  sigPath,
	}
	p := NewOAuthProvider(cfg)
	p.SetBaseURL(srv.URL + "/v1/api")
	p.httpCli = srv.Client()

	if err := p.EnsureSession(context.Background()); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if !sawSignedRequest || !sawSSODH {
		t.Fatalf("flow incomplete: signed probe=%v ssodh=%v", sawSignedRequest, sawSSODH)
	}
	if p.lst == "" || !p.lstValid() {
		t.Fatal("expected a cached valid LST")
	}
	// A second EnsureSession must reuse the cached LST (no second
	// exchange) and skip ssodh because the probe now succeeds... the
	// stub still reports not-connected, so ssodh runs again; the LST
	// exchange must not.
	lstBefore := p.lst
	if err := p.EnsureSession(context.Background()); err != nil {
		t.Fatalf("second EnsureSession: %v", err)
	}
	if p.lst != lstBefore {
		t.Fatal("LST was re-exchanged despite being valid")
	}
}

// oauthHeaderValue extracts k="v" from an OAuth Authorization header.
func oauthHeaderValue(header, key string) string {
	needle := key + `="`
	i := strings.Index(header, needle)
	if i < 0 {
		return ""
	}
	rest := header[i+len(needle):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	v, _ := url.QueryUnescape(rest[:j])
	return v
}

// bigToBytesJavaStyle mirrors IBind's to_byte_array for the stub's
// shared-secret derivation (same semantics as the client code).
func bigToBytesJavaStyle(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) > 0 && b[0]&0x80 != 0 {
		return append([]byte{0}, b...)
	}
	return b
}

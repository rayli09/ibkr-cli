package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/config"
)

func TestLoopbackDetection(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "127.0.0.1": true, "::1": true,
		"example.com": false, "192.168.1.10": false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func gatewayFor(url string) *GatewayProvider {
	cfg := &config.Config{Mode: config.ModeGateway, GatewayURL: url}
	return NewGatewayProvider(cfg)
}

func TestGatewayStatusParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/api/iserver/auth/status" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"authenticated":true,"connected":true,"competing":true,` +
			`"serverInfo":{"serverName":"JifS21001"}}`))
	}))
	defer srv.Close()
	st, err := gatewayFor(srv.URL + "/v1/api").Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Authenticated || !st.BrokerageLive || !st.Competing || st.ServerName != "JifS21001" {
		t.Fatalf("state = %+v", st)
	}
}

// A dead brokerage session that reauthenticate cannot revive must
// fail as a SessionError naming IBeam as the session owner.
func TestGatewayEnsureSessionNamesIBeam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/iserver/auth/status"):
			_, _ = w.Write([]byte(`{"authenticated":true,"connected":false}`))
		case strings.HasSuffix(r.URL.Path, "/iserver/reauthenticate"):
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	err := gatewayFor(srv.URL + "/v1/api").EnsureSession(context.Background())
	if err == nil {
		t.Fatal("expected SessionError")
	}
	if apierr.ExitCode(err) != 3 {
		t.Fatalf("exit = %d, want 3", apierr.ExitCode(err))
	}
	if !strings.Contains(err.Error(), "IBeam") {
		t.Fatalf("error should name IBeam: %v", err)
	}
}

// An unreachable gateway is also a SessionError, not a raw dial error.
func TestGatewayUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listening now
	_, err := gatewayFor(url + "/v1/api").Status(context.Background())
	if err == nil || apierr.ExitCode(err) != 3 {
		t.Fatalf("err = %v, want SessionError (exit 3)", err)
	}
}

package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// stubWS is a combined REST + WebSocket stub for the stream tests:
// it serves the session endpoints the gateway provider needs and
// upgrades /v1/api/ws. On an smd subscribe it replies with one md
// message and one heartbeat; it records every payload it receives.
type stubWS struct {
	srv *httptest.Server

	mu       sync.Mutex
	payloads []string
	cookie   string
}

func newStubWS(t *testing.T) *stubWS {
	t.Helper()
	s := &stubWS{}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/api/iserver/auth/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"authenticated": true, "connected": true, "competing": false})
	})
	mux.HandleFunc("/v1/api/tickle", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"session": "wsstub",
			"iserver": map[string]any{"authStatus": map[string]any{"authenticated": true, "connected": true}}})
	})
	mux.HandleFunc("/v1/api/iserver/reauthenticate", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{})
	})
	mux.HandleFunc("/v1/api/ws", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.cookie = r.Header.Get("Cookie")
		s.mu.Unlock()
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.payloads = append(s.payloads, string(msg))
			s.mu.Unlock()
			if strings.HasPrefix(string(msg), "smd+") {
				_ = c.WriteMessage(websocket.TextMessage,
					[]byte(`{"topic":"md","conid":265598,"31":"750.25","84":"750.00","86":"750.50"}`))
				_ = c.WriteMessage(websocket.TextMessage,
					[]byte(`{"topic":"tic","args":{"hb":1}}`))
			}
		}
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubWS) config(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := fmt.Sprintf(`{"mode":"gateway","gateway_url":%q,"default_account_id":%q}`,
		s.srv.URL+"/v1/api", stubAccount)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (s *stubWS) got(substr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.payloads {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}

func TestStreamMarketAgainstStubWS(t *testing.T) {
	stub := newStubWS(t)
	cfg := stub.config(t)

	stdout, stderr, code := runRoot(t, "--config", cfg,
		"stream", "market", "265598", "--duration", "10s", "--max-events", "1")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `"topic":"md"`) || !strings.Contains(stdout, "750.25") {
		t.Fatalf("stream output missing md message:\n%s", stdout)
	}
	if !stub.got(`smd+265598+{"fields":["31","84","86"]}`) {
		t.Fatalf("server never saw the smd subscribe; payloads: %v", stub.payloads)
	}
	if !stub.got("umd+265598+{}") {
		t.Fatalf("server never saw the umd unsubscribe; payloads: %v", stub.payloads)
	}
	if !strings.Contains(stub.cookie, `"session"`) || !strings.Contains(stub.cookie, "wsstub") {
		t.Fatalf("gateway cookie not in IBind format api={\"session\": ...}: %q", stub.cookie)
	}
}

func TestStreamOrdersDurationBounded(t *testing.T) {
	stub := newStubWS(t)
	cfg := stub.config(t)

	_, stderr, code := runRoot(t, "--config", cfg,
		"stream", "orders", "--duration", "300ms")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !stub.got("sor+{}") || !stub.got("uor+{}") {
		t.Fatalf("orders subscribe/unsubscribe missing; payloads: %v", stub.payloads)
	}
}

func TestStreamAccountAndNotifications(t *testing.T) {
	stub := newStubWS(t)
	cfg := stub.config(t)

	_, stderr, code := runRoot(t, "--config", cfg,
		"stream", "account", "--duration", "300ms")
	if code != 0 {
		t.Fatalf("account stream exit = %d (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"ssd+" + stubAccount, "sld+" + stubAccount, "spl", "usd+" + stubAccount, "uld+" + stubAccount, "upl"} {
		if !stub.got(want) {
			t.Fatalf("account stream missing payload %q; payloads: %v", want, stub.payloads)
		}
	}

	_, stderr, code = runRoot(t, "--config", cfg,
		"stream", "notifications", "--duration", "300ms")
	if code != 0 {
		t.Fatalf("notifications stream exit = %d (stderr: %s)", code, stderr)
	}
}

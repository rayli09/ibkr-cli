package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, body string, perm os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMode() != ModeGateway {
		t.Errorf("mode = %v, want gateway", cfg.AuthMode())
	}
	if cfg.GatewayBaseURL() != DefaultGatewayURL {
		t.Errorf("gateway url = %q", cfg.GatewayBaseURL())
	}
}

func TestLoadRejectsWorldReadable(t *testing.T) {
	p := writeCfg(t, `{"mode":"oauth"}`, 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Errorf("err = %v, want 0600 enforcement error", err)
	}
}

func TestLoadValid(t *testing.T) {
	p := writeCfg(t, `{"mode":"oauth","default_account_id":"DU123",`+
		`"oauth":{"consumer_key":"ABC","dh_generator":2}}`, 0o600)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMode() != ModeOAuth || cfg.DefaultAccount != "DU123" ||
		cfg.OAuth.ConsumerKey != "ABC" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestSaveRoundTrip0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	cfg := &Config{path: p, Mode: ModeGateway, DefaultAccount: "U999"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("saved mode = %04o, want 0600", info.Mode().Perm())
	}
	back, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if back.DefaultAccount != "U999" {
		t.Errorf("round trip account = %q", back.DefaultAccount)
	}
}

func TestGatewayURLEnvOverride(t *testing.T) {
	t.Setenv("IBKR_GATEWAY_URL", "https://localhost:9999/v1/api/")
	cfg := &Config{GatewayURL: "https://example.test/v1/api"}
	if got := cfg.GatewayBaseURL(); got != "https://localhost:9999/v1/api" {
		t.Errorf("got %q", got)
	}
}

func TestPaperTag(t *testing.T) {
	if !IsPaperAccount("DU1234567") || IsPaperAccount("U1234567") {
		t.Error("paper detection wrong")
	}
	if AccountTag("DU1") != "PAPER" || AccountTag("U1") != "LIVE" {
		t.Error("tag wrong")
	}
}

func TestPathPrecedence(t *testing.T) {
	t.Setenv("IBKR_CLI_CONFIG", "/tmp/from-env.json")
	if p, _ := Path(""); p != "/tmp/from-env.json" {
		t.Errorf("env path = %q", p)
	}
	if p, _ := Path("/tmp/flag.json"); p != "/tmp/flag.json" {
		t.Errorf("flag path = %q", p)
	}
}

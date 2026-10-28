package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayli09/ibkr-cli/internal/apierr"
)

// The setup tests drive every branch without docker or IBKR: a fake
// `docker` shell script goes on PATH (it answers info / compose ps
// from a state file and logs every invocation), and the gateway probe
// points at a stub CPAPI (the shared harness, or a tiny local one for
// the not-authenticated payload).

const sentinelPassword = "SENTINEL_PW_9f3e2a"
const sentinelTOTPSecret = "SENTINEL_TOTP_7b41c0"

const fakePSExited = `[{"Name":"ibeam","Service":"ibeam","State":"exited","Status":"Exited (0) 2 hours ago"}]`
const fakePSRunning = `[{"Name":"ibeam","Service":"ibeam","State":"running","Status":"Up 3 minutes"}]`

const fakeDockerScript = `#!/bin/sh
echo "$*" >> "$FAKE_DOCKER_LOG"
if [ "$1" = "info" ]; then
  echo "Server Version: 27.0.3"
  exit 0
fi
if [ "$1" = "compose" ]; then
  shift
  sub=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -f) shift 2 ;;
      ps|up|down|version) sub="$1"; break ;;
      *) shift ;;
    esac
  done
  case "$sub" in
    ps) cat "$FAKE_DOCKER_PS" ;;
    up)
      printf '%s' '[{"Name":"ibeam","Service":"ibeam","State":"running","Status":"Up 1 second"}]' > "$FAKE_DOCKER_PS"
      echo "Container ibeam Started" ;;
    down)
      printf '%s' '[]' > "$FAKE_DOCKER_PS"
      echo "Container ibeam Removed" ;;
    version) echo "Docker Compose version v2.29.0" ;;
  esac
  exit 0
fi
exit 1
`

// installFakeDocker writes the fake docker script into a temp bin
// dir, prepends it to PATH, and returns the invocation log path.
func installFakeDocker(t *testing.T, psState string) string {
	t.Helper()
	bin := t.TempDir()
	script := filepath.Join(bin, "docker")
	if err := os.WriteFile(script, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	psFile := filepath.Join(stateDir, "ps.json")
	if err := os.WriteFile(psFile, []byte(psState), 0o644); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(stateDir, "docker.log")
	t.Setenv("FAKE_DOCKER_LOG", logFile)
	t.Setenv("FAKE_DOCKER_PS", psFile)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logFile
}

// makeDeployDir creates a scaffold dir: compose.yaml always, plus an
// env.list (0600) when withEnv.
func makeDeployDir(t *testing.T, withEnv bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"),
		[]byte("services:\n  ibeam:\n    image: voyz/ibeam\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "env.list.example"),
		[]byte("IBEAM_ACCOUNT=your_ibkr_username\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withEnv {
		body := fmt.Sprintf("IBEAM_ACCOUNT=testuser\nIBEAM_PASSWORD=%s\nIBEAM_USE_PAPER_ACCOUNT=true\nIBEAM_TWO_FA_HANDLER=PyotpHandler\nIBEAM_PYOTP_SECRET=%s\n",
			sentinelPassword, sentinelTOTPSecret)
		if err := os.WriteFile(filepath.Join(dir, "env.list"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func dockerLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

type setupVerdict struct {
	Ready bool `json:"ready"`
	Stage string
	Steps []struct {
		Name   string
		Status string
		Detail string
		Fix    string
	}
	NextActions []string `json:"next_actions"`
}

func parseVerdict(t *testing.T, stdout string) setupVerdict {
	t.Helper()
	var v setupVerdict
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("setup --json output does not parse: %v\n%s", err, stdout)
	}
	return v
}

func stepStatus(v setupVerdict, name string) string {
	for _, s := range v.Steps {
		if s.Name == name {
			return s.Status
		}
	}
	return ""
}

// No docker on PATH at all -> usage-level verdict, stage no-docker.
func TestSetupNoDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty: no docker anywhere
	dir := makeDeployDir(t, true)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, _, code := runRoot(t, "--json", "--config", cfg, "setup", "--dir", dir)
	if code != apierr.ExitUsage {
		t.Fatalf("exit = %d, want 2", code)
	}
	v := parseVerdict(t, stdout)
	if v.Ready || v.Stage != "no-docker" {
		t.Fatalf("verdict = %+v, want ready=false stage=no-docker", v)
	}
	if stepStatus(v, "docker-cli") != "fail" || stepStatus(v, "docker-daemon") != "skipped" {
		t.Fatalf("unexpected steps: %+v", v.Steps)
	}
	if len(v.NextActions) == 0 {
		t.Fatal("next_actions empty on a failed verdict")
	}
}

// env.list missing -> stage no-env, exit 2.
func TestSetupEnvMissing(t *testing.T) {
	installFakeDocker(t, fakePSRunning)
	dir := makeDeployDir(t, false)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, _, code := runRoot(t, "--json", "--config", cfg, "setup", "--dir", dir)
	if code != apierr.ExitUsage {
		t.Fatalf("exit = %d, want 2", code)
	}
	v := parseVerdict(t, stdout)
	if v.Stage != "no-env" || stepStatus(v, "env-file") != "fail" {
		t.Fatalf("verdict = %+v", v)
	}
}

// Container down and no --up: report only — compose up must NOT be
// invoked — and the stage is stack-down (exit 3).
func TestSetupContainerDownReportOnly(t *testing.T) {
	log := installFakeDocker(t, fakePSExited)
	dir := makeDeployDir(t, true)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, _, code := runRoot(t, "--json", "--config", cfg, "setup", "--dir", dir,
		"--base-url", "http://127.0.0.1:1/v1/api")
	if code != apierr.ExitSession {
		t.Fatalf("exit = %d, want 3", code)
	}
	v := parseVerdict(t, stdout)
	if v.Stage != "stack-down" || stepStatus(v, "container") != "fail" {
		t.Fatalf("verdict = %+v", v)
	}
	if strings.Contains(dockerLog(t, log), "up -d") {
		t.Fatalf("compose up was invoked without --up; log:\n%s", dockerLog(t, log))
	}
}

// Container down with --up: compose up runs, the (fake) container
// comes up, the probe hits the stub, and the verdict is ready.
func TestSetupUpStartsStack(t *testing.T) {
	log := installFakeDocker(t, fakePSExited)
	dir := makeDeployDir(t, true)
	stub := newStubCPAPI(t)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, stderr, code := runRoot(t, "--json", "--config", cfg, "setup", "--dir", dir,
		"--base-url", stub.baseURL(), "--up")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)\nstdout: %s", code, stderr, stdout)
	}
	v := parseVerdict(t, stdout)
	if !v.Ready || v.Stage != "paper-ready" {
		t.Fatalf("verdict = %+v, want ready paper-ready", v)
	}
	if !strings.Contains(dockerLog(t, log), "up -d") {
		t.Fatalf("compose up not invoked; log:\n%s", dockerLog(t, log))
	}
}

// Container reported running but nothing answers on the gateway port.
func TestSetupGatewayUnreachable(t *testing.T) {
	installFakeDocker(t, fakePSRunning)
	dir := makeDeployDir(t, true)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, _, code := runRoot(t, "--json", "--config", cfg, "setup", "--dir", dir,
		"--base-url", "http://127.0.0.1:1/v1/api")
	if code != apierr.ExitSession {
		t.Fatalf("exit = %d, want 3", code)
	}
	v := parseVerdict(t, stdout)
	if v.Stage != "gateway-unreachable" || stepStatus(v, "gateway-probe") != "fail" {
		t.Fatalf("verdict = %+v", v)
	}
}

// All green with --init-config: ready, exit 0, the JSON carries the
// documented verdict fields, the config file is written 0600 in
// gateway mode, and no credential value ever reaches the output.
func TestSetupAllGreenReadyAndInitConfig(t *testing.T) {
	installFakeDocker(t, fakePSRunning)
	dir := makeDeployDir(t, true)
	stub := newStubCPAPI(t)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, stderr, code := runRoot(t, "--json", "--config", cfg, "setup", "--dir", dir,
		"--base-url", stub.baseURL(), "--init-config")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, secret := range []string{sentinelPassword, sentinelTOTPSecret} {
		if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
			t.Fatalf("credential value %q leaked into output", secret)
		}
	}
	v := parseVerdict(t, stdout)
	if !v.Ready || v.Stage != "paper-ready" {
		t.Fatalf("verdict = %+v", v)
	}
	for _, name := range []string{"scaffold", "docker-cli", "docker-daemon", "env-file", "container", "cli-config", "gateway-probe", "session"} {
		if stepStatus(v, name) == "" {
			t.Errorf("step %q missing from verdict", name)
		}
	}
	if len(v.NextActions) == 0 {
		t.Error("ready verdict should carry the smoke-ladder next_actions")
	}
	// --init-config wrote a 0600 gateway config pointing at the stub.
	info, err := os.Stat(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), `"gateway"`) || !strings.Contains(string(data), stub.baseURL()) {
		t.Fatalf("config not gateway-mode at the stub URL: %s", data)
	}
	// The env step reports key NAMES and set-state only.
	for _, s := range v.Steps {
		if s.Name == "env-file" && !strings.Contains(s.Detail, "IBEAM_PASSWORD=set") {
			t.Errorf("env step should report IBEAM_PASSWORD=set, got %q", s.Detail)
		}
	}
}

// Gateway answers but the session is not authenticated ->
// session-not-authenticated, exit 3. Uses a tiny local stub because
// the shared harness always answers authenticated.
func TestSetupSessionNotAuthenticated(t *testing.T) {
	installFakeDocker(t, fakePSRunning)
	dir := makeDeployDir(t, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"authenticated":false,"connected":false,"competing":false}`)
	}))
	t.Cleanup(srv.Close)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, _, code := runRoot(t, "--json", "--config", cfg, "setup", "--dir", dir,
		"--base-url", srv.URL+"/v1/api")
	if code != apierr.ExitSession {
		t.Fatalf("exit = %d, want 3", code)
	}
	v := parseVerdict(t, stdout)
	if v.Stage != "session-not-authenticated" || stepStatus(v, "session") != "fail" {
		t.Fatalf("verdict = %+v", v)
	}
}

// --down stops the stack and reports stack-down without probing.
func TestSetupDownStopsStack(t *testing.T) {
	log := installFakeDocker(t, fakePSRunning)
	dir := makeDeployDir(t, true)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, _, code := runRoot(t, "--json", "--config", cfg, "setup", "--dir", dir, "--down")
	if code != apierr.ExitSession {
		t.Fatalf("exit = %d, want 3", code)
	}
	v := parseVerdict(t, stdout)
	if v.Stage != "stack-down" || stepStatus(v, "gateway-probe") != "skipped" {
		t.Fatalf("verdict = %+v", v)
	}
	if !strings.Contains(dockerLog(t, log), "down") {
		t.Fatalf("compose down not invoked; log:\n%s", dockerLog(t, log))
	}
}

// Human output (no --json) renders the step table and verdict.
func TestSetupHumanOutput(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := makeDeployDir(t, true)
	cfg := filepath.Join(t.TempDir(), "config.json")
	stdout, _, code := runRoot(t, "--config", cfg, "setup", "--dir", dir)
	if code != apierr.ExitUsage {
		t.Fatalf("exit = %d, want 2", code)
	}
	for _, want := range []string{"docker-cli", "NOT READY", "no-docker", "next actions:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("human output missing %q:\n%s", want, stdout)
		}
	}
}

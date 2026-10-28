package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/auth"
	"github.com/rayli09/ibkr-cli/internal/config"
)

// `ibkr-cli setup` is the agent-first bootstrap for Mode G: it walks
// the gateway stack (docker -> IBeam scaffold -> env.list -> container
// -> gateway -> CLI config -> two-tier session) and reports every
// step with a named fix, without ever prompting and without ever
// printing a credential value. The exit-code contract:
//
//	0  ready: gateway authenticated + brokerage session live
//	3  the stack or the session is not up (stack-down,
//	   gateway-unreachable, session-not-authenticated)
//	2  usage-level problem (no-docker, bad-dir, no-env)
//
// With --json the stdout payload is a single verdict object:
// {"ready", "stage", "steps":[{name,status,detail,fix}],
// "next_actions":[...]} — see README "Agent quickstart".

type setupStep struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail | skipped
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

type setupReport struct {
	Ready       bool        `json:"ready"`
	Stage       string      `json:"stage"`
	DeployDir   string      `json:"deploy_dir"`
	BaseURL     string      `json:"base_url"`
	Paper       bool        `json:"paper_account"`
	Steps       []setupStep `json:"steps"`
	NextActions []string    `json:"next_actions"`
}

// shellRunner is the small exec seam for docker: everything goes
// through the `docker` binary on PATH, so tests can prepend a fake
// docker script to PATH and drive every branch deterministically.
type shellRunner struct{}

func (shellRunner) lookPath(name string) (string, error) { return exec.LookPath(name) }

func (shellRunner) run(dir, name string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	c := exec.CommandContext(cctx, name, args...)
	if dir != "" {
		c.Dir = dir
	}
	out, err := c.CombinedOutput()
	return string(out), err
}

var shell = shellRunner{}

type setupFlags struct {
	dir        string
	baseURL    string
	up         bool
	down       bool
	wait       time.Duration
	initConfig bool
}

func newSetupCmd(opts *options) *cobra.Command {
	f := &setupFlags{}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Diagnose (and optionally start) the IBeam gateway stack for Mode G; never prompts",
		Long: `Walk the Mode G gateway stack and report each step with a fix:

  docker CLI -> daemon -> deploy/ibeam scaffold -> env.list ->
  ibeam container -> gateway probe -> CLI config -> session verdict

The scaffold lives in deploy/ibeam in this repository (compose.yaml +
env.list.example + runbook). setup never reads the IBKR password:
compose reads env.list itself; setup only reports which keys are set.
It never prompts — missing inputs are reported as steps with fixes.

Exit codes: 0 = ready (gateway authenticated, brokerage session
live); 3 = stack/session not up; 2 = usage-level problem (no docker,
no env.list, bad scaffold dir). With --json, stdout is one verdict
object: {"ready","stage","steps","next_actions"}.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.up && f.down {
				return &apierr.UsageError{Msg: "--up and --down are mutually exclusive"}
			}
			rep, code := runSetup(opts, f)
			printSetupReport(opts, rep)
			switch code {
			case apierr.ExitOK:
				return nil
			case apierr.ExitUsage:
				return &apierr.UsageError{Msg: "setup: not ready (stage " + rep.Stage + ")"}
			default:
				return &apierr.SessionError{Msg: "setup: not ready (stage " + rep.Stage + ")"}
			}
		},
	}
	cmd.Flags().StringVar(&f.dir, "dir", "",
		"scaffold dir (default: $IBKR_CLI_DEPLOY_DIR or ./deploy/ibeam)")
	cmd.Flags().StringVar(&f.baseURL, "base-url", "",
		"gateway base URL to probe (default: global --base-url, else config, else https://localhost:5000/v1/api)")
	cmd.Flags().BoolVar(&f.up, "up", false,
		"if the container is not running, start it (docker compose up -d)")
	cmd.Flags().BoolVar(&f.down, "down", false,
		"stop the stack (docker compose down), then report")
	cmd.Flags().DurationVar(&f.wait, "wait", 0,
		"poll the gateway probe until the brokerage session is live or this elapses (e.g. 180s)")
	cmd.Flags().BoolVar(&f.initConfig, "init-config", false,
		"write a gateway-mode CLI config non-interactively (base URL + optional global --account)")
	return cmd
}

// resolveSetupBaseURL: setup flag -> global flag -> config -> default.
func resolveSetupBaseURL(opts *options, f *setupFlags) string {
	if f.baseURL != "" {
		return strings.TrimSuffix(f.baseURL, "/")
	}
	if opts.baseURL != "" {
		return strings.TrimSuffix(opts.baseURL, "/")
	}
	if cfg, err := config.Load(opts.configPath); err == nil {
		return cfg.GatewayBaseURL()
	}
	return config.DefaultGatewayURL
}

func resolveDeployDir(f *setupFlags) string {
	if f.dir != "" {
		return f.dir
	}
	if d := os.Getenv("IBKR_CLI_DEPLOY_DIR"); d != "" {
		return d
	}
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Join("deploy", "ibeam")
	}
	return filepath.Join(wd, "deploy", "ibeam")
}

func runSetup(opts *options, f *setupFlags) (*setupReport, int) {
	rep := &setupReport{BaseURL: resolveSetupBaseURL(opts, f)}
	dir := resolveDeployDir(f)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	rep.DeployDir = dir
	composePath := filepath.Join(dir, "compose.yaml")
	envPath := filepath.Join(dir, "env.list")

	add := func(name, status, detail, fix string) {
		rep.Steps = append(rep.Steps, setupStep{Name: name, Status: status, Detail: detail, Fix: fix})
	}

	// --- scaffold dir -------------------------------------------------
	dirOK := false
	if info, err := os.Stat(composePath); err == nil && !info.IsDir() {
		dirOK = true
		add("scaffold", "ok", dir, "")
	} else {
		add("scaffold", "fail", "no compose.yaml in "+dir,
			"run from the ibkr-cli repo root, or pass --dir <repo>/deploy/ibeam")
	}

	// --- docker CLI + daemon -------------------------------------------
	dockerPath, lookErr := shell.lookPath("docker")
	dockerOK := lookErr == nil
	if dockerOK {
		add("docker-cli", "ok", dockerPath, "")
	} else {
		add("docker-cli", "fail", "docker not found on PATH",
			"install Docker first (deploy/README.md step 0); the scaffold does not install it")
	}
	daemonOK := false
	switch {
	case !dockerOK:
		add("docker-daemon", "skipped", "no docker CLI", "")
	case f.down:
		// A down request is meaningful even with the daemon state
		// unknown; compose down reports its own failure below.
		daemonOK = true
		add("docker-daemon", "ok", "not probed (--down requested)", "")
	default:
		if out, err := shell.run("", "docker", "info"); err == nil {
			daemonOK = true
			add("docker-daemon", "ok", "daemon reachable", "")
		} else {
			add("docker-daemon", "fail", "docker daemon not reachable: "+firstLine(out),
				"start Docker (Docker Desktop / dockerd), then re-run")
		}
	}

	// --- env.list --------------------------------------------------------
	envState := "missing" // missing | incomplete | ok
	envVals := map[string]string{}
	if data, err := os.ReadFile(envPath); err == nil {
		envVals = parseEnvFile(data)
		var failIssues, warnIssues []string
		info, _ := os.Stat(envPath)
		if info != nil && info.Mode().Perm()&0o077 != 0 {
			warnIssues = append(warnIssues, fmt.Sprintf("mode is %04o, want 0600", info.Mode().Perm()))
		}
		if !envKeySet(envVals, "IBEAM_ACCOUNT") {
			failIssues = append(failIssues, "IBEAM_ACCOUNT unset")
		}
		if !envKeySet(envVals, "IBEAM_PASSWORD") {
			failIssues = append(failIssues, "IBEAM_PASSWORD unset")
		}
		if v, present := envVals["IBEAM_USE_PAPER_ACCOUNT"]; present {
			rep.Paper = strings.EqualFold(strings.TrimSpace(v), "true")
		} else {
			warnIssues = append(warnIssues, "IBEAM_USE_PAPER_ACCOUNT unset (IBeam defaults to LIVE)")
		}
		detail := fmt.Sprintf("keys: IBEAM_ACCOUNT=%s, IBEAM_PASSWORD=%s, IBEAM_USE_PAPER_ACCOUNT=%s",
			setUnset(envVals, "IBEAM_ACCOUNT"), setUnset(envVals, "IBEAM_PASSWORD"), paperString(envVals))
		switch {
		case len(failIssues) > 0:
			envState = "incomplete"
			add("env-file", "fail", detail+"; "+strings.Join(failIssues, "; "),
				"fill in the missing keys in "+envPath+" (template: env.list.example)")
		case len(warnIssues) > 0:
			envState = "ok"
			add("env-file", "warn", detail+"; "+strings.Join(warnIssues, "; "),
				"chmod 600 "+envPath+" and set IBEAM_USE_PAPER_ACCOUNT explicitly")
		default:
			envState = "ok"
			add("env-file", "ok", detail, "")
		}
	} else {
		add("env-file", "fail", "no env.list in "+dir,
			"cp "+filepath.Join(dir, "env.list.example")+" "+envPath+", chmod 600 it, and fill in IBEAM_ACCOUNT/IBEAM_PASSWORD")
	}

	// --- container -------------------------------------------------------
	containerRunning := false
	containerChecked := false
	switch {
	case !dirOK || !dockerOK || !daemonOK:
		add("container", "skipped", "docker/scaffold prerequisites unmet", "")
	case f.down:
		out, err := shell.run(dir, "docker", "compose", "-f", composePath, "down")
		containerChecked = true
		if err != nil {
			add("container", "fail", "docker compose down failed: "+firstLine(out),
				"inspect manually: docker compose -f "+composePath+" down")
		} else {
			add("container", "ok", "stack stopped via docker compose down", "")
		}
	default:
		containerChecked = true
		running, detail, err := composePS(dir, composePath)
		if err != nil {
			add("container", "fail", "docker compose ps failed: "+detail,
				"check the scaffold: docker compose -f "+composePath+" ps")
			break
		}
		containerRunning = running
		if !running && f.up {
			out, upErr := shell.run(dir, "docker", "compose", "-f", composePath, "up", "-d")
			if upErr != nil {
				add("container", "fail", "was not running; docker compose up -d failed: "+firstLine(out),
					"read the compose output, fix it, then re-run setup --up")
				break
			}
			detail += "; started via docker compose up -d"
			running, _, err = composePS(dir, composePath)
			if err == nil {
				containerRunning = running
			}
		}
		if containerRunning {
			add("container", "ok", detail, "")
		} else {
			add("container", "fail", detail,
				"start it: ibkr-cli setup --up (or docker compose -f "+composePath+" up -d)")
		}
	}
	_ = containerChecked

	// --- CLI config (before the probe so --init-config lands regardless) --
	writeConfigStep(opts, f, rep.BaseURL, add)

	// --- gateway probe + session verdict --------------------------------
	probeReachable, sessionLive := false, false
	if f.down {
		add("gateway-probe", "skipped", "stack intentionally down (--down)", "")
		add("session", "skipped", "stack intentionally down (--down)", "")
	} else {
		st, reachable, perr := probeGateway(rep.BaseURL, f.wait)
		probeReachable = reachable
		switch {
		case !reachable:
			add("gateway-probe", "fail", "no answer from "+rep.BaseURL+": "+perr,
				"start the stack (setup --up) and check: docker compose -f "+composePath+" logs ibeam")
			add("session", "skipped", "gateway unreachable", "")
		case perr != "":
			// Gateway answered but the session is not authenticated
			// (401/403 surface from the provider as an error).
			add("gateway-probe", "ok", "gateway answered at "+rep.BaseURL+" (session not authenticated)", "")
			add("session", "fail", perr,
				"IBeam login has not completed: check env.list credentials and 2FA handler, then docker compose logs ibeam")
		default:
			detail := fmt.Sprintf("authenticated=%v connected=%v competing=%v", st.Authenticated, st.BrokerageLive, st.Competing)
			if st.ServerName != "" {
				detail += " server=" + st.ServerName
			}
			add("gateway-probe", "ok", detail, "")
			switch {
			case st.Authenticated && st.BrokerageLive:
				sessionLive = true
				add("session", "ok", "authenticated + brokerage session live (two tiers up)", "")
			case st.Authenticated:
				add("session", "fail", "authenticated but brokerage session not live",
					"wait for IBeam to finish login, or run: ibkr-cli auth reauth")
			default:
				add("session", "fail", "gateway session not authenticated",
					"IBeam login has not completed: check env.list credentials and 2FA handler, then docker compose logs ibeam")
			}
		}
	}

	// --- stage + exit code ------------------------------------------------
	code := apierr.ExitOK
	switch {
	case !dirOK:
		rep.Stage = "bad-dir"
		code = apierr.ExitUsage
	case !dockerOK:
		rep.Stage = "no-docker"
		code = apierr.ExitUsage
	case envState != "ok":
		rep.Stage = "no-env"
		code = apierr.ExitUsage
	case !daemonOK || !containerRunning:
		rep.Stage = "stack-down"
		code = apierr.ExitSession
	case !probeReachable:
		rep.Stage = "gateway-unreachable"
		code = apierr.ExitSession
	case !sessionLive:
		rep.Stage = "session-not-authenticated"
		code = apierr.ExitSession
	default:
		rep.Ready = true
		if rep.Paper {
			rep.Stage = "paper-ready"
		} else {
			rep.Stage = "ready"
		}
	}

	if rep.Ready {
		rep.NextActions = []string{
			"ibkr-cli auth status",
			"ibkr-cli accounts list",
			"ibkr-cli market quote 265598",
			"ibkr-cli orders list",
		}
	} else {
		seen := map[string]bool{}
		for _, s := range rep.Steps {
			if (s.Status == "fail" || s.Status == "warn") && s.Fix != "" && !seen[s.Fix] {
				seen[s.Fix] = true
				rep.NextActions = append(rep.NextActions, s.Fix)
			}
		}
	}
	if rep.NextActions == nil {
		rep.NextActions = []string{}
	}
	return rep, code
}

// writeConfigStep implements step 7: report the CLI config state, or
// write a gateway-mode config when --init-config is set.
func writeConfigStep(opts *options, f *setupFlags, baseURL string, add func(name, status, detail, fix string)) {
	cfgPath, err := config.Path(opts.configPath)
	if err != nil {
		add("cli-config", "fail", "cannot resolve config path: "+err.Error(), "")
		return
	}
	if f.initConfig {
		cfg, err := config.Load(opts.configPath)
		if err != nil {
			add("cli-config", "fail", "cannot load existing config: "+err.Error(),
				"fix the config file (mode 0600) at "+cfgPath+", then re-run")
			return
		}
		was := cfg.AuthMode()
		cfg.Mode = config.ModeGateway
		cfg.GatewayURL = baseURL
		if opts.account != "" {
			cfg.DefaultAccount = opts.account
		}
		if err := cfg.Save(); err != nil {
			add("cli-config", "fail", "writing config failed: "+err.Error(), "")
			return
		}
		detail := "wrote gateway-mode config at " + cfgPath + " (0600)"
		if was == config.ModeOAuth {
			detail += "; previous mode was oauth — switched to gateway"
		}
		if cfg.DefaultAccount != "" {
			detail += "; default account " + cfg.DefaultAccount
		}
		add("cli-config", "ok", detail, "")
		return
	}
	if _, err := os.Stat(cfgPath); err != nil {
		add("cli-config", "warn", "no config file at "+cfgPath,
			"write one non-interactively: ibkr-cli setup --init-config")
		return
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		add("cli-config", "warn", "config at "+cfgPath+" does not load: "+err.Error(),
			"fix it, or rewrite it: ibkr-cli setup --init-config")
		return
	}
	if cfg.AuthMode() != config.ModeGateway {
		add("cli-config", "warn", "config at "+cfgPath+" is mode "+string(cfg.AuthMode())+"; other commands will not use the gateway stack",
			"switch it: ibkr-cli setup --init-config")
		return
	}
	detail := "gateway-mode config at " + cfgPath + " (gateway_url " + cfg.GatewayBaseURL() + ")"
	if cfg.DefaultAccount != "" {
		detail += "; default account " + cfg.DefaultAccount
	}
	add("cli-config", "ok", detail, "")
}

// probeGateway runs the existing gateway code path (GatewayProvider
// .Status -> POST /iserver/auth/status, loopback TLS posture
// included) and polls while wait > 0. It returns the last state,
// whether the gateway answered at all, and a session-error string
// when the gateway answered "not authenticated" (401/403).
func probeGateway(baseURL string, wait time.Duration) (auth.SessionState, bool, string) {
	cfg := &config.Config{Mode: config.ModeGateway, GatewayURL: baseURL}
	provider := auth.NewGatewayProvider(cfg)
	deadline := time.Now().Add(wait)
	for {
		st, err := provider.Status(context.Background())
		if err == nil {
			if st.BrokerageLive || wait <= 0 || !time.Now().Before(deadline) {
				return st, true, ""
			}
		} else {
			msg := err.Error()
			if strings.Contains(msg, "not authenticated") {
				if wait <= 0 || !time.Now().Before(deadline) {
					return st, true, "gateway session not authenticated (401/403)"
				}
			} else if wait <= 0 || !time.Now().Before(deadline) {
				return st, false, msg
			}
		}
		pause := 5 * time.Second
		if rem := time.Until(deadline); rem < pause {
			pause = rem
		}
		if pause <= 0 {
			continue
		}
		time.Sleep(pause)
	}
}

// composePS runs `docker compose ps --format json` and reports
// whether the ibeam container is running.
func composePS(dir, composePath string) (bool, string, error) {
	out, err := shell.run(dir, "docker", "compose", "-f", composePath, "ps", "--format", "json")
	if err != nil {
		return false, firstLine(out), err
	}
	entries := parseComposePS(out)
	if len(entries) == 0 {
		return false, "no containers in the compose project (never started)", nil
	}
	for _, e := range entries {
		name := e.Name
		if name == "" {
			name = e.Service
		}
		if e.Service == "ibeam" || strings.Contains(name, "ibeam") {
			return psRunning(e), "ibeam container: " + psStateText(e), nil
		}
	}
	e := entries[0]
	return psRunning(e), "container " + e.Name + ": " + psStateText(e), nil
}

type composePSEntry struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Status  string `json:"Status"`
}

// parseComposePS tolerates both compose ps output shapes: a JSON
// array (current) and NDJSON (older compose versions).
func parseComposePS(out string) []composePSEntry {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var arr []composePSEntry
	if err := json.Unmarshal([]byte(out), &arr); err == nil {
		return arr
	}
	var entries []composePSEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e composePSEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			entries = append(entries, e)
		}
	}
	return entries
}

func psRunning(e composePSEntry) bool {
	if strings.EqualFold(e.State, "running") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(e.Status), "up")
}

func psStateText(e composePSEntry) string {
	if e.Status != "" {
		return e.Status
	}
	return e.State
}

// parseEnvFile parses docker env-file lines (KEY=VALUE, # comments,
// optional `export` and one pair of quotes). Values are kept in
// memory only and are never printed by setup — the single exception
// is IBEAM_USE_PAPER_ACCOUNT, a setting, reported as a boolean.
func parseEnvFile(data []byte) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && v[0] == v[len(v)-1] && (v[0] == '"' || v[0] == '\'') {
			v = v[1 : len(v)-1]
		}
		m[k] = v
	}
	return m
}

// envKeySet reports whether a key carries a real value (non-empty
// and not the shipped placeholder for that key).
func envKeySet(vals map[string]string, key string) bool {
	v := strings.TrimSpace(vals[key])
	if v == "" {
		return false
	}
	if key == "IBEAM_ACCOUNT" && v == "your_ibkr_username" {
		return false
	}
	return true
}

func setUnset(vals map[string]string, key string) string {
	if envKeySet(vals, key) {
		return "set"
	}
	return "UNSET"
}

func paperString(vals map[string]string) string {
	if v, ok := vals["IBEAM_USE_PAPER_ACCOUNT"]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return "unset"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if s == "" {
		return "(no output)"
	}
	return s
}

// printSetupReport renders the verdict: one JSON object with --json,
// a step table otherwise.
func printSetupReport(opts *options, rep *setupReport) {
	if opts.jsonOut {
		out, err := json.MarshalIndent(rep, "", "  ")
		if err == nil {
			fmt.Println(string(out))
			return
		}
	}
	fmt.Printf("ibkr-cli setup — scaffold: %s\n", rep.DeployDir)
	fmt.Printf("gateway: %s\n\n", rep.BaseURL)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "step\tstatus\tdetail")
	for _, s := range rep.Steps {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.Status, s.Detail)
	}
	w.Flush()
	fmt.Println()
	if rep.Ready {
		fmt.Printf("verdict: READY (stage %s)\n", rep.Stage)
	} else {
		fmt.Printf("verdict: NOT READY (stage %s)\n", rep.Stage)
	}
	if len(rep.NextActions) > 0 {
		fmt.Println("next actions:")
		for _, a := range rep.NextActions {
			fmt.Println("  -", a)
		}
	}
}

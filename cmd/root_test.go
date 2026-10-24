package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayli09/ibkr-cli/internal/apierr"
)

// captureOutput runs fn with stdout and stderr redirected to pipes.
func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr
	doneOut := make(chan string, 1)
	doneErr := make(chan string, 1)
	go func() { b, _ := io.ReadAll(rOut); doneOut <- string(b) }()
	go func() { b, _ := io.ReadAll(rErr); doneErr <- string(b) }()
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()
	fn()
	wOut.Close()
	wErr.Close()
	return <-doneOut, <-doneErr
}

// runRoot executes the real command tree against args and reports
// the exit code exactly as Execute() computes it.
func runRoot(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	root, opts := NewRootCmd()
	root.SetArgs(args)
	var runErr error
	stdout, stderr := captureOutput(t, func() {
		runErr = root.Execute()
		if runErr != nil {
			fmt.Fprintln(os.Stderr, "error:", runErr)
		}
	})
	if runErr == nil {
		return stdout, stderr, apierr.ExitOK
	}
	if code := apierr.ExitCode(runErr); code != 1 {
		return stdout, stderr, code
	}
	if !opts.preRunReached {
		return stdout, stderr, apierr.ExitUsage
	}
	return stdout, stderr, 1
}

// writeStubConfig writes a 0600 gateway-mode config pointing at the
// stub and returns its path.
func writeStubConfig(t *testing.T, stub *stubCPAPI) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := fmt.Sprintf(`{"mode":"gateway","gateway_url":%q,"default_account_id":%q}`,
		stub.baseURL(), stubAccount)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHelpListsCommands(t *testing.T) {
	stdout, _, code := runRoot(t, "--help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"auth", "accounts", "market", "orders"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help missing %q", want)
		}
	}
	// The full leaf set: 6 auth + 5 accounts + 7 market + 7 orders.
	stdout, _, _ = runRoot(t, "orders", "--help")
	for _, want := range []string{"list", "status", "trades", "place", "reply", "modify", "cancel"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("orders help missing %q", want)
		}
	}
}

func TestAuthStatusAgainstStub(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	stdout, _, code := runRoot(t, "--config", cfg, "auth", "status")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "STUB1") || !strings.Contains(stdout, "brokerage session:") ||
		!strings.Contains(stdout, "true") {
		t.Fatalf("unexpected status output:\n%s", stdout)
	}
}

func TestAccountsListAndPositions(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	stdout, _, code := runRoot(t, "--config", cfg, "accounts", "list")
	if code != 0 || !strings.Contains(stdout, stubAccount) || !strings.Contains(stdout, "PAPER") {
		t.Fatalf("list: exit=%d out=%s", code, stdout)
	}
	stdout, _, code = runRoot(t, "--config", cfg, "accounts", "positions")
	if code != 0 || !strings.Contains(stdout, "AAPL") || !strings.Contains(stdout, "7502.50") {
		t.Fatalf("positions: exit=%d out=%s", code, stdout)
	}
}

// Snapshot priming: the stub's first snapshot for a conid carries no
// market data; the CLI must re-request and print the filled quote.
func TestQuotePrimingEndToEnd(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	stdout, stderr, code := runRoot(t, "--config", cfg, "market", "quote", "265598")
	if code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "750.25") {
		t.Fatalf("primed last price missing:\n%s", stdout)
	}
	if strings.Contains(stderr, "no market data") {
		t.Fatalf("priming should have filled the quote, stderr: %s", stderr)
	}
}

// Dry-run rail: place without --confirm runs whatif and sends
// nothing (the stub counts placements).
func TestOrderDryRunSendsNothing(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	stdout, _, code := runRoot(t, "--config", cfg,
		"orders", "place", "--conid", "265598", "--qty", "10", "--price", "200")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	place, whatif := stub.counts()
	if place != 0 {
		t.Fatalf("dry run placed %d orders, want 0", place)
	}
	if whatif != 1 {
		t.Fatalf("dry run ran whatif %d times, want 1", whatif)
	}
	for _, want := range []string{"DRY RUN", "PAPER", stubAccount, "whatif"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, stdout)
		}
	}
}

// The two-question flow: place -> question q1 (exit 5) -> reply ->
// question q2 (exit 5) -> reply -> order id (exit 0).
func TestOrderTwoQuestionFlow(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	stdout, _, code := runRoot(t, "--config", cfg,
		"orders", "place", "--conid", "265598", "--qty", "10", "--price", "200", "--confirm")
	if code != apierr.ExitQuestion {
		t.Fatalf("place exit = %d, want 5 (question pending)", code)
	}
	if !strings.Contains(stdout, "q1") || !strings.Contains(stdout, "significantly exceeds") {
		t.Fatalf("first question not printed verbatim:\n%s", stdout)
	}
	if place, _ := stub.counts(); place != 1 {
		t.Fatalf("place calls = %d, want 1", place)
	}

	stdout, _, code = runRoot(t, "--config", cfg, "orders", "reply", "q1", "--confirm")
	if code != apierr.ExitQuestion {
		t.Fatalf("reply q1 exit = %d, want 5", code)
	}
	if !strings.Contains(stdout, "q2") || !strings.Contains(stdout, "margin deficit") {
		t.Fatalf("second question not printed verbatim:\n%s", stdout)
	}

	stdout, _, code = runRoot(t, "--config", cfg, "orders", "reply", "q2", "--confirm")
	if code != 0 {
		t.Fatalf("reply q2 exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "9001") {
		t.Fatalf("order id missing after final reply:\n%s", stdout)
	}
}

// Replying without an explicit --confirm/--decline is a usage error:
// the CLI never answers a question implicitly.
func TestReplyRequiresExplicitAnswer(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	_, _, code := runRoot(t, "--config", cfg, "orders", "reply", "q1")
	if code != apierr.ExitUsage {
		t.Fatalf("exit = %d, want 2", code)
	}
	_, _, code = runRoot(t, "--config", cfg, "orders", "reply", "q1", "--confirm", "--decline")
	if code != apierr.ExitUsage {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// Session expiry: the stub answers 401 for account EXPIRED and the
// CLI must exit 3 (auth/session), not a generic failure.
func TestSessionExpiryExit3(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	_, _, code := runRoot(t, "--config", cfg, "--account", "EXPIRED", "accounts", "summary")
	if code != apierr.ExitSession {
		t.Fatalf("exit = %d, want 3", code)
	}
}

// Rate limiting: two forced 429s are retried through; a persistent
// 429 surfaces as exit 4.
func TestRateLimitRetryAndExhaustion(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	stub.armValidate429(2)
	_, _, code := runRoot(t, "--config", cfg, "auth", "validate")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 after retries", code)
	}
	stub.armValidate429(5)
	_, _, code = runRoot(t, "--config", cfg, "auth", "validate")
	if code != apierr.ExitAPI {
		t.Fatalf("exit = %d, want 4 on persistent 429", code)
	}
}

// Chain assembly end to end: strikes -> secdef/info per leg -> one
// primed snapshot set -> a strike x call/put table with IV/delta.
func TestChainAssemblyEndToEnd(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	stdout, _, code := runRoot(t, "--config", cfg,
		"market", "chain", "AAPL", "--expiry", "20261120")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"750", "28.50", "0.52"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("chain output missing %q:\n%s", want, stdout)
		}
	}
}

// Cancel dry-run shows the order but sends no DELETE (verified by
// re-checking: with --confirm the stub's cancel body comes back).
func TestCancelConfirmFlow(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)
	stdout, _, code := runRoot(t, "--config", cfg, "orders", "cancel", "12345")
	if code != 0 || !strings.Contains(stdout, "DRY RUN") {
		t.Fatalf("dry cancel: exit=%d out=%s", code, stdout)
	}
	stdout, _, code = runRoot(t, "--config", cfg, "orders", "cancel", "12345", "--confirm")
	if code != 0 || !strings.Contains(stdout, "Request was submitted") {
		t.Fatalf("confirmed cancel: exit=%d out=%s", code, stdout)
	}
}

// auth init writes a 0600 config via piped answers.
func TestAuthInitWritesConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	orig := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		fmt.Fprintln(w, "") // mode: default gateway
		fmt.Fprintln(w, "") // gateway URL: default
		fmt.Fprintln(w, "DU999999")
		w.Close()
	}()
	defer func() { os.Stdin = orig }()
	_, _, code := runRoot(t, "auth", "init", "--file", path)
	if code != 0 {
		t.Fatalf("init exit = %d, want 0", code)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "DU999999") {
		t.Fatalf("config missing account: %s", data)
	}
}

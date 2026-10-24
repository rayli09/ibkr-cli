package cmd

import (
	"strings"
	"testing"
)

// E2E coverage for the IBind parity extension: every new command
// group runs against the stub CPAPI (see stub_parity_test.go for the
// fixtures). Mutations are proven dry-run-safe by the stub counters.

func runOK(t *testing.T, cfg string, want string, args ...string) string {
	t.Helper()
	full := append([]string{"--config", cfg}, args...)
	stdout, stderr, code := runRoot(t, full...)
	if code != 0 {
		t.Fatalf("%v: exit = %d, want 0 (stderr: %s)", args, code, stderr)
	}
	if want != "" && !strings.Contains(stdout, want) {
		t.Fatalf("%v: output missing %q:\n%s", args, want, stdout)
	}
	return stdout
}

func TestParitySessionCommands(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	runOK(t, cfg, "healthy", "auth", "health")

	// logout is gated: dry run sends nothing.
	out := runOK(t, cfg, "DRY RUN", "auth", "logout")
	_ = out
	if stub.logoutCalls != 0 {
		t.Fatalf("logout dry run sent %d calls", stub.logoutCalls)
	}
	runOK(t, cfg, "success", "auth", "logout", "--confirm")
	if stub.logoutCalls != 1 {
		t.Fatalf("logout --confirm sent %d calls, want 1", stub.logoutCalls)
	}
}

func TestParityAccountsCommands(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	runOK(t, cfg, "netliquidation", "accounts", "summary-iserver")
	runOK(t, cfg, stubAccount, "accounts", "search", "paper")
	runOK(t, cfg, "Test Owner", "accounts", "signatures")

	runOK(t, cfg, "DRY RUN", "accounts", "switch", stubAccount)
	runOK(t, cfg, "DRY RUN", "accounts", "set-dynamic", stubAccount)
	if stub.switchCalls != 0 || stub.dynCalls != 0 {
		t.Fatalf("dry runs mutated: switch=%d dyn=%d", stub.switchCalls, stub.dynCalls)
	}
	runOK(t, cfg, "", "accounts", "switch", stubAccount, "--confirm")
	runOK(t, cfg, "", "accounts", "set-dynamic", stubAccount, "--confirm")
	if stub.switchCalls != 1 || stub.dynCalls != 1 {
		t.Fatalf("confirmed calls: switch=%d dyn=%d, want 1/1", stub.switchCalls, stub.dynCalls)
	}
}

func TestParityOrdersSuppress(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	runOK(t, cfg, "", "orders", "list", "--filters", "filled", "--force")
	runOK(t, cfg, "DRY RUN", "orders", "suppress", "o35442")
	runOK(t, cfg, "DRY RUN", "orders", "suppress-reset")
	if stub.suppressCalls != 0 {
		t.Fatalf("suppress dry runs sent %d calls", stub.suppressCalls)
	}
	runOK(t, cfg, "", "orders", "suppress", "o35442", "--confirm")
	runOK(t, cfg, "", "orders", "suppress-reset", "--confirm")
	if stub.suppressCalls != 2 {
		t.Fatalf("suppress confirmed calls = %d, want 2", stub.suppressCalls)
	}
}

func TestParityPortfolioCommands(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	runOK(t, cfg, "INDIVIDUAL", "portfolio", "accounts")
	runOK(t, cfg, stubAccount, "portfolio", "subaccounts")
	runOK(t, cfg, "subAccounts", "portfolio", "subaccounts-large")
	runOK(t, cfg, "INDIVIDUAL", "portfolio", "info")
	runOK(t, cfg, "netliquidationvalue", "portfolio", "summary")
	runOK(t, cfg, "assetClass", "portfolio", "allocation")
	runOK(t, cfg, stubAccount, "portfolio", "allocations")
	runOK(t, cfg, "9900001000", "portfolio", "combo-positions")
	runOK(t, cfg, "AAPL", "portfolio", "positions")
	runOK(t, cfg, "265598", "portfolio", "positions2")
	runOK(t, cfg, "position", "portfolio", "position", "265598")
	runOK(t, cfg, "AAPL", "portfolio", "position-info", "265598")
	runOK(t, cfg, "cashbalance", "portfolio", "ledger")
	runOK(t, cfg, "returns", "portfolio", "performance")
	runOK(t, cfg, stubAccount, "portfolio", "all-periods")
	runOK(t, cfg, "DIVIDEND", "portfolio", "transactions")

	runOK(t, cfg, "DRY RUN", "portfolio", "invalidate-cache")
	if stub.invalidateCalls != 0 {
		t.Fatalf("invalidate dry run sent %d calls", stub.invalidateCalls)
	}
	runOK(t, cfg, "", "portfolio", "invalidate-cache", "--confirm")
	if stub.invalidateCalls != 1 {
		t.Fatalf("invalidate confirmed = %d, want 1", stub.invalidateCalls)
	}
}

func TestParityContractCommands(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	runOK(t, cfg, "AAPL", "contract", "secdef", "265598")
	runOK(t, cfg, "265598", "contract", "all-conids")
	runOK(t, cfg, "AAPL", "contract", "info", "265598")
	runOK(t, cfg, "minTick", "contract", "info-rules", "265598")
	runOK(t, cfg, "minTick", "contract", "rules", "265598")
	runOK(t, cfg, "Adaptive", "contract", "algos", "265598")
	runOK(t, cfg, "stateCode", "contract", "bond-filters", "--symbol", "912797QK4")
	runOK(t, cfg, "EUR.USD", "contract", "pairs")
	runOK(t, cfg, "0.92", "contract", "exchangerate")
	runOK(t, cfg, "ESZ6", "contract", "futures", "ES")
	runOK(t, cfg, "tradingTimes", "contract", "schedule", "--asset-class", "STK", "--symbol", "AAPL")
	out := runOK(t, cfg, "", "contract", "stock-conid", "AAPL", "--json")
	if !strings.Contains(out, "265598") {
		t.Fatalf("stock-conid output missing conid:\n%s", out)
	}
}

func TestParityMarketCommands(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	out := runOK(t, cfg, "", "market", "quote-symbols", "AAPL")
	if !strings.Contains(out, "AAPL") || !strings.Contains(out, "750.25") {
		t.Fatalf("quote-symbols output:\n%s", out)
	}
	runOK(t, cfg, "749.90", "market", "regsnapshot", "265598")
	runOK(t, cfg, "750.25", "market", "history-beta", "265598")
	out = runOK(t, cfg, "", "market", "history-symbol", "AAPL")
	if !strings.Contains(out, "AAPL") {
		t.Fatalf("history-symbol output:\n%s", out)
	}
	out = runOK(t, cfg, "", "market", "history-multi", "265598")
	if !strings.Contains(out, "265598") {
		t.Fatalf("history-multi output:\n%s", out)
	}
	runOK(t, cfg, "", "market", "history", "265598", "--start-time", "20261001-09:30:00")
	runOK(t, cfg, "", "market", "strikes", "265598", "--month", "NOV26", "--sectype", "WAR")
}

func TestParityScannerCommands(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	runOK(t, cfg, "MOST_ACTIVE", "scanner", "params")
	runOK(t, cfg, "AAPL", "scanner", "run",
		"--instrument", "STK", "--type", "MOST_ACTIVE", "--location", "STK.US.MAJOR",
		"--filter", "priceAbove=100")
	runOK(t, cfg, "STK", "scanner", "hmds-params")
	runOK(t, cfg, "265598", "scanner", "hmds-run",
		"--instrument", "STK", "--location", "STK.US", "--scan-code", "HOT_BY_VOLUME", "--sectype", "STK")
}

func TestParityWatchlistCommands(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	runOK(t, cfg, "userWatchlists", "watchlist", "list")
	runOK(t, cfg, "Tech", "watchlist", "get", "--id", "12345")

	runOK(t, cfg, "DRY RUN", "watchlist", "create", "--name", "Tech", "--conids", "265598")
	runOK(t, cfg, "DRY RUN", "watchlist", "delete", "--id", "12345")
	if stub.wlCreateCalls != 0 || stub.wlDeleteCalls != 0 {
		t.Fatalf("watchlist dry runs mutated: create=%d delete=%d", stub.wlCreateCalls, stub.wlDeleteCalls)
	}
	runOK(t, cfg, "12345", "watchlist", "create", "--name", "Tech", "--conids", "265598", "--confirm")
	runOK(t, cfg, "deleted", "watchlist", "delete", "--id", "12345", "--confirm")
	if stub.wlCreateCalls != 1 || stub.wlDeleteCalls != 1 {
		t.Fatalf("watchlist confirmed: create=%d delete=%d, want 1/1", stub.wlCreateCalls, stub.wlDeleteCalls)
	}
}

func TestParityFACommands(t *testing.T) {
	stub := newStubCPAPI(t)
	cfg := writeStubConfig(t, stub)

	runOK(t, cfg, "M1", "fa", "models")
	runOK(t, cfg, "0.9", "fa", "model-positions", "--model", "M1")
	runOK(t, cfg, "aum", "fa", "model-summary", "--model", "M1")
	runOK(t, cfg, "invested", "fa", "model-accounts", "--model", "M1")
	runOK(t, cfg, "265598", "fa", "invested", "--model", "M1")
	runOK(t, cfg, "keepModelOpen", "fa", "preset")

	runOK(t, cfg, "DRY RUN", "fa", "model-save", "--model", "M1", "--desc", "d",
		"--position-targets", `[{"conid":265598,"target":1.0}]`)
	runOK(t, cfg, "DRY RUN", "fa", "invest-divest", "--model", "M1",
		"--account-list", `[{"account":"DU123456","amtToInvest":1000}]`)
	runOK(t, cfg, "DRY RUN", "fa", "submit-transfers", "--fp-order-id", "777")
	runOK(t, cfg, "DRY RUN", "fa", "preset-save", "--keep-model-open")
	if stub.faSaveCalls+stub.faInvestCalls+stub.faSubmitCalls+stub.faPresetCalls != 0 {
		t.Fatalf("FA dry runs mutated: save=%d invest=%d submit=%d preset=%d",
			stub.faSaveCalls, stub.faInvestCalls, stub.faSubmitCalls, stub.faPresetCalls)
	}
	runOK(t, cfg, "saved", "fa", "model-save", "--model", "M1", "--desc", "d",
		"--position-targets", `[{"conid":265598,"target":1.0}]`, "--confirm")
	runOK(t, cfg, "777", "fa", "invest-divest", "--model", "M1",
		"--account-list", `[{"account":"DU123456","amtToInvest":1000}]`, "--confirm")
	runOK(t, cfg, "submitted", "fa", "submit-transfers", "--fp-order-id", "777", "--confirm")
	runOK(t, cfg, "saved", "fa", "preset-save", "--keep-model-open=false", "--confirm")
	if stub.faSaveCalls != 1 || stub.faInvestCalls != 1 || stub.faSubmitCalls != 1 || stub.faPresetCalls != 1 {
		t.Fatalf("FA confirmed calls: save=%d invest=%d submit=%d preset=%d, want 1 each",
			stub.faSaveCalls, stub.faInvestCalls, stub.faSubmitCalls, stub.faPresetCalls)
	}
}

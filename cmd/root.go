// Package cmd defines the ibkr-cli command tree, mirroring the
// sibling CLIs: an auth group for the session lifecycle and one
// command group per API area (accounts, market, orders). Every
// command maps to one Client Portal Web API 1.0 endpoint — see
// README.md for the parity checklist and docs/SPEC.md for the design
// and the order safety rails.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/audit"
	"github.com/rayli09/ibkr-cli/internal/auth"
	"github.com/rayli09/ibkr-cli/internal/config"
	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

// Execute runs the root command and maps typed errors to the stable
// exit codes documented in docs/SPEC.md (0 ok, 2 usage, 3
// auth/session, 4 API error, 5 question-pending).
func Execute() {
	os.Exit(execute())
}

func execute() int {
	root, opts := NewRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if code := apierr.ExitCode(err); code != 1 {
			return code
		}
		if !opts.preRunReached {
			return apierr.ExitUsage // cobra parse/usage failure
		}
		return 1
	}
	return apierr.ExitOK
}

// options carries the persistent flags.
type options struct {
	configPath string
	baseURL    string
	account    string
	jsonOut    bool
	timeout    time.Duration

	preRunReached bool
	envCache      *appEnv
}

// appEnv is the wired-up runtime: config, provider, client.
type appEnv struct {
	cfg      *config.Config
	provider auth.Provider
	client   *cpapi.Client
}

// NewRootCmd builds a fresh command tree (tests build their own).
func NewRootCmd() (*cobra.Command, *options) {
	opts := &options{}
	root := &cobra.Command{
		Use:   "ibkr-cli",
		Short: "Command-line client for the Interactive Brokers Client Portal Web API 1.0",
		Long: `ibkr-cli is a Go command-line client for the Interactive Brokers
Client Portal Web API 1.0 (CPAPI 1.0), modeled on schwab-cli and
etrade-cli. Two authentication modes are supported (see auth init):

  gateway  A local Client Portal Gateway maintained by IBeam holds
           the session; the CLI never sees your IBKR password.
  oauth    The CLI signs requests directly with IBKR's OAuth 1.0a
           variant (live session token via Diffie-Hellman exchange).

Note: IBKR allows ONE brokerage session per username across all
platforms — a live session here and a TWS/website login under the
same username will kick each other.

Order commands are dry-run by default: they run the whatif preview,
print the ticket, and send nothing. Pass --confirm to actually send.
When IBKR answers a placement with a confirmation question, the CLI
prints it verbatim and stops (exit code 5); answer it with
'orders reply <id> --confirm'. Questions are never auto-answered.`,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			opts.preRunReached = true
			return nil
		},
	}
	root.PersistentFlags().StringVar(&opts.configPath, "config", "",
		"path to the config file (default ~/.config/ibkr-cli/config.json; env IBKR_CLI_CONFIG)")
	root.PersistentFlags().StringVar(&opts.baseURL, "base-url", "",
		"override the API base URL (testing; gateway mode default https://localhost:5000/v1/api)")
	root.PersistentFlags().StringVar(&opts.account, "account", "",
		"account id (default: default_account_id from config)")
	root.PersistentFlags().BoolVar(&opts.jsonOut, "json", false,
		"print raw JSON responses instead of tables")
	root.PersistentFlags().DurationVar(&opts.timeout, "timeout", cpapi.DefaultTimeout,
		"HTTP timeout for API calls")

	root.AddCommand(newAuthCmd(opts))
	root.AddCommand(newSetupCmd(opts))
	root.AddCommand(newAccountsCmd(opts))
	root.AddCommand(newPortfolioCmd(opts))
	root.AddCommand(newMarketCmd(opts))
	root.AddCommand(newContractCmd(opts))
	root.AddCommand(newOrdersCmd(opts))
	root.AddCommand(newScannerCmd(opts))
	root.AddCommand(newWatchlistCmd(opts))
	root.AddCommand(newFACmd(opts))
	root.AddCommand(newStreamCmd(opts))
	return root, opts
}

// env lazily builds (and caches) the runtime wiring.
func (o *options) env() (*appEnv, error) {
	if o.envCache != nil {
		return o.envCache, nil
	}
	cfg, err := config.Load(o.configPath)
	if err != nil {
		return nil, err
	}
	provider, err := auth.New(cfg)
	if err != nil {
		return nil, err
	}
	if o.baseURL != "" {
		switch p := provider.(type) {
		case *auth.GatewayProvider:
			p.SetBaseURL(o.baseURL)
		case *auth.OAuthProvider:
			p.SetBaseURL(o.baseURL)
		}
	}
	client := cpapi.New(provider.BaseURL(), provider.Transport())
	client.Timeout = o.timeout
	o.envCache = &appEnv{cfg: cfg, provider: provider, client: client}
	return o.envCache, nil
}

// configOnly loads just the config (commands that must not touch the
// network or a provider, e.g. auth init / auth oauth-status).
func (o *options) configOnly() (*config.Config, error) {
	return config.Load(o.configPath)
}

// resolveAccount picks the account id: --account, else config default.
func (o *options) resolveAccount() (string, error) {
	if o.account != "" {
		return o.account, nil
	}
	cfg, err := o.configOnly()
	if err != nil {
		return "", err
	}
	if cfg.DefaultAccount != "" {
		return cfg.DefaultAccount, nil
	}
	return "", &apierr.UsageError{Msg: "no account specified: pass --account or set default_account_id (auth init)"}
}

// ctx returns a command context.
func ctx() context.Context { return context.Background() }

// readCall runs a read-only client call and prints the raw body
// (the parity-extension commands print JSON; --json is therefore a
// no-op for them, kept for uniformity with the older groups).
func (o *options) readCall(fn func(cl *cpapi.Client) (*cpapi.Response, error)) error {
	env, err := o.env()
	if err != nil {
		return err
	}
	resp, err := fn(env.client)
	if err != nil {
		return err
	}
	printBody(resp.Body)
	return nil
}

// mutateCall is the dry-run rail for non-order mutations, mirroring
// orders: the payload is printed first; without confirm nothing is
// sent. With confirm the session is ensured, the call is sent, and
// the exchange is written to the audit log.
func (o *options) mutateCall(name, account string, confirm bool, payload any, fn func(cl *cpapi.Client) (*cpapi.Response, error)) error {
	env, err := o.env()
	if err != nil {
		return err
	}
	reqJSON, _ := json.Marshal(payload)
	fmt.Printf("%s payload:\n", name)
	printBody(reqJSON)
	if !confirm {
		fmt.Println("DRY RUN — nothing was sent. Re-run with --confirm to send.")
		return nil
	}
	if err := env.provider.EnsureSession(ctx()); err != nil {
		return err
	}
	resp, err := fn(env.client)
	if resp != nil {
		o.auditCall(name, account, reqJSON, resp.Body, "")
	}
	if err != nil {
		return err
	}
	fmt.Println("sent:")
	printBody(resp.Body)
	return nil
}

// printBody renders a raw response body: pretty JSON when it parses.
func printBody(body []byte) {
	if len(body) == 0 {
		return
	}
	var v any
	if err := json.Unmarshal(body, &v); err == nil {
		out, err := json.MarshalIndent(v, "", "  ")
		if err == nil {
			fmt.Println(string(out))
			return
		}
	}
	fmt.Println(string(body))
}

// auditCall records a mutating call in the local audit log
// (SPEC §5.1.5). Failures are reported on stderr but never mask the
// call's own outcome.
func (o *options) auditCall(command, account string, reqBody, respBody []byte, note string) {
	cfg, err := o.configOnly()
	if err != nil {
		return
	}
	if err := audit.Log(config.AuditLogPath(cfg.FilePath()), command, account, reqBody, respBody, note); err != nil {
		fmt.Fprintf(os.Stderr, "warning: audit log write failed: %v\n", err)
	}
}

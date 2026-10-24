package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/auth"
	"github.com/rayli09/ibkr-cli/internal/config"
	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

func newAuthCmd(opts *options) *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Set up credentials and check session status",
	}
	authCmd.AddCommand(newAuthInitCmd(opts))
	authCmd.AddCommand(newAuthStatusCmd(opts))
	authCmd.AddCommand(newAuthHealthCmd(opts))
	authCmd.AddCommand(newAuthTickleCmd(opts))
	authCmd.AddCommand(newAuthLogoutCmd(opts))
	authCmd.AddCommand(newAuthReauthCmd(opts))
	authCmd.AddCommand(newAuthValidateCmd(opts))
	authCmd.AddCommand(newAuthOAuthStatusCmd(opts))
	return authCmd
}

// --- auth init ----------------------------------------------------

func newAuthInitCmd(opts *options) *cobra.Command {
	var modeFlag, filePath string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Interactively write the config file (0600)",
		Long: `Interactively configure ibkr-cli and write the config file
(mode 0600).

Mode gateway: you need a running Client Portal Gateway — the
maintained setup is IBeam (Docker), which owns login and 2FA; the
CLI never asks for your IBKR password. Default gateway URL:
https://localhost:5000/v1/api.

Mode oauth: collect the credentials from your IBKR OAuth
application (consumer key, access token, access token secret), the
RSA private keys for encryption and signing, and the Diffie-Hellman
prime you generated during onboarding (openssl dhparam). Secret
values are read without echo and are never printed back.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(filePath)
			if err != nil {
				return err
			}
			in := bufio.NewReader(os.Stdin)
			mode := modeFlag
			if mode == "" {
				mode = prompt(in, "Auth mode [gateway/oauth]", string(cfg.AuthMode()))
			}
			switch config.Mode(strings.ToLower(mode)) {
			case config.ModeGateway:
				cfg.Mode = config.ModeGateway
				cfg.GatewayURL = prompt(in, "Gateway base URL", cfg.GatewayBaseURL())
			case config.ModeOAuth:
				cfg.Mode = config.ModeOAuth
				o := &cfg.OAuth
				o.ConsumerKey = prompt(in, "OAuth consumer key", o.ConsumerKey)
				o.AccessToken = promptSecret(in, "OAuth access token")
				o.AccessTokenSecret = promptSecret(in, "OAuth access token secret")
				o.DHPrime = promptSecret(in, "Diffie-Hellman prime (hex)")
				o.EncryptionKeyPath = prompt(in, "RSA encryption private key path", o.EncryptionKeyPath)
				o.SignatureKeyPath = prompt(in, "RSA signature private key path", o.SignatureKeyPath)
				if o.Realm == "" {
					o.Realm = "limited_poa"
				}
				o.Realm = prompt(in, "OAuth realm", o.Realm)
				genStr := prompt(in, "DH generator", strconv.Itoa(o.DHGenerator))
				if n, err := strconv.Atoi(genStr); err == nil && n > 0 {
					o.DHGenerator = n
				}
				if o.DHGenerator == 0 {
					o.DHGenerator = 2
				}
			default:
				return &apierr.UsageError{Msg: "mode must be gateway or oauth"}
			}
			cfg.DefaultAccount = prompt(in, "Default account id (blank to skip)", cfg.DefaultAccount)
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("Wrote %s (0600). Mode: %s\n", cfg.FilePath(), cfg.AuthMode())
			if cfg.AuthMode() == config.ModeGateway {
				fmt.Println("Make sure IBeam/the gateway is running, then: ibkr-cli auth status")
			} else {
				fmt.Println("Then: ibkr-cli auth oauth-status, ibkr-cli auth status")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&modeFlag, "mode", "", "auth mode: gateway or oauth (prompted when omitted)")
	cmd.Flags().StringVar(&filePath, "file", "", "config path to write (default: same resolution as --config)")
	return cmd
}

func prompt(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// promptSecret reads a value without terminal echo when stdin is a
// terminal; piped input falls back to a plain line read (tests).
func promptSecret(in *bufio.Reader, label string) string {
	fmt.Printf("%s: ", label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

// --- auth status --------------------------------------------------

func newAuthStatusCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication and brokerage-session state",
		Long: `Show the two-tier session state (docs/SPEC.md §3).

CPAPI separates an outer authenticated session (read-only data:
portfolio, market data, contract search) from the brokerage session
required for orders. Note the gotcha: IBKR allows one brokerage
session per username across ALL platforms — logging in on the
website or TWS under the same username competes with (kicks) this
session, and vice versa.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			st, err := env.provider.Status(ctx())
			if err != nil {
				return err
			}
			if opts.jsonOut {
				fmt.Printf("{\"mode\":%q,\"base_url\":%q,\"authenticated\":%v,\"brokerage_live\":%v,\"competing\":%v,\"server\":%q,\"detail\":%q}\n",
					env.cfg.AuthMode(), env.provider.BaseURL(), st.Authenticated, st.BrokerageLive, st.Competing, st.ServerName, st.Detail)
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "mode:\t%s\n", env.cfg.AuthMode())
			fmt.Fprintf(w, "base URL:\t%s\n", env.provider.BaseURL())
			fmt.Fprintf(w, "authenticated:\t%v\n", st.Authenticated)
			fmt.Fprintf(w, "brokerage session:\t%v\n", st.BrokerageLive)
			fmt.Fprintf(w, "competing session:\t%v\n", st.Competing)
			if st.ServerName != "" {
				fmt.Fprintf(w, "server:\t%s\n", st.ServerName)
			}
			if st.Detail != "" {
				fmt.Fprintf(w, "detail:\t%s\n", st.Detail)
			}
			w.Flush()
			if st.Competing {
				fmt.Println("note: only ONE brokerage session per username is allowed across platforms;")
				fmt.Println("      a website/TWS login under the same username will kick this session.")
			}
			return nil
		},
	}
}

// --- auth tickle --------------------------------------------------

func newAuthTickleCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "tickle",
		Short: "Keep the session alive (POST /tickle)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			resp, err := env.client.Tickle(ctx())
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			fmt.Println("tickle ok — session contacted; idle timeout reset.")
			if len(resp.Body) > 0 {
				fmt.Println(string(resp.Body))
			}
			return nil
		},
	}
}

// --- auth reauth --------------------------------------------------

func newAuthReauthCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "reauth",
		Short: "Re-establish the brokerage session",
		Long: `Gateway mode: POST /iserver/reauthenticate (works while the
gateway's SSO session lives; if that is gone, IBeam owns the fresh
login and this command fails loudly instead of pretending).

OAuth mode: acquire a fresh live session token if needed and open
the brokerage session via /iserver/auth/ssodh/init.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			if _, isOAuth := env.provider.(*auth.OAuthProvider); isOAuth {
				if err := env.provider.EnsureSession(ctx()); err != nil {
					return err
				}
				fmt.Println("brokerage session established (live session token + ssodh/init).")
				return nil
			}
			resp, err := env.client.Reauthenticate(ctx())
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			fmt.Println("reauthenticate requested; verify with: ibkr-cli auth status")
			return nil
		},
	}
}

// --- auth validate ------------------------------------------------

func newAuthValidateCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the SSO session (GET /sso/validate)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			resp, err := env.client.Validate(ctx())
			if err != nil {
				return err
			}
			printBody(resp.Body)
			return nil
		},
	}
}

// --- auth oauth-status ---------------------------------------------

func newAuthOAuthStatusCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "oauth-status",
		Short: "Show OAuth credential presence and live-session-token state (values never printed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := opts.configOnly()
			if err != nil {
				return err
			}
			if opts.jsonOut {
				o := cfg.OAuth
				fmt.Printf("{\"mode\":%q,\"consumer_key\":%v,\"access_token\":%v,\"access_token_secret\":%v,\"dh_prime\":%v,\"encryption_key_path\":%q,\"signature_key_path\":%q,\"live_session_token_cached\":%v,\"live_session_token_expires_ms\":%d}\n",
					cfg.AuthMode(), o.ConsumerKey != "", o.AccessToken != "", o.AccessTokenSecret != "", o.DHPrime != "",
					o.EncryptionKeyPath, o.SignatureKeyPath, o.LiveSessionToken != "", o.LiveSessionTokenExpires)
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			o := cfg.OAuth
			yn := func(b bool) string {
				if b {
					return "set"
				}
				return "MISSING"
			}
			fmt.Fprintf(w, "mode:\t%s\n", cfg.AuthMode())
			fmt.Fprintf(w, "consumer key:\t%s\n", yn(o.ConsumerKey != ""))
			fmt.Fprintf(w, "access token:\t%s\n", yn(o.AccessToken != ""))
			fmt.Fprintf(w, "access token secret:\t%s\n", yn(o.AccessTokenSecret != ""))
			fmt.Fprintf(w, "DH prime:\t%s\n", yn(o.DHPrime != ""))
			fmt.Fprintf(w, "encryption key:\t%s\n", o.EncryptionKeyPath)
			fmt.Fprintf(w, "signature key:\t%s\n", o.SignatureKeyPath)
			fmt.Fprintf(w, "realm:\t%s\n", o.Realm)
			fmt.Fprintf(w, "live session token:\t%s\n", yn(o.LiveSessionToken != ""))
			if o.LiveSessionTokenExpires > 0 {
				fmt.Fprintf(w, "LST expires (unix ms):\t%d\n", o.LiveSessionTokenExpires)
			}
			w.Flush()
			if cfg.AuthMode() != config.ModeOAuth {
				fmt.Println("note: current mode is gateway; OAuth fields are only used in oauth mode.")
			}
			return nil
		},
	}
}

// --- auth health / logout (IBind parity) --------------------------

func newAuthHealthCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Gateway health check: tickle + auth status verdict (IBind check_health)",
		Long: `Mirrors IBind's check_health: performs a tickle and reports
healthy only when the session is authenticated, connected, and not
competing. Prints the tickle payload; exits 3 when unhealthy.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			healthy, resp, err := env.client.Health(ctx())
			if err != nil {
				return err
			}
			printBody(resp.Body)
			if !healthy {
				return &apierr.SessionError{Msg: "session not healthy (not authenticated, not connected, or competing)"}
			}
			fmt.Println("healthy: authenticated, connected, not competing.")
			return nil
		},
	}
}

func newAuthLogoutCmd(opts *options) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "End the current session (POST /logout); requires --confirm",
		Long: `Logs out the current session (IBind: logout). This ends the
brokerage session — in gateway mode IBeam will log back in on its
own schedule. Dry-run by default like every mutation in this CLI.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.mutateCall("auth logout", "", confirm,
				map[string]any{"endpoint": "POST /logout"},
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.Logout(ctx())
				})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually log out (without it: dry run)")
	return cmd
}

var _ io.Reader // io retained for prompt helpers shared across files

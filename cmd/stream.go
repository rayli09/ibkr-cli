package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/config"
	"github.com/rayli09/ibkr-cli/internal/ws"
)

// The stream group is the WebSocket half of IBind parity: bounded
// equivalents of IbkrWsClient's subscription classes. Each command
// connects, subscribes, prints messages as NDJSON (one JSON object
// per line), and shuts down cleanly at --duration or --max-events,
// unsubscribing first. The REST marketdata_unsubscribe(_all) methods
// in IBind are this same lifecycle: subscriptions live and die with
// the WS session, so there is no separate REST command for them.

func newStreamCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "stream",
		Short: "Bounded WebSocket streams (IBind IbkrWsClient parity)",
	}
	c.AddCommand(newStreamMarketCmd(opts))
	c.AddCommand(newStreamHistoryCmd(opts))
	c.AddCommand(newStreamLadderCmd(opts))
	c.AddCommand(newStreamOrdersCmd(opts))
	c.AddCommand(newStreamTradesCmd(opts))
	c.AddCommand(newStreamAccountCmd(opts))
	c.AddCommand(newStreamNotificationsCmd(opts))
	return c
}

type streamFlags struct {
	duration  time.Duration
	maxEvents int
}

func addStreamFlags(cmd *cobra.Command, f *streamFlags) {
	cmd.Flags().DurationVar(&f.duration, "duration", 30*time.Second, "how long to stream before shutting down")
	cmd.Flags().IntVar(&f.maxEvents, "max-events", 0, "stop after this many events (0 = no cap; heartbeats do not count)")
}

// dialStream performs the CPAPI WS handshake for the active auth
// mode: a REST tickle yields the session id used as the cookie.
// OAuth mode additionally passes the access token as ?oauth_token=
// (IBind's IBIND_OAUTH1A_WS_URL) — see docs/SPEC.md for the
// verification status of the OAuth WS handshake.
func dialStream(opts *options) (*ws.Conn, error) {
	env, err := opts.env()
	if err != nil {
		return nil, err
	}
	if err := env.provider.EnsureSession(ctx()); err != nil {
		return nil, err
	}
	resp, err := env.client.Tickle(ctx())
	if err != nil {
		return nil, err
	}
	var tickle struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(resp.Body, &tickle); err != nil || tickle.Session == "" {
		return nil, fmt.Errorf("tickle returned no session id; cannot open the websocket (body: %s)", string(resp.Body))
	}
	dopts := ws.DialOpts{BaseURL: env.provider.BaseURL()}
	if tr, ok := env.provider.Transport().(*http.Transport); ok && tr.TLSClientConfig != nil {
		dopts.TLSConfig = tr.TLSClientConfig
	} else if env.cfg.AuthMode() == config.ModeGateway {
		// Gateway loopback uses a self-signed certificate (the REST
		// provider transport already accepts it for loopback).
		dopts.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // loopback gateway only, mirrors REST transport
	}
	if env.cfg.AuthMode() == config.ModeOAuth {
		dopts.Cookie = ws.OAuthCookie(tickle.Session)
		dopts.OAuthToken = env.cfg.OAuth.AccessToken
	} else {
		dopts.Cookie = ws.GatewayCookie(tickle.Session)
	}
	return ws.Dial(ctx(), dopts)
}

func runStream(opts *options, f *streamFlags, subscribe []string, unsubscribe func() []string, capture func(msg []byte)) error {
	conn, err := dialStream(opts)
	if err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(ctx(), f.duration)
	defer cancel()
	emit := func(msg []byte) {
		fmt.Println(string(msg))
		if capture != nil {
			capture(msg)
		}
	}
	_, err = ws.Run(sctx, conn, subscribe, unsubscribe, f.maxEvents, emit)
	return err
}

func staticUnsub(payloads ...string) func() []string {
	return func() []string { return payloads }
}

func newStreamMarketCmd(opts *options) *cobra.Command {
	f := &streamFlags{}
	var fieldsFlag string
	cmd := &cobra.Command{
		Use:   "market <conid>...",
		Short: "Stream market data (smd; topic md)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			fields := cpapiFields(fieldsFlag)
			var subs, unsubs []string
			for _, id := range ids {
				subs = append(subs, ws.SMD(id, fields))
				unsubs = append(unsubs, ws.UMD(id))
			}
			return runStream(opts, f, subs, staticUnsub(unsubs...), nil)
		},
	}
	addStreamFlags(cmd, f)
	cmd.Flags().StringVar(&fieldsFlag, "fields", "31,84,86", "comma-separated snapshot field codes (31 last, 84 bid, 86 ask)")
	return cmd
}

func newStreamHistoryCmd(opts *options) *cobra.Command {
	f := &streamFlags{}
	var exchange, period, bar, source, format string
	var outsideRTH bool
	cmd := &cobra.Command{
		Use:   "history <conid>",
		Short: "Stream history bars (smh; topic mh)",
		Long: `Streams bars for one conid. The unsubscribe uses the serverId
from the first mh message (IBind requires it); if no bar arrives
before shutdown, the session close ends the subscription.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			var ort *bool
			if cmd.Flags().Changed("outside-rth") {
				ort = &outsideRTH
			}
			serverID := ""
			capture := func(msg []byte) {
				if ws.TopicOf(msg) != "mh" || serverID != "" {
					return
				}
				var head struct {
					ServerID string `json:"serverId"`
				}
				if err := json.Unmarshal(msg, &head); err == nil {
					serverID = head.ServerID
				}
			}
			unsub := func() []string {
				if serverID == "" {
					return nil
				}
				return []string{ws.UMH(serverID)}
			}
			sub := ws.SMH(ids[0], exchange, period, bar, ort, source, format)
			return runStream(opts, f, []string{sub}, unsub, capture)
		},
	}
	addStreamFlags(cmd, f)
	cmd.Flags().StringVar(&exchange, "exchange", "", "exchange")
	cmd.Flags().StringVar(&period, "period", "", "period (e.g. 1d)")
	cmd.Flags().StringVar(&bar, "bar", "", "bar size (e.g. 1min)")
	cmd.Flags().BoolVar(&outsideRTH, "outside-rth", false, "include outside-RTH bars")
	cmd.Flags().StringVar(&source, "source", "", "data source")
	cmd.Flags().StringVar(&format, "format", "", "format")
	return cmd
}

func newStreamLadderCmd(opts *options) *cobra.Command {
	f := &streamFlags{}
	var exchange string
	cmd := &cobra.Command{
		Use:   "ladder <conid>",
		Short: "Stream the BookTrader price ladder (sbd; topic bd) — needs an L2 subscription",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			return runStream(opts, f,
				[]string{ws.SBD(acct, ids[0], exchange)},
				staticUnsub(ws.UBD(acct)), nil)
		},
	}
	addStreamFlags(cmd, f)
	cmd.Flags().StringVar(&exchange, "exchange", "", "routing exchange (default: all deep exchanges)")
	return cmd
}

func newStreamOrdersCmd(opts *options) *cobra.Command {
	f := &streamFlags{}
	var filter string
	cmd := &cobra.Command{
		Use:   "orders",
		Short: "Stream live-order updates (sor; topic or)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStream(opts, f,
				[]string{ws.SOR(filter)}, staticUnsub(ws.UOR()), nil)
		},
	}
	addStreamFlags(cmd, f)
	cmd.Flags().StringVar(&filter, "filter", "", "exclusive order-status filter (e.g. filled)")
	return cmd
}

func newStreamTradesCmd(opts *options) *cobra.Command {
	f := &streamFlags{}
	var days int
	var realtimeOnly bool
	cmd := &cobra.Command{
		Use:   "trades",
		Short: "Stream execution updates (str; topic tr)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var ro *bool
			if cmd.Flags().Changed("realtime-only") {
				ro = &realtimeOnly
			}
			var d *int
			if cmd.Flags().Changed("days") {
				d = &days
			}
			return runStream(opts, f,
				[]string{ws.STR(ro, d)}, staticUnsub(ws.UTR()), nil)
		},
	}
	addStreamFlags(cmd, f)
	cmd.Flags().IntVar(&days, "days", 0, "days of executions to include")
	cmd.Flags().BoolVar(&realtimeOnly, "realtime-only", false, "only real-time executions")
	return cmd
}

func newStreamAccountCmd(opts *options) *cobra.Command {
	f := &streamFlags{}
	var keysFlag, fieldsFlag string
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Stream account summary (ssd), ledger (sld) and PnL (spl) updates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			keys := splitCSV(keysFlag)
			fields := splitCSV(fieldsFlag)
			subs := []string{ws.SSD(acct, keys, fields), ws.SLD(acct, keys, fields), ws.SPL()}
			unsubs := []string{ws.USD(acct), ws.ULD(acct), ws.UPL()}
			return runStream(opts, f, subs, staticUnsub(unsubs...), nil)
		},
	}
	addStreamFlags(cmd, f)
	cmd.Flags().StringVar(&keysFlag, "keys", "", "comma-separated summary/ledger keys to filter to")
	cmd.Flags().StringVar(&fieldsFlag, "fields", "", "comma-separated fields to filter to")
	return cmd
}

func newStreamNotificationsCmd(opts *options) *cobra.Command {
	f := &streamFlags{}
	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "Stream unsolicited notifications and bulletins (topics ntf, blt)",
		Long: `Notifications (ntf), bulletins (blt), account updates (act)
and auth-status messages (sts) are unsolicited in CPAPI — there is
no subscribe payload; the command connects and relays whatever the
session receives until --duration/--max-events.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStream(opts, f, nil, staticUnsub(), nil)
		},
	}
	addStreamFlags(cmd, f)
	return cmd
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func cpapiFields(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

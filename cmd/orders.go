package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/config"
	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

// Order safety rails (docs/SPEC.md §5.1) — these are the contract of
// this command group:
//   - dry-run by default: place/modify/cancel send NOTHING without
//     --confirm; a whatif preview runs first and the ticket prints
//     the account id with its PAPER/LIVE tag;
//   - a live brokerage session is required for any send;
//   - when IBKR answers with confirmation questions, they are
//     printed verbatim and the command exits 5 — questions are
//     answered one at a time, explicitly, via `orders reply`,
//     never automatically.

func newOrdersCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "orders",
		Short: "List, preview, place, modify, cancel orders; answer order questions",
	}
	c.AddCommand(newOrdersListCmd(opts))
	c.AddCommand(newOrdersStatusCmd(opts))
	c.AddCommand(newOrdersTradesCmd(opts))
	c.AddCommand(newOrdersPlaceCmd(opts))
	c.AddCommand(newOrdersReplyCmd(opts))
	c.AddCommand(newOrdersModifyCmd(opts))
	c.AddCommand(newOrdersCancelCmd(opts))
	c.AddCommand(newOrdersSuppressCmd(opts))
	c.AddCommand(newOrdersSuppressResetCmd(opts))
	return c
}

// orderFlags bundles the flags shared by place/modify.
type orderFlags struct {
	conid     int64
	secType   string
	side      string
	orderType string
	qty       float64
	price     float64
	auxPrice  float64
	tif       string
	outsideRH bool
	confirm   bool
}

func addOrderFlags(cmd *cobra.Command, f *orderFlags, withConid bool) {
	if withConid {
		cmd.Flags().Int64Var(&f.conid, "conid", 0, "contract id (required)")
	}
	cmd.Flags().StringVar(&f.secType, "sectype", "", "security type for secType (e.g. STK:OPT=..., usually derivable)")
	cmd.Flags().StringVar(&f.side, "side", "BUY", "BUY or SELL")
	cmd.Flags().StringVar(&f.orderType, "type", "LMT", "order type: MKT, LMT, STP, STP_LMT, ...")
	cmd.Flags().Float64Var(&f.qty, "qty", 0, "quantity (required)")
	cmd.Flags().Float64Var(&f.price, "price", 0, "limit price")
	cmd.Flags().Float64Var(&f.auxPrice, "aux-price", 0, "stop price")
	cmd.Flags().StringVar(&f.tif, "tif", "DAY", "time in force: DAY, GTC, IOC, ...")
	cmd.Flags().BoolVar(&f.outsideRH, "outside-rth", false, "allow execution outside regular trading hours")
	cmd.Flags().BoolVar(&f.confirm, "confirm", false, "actually send the order (without it: whatif preview only)")
}

func (f *orderFlags) request() (cpapi.OrderRequest, error) {
	if f.qty <= 0 {
		return cpapi.OrderRequest{}, &apierr.UsageError{Msg: "--qty is required and must be positive"}
	}
	return cpapi.OrderRequest{
		Conid:      f.conid,
		SecType:    f.secType,
		OrderType:  f.orderType,
		Side:       f.side,
		Quantity:   f.qty,
		Price:      f.price,
		AuxPrice:   f.auxPrice,
		TIF:        f.tif,
		OutsideRTH: f.outsideRH,
	}, nil
}

// printTicket prints the dry-run/confirm ticket header.
func printTicket(account string, req cpapi.OrderRequest) {
	fmt.Printf("account: %s (%s)\n", account, config.AccountTag(account))
	fmt.Printf("order:   %s %g x conid %d @ %s", req.Side, req.Quantity, req.Conid, req.OrderType)
	if req.Price > 0 {
		fmt.Printf(" limit %g", req.Price)
	}
	if req.AuxPrice > 0 {
		fmt.Printf(" stop %g", req.AuxPrice)
	}
	fmt.Printf(" tif %s\n", req.TIF)
}

// printWhatIf prints the whatif preview body under its own heading.
func printWhatIf(body []byte) {
	fmt.Println("whatif preview (margin/commission impact):")
	printBody(body)
}

// printOutcome renders a terminal order outcome.
func printOutcome(out *cpapi.OrderOutcome) {
	fmt.Printf("order id: %s\nstatus:   %s\n", out.OrderID, out.Status)
}

// handleSendOutcome converts a send result into command output: a
// question becomes a printed question + exit-5 error.
func handleSendOutcome(out *cpapi.OrderOutcome, err error) error {
	if err != nil {
		var qp *apierr.QuestionPending
		if errors.As(err, &qp) {
			fmt.Println("IBKR needs confirmation before this order can proceed:")
			for _, q := range qp.Questions {
				fmt.Printf("  reply id: %s\n", q.ID)
				for _, line := range q.Messages {
					fmt.Printf("    %s\n", line)
				}
			}
			fmt.Println("Answer ONE question with: ibkr-cli orders reply <reply id> --confirm")
			fmt.Println("(or decline it with --decline). Nothing further was sent.")
			return err
		}
		return err
	}
	printOutcome(out)
	return nil
}

func newOrdersListCmd(opts *options) *cobra.Command {
	var filterStr string
	var force bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Working orders for the session (GET /iserver/account/orders)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			acctFilter := opts.account
			var filters []string
			if filterStr != "" {
				filters = strings.Split(filterStr, ",")
			}
			resp, err := env.client.LiveOrders(ctx(), acctFilter, filters, force)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			var body struct {
				Orders []orderRow `json:"orders"`
			}
			if err := json.Unmarshal(resp.Body, &body); err != nil {
				printBody(resp.Body)
				return nil
			}
			printOrdersTable(body.Orders)
			return nil
		},
	}
	cmd.Flags().StringVar(&filterStr, "filters", "", "comma-separated order-status filters (IBind live_orders: inactive, submitted, filled, cancelled, sort_by_time, ...)")
	cmd.Flags().BoolVar(&force, "force", false, "force a fresh server fetch; also clears cached filter state that would suppress the WS sor topic")
	return cmd
}

type orderRow struct {
	OrderID     int64  `json:"orderId"`
	Account     string `json:"account"`
	Ticker      string `json:"ticker"`
	Description string `json:"description1"`
	Status      string `json:"status"`
	Side        string `json:"side"`
	Price       any    `json:"price"`
	TotalSize   any    `json:"totalSize"`
	Remaining   any    `json:"remainingQuantity"`
}

func printOrdersTable(rows []orderRow) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ORDER ID\tACCOUNT\tTICKER\tSIDE\tSIZE\tPRICE\tSTATUS")
	for _, r := range rows {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%v\t%v\t%s\n",
			r.OrderID, r.Account, r.Ticker, r.Side, r.TotalSize, r.Price, r.Status)
	}
	w.Flush()
}

func newOrdersStatusCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status <order-id>",
		Short: "One order's status (GET /iserver/account/order/status/{id})",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			resp, err := env.client.OrderStatus(ctx(), args[0])
			if err != nil {
				return err
			}
			printBody(resp.Body)
			return nil
		},
	}
}

func newOrdersTradesCmd(opts *options) *cobra.Command {
	var days string
	cmd := &cobra.Command{
		Use:   "trades",
		Short: "Executions (GET /iserver/account/trades)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			resp, err := env.client.Trades(ctx(), days, opts.account)
			if err != nil {
				return err
			}
			printBody(resp.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&days, "days", "", "lookback window, e.g. 7 (default: today)")
	return cmd
}

func newOrdersPlaceCmd(opts *options) *cobra.Command {
	f := &orderFlags{}
	cmd := &cobra.Command{
		Use:   "place",
		Short: "Preview (whatif) an order; with --confirm, place it",
		Long: `Without --confirm this is a dry run: it requires a live
brokerage session, runs the whatif preview, prints the ticket, and
sends NOTHING. With --confirm the order is placed; if IBKR answers
with confirmation questions they are printed verbatim and the
command exits 5 — answer each with 'orders reply'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			if f.conid == 0 {
				return &apierr.UsageError{Msg: "--conid is required"}
			}
			req, err := f.request()
			if err != nil {
				return err
			}
			req.AcctID = acct
			if err := env.provider.EnsureSession(ctx()); err != nil {
				return err
			}
			wi, err := env.client.WhatIf(ctx(), acct, req)
			if err != nil {
				return err
			}
			reqJSON, _ := json.Marshal(req)
			opts.auditCall("orders place preview (whatif)", acct, reqJSON, wi.Body, "")
			printTicket(acct, req)
			printWhatIf(wi.Body)
			if !f.confirm {
				fmt.Println("DRY RUN — nothing was sent. Re-run with --confirm to place this order.")
				return nil
			}
			out, resp, err := env.client.PlaceOrder(ctx(), acct, req)
			if resp != nil {
				opts.auditCall("orders place", acct, reqJSON, resp.Body, "")
			}
			fmt.Println("order sent:")
			return handleSendOutcome(out, err)
		},
	}
	addOrderFlags(cmd, f, true)
	return cmd
}

func newOrdersReplyCmd(opts *options) *cobra.Command {
	var confirm, decline bool
	cmd := &cobra.Command{
		Use:   "reply <reply-id>",
		Short: "Answer ONE pending order question from a place/modify",
		Long: `Answer exactly one pending order question (POST
/iserver/reply/{id}). Pass --confirm to accept or --decline to
reject; one of the two is required — the CLI never answers a
question implicitly. A "yes" may raise the next question (another
exit 5) or complete the placement.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if confirm == decline {
				return &apierr.UsageError{Msg: "pass exactly one of --confirm or --decline"}
			}
			env, err := opts.env()
			if err != nil {
				return err
			}
			if err := env.provider.EnsureSession(ctx()); err != nil {
				return err
			}
			acct, _ := opts.resolveAccount()
			out, resp, err := env.client.Reply(ctx(), args[0], confirm)
			if resp != nil {
				note := "declined"
				if confirm {
					note = "confirmed"
				}
				body, _ := json.Marshal(map[string]any{"reply_id": args[0], "confirmed": confirm})
				opts.auditCall("orders reply", acct, body, resp.Body, note)
			}
			if err != nil {
				return handleSendOutcome(nil, err)
			}
			fmt.Println("reply accepted; order outcome:")
			printOutcome(out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "answer the question affirmatively")
	cmd.Flags().BoolVar(&decline, "decline", false, "answer the question negatively")
	return cmd
}

func newOrdersModifyCmd(opts *options) *cobra.Command {
	f := &orderFlags{}
	cmd := &cobra.Command{
		Use:   "modify <order-id>",
		Short: "Preview (whatif) a modification; with --confirm, send it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			// Current order, for the ticket and the replacement body.
			statusResp, err := env.client.OrderStatus(ctx(), args[0])
			if err != nil {
				return err
			}
			var cur struct {
				Conid int64 `json:"conid"`
			}
			_ = json.Unmarshal(statusResp.Body, &cur)
			f.conid = cur.Conid
			req, err := f.request()
			if err != nil {
				return err
			}
			req.AcctID = acct
			if err := env.provider.EnsureSession(ctx()); err != nil {
				return err
			}
			wi, err := env.client.WhatIf(ctx(), acct, req)
			if err != nil {
				return err
			}
			reqJSON, _ := json.Marshal(req)
			opts.auditCall("orders modify preview (whatif)", acct, reqJSON, wi.Body, "order "+args[0])
			fmt.Printf("modifying order %s\n", args[0])
			printTicket(acct, req)
			printWhatIf(wi.Body)
			if !f.confirm {
				fmt.Println("DRY RUN — nothing was sent. Re-run with --confirm to modify this order.")
				return nil
			}
			out, resp, err := env.client.ModifyOrder(ctx(), acct, args[0], req)
			if resp != nil {
				opts.auditCall("orders modify", acct, reqJSON, resp.Body, "order "+args[0])
			}
			fmt.Println("modification sent:")
			return handleSendOutcome(out, err)
		},
	}
	addOrderFlags(cmd, f, false)
	return cmd
}

func newOrdersCancelCmd(opts *options) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "cancel <order-id>",
		Short: "Show an order, then cancel it only with --confirm",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			statusResp, err := env.client.OrderStatus(ctx(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("order %s on account %s (%s):\n", args[0], acct, config.AccountTag(acct))
			printBody(statusResp.Body)
			if !confirm {
				fmt.Println("DRY RUN — nothing was sent. Re-run with --confirm to cancel this order.")
				return nil
			}
			if err := env.provider.EnsureSession(ctx()); err != nil {
				return err
			}
			resp, err := env.client.CancelOrder(ctx(), acct, args[0])
			if resp != nil {
				opts.auditCall("orders cancel", acct, nil, resp.Body, "order "+args[0])
			}
			if err != nil {
				return err
			}
			fmt.Println("cancel requested:")
			printBody(resp.Body)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually cancel (without it: show the order only)")
	return cmd
}

// --- orders parity additions (IBind order_mixin) --------------------

func newOrdersSuppressCmd(opts *options) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "suppress <message-id>...",
		Short: "Suppress order-confirmation messages by id (POST /iserver/questions/suppress); requires --confirm",
		Long: `Suppresses the named confirmation-message ids for the session
(IBind: suppress_messages). Note the safety posture of this CLI:
suppression silences future questions of those types; questions
that do arrive are still never auto-answered.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.mutateCall("orders suppress", opts.account, confirm,
				map[string]any{"messageIds": args},
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.SuppressMessages(ctx(), args)
				})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually suppress (without it: dry run)")
	return cmd
}

func newOrdersSuppressResetCmd(opts *options) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "suppress-reset",
		Short: "Clear all suppressed message ids (POST /iserver/questions/suppress/reset); requires --confirm",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.mutateCall("orders suppress-reset", opts.account, confirm,
				map[string]any{"endpoint": "POST /iserver/questions/suppress/reset"},
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.ResetSuppressedMessages(ctx())
				})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually reset (without it: dry run)")
	return cmd
}

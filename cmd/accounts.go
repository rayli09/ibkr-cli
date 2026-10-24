package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/config"
)

func newAccountsCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "accounts",
		Short: "List accounts and read balances, positions, ledger, P&L",
	}
	c.AddCommand(newAccountsListCmd(opts))
	c.AddCommand(newAccountsSummaryCmd(opts))
	c.AddCommand(newAccountsPositionsCmd(opts))
	c.AddCommand(newAccountsLedgerCmd(opts))
	c.AddCommand(newAccountsPnLCmd(opts))
	return c
}

func newAccountsListCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List session accounts (iserver + portfolio views)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			iserver, iresp, err := env.client.IServerAccounts(ctx())
			if err != nil {
				return err
			}
			portfolio, presp, err := env.client.PortfolioAccounts(ctx())
			if err != nil {
				return err
			}
			if opts.jsonOut {
				fmt.Printf("{\"iserver\":%s,\"portfolio\":%s}\n", iresp.Body, presp.Body)
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ACCOUNT\tTITLE\tTYPE\tCURRENCY\tENV")
			seen := map[string]bool{}
			for _, a := range portfolio {
				seen[a.ID] = true
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", a.ID, a.AccountTitle, a.Type, a.Currency, config.AccountTag(a.ID))
			}
			for _, id := range iserver.Accounts {
				if !seen[id] {
					fmt.Fprintf(w, "%s\t\t\t\t%s\n", id, config.AccountTag(id))
				}
			}
			return w.Flush()
		},
	}
}

func newAccountsSummaryCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Account summary: balances, buying power, margin (GET /portfolio/{id}/summary)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			resp, err := env.client.AccountSummary(ctx(), acct)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			fmt.Printf("account %s (%s)\n", acct, config.AccountTag(acct))
			printSummaryTable(resp.Body)
			return nil
		},
	}
}

// printSummaryTable renders the summary object as metric rows. The
// endpoint returns {metric: {amount, currency, isNull}}.
func printSummaryTable(body []byte) {
	var m map[string]struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		printBody(body)
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, k := range keys {
		fmt.Fprintf(w, "%s\t%.2f\t%s\n", k, m[k].Amount, m[k].Currency)
	}
	w.Flush()
}

func newAccountsPositionsCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "positions",
		Short: "All positions (pages aggregated)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			items, err := env.client.AllPositions(ctx(), acct)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				fmt.Printf("[%s]\n", joinRaw(items))
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TICKER\tCONID\tQTY\tMKT PRICE\tMKT VALUE\tUNREAL P&L")
			for _, raw := range items {
				var p struct {
					Conid         int64   `json:"conid"`
					Ticker        string  `json:"ticker"`
					Position      float64 `json:"position"`
					MktPrice      float64 `json:"mktPrice"`
					MktValue      float64 `json:"mktValue"`
					UnrealizedPnl float64 `json:"unrealizedPnl"`
				}
				if err := json.Unmarshal(raw, &p); err != nil {
					continue
				}
				fmt.Fprintf(w, "%s\t%d\t%g\t%.2f\t%.2f\t%.2f\n",
					p.Ticker, p.Conid, p.Position, p.MktPrice, p.MktValue, p.UnrealizedPnl)
			}
			return w.Flush()
		},
	}
}

func joinRaw(items []json.RawMessage) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += ","
		}
		out += string(it)
	}
	return out
}

func newAccountsLedgerCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "ledger",
		Short: "Cash ledger by currency (GET /portfolio/{id}/ledger)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			resp, err := env.client.AccountLedger(ctx(), acct)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			var m map[string]map[string]any
			if err := json.Unmarshal(resp.Body, &m); err != nil {
				printBody(resp.Body)
				return nil
			}
			curs := make([]string, 0, len(m))
			for c := range m {
				if c != "ledger" {
					curs = append(curs, c)
				}
			}
			sort.Strings(curs)
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			for _, cur := range curs {
				keys := make([]string, 0, len(m[cur]))
				for k := range m[cur] {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(w, "%s\t%s\t%v\n", cur, k, m[cur][k])
				}
			}
			return w.Flush()
		},
	}
}

func newAccountsPnLCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "pnl",
		Short: "Partitioned P&L (GET /iserver/account/pnl/partitioned)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			resp, err := env.client.AccountPnL(ctx())
			if err != nil {
				return err
			}
			// The partitioned P&L shape is per-account/per-model and
			// varies with holdings; the faithful rendering is the
			// JSON itself.
			printBody(resp.Body)
			return nil
		},
	}
}

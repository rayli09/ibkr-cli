package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

// The portfolio group mirrors IBind's portfolio_mixin one-to-one.
// The accounts group keeps its original five views (list, summary,
// positions, ledger, pnl); several portfolio leaves expose the same
// endpoints with the mixin's full parameter surface.

func newPortfolioCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "portfolio",
		Short: "Portfolio endpoints (IBind portfolio_mixin parity)",
	}
	c.AddCommand(newPortfolioAccountsCmd(opts))
	c.AddCommand(newPortfolioSubaccountsCmd(opts))
	c.AddCommand(newPortfolioSubaccountsLargeCmd(opts))
	c.AddCommand(newPortfolioInfoCmd(opts))
	c.AddCommand(newPortfolioSummaryCmd(opts))
	c.AddCommand(newPortfolioAllocationCmd(opts))
	c.AddCommand(newPortfolioAllocationsCmd(opts))
	c.AddCommand(newPortfolioComboCmd(opts))
	c.AddCommand(newPortfolioPositionsCmd(opts))
	c.AddCommand(newPortfolioPositions2Cmd(opts))
	c.AddCommand(newPortfolioPositionCmd(opts))
	c.AddCommand(newPortfolioPositionInfoCmd(opts))
	c.AddCommand(newPortfolioLedgerCmd(opts))
	c.AddCommand(newPortfolioInvalidateCmd(opts))
	c.AddCommand(newPortfolioPerformanceCmd(opts))
	c.AddCommand(newPortfolioAllPeriodsCmd(opts))
	c.AddCommand(newPortfolioTransactionsCmd(opts))
	return c
}

func acctRead(opts *options, fn func(cl *cpapi.Client, acct string) (*cpapi.Response, error)) error {
	acct, err := opts.resolveAccount()
	if err != nil {
		return err
	}
	return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
		return fn(cl, acct)
	})
}

// accountList resolves the --accounts flag (comma-separated) or
// falls back to the single resolved account.
func accountList(opts *options, flagVal string) ([]string, error) {
	if flagVal != "" {
		parts := strings.Split(flagVal, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts, nil
	}
	acct, err := opts.resolveAccount()
	if err != nil {
		return nil, err
	}
	return []string{acct}, nil
}

func newPortfolioAccountsCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "accounts",
		Short: "Portfolio account list (GET /portfolio/accounts)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				_, resp, err := cl.PortfolioAccounts(ctx())
				return resp, err
			})
		},
	}
}

func newPortfolioSubaccountsCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "subaccounts",
		Short: "Sub-accounts (GET /portfolio/subaccounts)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.PortfolioSubaccounts(ctx())
			})
		},
	}
}

func newPortfolioSubaccountsLargeCmd(opts *options) *cobra.Command {
	var page int
	cmd := &cobra.Command{
		Use:   "subaccounts-large",
		Short: "Sub-accounts for large families, paged (GET /portfolio/subaccounts2)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.PortfolioSubaccountsLarge(ctx(), page)
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", 0, "page number (100 accounts per page)")
	return cmd
}

func newPortfolioInfoCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Account metadata (GET /portfolio/{id}/meta)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return acctRead(opts, func(cl *cpapi.Client, acct string) (*cpapi.Response, error) {
				return cl.PortfolioAccountInfo(ctx(), acct)
			})
		},
	}
}

func newPortfolioSummaryCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Account summary (GET /portfolio/{id}/summary; same endpoint as accounts summary)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return acctRead(opts, func(cl *cpapi.Client, acct string) (*cpapi.Response, error) {
				return cl.AccountSummary(ctx(), acct)
			})
		},
	}
}

func newPortfolioAllocationCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "allocation",
		Short: "One account's allocation (GET /portfolio/{id}/allocation)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return acctRead(opts, func(cl *cpapi.Client, acct string) (*cpapi.Response, error) {
				return cl.PortfolioAccountAllocation(ctx(), acct)
			})
		},
	}
}

func newPortfolioAllocationsCmd(opts *options) *cobra.Command {
	var accountsFlag string
	cmd := &cobra.Command{
		Use:   "allocations",
		Short: "Allocations for several accounts (GET /portfolio/allocation)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			accts, err := accountList(opts, accountsFlag)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.PortfolioAllocations(ctx(), accts)
			})
		},
	}
	cmd.Flags().StringVar(&accountsFlag, "accounts", "", "comma-separated account ids (default: the resolved account)")
	return cmd
}

func newPortfolioComboCmd(opts *options) *cobra.Command {
	var nocache bool
	cmd := &cobra.Command{
		Use:   "combo-positions",
		Short: "Combination (spread) positions (GET /portfolio/{id}/combo/positions)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return acctRead(opts, func(cl *cpapi.Client, acct string) (*cpapi.Response, error) {
				return cl.CombinationPositions(ctx(), acct, nocache)
			})
		},
	}
	cmd.Flags().BoolVar(&nocache, "nocache", false, "bypass the backend cache")
	return cmd
}

func newPortfolioPositionsCmd(opts *options) *cobra.Command {
	var page int
	var model, sort, direction, period string
	cmd := &cobra.Command{
		Use:   "positions",
		Short: "One positions page with IBind's sort params (GET /portfolio/{id}/positions/{page})",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return acctRead(opts, func(cl *cpapi.Client, acct string) (*cpapi.Response, error) {
				return cl.AccountPositionsSorted(ctx(), acct, page, model, sort, direction, period)
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", 0, "page number")
	cmd.Flags().StringVar(&model, "model", "", "model filter")
	cmd.Flags().StringVar(&sort, "sort", "", "sort field (e.g. ticker, position)")
	cmd.Flags().StringVar(&direction, "direction", "", "sort direction: a or d")
	cmd.Flags().StringVar(&period, "period", "", "period (e.g. 7d)")
	return cmd
}

func newPortfolioPositions2Cmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "positions2",
		Short: "Lightweight positions (GET /portfolio2/{id}/positions)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return acctRead(opts, func(cl *cpapi.Client, acct string) (*cpapi.Response, error) {
				return cl.Positions2(ctx(), acct)
			})
		},
	}
}

func newPortfolioPositionCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "position <conid>",
		Short: "Position in one contract (GET /portfolio/{id}/position/{conid})",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			return acctRead(opts, func(cl *cpapi.Client, acct string) (*cpapi.Response, error) {
				return cl.PositionByConid(ctx(), acct, ids[0])
			})
		},
	}
}

func newPortfolioPositionInfoCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "position-info <conid>",
		Short: "Position + contract info across accounts (GET /portfolio/positions/{conid})",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.PositionAndContractInfo(ctx(), ids[0])
			})
		},
	}
}

func newPortfolioLedgerCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "ledger",
		Short: "Per-currency ledger (GET /portfolio/{id}/ledger; same endpoint as accounts ledger)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return acctRead(opts, func(cl *cpapi.Client, acct string) (*cpapi.Response, error) {
				return cl.AccountLedger(ctx(), acct)
			})
		},
	}
}

func newPortfolioInvalidateCmd(opts *options) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "invalidate-cache",
		Short: "Invalidate the backend positions cache (POST /portfolio/{id}/positions/invalidate); requires --confirm",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			acct, err := opts.resolveAccount()
			if err != nil {
				return err
			}
			return opts.mutateCall("portfolio invalidate-cache", acct, confirm,
				map[string]any{"accountId": acct},
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.InvalidatePortfolioCache(ctx(), acct)
				})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually invalidate (without it: dry run)")
	return cmd
}

func newPortfolioPerformanceCmd(opts *options) *cobra.Command {
	var accountsFlag, period string
	cmd := &cobra.Command{
		Use:   "performance",
		Short: "Performance series (POST /pa/performance; rate-limited to 1 req/15 min upstream)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			accts, err := accountList(opts, accountsFlag)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.AccountPerformance(ctx(), accts, period)
			})
		},
	}
	cmd.Flags().StringVar(&accountsFlag, "accounts", "", "comma-separated account ids (default: the resolved account)")
	cmd.Flags().StringVar(&period, "period", "1M", "period: 1D, 7D, MTD, 1M, YTD, 1Y")
	return cmd
}

func newPortfolioAllPeriodsCmd(opts *options) *cobra.Command {
	var accountsFlag string
	cmd := &cobra.Command{
		Use:   "all-periods",
		Short: "Performance across all periods (POST /pa/allperiods)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			accts, err := accountList(opts, accountsFlag)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.AllPeriods(ctx(), accts)
			})
		},
	}
	cmd.Flags().StringVar(&accountsFlag, "accounts", "", "comma-separated account ids (default: the resolved account)")
	return cmd
}

func newPortfolioTransactionsCmd(opts *options) *cobra.Command {
	var accountsFlag, conidsFlag, currency string
	var days int
	cmd := &cobra.Command{
		Use:   "transactions",
		Short: "Transaction history (POST /pa/transactions)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			accts, err := accountList(opts, accountsFlag)
			if err != nil {
				return err
			}
			var conids []int64
			if conidsFlag != "" {
				conids, err = parseConids(strings.Split(conidsFlag, ","))
				if err != nil {
					return err
				}
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.TransactionHistory(ctx(), accts, conids, currency, days)
			})
		},
	}
	cmd.Flags().StringVar(&accountsFlag, "accounts", "", "comma-separated account ids (default: the resolved account)")
	cmd.Flags().StringVar(&conidsFlag, "conids", "", "comma-separated conids (upstream supports one at a time)")
	cmd.Flags().StringVar(&currency, "currency", "USD", "currency for amounts")
	cmd.Flags().IntVar(&days, "days", 0, "lookback days (default: 90 upstream)")
	return cmd
}

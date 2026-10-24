package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

// The contract group mirrors IBind's contract_mixin. Four mixin
// methods live in the market group (search → search_contract_by_symbol,
// info → search_secdef_info_by_conid, strikes →
// search_strikes_by_conid, stocks → security_stocks_by_symbol).

func newContractCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "contract",
		Short: "Contract definitions, rules and reference data (IBind contract_mixin parity)",
	}
	c.AddCommand(newContractSecdefCmd(opts))
	c.AddCommand(newContractAllConidsCmd(opts))
	c.AddCommand(newContractInfoCmd(opts))
	c.AddCommand(newContractInfoRulesCmd(opts))
	c.AddCommand(newContractRulesCmd(opts))
	c.AddCommand(newContractAlgosCmd(opts))
	c.AddCommand(newContractBondFiltersCmd(opts))
	c.AddCommand(newContractPairsCmd(opts))
	c.AddCommand(newContractExchangeRateCmd(opts))
	c.AddCommand(newContractFuturesCmd(opts))
	c.AddCommand(newContractScheduleCmd(opts))
	c.AddCommand(newContractStockConidCmd(opts))
	return c
}

func oneConid(args []string) (int64, error) {
	ids, err := parseConids(args)
	if err != nil {
		return 0, err
	}
	return ids[0], nil
}

func newContractSecdefCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "secdef <conid>...",
		Short: "Full security definitions by conid (GET /trsrv/secdef; max 200 conids per request)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.SecdefByConids(ctx(), ids)
			})
		},
	}
}

func newContractAllConidsCmd(opts *options) *cobra.Command {
	var exchange string
	cmd := &cobra.Command{
		Use:   "all-conids",
		Short: "Every conid on an exchange (GET /trsrv/all-conids)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.AllConidsByExchange(ctx(), exchange)
			})
		},
	}
	cmd.Flags().StringVar(&exchange, "exchange", "SMART", "exchange")
	return cmd
}

func newContractInfoCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "info <conid>",
		Short: "Contract information (GET /iserver/contract/{conid}/info)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := oneConid(args)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.ContractInfo(ctx(), id)
			})
		},
	}
}

func newContractInfoRulesCmd(opts *options) *cobra.Command {
	var isBuy bool
	cmd := &cobra.Command{
		Use:   "info-rules <conid>",
		Short: "Contract info + trading rules (GET /iserver/contract/{conid}/info-and-rules)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := oneConid(args)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.InfoAndRules(ctx(), id, isBuy)
			})
		},
	}
	cmd.Flags().BoolVar(&isBuy, "is-buy", true, "rules for the buy side (false: sell side)")
	return cmd
}

func newContractRulesCmd(opts *options) *cobra.Command {
	var exchange string
	var isBuy, modifyOrder bool
	var orderID int64
	cmd := &cobra.Command{
		Use:   "rules <conid>",
		Short: "Order rules for a contract (POST /iserver/contract/rules)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := oneConid(args)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.ContractRules(ctx(), id, exchange, isBuy, orderID, modifyOrder)
			})
		},
	}
	cmd.Flags().StringVar(&exchange, "exchange", "", "exchange")
	cmd.Flags().BoolVar(&isBuy, "is-buy", true, "rules for the buy side")
	cmd.Flags().BoolVar(&modifyOrder, "modify-order", false, "rules for modifying an existing order (requires --order-id)")
	cmd.Flags().Int64Var(&orderID, "order-id", 0, "existing order id (for --modify-order)")
	return cmd
}

func newContractAlgosCmd(opts *options) *cobra.Command {
	var algosFlag string
	var addDescription, addParams bool
	cmd := &cobra.Command{
		Use:   "algos <conid>",
		Short: "Supported IB Algos for a contract (GET /iserver/contract/{conid}/algos)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := oneConid(args)
			if err != nil {
				return err
			}
			var algos []string
			if algosFlag != "" {
				algos = strings.Split(algosFlag, ";")
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.AlgoParams(ctx(), id, algos, addDescription, addParams)
			})
		},
	}
	cmd.Flags().StringVar(&algosFlag, "algos", "", "semicolon-separated algo ids (max 8)")
	cmd.Flags().BoolVar(&addDescription, "description", false, "include algo descriptions")
	cmd.Flags().BoolVar(&addParams, "params", false, "include algo parameters")
	return cmd
}

func newContractBondFiltersCmd(opts *options) *cobra.Command {
	var symbol, issuerID string
	cmd := &cobra.Command{
		Use:   "bond-filters",
		Short: "Bond filter dimensions for a symbol/issuer (GET /iserver/secdef/bond-filters)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.BondFilters(ctx(), symbol, issuerID)
			})
		},
	}
	cmd.Flags().StringVar(&symbol, "symbol", "", "bond symbol")
	cmd.Flags().StringVar(&issuerID, "issuer-id", "", "issuer id")
	return cmd
}

func newContractPairsCmd(opts *options) *cobra.Command {
	var currency string
	cmd := &cobra.Command{
		Use:   "pairs",
		Short: "Tradable currency pairs (GET /iserver/currency/pairs)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.CurrencyPairs(ctx(), currency)
			})
		},
	}
	cmd.Flags().StringVar(&currency, "currency", "USD", "base currency")
	return cmd
}

func newContractExchangeRateCmd(opts *options) *cobra.Command {
	var source, target string
	cmd := &cobra.Command{
		Use:   "exchangerate",
		Short: "Currency exchange rate (GET /iserver/exchangerate)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.CurrencyExchangeRate(ctx(), source, target)
			})
		},
	}
	cmd.Flags().StringVar(&source, "source", "USD", "source currency")
	cmd.Flags().StringVar(&target, "target", "EUR", "target currency")
	return cmd
}

func newContractFuturesCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "futures <symbol>...",
		Short: "Non-expired futures contracts by underlier symbol (GET /trsrv/futures)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.FuturesBySymbol(ctx(), args)
			})
		},
	}
}

func newContractScheduleCmd(opts *options) *cobra.Command {
	var assetClass, symbol, exchange, exchangeFilter string
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Trading schedule / hours (GET /trsrv/secdef/schedule)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if assetClass == "" || symbol == "" {
				return &apierr.UsageError{Msg: "--asset-class and --symbol are required"}
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.TradingSchedule(ctx(), assetClass, symbol, exchange, exchangeFilter)
			})
		},
	}
	cmd.Flags().StringVar(&assetClass, "asset-class", "", "asset class: STK, OPT, FUT, ... (required)")
	cmd.Flags().StringVar(&symbol, "symbol", "", "symbol (required)")
	cmd.Flags().StringVar(&exchange, "exchange", "", "exchange")
	cmd.Flags().StringVar(&exchangeFilter, "exchange-filter", "", "exchange filter")
	return cmd
}

func newContractStockConidCmd(opts *options) *cobra.Command {
	var noDefaultFiltering bool
	cmd := &cobra.Command{
		Use:   "stock-conid <symbol>...",
		Short: "Resolve symbols to exactly one conid each (IBind stock_conid_by_symbol)",
		Long: `Mirrors IBind: contracts are filtered to US listings by default
and any symbol that does not resolve to exactly one instrument with
exactly one surviving contract is reported as ambiguous instead of
being guessed.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			out := make(map[string]int64, len(args))
			for _, sym := range args {
				id, _, err := env.client.StockConid(ctx(), sym, !noDefaultFiltering)
				if err != nil {
					return err
				}
				out[sym] = id
			}
			if opts.jsonOut {
				data, _ := json.Marshal(out)
				printBody(data)
				return nil
			}
			for _, sym := range args {
				fmt.Printf("%s\t%d\n", sym, out[sym])
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noDefaultFiltering, "no-default-filtering", false, "do not restrict to US contracts (IBind default_filtering=False)")
	return cmd
}

package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/config"
	"github.com/rayli09/ibkr-cli/internal/contracts"
	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

func newMarketCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "market",
		Short: "Contract search, quotes (with priming), history, strikes, option chains",
	}
	c.AddCommand(newMarketSearchCmd(opts))
	c.AddCommand(newMarketStocksCmd(opts))
	c.AddCommand(newMarketInfoCmd(opts))
	c.AddCommand(newMarketQuoteCmd(opts))
	c.AddCommand(newMarketQuoteSymbolsCmd(opts))
	c.AddCommand(newMarketRegsnapshotCmd(opts))
	c.AddCommand(newMarketHistoryCmd(opts))
	c.AddCommand(newMarketHistoryBetaCmd(opts))
	c.AddCommand(newMarketHistorySymbolCmd(opts))
	c.AddCommand(newMarketHistorySymbolsCmd(opts))
	c.AddCommand(newMarketHistoryMultiCmd(opts))
	c.AddCommand(newMarketStrikesCmd(opts))
	c.AddCommand(newMarketChainCmd(opts))
	return c
}

func parseConids(args []string) ([]int64, error) {
	out := make([]int64, 0, len(args))
	for _, a := range args {
		n, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			return nil, &apierr.UsageError{Msg: fmt.Sprintf("invalid conid %q", a)}
		}
		out = append(out, n)
	}
	return out, nil
}

func newMarketSearchCmd(opts *options) *cobra.Command {
	var name bool
	var secType string
	cmd := &cobra.Command{
		Use:   "search <text>",
		Short: "Search contracts by symbol or company name (GET /iserver/secdef/search)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			results, resp, err := env.client.SecdefSearch(ctx(), args[0], name, secType)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "CONID\tSYMBOL\tNAME\tSECTYPES")
			for _, r := range results {
				types := []string{}
				for _, s := range r.Sections {
					types = append(types, s.SecType)
				}
				cid, _ := r.Conid.Int64()
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", cid, r.Symbol, r.CompanyName, strings.Join(types, ","))
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&name, "name", false, "search by company name instead of symbol")
	cmd.Flags().StringVar(&secType, "sectype", "", "restrict to a security type (STK, OPT, FUT, ...)")
	return cmd
}

func newMarketStocksCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "stocks <symbol>...",
		Short: "Resolve stock symbols to per-exchange conids (GET /trsrv/stocks)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			symbols := make([]string, len(args))
			for i, s := range args {
				symbols[i] = strings.ToUpper(s)
			}
			stocks, resp, err := env.client.Stocks(ctx(), symbols)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "SYMBOL\tNAME\tCONID\tEXCHANGE")
			for _, sym := range symbols {
				for _, inst := range stocks[sym] {
					for _, c := range inst.Contracts {
						fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", sym, inst.Name, c.Conid, c.Exchange)
					}
				}
			}
			return w.Flush()
		},
	}
}

func newMarketInfoCmd(opts *options) *cobra.Command {
	var secType, month, exchange, right, issuerID string
	var strike float64
	cmd := &cobra.Command{
		Use:   "info <conid>",
		Short: "Contract details; resolves option conids when combined with --month/--strike/--right (GET /iserver/secdef/info)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			resp, err := env.client.SecdefInfo(ctx(), ids[0], secType, month, exchange, right, strike, issuerID)
			if err != nil {
				return err
			}
			printBody(resp.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&secType, "sectype", "", "security type (OPT for option resolution)")
	cmd.Flags().StringVar(&month, "month", "", "option month, e.g. NOV26")
	cmd.Flags().Float64Var(&strike, "strike", 0, "option strike")
	cmd.Flags().StringVar(&right, "right", "", "option right: C or P")
	cmd.Flags().StringVar(&exchange, "exchange", "", "exchange filter")
	cmd.Flags().StringVar(&issuerID, "issuer-id", "", "bond issuer id (IBind: issuerId)")
	return cmd
}

func newMarketQuoteCmd(opts *options) *cobra.Command {
	var fields string
	cmd := &cobra.Command{
		Use:   "quote <conid>...",
		Short: "Snapshot quotes; subscription priming is absorbed (GET /iserver/marketdata/snapshot)",
		Long: `Fetch snapshot quotes. CPAPI's lazy market-data subscription
means a first snapshot often returns only the conid; this command
waits briefly and re-requests the unfilled conids once, then reports
any conid that still has no data (likely no subscription / market
closed) instead of printing a silent empty row.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			useFields := cpapi.QuoteFields
			if fields != "" {
				useFields = strings.Split(fields, ",")
			}
			entries, missing, err := env.client.Snapshot(ctx(), ids, useFields)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				data, _ := json.Marshal(entries)
				printBody(data)
			} else {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "CONID\tSYMBOL\tLAST\tBID\tASK\tHIGH\tLOW\tVOLUME")
				for _, e := range entries {
					fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
						e.Conid(),
						e.Str(cpapi.FieldSymbol),
						e.Str(cpapi.FieldLast),
						e.Str(cpapi.FieldBid),
						e.Str(cpapi.FieldAsk),
						e.Str(cpapi.FieldHigh),
						e.Str(cpapi.FieldLow),
						e.Str(cpapi.FieldVolume))
				}
				w.Flush()
			}
			if len(missing) > 0 {
				parts := make([]string, len(missing))
				for i, m := range missing {
					parts[i] = fmt.Sprint(m)
				}
				fmt.Fprintf(os.Stderr, "note: no market data returned for conid(s) %s after priming (subscription or market hours?)\n",
					strings.Join(parts, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fields, "fields", "", "comma-separated snapshot field codes (default: quote set)")
	return cmd
}

func newMarketHistoryCmd(opts *options) *cobra.Command {
	var period, bar, exchange, startTime string
	var outsideRTH bool
	cmd := &cobra.Command{
		Use:   "history <conid>",
		Short: "OHLC history bars (GET /iserver/marketdata/history)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			resp, err := env.client.HistoryEx(ctx(), ids[0], period, bar, outsideRTH, exchange, startTime)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			var h struct {
				Data []struct {
					T int64   `json:"t"`
					O float64 `json:"o"`
					H float64 `json:"h"`
					L float64 `json:"l"`
					C float64 `json:"c"`
					V float64 `json:"v"`
				} `json:"data"`
			}
			if err := json.Unmarshal(resp.Body, &h); err != nil || len(h.Data) == 0 {
				printBody(resp.Body)
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TIME\tOPEN\tHIGH\tLOW\tCLOSE\tVOLUME")
			for _, b := range h.Data {
				fmt.Fprintf(w, "%s\t%.2f\t%.2f\t%.2f\t%.2f\t%g\n",
					time.UnixMilli(b.T).Format("2006-01-02 15:04"), b.O, b.H, b.L, b.C, b.V)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&period, "period", "1w", "period: 1d, 1w, 1m, 3m, 6m, 1y, ...")
	cmd.Flags().StringVar(&bar, "bar", "1d", "bar size: 1min, 5min, 1h, 1d, 1w, 1m")
	cmd.Flags().BoolVar(&outsideRTH, "outside-rth", false, "include data outside regular trading hours")
	cmd.Flags().StringVar(&exchange, "exchange", "", "exchange to source bars from (IBind: exchange)")
	cmd.Flags().StringVar(&startTime, "start-time", "", "start time YYYYMMDD-HH:MM:SS (IBind: startTime)")
	return cmd
}

var monthCodeRe = regexp.MustCompile(`^[A-Z]{3}\d{2}$`)

func newMarketStrikesCmd(opts *options) *cobra.Command {
	var month, secType, exchange string
	cmd := &cobra.Command{
		Use:   "strikes <underlier-conid>",
		Short: "Strikes for an underlier and month (GET /iserver/secdef/strikes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			if !monthCodeRe.MatchString(month) {
				return &apierr.UsageError{Msg: "--month must look like NOV26 (three-letter month + two-digit year)"}
			}
			strikes, resp, err := env.client.SecdefStrikes(ctx(), ids[0], month, secType, exchange)
			if err != nil {
				return err
			}
			if opts.jsonOut {
				printBody(resp.Body)
				return nil
			}
			fmt.Printf("CALL strikes: %s\n", formatStrikes(strikes.Call))
			fmt.Printf("PUT strikes:  %s\n", formatStrikes(strikes.Put))
			return nil
		},
	}
	cmd.Flags().StringVar(&month, "month", "", "option month, e.g. NOV26 (required)")
	cmd.Flags().StringVar(&secType, "sectype", "OPT", "security type: OPT or WAR")
	cmd.Flags().StringVar(&exchange, "exchange", "", "exchange filter (default SMART)")
	return cmd
}

func formatStrikes(in []float64) string {
	sorted := append([]float64(nil), in...)
	sort.Float64s(sorted)
	parts := make([]string, len(sorted))
	for i, f := range sorted {
		parts[i] = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.Join(parts, " ")
}

// --- market chain (SPEC §6) ---------------------------------------

var monthNames = []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}

type chainLeg struct {
	Conid int64  `json:"conid"`
	Bid   string `json:"bid,omitempty"`
	Ask   string `json:"ask,omitempty"`
	Last  string `json:"last,omitempty"`
	IV    string `json:"iv,omitempty"`
	Delta string `json:"delta,omitempty"`
	OI    string `json:"oi,omitempty"`
	Stale bool   `json:"stale"`
}

type chainRow struct {
	Strike float64   `json:"strike"`
	Call   *chainLeg `json:"call,omitempty"`
	Put    *chainLeg `json:"put,omitempty"`
}

func newMarketChainCmd(opts *options) *cobra.Command {
	var expiry string
	var all bool
	var band float64
	var refresh bool
	cmd := &cobra.Command{
		Use:   "chain <symbol>",
		Short: "Assemble an option chain (strikes -> secdef/info -> snapshot) for one expiry",
		Long: `Assemble an option chain for one expiry. CPAPI has no
chain endpoint, so this implements the spec's algorithm: resolve the
underlier conid, list strikes for the expiry month, keep strikes
within --band of the last price (default ±15%; --all for everything),
resolve each strike/right conid via secdef/info, then snapshot the
whole set once with priming. Legs whose snapshot carries no quote
update are marked stale (*).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			exp, err := time.Parse("20060102", expiry)
			if err != nil {
				return &apierr.UsageError{Msg: "--expiry must be YYYYMMDD"}
			}
			symbol := strings.ToUpper(args[0])
			cache, err := contracts.OpenCache(config.CachePath(env.cfg.FilePath()))
			if err != nil {
				return err
			}
			resolver := &contracts.Resolver{Client: env.client, Cache: cache}
			underlier, err := resolver.StockConid(ctx(), symbol, refresh)
			if err != nil {
				return err
			}
			month := fmt.Sprintf("%s%02d", monthNames[int(exp.Month())-1], exp.Year()%100)
			strikes, _, err := env.client.SecdefStrikes(ctx(), underlier, month, "OPT", "")
			if err != nil {
				return err
			}
			universe := map[float64]bool{}
			for _, s := range strikes.Call {
				universe[s] = true
			}
			for _, s := range strikes.Put {
				universe[s] = true
			}
			// Underlier last price for the band filter.
			var lastPrice float64
			ues, _, err := env.client.Snapshot(ctx(), []int64{underlier}, []string{cpapi.FieldLast, cpapi.FieldClose})
			if err == nil && len(ues) > 0 {
				if v, ok := ues[0].Float(cpapi.FieldLast); ok {
					lastPrice = v
				} else if v, ok := ues[0].Float(cpapi.FieldClose); ok {
					lastPrice = v
				}
			}
			sorted := make([]float64, 0, len(universe))
			for s := range universe {
				if !all && lastPrice > 0 {
					if math.Abs(s-lastPrice) > band*lastPrice {
						continue
					}
				}
				sorted = append(sorted, s)
			}
			sort.Float64s(sorted)
			if len(sorted) == 0 {
				return &apierr.APIError{Msg: "no strikes in band for " + symbol + " " + expiry}
			}
			// Resolve a conid per strike x right, keeping only legs
			// whose maturity matches the requested expiry date.
			rows := make([]chainRow, 0, len(sorted))
			conids := []int64{}
			legIndex := map[int64]struct {
				row int
				put bool
			}{}
			for _, strike := range sorted {
				row := chainRow{Strike: strike}
				rowIdx := len(rows)
				rows = append(rows, row)
				for _, right := range []string{"C", "P"} {
					cid, err := resolveOptionConid(env.client, underlier, month, expiry, strike, right)
					if err != nil {
						return err
					}
					if cid == 0 {
						continue
					}
					leg := &chainLeg{Conid: cid, Stale: true}
					if right == "C" {
						rows[rowIdx].Call = leg
					} else {
						rows[rowIdx].Put = leg
					}
					conids = append(conids, cid)
					legIndex[cid] = struct {
						row int
						put bool
					}{rowIdx, right == "P"}
				}
			}
			// Snapshot in chunks (the endpoint is happiest with
			// bounded batches); fill legs, marking freshness.
			for start := 0; start < len(conids); start += 40 {
				end := min(start+40, len(conids))
				entries, _, err := env.client.Snapshot(ctx(), conids[start:end], cpapi.OptionFields)
				if err != nil {
					return err
				}
				byConid := map[int64]cpapi.SnapshotEntry{}
				for _, e := range entries {
					byConid[e.Conid()] = e
				}
				for _, cid := range conids[start:end] {
					e, ok := byConid[cid]
					if !ok {
						continue
					}
					idx := legIndex[cid]
					var leg *chainLeg
					if idx.put {
						leg = rows[idx.row].Put
					} else {
						leg = rows[idx.row].Call
					}
					leg.Bid = e.Str(cpapi.FieldBid)
					leg.Ask = e.Str(cpapi.FieldAsk)
					leg.Last = e.Str(cpapi.FieldLast)
					leg.IV = e.Str(cpapi.FieldOptionIV)
					leg.Delta = e.Str(cpapi.FieldDelta)
					leg.OI = e.Str(cpapi.FieldOptionOI)
					leg.Stale = !e.HasMarketData()
				}
			}
			if opts.jsonOut {
				out := map[string]any{
					"symbol": symbol, "underlier_conid": underlier,
					"underlier_last": lastPrice, "expiry": expiry, "rows": rows,
				}
				data, _ := json.Marshal(out)
				printBody(data)
				return nil
			}
			fmt.Printf("%s (conid %d) last %s — expiry %s\n\n", symbol, underlier,
				strconv.FormatFloat(lastPrice, 'f', -1, 64), expiry)
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "STRIKE\tC BID\tC ASK\tC LAST\tC IV%\tC Δ\tP BID\tP ASK\tP LAST\tP IV%\tP Δ")
			for _, r := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					strconv.FormatFloat(r.Strike, 'f', -1, 64),
					legCell(r.Call, func(l *chainLeg) string { return l.Bid }),
					legCell(r.Call, func(l *chainLeg) string { return l.Ask }),
					legCell(r.Call, func(l *chainLeg) string { return l.Last }),
					legCell(r.Call, func(l *chainLeg) string { return l.IV }),
					legCell(r.Call, func(l *chainLeg) string { return l.Delta }),
					legCell(r.Put, func(l *chainLeg) string { return l.Bid }),
					legCell(r.Put, func(l *chainLeg) string { return l.Ask }),
					legCell(r.Put, func(l *chainLeg) string { return l.Last }),
					legCell(r.Put, func(l *chainLeg) string { return l.IV }),
					legCell(r.Put, func(l *chainLeg) string { return l.Delta }))
			}
			w.Flush()
			fmt.Println("(* = stale: snapshot carried no market data for the leg)")
			return nil
		},
	}
	cmd.Flags().StringVar(&expiry, "expiry", "", "expiry date YYYYMMDD (required)")
	cmd.Flags().BoolVar(&all, "all", false, "include every strike (ignore --band)")
	cmd.Flags().Float64Var(&band, "band", 0.15, "strike band around the last price, as a fraction")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "ignore the conid cache for the underlier")
	return cmd
}

func legCell(l *chainLeg, get func(*chainLeg) string) string {
	if l == nil {
		return "-"
	}
	v := get(l)
	if v == "" {
		v = "-"
	}
	if l.Stale {
		return v + "*"
	}
	return v
}

// resolveOptionConid resolves one option leg via secdef/info and
// returns the conid whose maturityDate equals expiry (YYYYMMDD), or 0
// when the strike/right has no contract for that date.
func resolveOptionConid(client *cpapi.Client, underlier int64, month, expiry string, strike float64, right string) (int64, error) {
	resp, err := client.SecdefInfo(ctx(), underlier, "OPT", month, "", right, strike, "")
	if err != nil {
		return 0, err
	}
	var contracts []struct {
		Conid        int64  `json:"conid"`
		MaturityDate string `json:"maturityDate"`
	}
	if err := json.Unmarshal(resp.Body, &contracts); err != nil {
		return 0, err
	}
	for _, c := range contracts {
		if digitsOnly(c.MaturityDate) == expiry {
			return c.Conid, nil
		}
	}
	return 0, nil
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- market parity additions (IBind marketdata_mixin) ---------------

func newMarketQuoteSymbolsCmd(opts *options) *cobra.Command {
	var fields string
	var noDefaultFiltering bool
	cmd := &cobra.Command{
		Use:   "quote-symbols <symbol>...",
		Short: "Snapshot quotes by symbol (IBind live_marketdata_snapshot_by_symbol)",
		Long: `Resolves each symbol to a conid (IBind's stock_conid_by_symbol
rules: US contracts only, ambiguity is an error) and snapshots them
in one primed call. Unresolvable symbols are named on stderr.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			useFields := cpapi.QuoteFields
			if fields != "" {
				useFields = strings.Split(fields, ",")
			}
			bySymbol, unresolved, err := env.client.SnapshotBySymbol(ctx(), args, useFields, !noDefaultFiltering)
			if err != nil {
				return err
			}
			data, _ := json.Marshal(bySymbol)
			printBody(data)
			for _, u := range unresolved {
				fmt.Fprintf(os.Stderr, "note: unresolved symbol %s\n", u)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fields, "fields", "", "comma-separated snapshot field codes (default: quote set)")
	cmd.Flags().BoolVar(&noDefaultFiltering, "no-default-filtering", false, "do not restrict resolution to US contracts (IBind default_filtering=False)")
	return cmd
}

func newMarketRegsnapshotCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "regsnapshot <conid>",
		Short: "Regulatory (FINRA/NBBO) snapshot (GET /md/regsnapshot)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.RegulatorySnapshot(ctx(), ids[0])
			})
		},
	}
}

func newMarketHistoryBetaCmd(opts *options) *cobra.Command {
	var period, bar, startTime, direction, barType string
	var outsideRTH bool
	cmd := &cobra.Command{
		Use:   "history-beta <conid>",
		Short: "History via the HMDS service (GET /hmds/history; IBind historical_marketdata_beta)",
		Long: `Queries the historical market data service directly. Upstream
quirk (documented by IBind): the first call for a conid can return
404 while the service instantiates — retry in that case.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.HMDSHistory(ctx(), ids[0], period, bar, outsideRTH, startTime, direction, barType)
			})
		},
	}
	cmd.Flags().StringVar(&period, "period", "1w", "period: 1d, 1w, 1m, 3m, 6m, 1y, ...")
	cmd.Flags().StringVar(&bar, "bar", "1d", "bar size: 1min, 5min, 1h, 1d, 1w, 1m")
	cmd.Flags().BoolVar(&outsideRTH, "outside-rth", false, "include data outside regular trading hours")
	cmd.Flags().StringVar(&startTime, "start-time", "", "start time YYYYMMDD-HH:MM:SS")
	cmd.Flags().StringVar(&direction, "direction", "", "direction: 1 (forward) or -1")
	cmd.Flags().StringVar(&barType, "bar-type", "", "bar type (e.g. TRADES)")
	return cmd
}

func historyBySymbols(opts *options, symbols []string, period, bar string, outsideRTH, noDefaultFiltering bool) error {
	env, err := opts.env()
	if err != nil {
		return err
	}
	conids := make([]int64, 0, len(symbols))
	for _, sym := range symbols {
		id, _, err := env.client.StockConid(ctx(), sym, !noDefaultFiltering)
		if err != nil {
			return err
		}
		conids = append(conids, id)
	}
	results, errs := env.client.HistoryByConids(ctx(), conids, period, bar, outsideRTH)
	out := make(map[string]json.RawMessage, len(symbols))
	for i, sym := range symbols {
		if raw, ok := results[conids[i]]; ok {
			out[sym] = raw
		}
	}
	data, _ := json.Marshal(out)
	printBody(data)
	for id, err := range errs {
		fmt.Fprintf(os.Stderr, "note: history failed for conid %d: %v\n", id, err)
	}
	return nil
}

func newMarketHistorySymbolCmd(opts *options) *cobra.Command {
	var period, bar string
	var outsideRTH, noDefaultFiltering bool
	cmd := &cobra.Command{
		Use:   "history-symbol <symbol>",
		Short: "History bars by symbol (IBind marketdata_history_by_symbol)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return historyBySymbols(opts, args, period, bar, outsideRTH, noDefaultFiltering)
		},
	}
	addHistoryFlags(cmd, &period, &bar, &outsideRTH, &noDefaultFiltering)
	return cmd
}

func newMarketHistorySymbolsCmd(opts *options) *cobra.Command {
	var period, bar string
	var outsideRTH, noDefaultFiltering bool
	cmd := &cobra.Command{
		Use:   "history-symbols <symbol>...",
		Short: "History bars for several symbols (IBind marketdata_history_by_symbols)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return historyBySymbols(opts, args, period, bar, outsideRTH, noDefaultFiltering)
		},
	}
	addHistoryFlags(cmd, &period, &bar, &outsideRTH, &noDefaultFiltering)
	return cmd
}

func newMarketHistoryMultiCmd(opts *options) *cobra.Command {
	var period, bar string
	var outsideRTH bool
	cmd := &cobra.Command{
		Use:   "history-multi <conid>...",
		Short: "History bars for several conids in parallel (IBind marketdata_history_by_conids)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			ids, err := parseConids(args)
			if err != nil {
				return err
			}
			results, errs := env.client.HistoryByConids(ctx(), ids, period, bar, outsideRTH)
			out := make(map[string]json.RawMessage, len(results))
			for id, raw := range results {
				out[fmt.Sprint(id)] = raw
			}
			data, _ := json.Marshal(out)
			printBody(data)
			for id, err := range errs {
				fmt.Fprintf(os.Stderr, "note: history failed for conid %d: %v\n", id, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&period, "period", "1w", "period: 1d, 1w, 1m, 3m, 6m, 1y, ...")
	cmd.Flags().StringVar(&bar, "bar", "1d", "bar size: 1min, 5min, 1h, 1d, 1w, 1m")
	cmd.Flags().BoolVar(&outsideRTH, "outside-rth", false, "include data outside regular trading hours")
	return cmd
}

func addHistoryFlags(cmd *cobra.Command, period, bar *string, outsideRTH, noDefaultFiltering *bool) {
	cmd.Flags().StringVar(period, "period", "1w", "period: 1d, 1w, 1m, 3m, 6m, 1y, ...")
	cmd.Flags().StringVar(bar, "bar", "1d", "bar size: 1min, 5min, 1h, 1d, 1w, 1m")
	cmd.Flags().BoolVar(outsideRTH, "outside-rth", false, "include data outside regular trading hours")
	cmd.Flags().BoolVar(noDefaultFiltering, "no-default-filtering", false, "do not restrict symbol resolution to US contracts")
}

package cpapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// This file covers IBind's contract_mixin. Four methods predate the
// parity extension and live in market.go: search_contract_by_symbol
// (SecdefSearch), search_secdef_info_by_conid (SecdefInfo),
// search_strikes_by_conid (SecdefStrikes), security_stocks_by_symbol
// (Stocks). Everything else is here.

// SecdefByConids returns full security definitions for a batch of
// conids. GET /trsrv/secdef?conids=a,b (IBind:
// security_definition_by_conid; the docs cap one request at 200
// conids.)
func (c *Client) SecdefByConids(ctx context.Context, conids []int64) (*Response, error) {
	parts := make([]string, len(conids))
	for i, id := range conids {
		parts[i] = fmt.Sprint(id)
	}
	q := url.Values{}
	q.Set("conids", strings.Join(parts, ","))
	return c.Get(ctx, "/trsrv/secdef", q)
}

// AllConidsByExchange lists every conid on one exchange.
// GET /trsrv/all-conids?exchange=SMART (IBind: all_conids_by_exchange)
func (c *Client) AllConidsByExchange(ctx context.Context, exchange string) (*Response, error) {
	q := url.Values{}
	q.Set("exchange", exchange)
	return c.Get(ctx, "/trsrv/all-conids", q)
}

// ContractInfo returns contract information for one conid.
// GET /iserver/contract/{conid}/info (IBind:
// contract_information_by_conid — distinct from /iserver/secdef/info.)
func (c *Client) ContractInfo(ctx context.Context, conid int64) (*Response, error) {
	return c.Get(ctx, fmt.Sprintf("/iserver/contract/%d/info", conid), nil)
}

// CurrencyPairs lists tradable pairs for a currency.
// GET /iserver/currency/pairs?currency=USD (IBind: currency_pairs)
func (c *Client) CurrencyPairs(ctx context.Context, currency string) (*Response, error) {
	q := url.Values{}
	q.Set("currency", currency)
	return c.Get(ctx, "/iserver/currency/pairs", q)
}

// CurrencyExchangeRate returns the rate between two currencies.
// GET /iserver/exchangerate?source=USD&target=EUR
// (IBind: currency_exchange_rate)
func (c *Client) CurrencyExchangeRate(ctx context.Context, source, target string) (*Response, error) {
	q := url.Values{}
	q.Set("source", source)
	q.Set("target", target)
	return c.Get(ctx, "/iserver/exchangerate", q)
}

// InfoAndRules returns contract info plus trading rules for one
// conid. GET /iserver/contract/{conid}/info-and-rules?isBuy=true
// (IBind: info_and_rules_by_conid)
func (c *Client) InfoAndRules(ctx context.Context, conid int64, isBuy bool) (*Response, error) {
	q := url.Values{}
	q.Set("isBuy", fmt.Sprint(isBuy))
	return c.Get(ctx, fmt.Sprintf("/iserver/contract/%d/info-and-rules", conid), q)
}

// AlgoParams returns the algo strategies available for one conid.
// GET /iserver/contract/{conid}/algos (IBind: algo_params_by_conid;
// algos is a ";"-joined list of at most 8 strategy ids.)
func (c *Client) AlgoParams(ctx context.Context, conid int64, algos []string, addDescription, addParams bool) (*Response, error) {
	q := url.Values{}
	if len(algos) > 0 {
		q.Set("algos", strings.Join(algos, ";"))
	}
	if addDescription {
		q.Set("addDescription", "1")
	}
	if addParams {
		q.Set("addParams", "1")
	}
	return c.Get(ctx, fmt.Sprintf("/iserver/contract/%d/algos", conid), q)
}

// BondFilters returns the bond-filter dimensions for a symbol or
// issuer. GET /iserver/secdef/bond-filters (IBind:
// search_bond_filter_information)
func (c *Client) BondFilters(ctx context.Context, symbol, issuerID string) (*Response, error) {
	q := url.Values{}
	setParam(q, "symbol", symbol)
	setParam(q, "issuerId", issuerID)
	return c.Get(ctx, "/iserver/secdef/bond-filters", q)
}

// ContractRules returns the order rules for a contract.
// POST /iserver/contract/rules (IBind: search_contract_rules;
// orderId is required when modifyOrder is set.)
func (c *Client) ContractRules(ctx context.Context, conid int64, exchange string, isBuy bool, orderID int64, modifyOrder bool) (*Response, error) {
	body := map[string]any{"conid": conid, "isBuy": isBuy}
	if exchange != "" {
		body["exchange"] = exchange
	}
	if modifyOrder {
		body["modifyOrder"] = true
	}
	if orderID != 0 {
		body["orderId"] = orderID
	}
	return c.Post(ctx, "/iserver/contract/rules", body)
}

// FuturesBySymbol returns futures contract definitions for symbols.
// GET /trsrv/futures?symbols=ES,CL (IBind: security_future_by_symbol)
func (c *Client) FuturesBySymbol(ctx context.Context, symbols []string) (*Response, error) {
	q := url.Values{}
	q.Set("symbols", strings.Join(symbols, ","))
	return c.Get(ctx, "/trsrv/futures", q)
}

// TradingSchedule returns trading hours for a symbol/asset class.
// GET /trsrv/secdef/schedule (IBind: trading_schedule_by_symbol)
func (c *Client) TradingSchedule(ctx context.Context, assetClass, symbol, exchange, exchangeFilter string) (*Response, error) {
	q := url.Values{}
	q.Set("assetClass", assetClass)
	q.Set("symbol", symbol)
	setParam(q, "exchange", exchange)
	setParam(q, "exchangeFilter", exchangeFilter)
	return c.Get(ctx, "/trsrv/secdef/schedule", q)
}

// StockConid resolves one symbol to exactly one conid, mirroring
// IBind's stock_conid_by_symbol: contracts are filtered to US
// listings by default and anything but exactly one instrument with
// exactly one surviving contract is an error (ambiguity must be
// resolved by the caller, never guessed).
func (c *Client) StockConid(ctx context.Context, symbol string, defaultFiltering bool) (int64, *Response, error) {
	stocks, resp, err := c.Stocks(ctx, []string{symbol})
	if err != nil {
		return 0, resp, err
	}
	instruments, ok := stocks[symbol]
	if !ok || len(instruments) == 0 {
		return 0, resp, fmt.Errorf("no stock found for symbol %s", symbol)
	}
	type candidate struct {
		conid    int64
		exchange string
	}
	var found []candidate
	for _, inst := range instruments {
		for _, ct := range inst.Contracts {
			if defaultFiltering && !ct.IsUS {
				continue
			}
			found = append(found, candidate{ct.Conid, ct.Exchange})
		}
	}
	if len(instruments) != 1 || len(found) != 1 {
		return 0, resp, fmt.Errorf("symbol %s is ambiguous: %d instruments, %d candidate contracts after filtering (isUS=%v); use market stocks to inspect",
			symbol, len(instruments), len(found), defaultFiltering)
	}
	return found[0].conid, resp, nil
}

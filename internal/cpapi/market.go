package cpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Snapshot field codes (IBKR Web API market-data field table,
// cross-checked against IBind's ibkr_definitions.py):
const (
	FieldLast       = "31"   // last price (may carry C/H prefix)
	FieldSymbol     = "55"   // symbol
	FieldHigh       = "70"   // session high
	FieldLow        = "71"   // session low
	FieldBid        = "84"   // bid price
	FieldAskSize    = "85"   // ask size
	FieldAsk        = "86"   // ask price
	FieldVolume     = "87"   // volume
	FieldBidSize    = "88"   // bid size
	FieldOpen       = "7295" // session open
	FieldClose      = "7296" // previous close
	FieldOptionIV   = "7633" // implied volatility % for a specific strike
	FieldOptionOI   = "7638" // option open interest
	FieldDelta      = "7308" // option delta
	FieldGamma      = "7309" // option gamma
	FieldTheta      = "7310" // option theta
	FieldVega       = "7311" // option vega
	FieldUpdateTime = "_updated"
	FieldConid      = "conid"
	FieldConidEx    = "conidex"
)

// QuoteFields is the default snapshot field set for `market quote`.
var QuoteFields = []string{
	FieldLast, FieldSymbol, FieldHigh, FieldLow, FieldBid, FieldAskSize,
	FieldAsk, FieldVolume, FieldBidSize, FieldOpen, FieldClose,
}

// OptionFields extends QuoteFields with IV, open interest, and Greeks
// for `market chain`.
var OptionFields = []string{
	FieldLast, FieldSymbol, FieldBid, FieldAskSize, FieldAsk, FieldVolume,
	FieldBidSize, FieldOptionIV, FieldOptionOI, FieldDelta, FieldGamma,
	FieldTheta, FieldVega,
}

// SecdefSearchResult is one entry of GET /iserver/secdef/search.
type SecdefSearchResult struct {
	Conid       json.Number `json:"conid"`
	CompanyName string      `json:"companyName"`
	Symbol      string      `json:"symbol"`
	Description string      `json:"description"`
	Restricted  string      `json:"restricted"`
	Sections    []struct {
		SecType         string `json:"secType"`
		Months          string `json:"months"`
		Exchange        string `json:"exchange"`
		ExchangeConidEx string `json:"conidex"`
	} `json:"sections"`
}

// SecdefSearch searches contracts by symbol or company name.
// GET /iserver/secdef/search
func (c *Client) SecdefSearch(ctx context.Context, symbol string, name bool, secType string) ([]SecdefSearchResult, *Response, error) {
	q := url.Values{}
	q.Set("symbol", symbol)
	if name {
		q.Set("name", "true")
	}
	setParam(q, "secType", secType)
	resp, err := c.Get(ctx, "/iserver/secdef/search", q)
	if err != nil {
		return nil, resp, err
	}
	var out []SecdefSearchResult
	if err := resp.JSON(&out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// StockContracts is GET /trsrv/stocks decoded loosely: symbol ->
// instruments -> contracts (per exchange conids).
type StockContracts map[string][]struct {
	Name       string `json:"name"`
	AssetClass string `json:"assetClass"`
	Contracts  []struct {
		Conid    int64  `json:"conid"`
		Exchange string `json:"exchange"`
		IsUS     bool   `json:"isUS"`
	} `json:"contracts"`
}

// Stocks resolves symbols to per-exchange conids. GET /trsrv/stocks
func (c *Client) Stocks(ctx context.Context, symbols []string) (StockContracts, *Response, error) {
	q := url.Values{}
	q.Set("symbols", strings.Join(symbols, ","))
	resp, err := c.Get(ctx, "/trsrv/stocks", q)
	if err != nil {
		return nil, resp, err
	}
	var out StockContracts
	if err := resp.JSON(&out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// SecdefInfo queries contract details; for options it resolves the
// conid of one (underlier, month, strike, right) combination.
// GET /iserver/secdef/info (IBind: search_secdef_info_by_conid;
// issuerId selects a specific bond issuer.)
func (c *Client) SecdefInfo(ctx context.Context, conid int64, secType, month, exchange, right string, strike float64, issuerID string) (*Response, error) {
	q := url.Values{}
	q.Set("conid", fmt.Sprint(conid))
	setParam(q, "sectype", secType)
	setParam(q, "month", month)
	setParam(q, "exchange", exchange)
	if strike > 0 {
		q.Set("strike", strconv.FormatFloat(strike, 'f', -1, 64))
	}
	setParam(q, "right", right)
	setParam(q, "issuerId", issuerID)
	return c.Get(ctx, "/iserver/secdef/info", q)
}

// Strikes is the decoded GET /iserver/secdef/strikes response.
type Strikes struct {
	Call []float64 `json:"call"`
	Put  []float64 `json:"put"`
}

// SecdefStrikes lists strikes for an underlier and month
// (month as JAN26-style: three-letter month + two-digit year).
// GET /iserver/secdef/strikes (IBind: search_strikes_by_conid;
// secType defaults to OPT, exchange filters the listing venue.)
func (c *Client) SecdefStrikes(ctx context.Context, underlierConid int64, month, secType, exchange string) (*Strikes, *Response, error) {
	if secType == "" {
		secType = "OPT"
	}
	q := url.Values{}
	q.Set("conid", fmt.Sprint(underlierConid))
	q.Set("sectype", secType)
	q.Set("month", month)
	setParam(q, "exchange", exchange)
	resp, err := c.Get(ctx, "/iserver/secdef/strikes", q)
	if err != nil {
		return nil, resp, err
	}
	var out Strikes
	if err := resp.JSON(&out); err != nil {
		return nil, resp, err
	}
	return &out, resp, nil
}

// SnapshotEntry is one contract's snapshot: numeric field codes to
// raw values (strings or numbers in the API's JSON), plus conid.
type SnapshotEntry map[string]any

// Conid returns the entry's conid.
func (e SnapshotEntry) Conid() int64 {
	switch v := e[FieldConid].(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}

// Str returns a field as a string ("" when absent).
func (e SnapshotEntry) Str(field string) string {
	switch v := e[field].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	}
	return ""
}

// Float parses a field as a float, stripping CPAPI's one-letter
// prefixes on the last price (C = previous close, H = halted).
func (e SnapshotEntry) Float(field string) (float64, bool) {
	s := strings.TrimSpace(e.Str(field))
	if s == "" {
		return 0, false
	}
	if len(s) > 1 && (s[0] == 'C' || s[0] == 'H') && (s[1] >= '0' && s[1] <= '9') {
		s = s[1:]
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	return f, err == nil
}

// HasMarketData reports whether the entry carries any core quote
// field (the priming test, SPEC §7.1).
func (e SnapshotEntry) HasMarketData() bool {
	for _, f := range []string{FieldLast, FieldBid, FieldAsk} {
		if _, ok := e[f]; ok {
			return true
		}
	}
	return false
}

// snapshotOnce performs a single snapshot request.
func (c *Client) snapshotOnce(ctx context.Context, conids []int64, fields []string) ([]SnapshotEntry, error) {
	q := url.Values{}
	ids := make([]string, len(conids))
	for i, id := range conids {
		ids[i] = fmt.Sprint(id)
	}
	q.Set("conids", strings.Join(ids, ","))
	q.Set("fields", strings.Join(fields, ","))
	resp, err := c.Get(ctx, "/iserver/marketdata/snapshot", q)
	if err != nil {
		return nil, err
	}
	var out []SnapshotEntry
	if err := resp.JSON(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Snapshot fetches snapshots with CPAPI's subscription priming
// absorbed (SPEC §7.1): conids whose first response carries no
// market data are re-requested once after PrimeDelay. It returns the
// merged entries plus the conids that stayed empty after the retry.
func (c *Client) Snapshot(ctx context.Context, conids []int64, fields []string) ([]SnapshotEntry, []int64, error) {
	byConid := map[int64]SnapshotEntry{}
	var missing []int64
	entries, err := c.snapshotOnce(ctx, conids, fields)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		byConid[e.Conid()] = e
	}
	for _, id := range conids {
		if e, ok := byConid[id]; !ok || !e.HasMarketData() {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		c.sleep(c.PrimeDelay)
		more, err := c.snapshotOnce(ctx, missing, fields)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range more {
			byConid[e.Conid()] = e
		}
		missing = missing[:0]
		for _, id := range conids {
			if e, ok := byConid[id]; !ok || !e.HasMarketData() {
				missing = append(missing, id)
			}
		}
	}
	out := make([]SnapshotEntry, 0, len(conids))
	for _, id := range conids {
		if e, ok := byConid[id]; ok {
			out = append(out, e)
		}
	}
	return out, missing, nil
}

// History returns OHLC bars. GET /iserver/marketdata/history with
// period like "1w"/"1m"/"1y" and bar like "1d"/"1h"/"5min".
func (c *Client) History(ctx context.Context, conid int64, period, bar string, outsideRTH bool) (*Response, error) {
	q := url.Values{}
	q.Set("conid", fmt.Sprint(conid))
	setParam(q, "period", period)
	setParam(q, "bar", bar)
	if outsideRTH {
		q.Set("outsideRth", "true")
	}
	return c.Get(ctx, "/iserver/marketdata/history", q)
}

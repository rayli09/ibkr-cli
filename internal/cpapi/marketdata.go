package cpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
)

// This file covers the parts of IBind's marketdata_mixin not already
// in market.go (live_marketdata_snapshot → Snapshot,
// marketdata_history_by_conid → History). The unsubscribe methods
// are WebSocket-session state, not REST: they map to the stream
// commands' lifecycle (see internal/ws and cmd/stream.go).

// RegulatorySnapshot returns the FINRA/NBBO regulatory snapshot for
// one conid. GET /md/regsnapshot (IBind: regulatory_snapshot)
func (c *Client) RegulatorySnapshot(ctx context.Context, conid int64) (*Response, error) {
	q := url.Values{}
	q.Set("conid", fmt.Sprint(conid))
	return c.Get(ctx, "/md/regsnapshot", q)
}

// HistoryEx is History with IBind's optional exchange and startTime
// parameters (startTime format YYYYMMDD-HH:MM:SS).
// GET /iserver/marketdata/history
func (c *Client) HistoryEx(ctx context.Context, conid int64, period, bar string, outsideRTH bool, exchange, startTime string) (*Response, error) {
	q := url.Values{}
	q.Set("conid", fmt.Sprint(conid))
	setParam(q, "period", period)
	setParam(q, "bar", bar)
	if outsideRTH {
		q.Set("outsideRth", "true")
	}
	setParam(q, "exchange", exchange)
	setParam(q, "startTime", startTime)
	return c.Get(ctx, "/iserver/marketdata/history", q)
}

// HMDSHistory queries the historical-market-data service directly.
// GET /hmds/history (IBind: historical_marketdata_beta). Note the
// upstream quirk IBind documents: the very first call for a conid
// can 404 while the HMDS farmer instantiates; a retry succeeds.
func (c *Client) HMDSHistory(ctx context.Context, conid int64, period, bar string, outsideRTH bool, startTime, direction, barType string) (*Response, error) {
	q := url.Values{}
	q.Set("conid", fmt.Sprint(conid))
	setParam(q, "period", period)
	setParam(q, "bar", bar)
	if outsideRTH {
		q.Set("outsideRth", "true")
	}
	setParam(q, "startTime", startTime)
	setParam(q, "direction", direction)
	setParam(q, "barType", barType)
	return c.Get(ctx, "/hmds/history", q)
}

// HistoryByConids fetches history for several conids in parallel
// (IBind: marketdata_history_by_conids, at most 4 workers). Per-conid
// failures are reported in the errors map rather than failing the
// whole call, exactly like IBind's gather.
func (c *Client) HistoryByConids(ctx context.Context, conids []int64, period, bar string, outsideRTH bool) (map[int64]json.RawMessage, map[int64]error) {
	results := make(map[int64]json.RawMessage, len(conids))
	errs := make(map[int64]error)
	var mu sync.Mutex
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, id := range conids {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			resp, err := c.History(ctx, id, period, bar, outsideRTH)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[id] = err
				return
			}
			results[id] = json.RawMessage(resp.Body)
		}(id)
	}
	wg.Wait()
	return results, errs
}

// SnapshotBySymbol resolves symbols to conids (IBind's
// stock_conid_by_symbol rules) and snapshots them in one call,
// keyed by symbol. IBind: live_marketdata_snapshot_by_symbol.
func (c *Client) SnapshotBySymbol(ctx context.Context, symbols []string, fields []string, defaultFiltering bool) (map[string]SnapshotEntry, []string, error) {
	byConid := make(map[int64]string, len(symbols))
	conids := make([]int64, 0, len(symbols))
	var unresolved []string
	for _, sym := range symbols {
		id, _, err := c.StockConid(ctx, sym, defaultFiltering)
		if err != nil {
			unresolved = append(unresolved, sym+": "+err.Error())
			continue
		}
		byConid[id] = sym
		conids = append(conids, id)
	}
	if len(conids) == 0 {
		return map[string]SnapshotEntry{}, unresolved, nil
	}
	entries, _, err := c.Snapshot(ctx, conids, fields)
	if err != nil {
		return nil, unresolved, err
	}
	out := make(map[string]SnapshotEntry, len(entries))
	for _, e := range entries {
		if sym, ok := byConid[e.Conid()]; ok {
			out[sym] = e
		}
	}
	return out, unresolved, nil
}

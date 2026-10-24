package cpapi

import (
	"context"
	"fmt"
	"net/url"
)

// This file covers IBind's portfolio_mixin in full. The four methods
// that predate the parity extension (portfolio_accounts,
// portfolio_summary, positions, get_ledger) live in accounts.go;
// everything else is here.

// PortfolioSubaccounts lists sub-accounts. GET /portfolio/subaccounts
// (IBind: portfolio_subaccounts)
func (c *Client) PortfolioSubaccounts(ctx context.Context) (*Response, error) {
	return c.Get(ctx, "/portfolio/subaccounts", nil)
}

// PortfolioSubaccountsLarge lists sub-accounts for large account
// families, paged 100 at a time. GET /portfolio/subaccounts2
// (IBind: large_portfolio_subaccounts)
func (c *Client) PortfolioSubaccountsLarge(ctx context.Context, page int) (*Response, error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", fmt.Sprint(page))
	}
	return c.Get(ctx, "/portfolio/subaccounts2", q)
}

// PortfolioAccountInfo returns account metadata (type, currency,
// business type, ...). GET /portfolio/{accountId}/meta
// (IBind: portfolio_account_information)
func (c *Client) PortfolioAccountInfo(ctx context.Context, accountID string) (*Response, error) {
	return c.Get(ctx, "/portfolio/"+url.PathEscape(accountID)+"/meta", nil)
}

// PortfolioAccountAllocation returns the allocation breakdown of one
// account (in an FA structure). GET /portfolio/{accountId}/allocation
// (IBind: portfolio_account_allocation)
func (c *Client) PortfolioAccountAllocation(ctx context.Context, accountID string) (*Response, error) {
	return c.Get(ctx, "/portfolio/"+url.PathEscape(accountID)+"/allocation", nil)
}

// PortfolioAllocations returns allocations for several accounts at
// once. GET /portfolio/allocation?acctIds=a,b (IBind:
// portfolio_account_allocations; IBind issues this as a GET with
// acctIds as query parameters.)
func (c *Client) PortfolioAllocations(ctx context.Context, accountIDs []string) (*Response, error) {
	q := url.Values{}
	for _, id := range accountIDs {
		q.Add("acctIds", id)
	}
	return c.Get(ctx, "/portfolio/allocation", q)
}

// CombinationPositions returns positions in combination (spread)
// instruments. GET /portfolio/{accountId}/combo/positions
// (IBind: combination_positions)
func (c *Client) CombinationPositions(ctx context.Context, accountID string, nocache bool) (*Response, error) {
	q := url.Values{}
	if nocache {
		q.Set("nocache", "true")
	}
	return c.Get(ctx, "/portfolio/"+url.PathEscape(accountID)+"/combo/positions", q)
}

// AccountPositionsSorted is positions with the query parameters IBind
// exposes (model, sort, direction, period). GET
// /portfolio/{accountId}/positions/{page} (IBind: positions)
func (c *Client) AccountPositionsSorted(ctx context.Context, accountID string, page int, model, sort, direction, period string) (*Response, error) {
	q := url.Values{}
	setParam(q, "model", model)
	setParam(q, "sort", sort)
	setParam(q, "direction", direction)
	setParam(q, "period", period)
	return c.Get(ctx, fmt.Sprintf("/portfolio/%s/positions/%d",
		url.PathEscape(accountID), page), q)
}

// Positions2 returns the lightweight positions form.
// GET /portfolio2/{accountId}/positions (IBind: positions2)
func (c *Client) Positions2(ctx context.Context, accountID string) (*Response, error) {
	return c.Get(ctx, "/portfolio2/"+url.PathEscape(accountID)+"/positions", nil)
}

// PositionByConid returns the position (if any) in one contract.
// GET /portfolio/{accountId}/position/{conid}
// (IBind: positions_by_conid)
func (c *Client) PositionByConid(ctx context.Context, accountID string, conid int64) (*Response, error) {
	return c.Get(ctx, fmt.Sprintf("/portfolio/%s/position/%d",
		url.PathEscape(accountID), conid), nil)
}

// PositionAndContractInfo returns position + contract detail for one
// conid across accounts. GET /portfolio/positions/{conid}
// (IBind: position_and_contract_info)
func (c *Client) PositionAndContractInfo(ctx context.Context, conid int64) (*Response, error) {
	return c.Get(ctx, fmt.Sprintf("/portfolio/positions/%d", conid), nil)
}

// InvalidatePortfolioCache asks the backend to drop its cached
// positions for the account. POST /portfolio/{accountId}/positions/invalidate
// (IBind: invalidate_backend_portfolio_cache). Server-state mutating:
// the CLI gates it behind --confirm.
func (c *Client) InvalidatePortfolioCache(ctx context.Context, accountID string) (*Response, error) {
	return c.Post(ctx, "/portfolio/"+url.PathEscape(accountID)+"/positions/invalidate", nil)
}

// AccountPerformance returns cumulative/returns performance series.
// POST /pa/performance {"acctIds": [...], "period": "1M"|"1Y"|...}
// (IBind: account_performance)
func (c *Client) AccountPerformance(ctx context.Context, accountIDs []string, period string) (*Response, error) {
	// IBind always sends period (one of 1D, 7D, MTD, 1M, YTD, 1Y).
	return c.Post(ctx, "/pa/performance", map[string]any{"acctIds": accountIDs, "period": period})
}

// AllPeriods returns the periods available for /pa queries.
// POST /pa/allperiods {"acctIds": [...]} (IBind: all_periods)
func (c *Client) AllPeriods(ctx context.Context, accountIDs []string) (*Response, error) {
	return c.Post(ctx, "/pa/allperiods", map[string]any{"acctIds": accountIDs})
}

// TransactionHistory returns account transactions (deposits, fees,
// trades, ...) for a lookback window. POST /pa/transactions
// {"acctIds": [...], "conids": [...], "currency": ..., "days": n}
// (IBind: transaction_history; empty fields are omitted like IBind's
// params_dict.)
func (c *Client) TransactionHistory(ctx context.Context, accountIDs []string, conids []int64, currency string, days int) (*Response, error) {
	body := map[string]any{"acctIds": accountIDs}
	if len(conids) > 0 {
		body["conids"] = conids
	}
	if currency != "" {
		body["currency"] = currency
	}
	if days > 0 {
		body["days"] = days
	}
	return c.Post(ctx, "/pa/transactions", body)
}

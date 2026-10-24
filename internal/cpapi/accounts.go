package cpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// IServerAccounts is the decoded GET /iserver/accounts response.
type IServerAccounts struct {
	Accounts        []string `json:"accounts"`
	SelectedAccount string   `json:"selectedAccount"`
	Aliases         map[string]struct {
		Account string `json:"account"`
		Alias   string `json:"alias"`
	} `json:"aliases"`
}

// PortfolioAccount is one entry of GET /portfolio/accounts.
type PortfolioAccount struct {
	ID             string `json:"id"`
	AccountID      string `json:"accountId"`
	AccountVan     string `json:"accountVan"`
	AccountTitle   string `json:"accountTitle"`
	DisplayName    string `json:"displayName"`
	AccountAlias   string `json:"accountAlias"`
	Type           string `json:"type"`
	TradingType    string `json:"tradingType"`
	Currency       string `json:"currency"`
	BusinessType   string `json:"businessType"`
	IbEntity       string `json:"ibEntity"`
	Faclient       bool   `json:"faclient"`
	ClearingStatus string `json:"clearingStatus"`
}

// IServerAccounts returns the accounts visible to the brokerage
// session. GET /iserver/accounts
func (c *Client) IServerAccounts(ctx context.Context) (*IServerAccounts, *Response, error) {
	resp, err := c.Get(ctx, "/iserver/accounts", nil)
	if err != nil {
		return nil, resp, err
	}
	var out IServerAccounts
	if err := resp.JSON(&out); err != nil {
		return nil, resp, err
	}
	return &out, resp, nil
}

// PortfolioAccounts returns the portfolio account list.
// GET /portfolio/accounts
func (c *Client) PortfolioAccounts(ctx context.Context) ([]PortfolioAccount, *Response, error) {
	resp, err := c.Get(ctx, "/portfolio/accounts", nil)
	if err != nil {
		return nil, resp, err
	}
	var out []PortfolioAccount
	if err := resp.JSON(&out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// AccountSummary returns the raw summary object for an account: a map
// of metric name to {amount, currency, isNull}. GET
// /portfolio/{accountId}/summary
func (c *Client) AccountSummary(ctx context.Context, accountID string) (*Response, error) {
	return c.Get(ctx, "/portfolio/"+url.PathEscape(accountID)+"/summary", nil)
}

// AccountPositions returns one page of positions; page numbering
// starts at 0 and a short page ends the list.
// GET /portfolio/{accountId}/positions/{page}
func (c *Client) AccountPositions(ctx context.Context, accountID string, page int) (*Response, error) {
	return c.Get(ctx, fmt.Sprintf("/portfolio/%s/positions/%d",
		url.PathEscape(accountID), page), nil)
}

// AllPositions aggregates every positions page (SPEC §5: the CLI
// aggregates upstream pagination). Pages are fetched until one
// comes back empty.
func (c *Client) AllPositions(ctx context.Context, accountID string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	for page := 0; ; page++ {
		resp, err := c.AccountPositions(ctx, accountID, page)
		if err != nil {
			return nil, err
		}
		var items []json.RawMessage
		if err := resp.JSON(&items); err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return all, nil
		}
		all = append(all, items...)
	}
}

// AccountLedger returns the per-currency ledger for an account.
// GET /portfolio/{accountId}/ledger
func (c *Client) AccountLedger(ctx context.Context, accountID string) (*Response, error) {
	return c.Get(ctx, "/portfolio/"+url.PathEscape(accountID)+"/ledger", nil)
}

// AccountPnL returns partitioned PnL for the session's accounts.
// GET /iserver/account/pnl/partitioned
func (c *Client) AccountPnL(ctx context.Context) (*Response, error) {
	return c.Get(ctx, "/iserver/account/pnl/partitioned", nil)
}

// IServerAccountSummary is IBind's account_summary: the /iserver
// variant of the account summary (distinct from the /portfolio
// summary behind AccountSummary). GET /iserver/account/{accountId}/summary
func (c *Client) IServerAccountSummary(ctx context.Context, accountID string) (*Response, error) {
	return c.Get(ctx, "/iserver/account/"+url.PathEscape(accountID)+"/summary", nil)
}

// SearchDynamicAccount resolves a dynamic-account pattern (account
// number / username / alias) to matching accounts.
// GET /iserver/account/search/{pattern} (IBind: search_dynamic_account)
func (c *Client) SearchDynamicAccount(ctx context.Context, pattern string) (*Response, error) {
	return c.Get(ctx, "/iserver/account/search/"+url.PathEscape(pattern), nil)
}

// SetDynamicAccount selects the dynamic account. POST /iserver/dynaccount
// with {"acctId": ...} (IBind: set_dynamic_account). Mutating: the CLI
// gates it behind --confirm.
func (c *Client) SetDynamicAccount(ctx context.Context, accountID string) (*Response, error) {
	return c.Post(ctx, "/iserver/dynaccount", map[string]any{"acctId": accountID})
}

// SignaturesAndOwners returns account owners and signatories.
// GET /acesws/{accountId}/signatures-and-owners (IBind: signatures_and_owners)
func (c *Client) SignaturesAndOwners(ctx context.Context, accountID string) (*Response, error) {
	return c.Get(ctx, "/acesws/"+url.PathEscape(accountID)+"/signatures-and-owners", nil)
}

// SwitchAccount switches the session's active account.
// POST /iserver/account with {"acctId": ...} (IBind: switch_account).
// Mutating: the CLI gates it behind --confirm.
func (c *Client) SwitchAccount(ctx context.Context, accountID string) (*Response, error) {
	return c.Post(ctx, "/iserver/account", map[string]any{"acctId": accountID})
}

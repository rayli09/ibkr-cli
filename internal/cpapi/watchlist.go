package cpapi

import (
	"context"
	"net/url"
)

// CreateWatchlist creates a user watchlist. POST /iserver/watchlist
// with {"id": ..., "name": ..., "rows": [{"C": conid}, ...]}
// (IBind: create_watchlist). Mutating: gated behind --confirm.
func (c *Client) CreateWatchlist(ctx context.Context, id, name string, conids []int64) (*Response, error) {
	rows := make([]map[string]any, len(conids))
	for i, id64 := range conids {
		rows[i] = map[string]any{"C": id64}
	}
	body := map[string]any{"name": name, "rows": rows}
	if id != "" {
		body["id"] = id
	}
	return c.Post(ctx, "/iserver/watchlist", body)
}

// AllWatchlists lists the user's watchlists.
// GET /iserver/watchlists?SC=USER_WATCHLIST (IBind: get_all_watchlists)
func (c *Client) AllWatchlists(ctx context.Context) (*Response, error) {
	q := url.Values{}
	q.Set("SC", "USER_WATCHLIST")
	return c.Get(ctx, "/iserver/watchlists", q)
}

// WatchlistInfo returns one watchlist's contents.
// GET /iserver/watchlist?id=... (IBind: get_watchlist_information)
func (c *Client) WatchlistInfo(ctx context.Context, id string) (*Response, error) {
	q := url.Values{}
	q.Set("id", id)
	return c.Get(ctx, "/iserver/watchlist", q)
}

// DeleteWatchlist deletes one watchlist.
// DELETE /iserver/watchlist?id=... (IBind: delete_watchlist).
// Mutating: gated behind --confirm.
func (c *Client) DeleteWatchlist(ctx context.Context, id string) (*Response, error) {
	q := url.Values{}
	q.Set("id", id)
	return c.DeleteQ(ctx, "/iserver/watchlist", q)
}

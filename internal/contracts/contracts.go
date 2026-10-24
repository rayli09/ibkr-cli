// Package contracts resolves symbols to CPAPI conids and caches the
// resolutions on disk (SPEC §7.2): CPAPI is conid-centric, symbols
// are display sugar. Cache entries carry their fetch time and are
// refreshed on a miss, on --refresh, or when older than 7 days.
package contracts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

// MaxCacheAge before a cached resolution is re-fetched.
const MaxCacheAge = 7 * 24 * time.Hour

// Entry is one cached symbol -> conid resolution.
type Entry struct {
	Symbol    string    `json:"symbol"`
	Conid     int64     `json:"conid"`
	Exchange  string    `json:"exchange,omitempty"`
	Name      string    `json:"name,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Cache is the on-disk conid cache.
type Cache struct {
	path    string
	Entries map[string]Entry `json:"entries"`
	Now     func() time.Time `json:"-"`
}

// OpenCache loads (or initialises) the cache at path.
func OpenCache(path string) (*Cache, error) {
	c := &Cache{path: path, Entries: map[string]Entry{}, Now: time.Now}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parsing conid cache %s: %w", path, err)
	}
	if c.Entries == nil {
		c.Entries = map[string]Entry{}
	}
	return c, nil
}

// Save persists the cache (0600; it sits next to the config).
func (c *Cache) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *Cache) fresh(e Entry) bool {
	return c.Now().Sub(e.FetchedAt) < MaxCacheAge
}

// Lookup returns a cached entry when fresh.
func (c *Cache) Lookup(symbol string) (Entry, bool) {
	e, ok := c.Entries[symbol]
	if !ok || !c.fresh(e) {
		return Entry{}, false
	}
	return e, true
}

// Put stores an entry.
func (c *Cache) Put(e Entry) {
	e.FetchedAt = c.Now()
	c.Entries[e.Symbol] = e
}

// Symbols lists cached symbols (sorted) — used by diagnostics/tests.
func (c *Cache) Symbols() []string {
	out := make([]string, 0, len(c.Entries))
	for s := range c.Entries {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Resolver resolves symbols via the API, consulting the cache first.
type Resolver struct {
	Client *cpapi.Client
	Cache  *Cache
}

// StockConid returns the primary (first US, else first) conid for a
// stock symbol via GET /trsrv/stocks, cached.
func (r *Resolver) StockConid(ctx context.Context, symbol string, refresh bool) (int64, error) {
	if !refresh {
		if e, ok := r.Cache.Lookup(symbol); ok {
			return e.Conid, nil
		}
	}
	stocks, _, err := r.Client.Stocks(ctx, []string{symbol})
	if err != nil {
		return 0, err
	}
	list, ok := stocks[symbol]
	if !ok || len(list) == 0 {
		return 0, fmt.Errorf("no contracts found for symbol %q", symbol)
	}
	inst := list[0]
	var chosen *struct {
		Conid    int64  `json:"conid"`
		Exchange string `json:"exchange"`
		IsUS     bool   `json:"isUS"`
	}
	for i := range inst.Contracts {
		if inst.Contracts[i].IsUS {
			chosen = &inst.Contracts[i]
			break
		}
	}
	if chosen == nil && len(inst.Contracts) > 0 {
		chosen = &inst.Contracts[0]
	}
	if chosen == nil {
		return 0, fmt.Errorf("no contracts found for symbol %q", symbol)
	}
	r.Cache.Put(Entry{Symbol: symbol, Conid: chosen.Conid, Exchange: chosen.Exchange, Name: inst.Name})
	_ = r.Cache.Save()
	return chosen.Conid, nil
}

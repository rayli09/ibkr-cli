package contracts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

func TestCacheRoundTripAndStaleness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conid-cache.json")
	c, err := OpenCache(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_700_000_000, 0)
	c.Now = func() time.Time { return base }
	c.Put(Entry{Symbol: "AAPL", Conid: 265598, Exchange: "NASDAQ"})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := OpenCache(path)
	if err != nil {
		t.Fatal(err)
	}
	c2.Now = func() time.Time { return base.Add(time.Hour) }
	if e, ok := c2.Lookup("AAPL"); !ok || e.Conid != 265598 {
		t.Fatalf("lookup = %+v %v", e, ok)
	}
	c2.Now = func() time.Time { return base.Add(MaxCacheAge + time.Hour) }
	if _, ok := c2.Lookup("AAPL"); ok {
		t.Fatal("stale entry should miss")
	}
	if _, ok := c2.Lookup("MSFT"); ok {
		t.Fatal("unknown symbol should miss")
	}
}

func TestResolverCaches(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tickle") {
			w.WriteHeader(200)
			return
		}
		hits.Add(1)
		_, _ = w.Write([]byte(`{"AAPL":[{"name":"APPLE INC","assetClass":"STK",` +
			`"contracts":[{"conid":111,"exchange":"ARCA","isUS":false},` +
			`{"conid":265598,"exchange":"NASDAQ","isUS":true}]}]}`))
	}))
	defer srv.Close()
	cache, err := OpenCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Client: cpapi.New(srv.URL+"/v1/api", nil), Cache: cache}
	conid, err := r.StockConid(context.Background(), "AAPL", false)
	if err != nil {
		t.Fatal(err)
	}
	if conid != 265598 {
		t.Fatalf("conid = %d, want the US contract 265598", conid)
	}
	conid, err = r.StockConid(context.Background(), "AAPL", false)
	if err != nil || conid != 265598 {
		t.Fatalf("cached resolve = %d %v", conid, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("stocks endpoint hits = %d, want 1 (second call cached)", hits.Load())
	}
	if _, err := r.StockConid(context.Background(), "ZZZZ", false); err == nil {
		t.Fatal("unknown symbol should error")
	}
}

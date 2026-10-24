package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// stubCPAPI is an in-process fake Client Portal API. It implements
// the behaviours the E2E tests exercise:
//   - snapshot priming: a conid's first snapshot returns no market
//     data, the second returns a full quote (CPAPI's lazy
//     subscription, docs/SPEC.md §7.1);
//   - a placement that raises TWO confirmation questions before an
//     order id comes back (docs/SPEC.md §5.1.3);
//   - session expiry for account "EXPIRED" (401 -> exit 3);
//   - forced 429s on /sso/validate for the retry/rate-limit tests.
type stubCPAPI struct {
	srv *httptest.Server

	mu            sync.Mutex
	snapshotCalls map[int64]int
	placeCalls    int
	whatifCalls   int
	validate429   int

	// Parity-extension mutation counters (dry-run proofs).
	logoutCalls     int
	switchCalls     int
	dynCalls        int
	suppressCalls   int
	invalidateCalls int
	wlCreateCalls   int
	wlDeleteCalls   int
	faSaveCalls     int
	faInvestCalls   int
	faSubmitCalls   int
	faPresetCalls   int
}

const stubAccount = "DU123456"
const stubUnderlier = 265598

func optionConid(strike float64, right string) int64 {
	base := int64(990000000 + int64(strike*100)*10)
	if right == "P" {
		base++
	}
	return base
}

func newStubCPAPI(t *testing.T) *stubCPAPI {
	t.Helper()
	s := &stubCPAPI{snapshotCalls: map[int64]int{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

// baseURL is what goes into the test config as gateway_url.
func (s *stubCPAPI) baseURL() string { return s.srv.URL + "/v1/api" }

func (s *stubCPAPI) counts() (place, whatif int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.placeCalls, s.whatifCalls
}

func (s *stubCPAPI) armValidate429(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validate429 = n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *stubCPAPI) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/api")
	q := r.URL.Query()
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	// --- session -------------------------------------------------
	case path == "/iserver/auth/status" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{
			"authenticated": true, "connected": true, "competing": false,
			"serverInfo": map[string]any{"serverName": "STUB1"},
		})
	case path == "/tickle" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"session": "stub",
			"iserver": map[string]any{"authStatus": map[string]any{"authenticated": true, "connected": true}}})
	case path == "/iserver/reauthenticate" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{})
	case path == "/sso/validate" && r.Method == http.MethodGet:
		if s.validate429 > 0 {
			s.validate429--
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, map[string]any{"error": "slow down"})
			return
		}
		writeJSON(w, 200, map[string]any{"USER_ID": 123, "RESULT": true})

	// --- accounts -------------------------------------------------
	case path == "/iserver/accounts":
		writeJSON(w, 200, map[string]any{
			"accounts":        []string{stubAccount},
			"selectedAccount": stubAccount,
			"aliases":         map[string]any{stubAccount: map[string]any{"account": stubAccount, "alias": stubAccount}},
		})
	case path == "/portfolio/accounts":
		writeJSON(w, 200, []map[string]any{{
			"id": stubAccount, "accountId": stubAccount, "accountTitle": "Paper Test",
			"type": "INDIVIDUAL", "currency": "USD", "tradingType": "MARGIN",
		}})
	case path == "/portfolio/"+stubAccount+"/summary":
		writeJSON(w, 200, map[string]any{
			"netliquidationvalue": map[string]any{"amount": 100000.55, "currency": "USD"},
			"buyingpower":         map[string]any{"amount": 50000.0, "currency": "USD"},
		})
	case path == "/portfolio/EXPIRED/summary":
		writeJSON(w, 401, map[string]any{"error": "session expired"})
	case path == "/portfolio/"+stubAccount+"/positions/invalidate" && r.Method == http.MethodPost:
		// Must precede the positions-page case below ("invalidate"
		// is not a page number); counted in handleParity's fixture.
		s.handleParity(w, r, path, q)
	case strings.HasPrefix(path, "/portfolio/"+stubAccount+"/positions/"):
		page, _ := strconv.Atoi(path[strings.LastIndex(path, "/")+1:])
		if page == 0 {
			writeJSON(w, 200, []map[string]any{{
				"conid": stubUnderlier, "ticker": "AAPL", "position": 10.0,
				"mktPrice": 750.25, "mktValue": 7502.5, "unrealizedPnl": 100.25,
			}})
		} else {
			writeJSON(w, 200, []any{})
		}
	case path == "/portfolio/"+stubAccount+"/ledger":
		writeJSON(w, 200, map[string]any{
			"USD": map[string]any{"cashbalance": 5000.0, "settledcash": 5000.0},
		})
	case path == "/iserver/account/pnl/partitioned":
		writeJSON(w, 200, map[string]any{"upnl": map[string]any{stubAccount: map[string]any{"dpl": 10.0, "upl": 20.0}}})

	// --- market ---------------------------------------------------
	case path == "/iserver/secdef/search":
		writeJSON(w, 200, []map[string]any{{
			"conid": stubUnderlier, "companyName": "APPLE INC", "symbol": "AAPL",
			"sections": []map[string]any{{"secType": "STK"}, {"secType": "OPT"}},
		}})
	case path == "/trsrv/stocks":
		writeJSON(w, 200, map[string]any{
			"AAPL": []map[string]any{{
				"name": "APPLE INC", "assetClass": "STK",
				"contracts": []map[string]any{{"conid": stubUnderlier, "exchange": "NASDAQ", "isUS": true}},
			}},
		})
	case path == "/iserver/secdef/info":
		if q.Get("sectype") == "OPT" {
			strike, _ := strconv.ParseFloat(q.Get("strike"), 64)
			right := q.Get("right")
			writeJSON(w, 200, []map[string]any{{
				"conid": optionConid(strike, right), "secType": "OPT",
				"symbol": "AAPL", "maturityDate": "20261120",
				"strike": strike, "right": right,
			}})
		} else {
			writeJSON(w, 200, []map[string]any{{
				"conid": stubUnderlier, "symbol": "AAPL", "secType": "STK",
			}})
		}
	case path == "/iserver/secdef/strikes":
		strikes := []float64{700, 725, 750, 775, 800}
		writeJSON(w, 200, map[string]any{"call": strikes, "put": strikes})
	case path == "/iserver/marketdata/snapshot":
		s.handleSnapshot(w, q)
	case path == "/iserver/marketdata/history":
		writeJSON(w, 200, map[string]any{"data": []map[string]any{
			{"t": 1760000000000, "o": 740.0, "h": 755.0, "l": 738.0, "c": 750.25, "v": 1000.0},
		}})

	// --- orders ---------------------------------------------------
	case path == "/iserver/account/orders":
		writeJSON(w, 200, map[string]any{"orders": []map[string]any{{
			"orderId": 12345, "account": stubAccount, "ticker": "AAPL",
			"side": "BUY", "totalSize": 10, "price": 150.5, "status": "Submitted",
		}}, "snapshot": true})
	case strings.HasPrefix(path, "/iserver/account/order/status/"):
		writeJSON(w, 200, map[string]any{
			"order_id": 12345, "order_status": "Submitted", "conid": stubUnderlier, "symbol": "AAPL",
		})
	case path == "/iserver/account/trades":
		writeJSON(w, 200, []map[string]any{{
			"account": stubAccount, "conid": stubUnderlier, "symbol": "AAPL",
			"side": "B", "shares": 10, "price": 150.5, "execution_id": "0001",
		}})
	case path == "/iserver/account/"+stubAccount+"/orders/whatif" && r.Method == http.MethodPost:
		s.whatifCalls++
		writeJSON(w, 200, map[string]any{
			"amount": 1505.0, "commission": 1.0, "initialMargin": 750.0, "maintenanceMargin": 500.0,
		})
	case path == "/iserver/account/"+stubAccount+"/orders" && r.Method == http.MethodPost:
		s.placeCalls++
		writeJSON(w, 200, []map[string]any{{
			"id":      "q1",
			"message": []string{"The price 200.00 significantly exceeds the current ask.", "Please confirm to continue."},
		}})
	case path == "/iserver/reply/q1" && r.Method == http.MethodPost:
		writeJSON(w, 200, []map[string]any{{
			"id":      "q2",
			"message": []string{"This order may create a margin deficit.", "Confirm to proceed anyway?"},
		}})
	case path == "/iserver/reply/q2" && r.Method == http.MethodPost:
		writeJSON(w, 200, []map[string]any{{
			"order_id": 9001, "order_status": "Submitted",
		}})
	case path == "/iserver/account/"+stubAccount+"/order/12345" && r.Method == http.MethodPost:
		writeJSON(w, 200, []map[string]any{{"order_id": 12345, "order_status": "Submitted"}})
	case path == "/iserver/account/"+stubAccount+"/order/12345" && r.Method == http.MethodDelete:
		writeJSON(w, 200, map[string]any{"msg": "Request was submitted", "order_id": 12345})

	default:
		if s.handleParity(w, r, path, q) {
			return
		}
		writeJSON(w, 404, map[string]any{"error": fmt.Sprintf("stub: unhandled %s %s", r.Method, path)})
	}
}

func (s *stubCPAPI) handleSnapshot(w http.ResponseWriter, q url.Values) {
	ids := strings.Split(q.Get("conids"), ",")
	out := []map[string]any{}
	for _, idStr := range ids {
		id, _ := strconv.ParseInt(idStr, 10, 64)
		s.snapshotCalls[id]++
		entry := map[string]any{"conid": id}
		if s.snapshotCalls[id] >= 2 {
			// Second request: the subscription is primed.
			if id == stubUnderlier {
				entry["31"] = "750.25"
				entry["55"] = "AAPL"
				entry["7296"] = "748.00"
			} else {
				entry["31"] = "5.10"
				entry["55"] = "OPT"
				entry["7633"] = "28.50"
				entry["7638"] = 1200
				entry["7308"] = "0.52"
			}
			entry["84"] = "5.00"
			entry["86"] = "5.20"
			entry["70"] = "755.00"
			entry["71"] = "745.00"
			entry["87"] = "123456"
		}
		out = append(out, entry)
	}
	writeJSON(w, 200, out)
}

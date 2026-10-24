package cmd

import (
	"net/http"
	"net/url"
	"strings"
)

// handleParity serves the endpoints added by the IBind parity
// extension. It runs with the stub mutex held (called from handle).
func (s *stubCPAPI) handleParity(w http.ResponseWriter, r *http.Request, path string, q url.Values) bool {
	acct := stubAccount
	switch {
	// --- session / accounts parity -------------------------------
	case path == "/logout" && r.Method == http.MethodPost:
		s.logoutCalls++
		writeJSON(w, 200, map[string]any{"msg": "success"})
	case path == "/iserver/account/"+acct+"/summary":
		writeJSON(w, 200, map[string]any{
			"netliquidation": map[string]any{"amount": 100000.55, "currency": "USD"},
		})
	case strings.HasPrefix(path, "/iserver/account/search/"):
		writeJSON(w, 200, []map[string]any{{"accountId": acct, "alias": "paper"}})
	case path == "/iserver/dynaccount" && r.Method == http.MethodPost:
		s.dynCalls++
		writeJSON(w, 200, map[string]any{"acctId": acct})
	case path == "/acesws/"+acct+"/signatures-and-owners":
		writeJSON(w, 200, []map[string]any{{"name": "Test Owner", "type": "owner"}})
	case path == "/iserver/account" && r.Method == http.MethodPost:
		s.switchCalls++
		writeJSON(w, 200, map[string]any{"acctId": acct})
	case path == "/iserver/questions/suppress" && r.Method == http.MethodPost:
		s.suppressCalls++
		writeJSON(w, 200, map[string]any{"messageIds": []string{"o35442"}})
	case path == "/iserver/questions/suppress/reset" && r.Method == http.MethodPost:
		s.suppressCalls++
		writeJSON(w, 200, map[string]any{})

	// --- portfolio parity -----------------------------------------
	case path == "/portfolio/subaccounts":
		writeJSON(w, 200, []map[string]any{{"id": acct}})
	case path == "/portfolio/subaccounts2":
		writeJSON(w, 200, map[string]any{"subAccounts": []map[string]any{{"id": acct}}})
	case path == "/portfolio/"+acct+"/meta":
		writeJSON(w, 200, map[string]any{"id": acct, "type": "INDIVIDUAL", "currency": "USD"})
	case path == "/portfolio/"+acct+"/allocation":
		writeJSON(w, 200, map[string]any{"assetClass": map[string]any{"long": map[string]any{"STK": 60.0}}})
	case path == "/portfolio/allocation":
		writeJSON(w, 200, []map[string]any{{"id": acct}})
	case path == "/portfolio/"+acct+"/combo/positions":
		writeJSON(w, 200, []map[string]any{{"conid": 9900001000, "position": 1.0}})
	case path == "/portfolio/"+acct+"/position/265598":
		writeJSON(w, 200, map[string]any{"conid": 265598, "position": 10.0})
	case path == "/portfolio/positions/265598":
		writeJSON(w, 200, map[string]any{"conid": 265598, "position": 10.0, "contractDesc": "AAPL"})
	case path == "/portfolio2/"+acct+"/positions":
		writeJSON(w, 200, []map[string]any{{"conid": 265598, "position": 10.0}})
	case path == "/portfolio/"+acct+"/positions/invalidate" && r.Method == http.MethodPost:
		s.invalidateCalls++
		writeJSON(w, 200, map[string]any{})
	case path == "/pa/performance" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"data": []map[string]any{{"id": acct, "returns": []float64{0.1, 0.2}}}})
	case path == "/pa/allperiods" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"data": []map[string]any{{"id": acct}}})
	case path == "/pa/transactions" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"transactions": []map[string]any{{"type": "DIVIDEND", "amt": 12.5}}})

	// --- contract parity ------------------------------------------
	case path == "/trsrv/secdef":
		writeJSON(w, 200, []map[string]any{{"conid": 265598, "symbol": "AAPL", "secType": "STK"}})
	case path == "/trsrv/all-conids":
		writeJSON(w, 200, map[string]any{"STK": []int64{265598}})
	case path == "/iserver/contract/265598/info":
		writeJSON(w, 200, map[string]any{"conid": 265598, "symbol": "AAPL", "secType": "STK"})
	case path == "/iserver/contract/265598/info-and-rules":
		writeJSON(w, 200, map[string]any{"info": map[string]any{"conid": 265598}, "rules": []map[string]any{{"rule": "minTick"}}})
	case path == "/iserver/contract/265598/algos":
		writeJSON(w, 200, []map[string]any{{"id": "Adaptive", "name": "Adaptive"}})
	case path == "/iserver/contract/rules" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"rules": []map[string]any{{"rule": "minTick", "value": 0.01}}})
	case path == "/iserver/currency/pairs":
		writeJSON(w, 200, []map[string]any{{"symbol": "EUR.USD", "conid": 12087792}})
	case path == "/iserver/exchangerate":
		writeJSON(w, 200, map[string]any{"rate": 0.92})
	case path == "/iserver/secdef/bond-filters":
		writeJSON(w, 200, []map[string]any{{"filter": "stateCode"}})
	case path == "/trsrv/futures":
		writeJSON(w, 200, map[string]any{"ES": []map[string]any{{"conid": 495512572, "symbol": "ESZ6"}}})
	case path == "/trsrv/secdef/schedule":
		writeJSON(w, 200, []map[string]any{{"tradingTimes": []string{"20261024:0930-1600"}}})

	// --- marketdata parity ----------------------------------------
	case path == "/md/regsnapshot":
		writeJSON(w, 200, map[string]any{"conid": 265598, "B": "749.90", "A": "750.30"})
	case path == "/hmds/history":
		writeJSON(w, 200, map[string]any{"data": []map[string]any{
			{"t": 1760000000000, "o": 740.0, "h": 755.0, "l": 738.0, "c": 750.25, "v": 1000.0},
		}})

	// --- scanner parity -------------------------------------------
	case path == "/iserver/scanner/params":
		writeJSON(w, 200, map[string]any{
			"instrument_list": []map[string]any{{"type": "STK"}},
			"scan_type_list":  []map[string]any{{"code": "MOST_ACTIVE"}},
			"location_tree":   []map[string]any{{"locationCode": "STK.US.MAJOR"}},
			"filter_list":     []map[string]any{{"code": "priceAbove"}},
		})
	case path == "/iserver/scanner/run" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"contracts": []map[string]any{{"conid": 265598, "symbol": "AAPL"}}})
	case path == "/hmds/scanner/params":
		writeJSON(w, 200, map[string]any{"instrument_list": []map[string]any{{"type": "STK"}}})
	case path == "/hmds/scanner/run" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"contracts": []map[string]any{{"conid": 265598}}})

	// --- watchlist parity -----------------------------------------
	case path == "/iserver/watchlists":
		writeJSON(w, 200, map[string]any{"data": map[string]any{"userWatchlists": []map[string]any{{"id": "12345", "name": "Tech"}}}})
	case path == "/iserver/watchlist" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]any{"id": "12345", "name": "Tech", "rows": []map[string]any{{"C": 265598}}})
	case path == "/iserver/watchlist" && r.Method == http.MethodPost:
		s.wlCreateCalls++
		writeJSON(w, 200, map[string]any{"id": "12345", "name": "Tech"})
	case path == "/iserver/watchlist" && r.Method == http.MethodDelete:
		s.wlDeleteCalls++
		writeJSON(w, 200, map[string]any{"msg": "deleted"})

	// --- FA parity -------------------------------------------------
	case path == "/fa/model/list" && r.Method == http.MethodPost:
		writeJSON(w, 200, []map[string]any{{"model": "M1", "desc": "test model"}})
	case path == "/fa/model/positions" && r.Method == http.MethodPost:
		writeJSON(w, 200, []map[string]any{{"conid": 265598, "allocation": 0.9}})
	case path == "/fa/model/summary" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"model": "M1", "aum": 1000.0})
	case path == "/fa/model/accounts-details" && r.Method == http.MethodPost:
		writeJSON(w, 200, []map[string]any{{"account": acct, "invested": true}})
	case path == "/fa/model/save" && r.Method == http.MethodPost:
		s.faSaveCalls++
		writeJSON(w, 200, map[string]any{"msg": "saved"})
	case path == "/fa/model/invest-divest" && r.Method == http.MethodPost:
		s.faInvestCalls++
		writeJSON(w, 200, map[string]any{"transfersInstructionId": 777})
	case path == "/fa/model/invest-divest-positions" && r.Method == http.MethodPost:
		writeJSON(w, 200, []map[string]any{{"conid": 265598}})
	case path == "/fa/model/submit-transfers" && r.Method == http.MethodPost:
		s.faSubmitCalls++
		writeJSON(w, 200, map[string]any{"msg": "submitted"})
	case path == "/fa/fa-preset/get" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"keepModelOpen": true})
	case path == "/fa/fa-preset/save" && r.Method == http.MethodPost:
		s.faPresetCalls++
		writeJSON(w, 200, map[string]any{"msg": "saved"})
	default:
		return false
	}
	return true
}

package ws

import (
	"encoding/json"
	"fmt"
)

// Subscription payload builders, byte-for-byte the formats IBind's
// ibkr_subscriptions.py produces (compact JSON, '+'-joined).

func compactJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// SMD subscribes market data for one conid (topic md).
func SMD(conid int64, fields []string) string {
	if fields == nil {
		fields = []string{}
	}
	return fmt.Sprintf("smd+%d+%s", conid, compactJSON(map[string]any{"fields": fields}))
}

// UMD unsubscribes market data.
func UMD(conid int64) string { return fmt.Sprintf("umd+%d+{}", conid) }

// SMH subscribes streaming history bars for one conid (topic mh).
// Empty optional fields are omitted, as IBind's filter_none does.
func SMH(conid int64, exchange, period, bar string, outsideRTH *bool, source, format string) string {
	data := map[string]any{}
	if exchange != "" {
		data["exchange"] = exchange
	}
	if period != "" {
		data["period"] = period
	}
	if bar != "" {
		data["bar"] = bar
	}
	if outsideRTH != nil {
		data["outside_rth"] = *outsideRTH
	}
	if source != "" {
		data["source"] = source
	}
	if format != "" {
		data["format"] = format
	}
	return fmt.Sprintf("smh+%d+%s", conid, compactJSON(data))
}

// UMH unsubscribes streaming history by the serverId the first mh
// message carried (IBind requires it; there is no conid form).
func UMH(serverID string) string { return "umh+" + serverID }

// SBD subscribes the BookTrader price ladder (topic bd).
func SBD(accountID string, conid int64, exchange string) string {
	s := fmt.Sprintf("sbd+%s+%d", accountID, conid)
	if exchange != "" {
		s += "+" + exchange
	}
	return s
}

// UBD unsubscribes the price ladder.
func UBD(accountID string) string { return "ubd+" + accountID }

func accountDataPayload(prefix, accountID string, keys, fields []string) string {
	data := map[string]any{}
	if len(keys) > 0 {
		data["keys"] = keys
	}
	if len(fields) > 0 {
		data["fields"] = fields
	}
	return fmt.Sprintf("%s+%s+%s", prefix, accountID, compactJSON(data))
}

// SSD subscribes account summary updates (topic sd).
func SSD(accountID string, keys, fields []string) string {
	return accountDataPayload("ssd", accountID, keys, fields)
}

// USD unsubscribes account summary.
func USD(accountID string) string { return "usd+" + accountID }

// SLD subscribes account ledger updates (topic ld).
func SLD(accountID string, keys, fields []string) string {
	return accountDataPayload("sld", accountID, keys, fields)
}

// ULD unsubscribes account ledger.
func ULD(accountID string) string { return "uld+" + accountID }

// SPL subscribes PnL updates (topic pl).
func SPL() string { return "spl" }

// UPL unsubscribes PnL.
func UPL() string { return "upl" }

// SOR subscribes live-order updates (topic or), optionally filtered
// to one order status.
func SOR(filter string) string {
	if filter == "" {
		return "sor+{}"
	}
	return "sor+" + compactJSON(map[string]any{"filters": []string{filter}})
}

// UOR unsubscribes order updates.
func UOR() string { return "uor+{}" }

// STR subscribes execution updates (topic tr).
func STR(realtimeOnly *bool, days *int) string {
	data := map[string]any{}
	if realtimeOnly != nil {
		data["realtime_updates_only"] = *realtimeOnly
	}
	if days != nil {
		data["days"] = *days
	}
	return "str+" + compactJSON(data)
}

// UTR unsubscribes execution updates (no '+' suffix, as in IBind).
func UTR() string { return "utr" }

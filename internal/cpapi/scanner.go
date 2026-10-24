package cpapi

import "context"

// ScannerParams returns the scanner's instrument/type/location/filter
// vocabulary. GET /iserver/scanner/params (IBind: scanner_parameters)
func (c *Client) ScannerParams(ctx context.Context) (*Response, error) {
	return c.Get(ctx, "/iserver/scanner/params", nil)
}

// ScannerFilter is one scanner filter object ({"code":..., "value":...}).
type ScannerFilter struct {
	Code  string `json:"code"`
	Value any    `json:"value"`
}

// ScannerRun runs a market scan (max 50 contracts per request).
// POST /iserver/scanner/run (IBind: market_scanner)
func (c *Client) ScannerRun(ctx context.Context, instrument, scanType, location string, filters []ScannerFilter) (*Response, error) {
	if filters == nil {
		filters = []ScannerFilter{}
	}
	body := map[string]any{
		"instrument": instrument,
		"type":       scanType,
		"location":   location,
		"filter":     filters,
	}
	return c.Post(ctx, "/iserver/scanner/run", body)
}

// HMDS Scanner vocabulary. GET /hmds/scanner/params
// (IBind: hmds_scanner_parameters)
func (c *Client) HMDSScannerParams(ctx context.Context) (*Response, error) {
	return c.Get(ctx, "/hmds/scanner/params", nil)
}

// HMDSScannerRun runs a historical-data-service scan.
// POST /hmds/scanner/run (IBind: hmds_market_scanner)
func (c *Client) HMDSScannerRun(ctx context.Context, instrument, location, scanCode, secType string, filters []ScannerFilter, maxItems int) (*Response, error) {
	body := map[string]any{
		"instrument": instrument,
		"location":   location,
		"scanCode":   scanCode,
		"secType":    secType,
		"filter":     filters,
	}
	if maxItems > 0 {
		body["maxItems"] = maxItems
	}
	return c.Post(ctx, "/hmds/scanner/run", body)
}

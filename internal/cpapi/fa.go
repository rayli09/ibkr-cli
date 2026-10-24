package cpapi

import (
	"context"
	"encoding/json"
)

// This file covers IBind's fa_mixin (financial-advisor model
// portfolios and presets). Every call is a POST carrying a reqID;
// read calls pass through, mutating calls (save, invest/divest,
// submit-transfers, preset save) are gated behind --confirm by the
// command layer.

// FAModelList lists model portfolios. POST /fa/model/list
// (IBind: fa_model_list)
func (c *Client) FAModelList(ctx context.Context, reqID int) (*Response, error) {
	return c.Post(ctx, "/fa/model/list", map[string]any{"reqID": reqID})
}

// FAModelPositions returns one model's target positions. Allocation
// values come back as fractions in [0, 1] (observed live by IBind).
// POST /fa/model/positions (IBind: fa_model_positions)
func (c *Client) FAModelPositions(ctx context.Context, reqID int, model, sortField, sortDirection string, limit int) (*Response, error) {
	body := map[string]any{"reqID": reqID, "model": model}
	if sortField != "" {
		body["sortField"] = sortField
	}
	if sortDirection != "" {
		body["sortDirection"] = sortDirection
	}
	if limit > 0 {
		body["limit"] = limit
	}
	return c.Post(ctx, "/fa/model/positions", body)
}

// FAModelSummary returns one model's summary.
// POST /fa/model/summary (IBind: fa_model_summary)
func (c *Client) FAModelSummary(ctx context.Context, reqID int, model string) (*Response, error) {
	return c.Post(ctx, "/fa/model/summary", map[string]any{"reqID": reqID, "model": model})
}

// FAModelAccounts returns the accounts invested in one model.
// POST /fa/model/accounts-details (IBind: fa_model_accounts_details)
func (c *Client) FAModelAccounts(ctx context.Context, reqID int, model string, calcPnLs bool) (*Response, error) {
	body := map[string]any{"reqID": reqID, "model": model}
	if calcPnLs {
		body["calcPnLs"] = true
	}
	return c.Post(ctx, "/fa/model/accounts-details", body)
}

// FAModelSave creates or modifies a model's targets. Targets are
// fractions in [0, 1] summing to 1.0 across cash and positions
// (IBind's live observation; IBKR's schema text says percentages
// but 0–100 values are rejected). POST /fa/model/save
// (IBind: fa_model_save). Mutating: gated behind --confirm.
func (c *Client) FAModelSave(ctx context.Context, reqID int, model, desc string, isStatic bool, cashTargets, positionTargets json.RawMessage) (*Response, error) {
	body := map[string]any{
		"reqID":    reqID,
		"model":    model,
		"desc":     desc,
		"isStatic": isStatic,
	}
	if len(cashTargets) > 0 {
		body["cashTargets"] = cashTargets
	}
	if len(positionTargets) > 0 {
		body["positionTargets"] = positionTargets
	}
	return c.Post(ctx, "/fa/model/save", body)
}

// FAInvestDivest drafts invest/divest transfers into a model; the
// response's transfersInstructionId is the fpOrderId later passed to
// FASubmitTransfers. An account with amtToInvest 0 divests it fully.
// POST /fa/model/invest-divest (IBind: fa_model_invest_divest).
// Mutating: gated behind --confirm.
func (c *Client) FAInvestDivest(ctx context.Context, reqID int, model string, accountList json.RawMessage) (*Response, error) {
	return c.Post(ctx, "/fa/model/invest-divest", map[string]any{
		"reqID": reqID, "model": model, "accountList": accountList,
	})
}

// FAInvestedPositions returns positions created by invest/divest.
// POST /fa/model/invest-divest-positions
// (IBind: fa_model_invest_divest_positions)
func (c *Client) FAInvestedPositions(ctx context.Context, reqID int, model, subscriptionStatus string) (*Response, error) {
	body := map[string]any{"reqID": reqID, "model": model}
	if subscriptionStatus != "" {
		body["subscriptionStatus"] = subscriptionStatus
	}
	return c.Post(ctx, "/fa/model/invest-divest-positions", body)
}

// FASubmitTransfers executes a drafted transfer instruction.
// POST /fa/model/submit-transfers (IBind: fa_model_submit_transfers).
// Mutating: gated behind --confirm.
func (c *Client) FASubmitTransfers(ctx context.Context, reqID int, fpOrderID int64) (*Response, error) {
	return c.Post(ctx, "/fa/model/submit-transfers", map[string]any{
		"reqID": reqID, "fpOrderId": fpOrderID,
	})
}

// FAPresetGet returns the FA preset flags.
// POST /fa/fa-preset/get (IBind: fa_preset_get)
func (c *Client) FAPresetGet(ctx context.Context, reqID int) (*Response, error) {
	return c.Post(ctx, "/fa/fa-preset/get", map[string]any{"reqID": reqID})
}

// FAPresetSave saves FA preset flags; only the flags present in the
// map are sent. POST /fa/fa-preset/save (IBind: fa_preset_save).
// Mutating: gated behind --confirm.
func (c *Client) FAPresetSave(ctx context.Context, reqID int, flags map[string]bool) (*Response, error) {
	body := map[string]any{"reqID": reqID}
	for k, v := range flags {
		body[k] = v
	}
	return c.Post(ctx, "/fa/fa-preset/save", body)
}

package cpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/rayli09/ibkr-cli/internal/apierr"
)

// OrderRequest is the CPAPI order object. The same shape is used for
// whatif, place, and modify. Fields follow the official order
// reference; unknown extra fields can be merged in by the caller via
// Extra.
type OrderRequest struct {
	Conid      int64   `json:"conid"`
	SecType    string  `json:"secType,omitempty"`
	OrderType  string  `json:"orderType"` // MKT, LMT, STP, STP_LMT, ...
	Side       string  `json:"side"`      // BUY or SELL
	Quantity   float64 `json:"quantity"`
	Price      float64 `json:"price,omitempty"`    // limit price
	AuxPrice   float64 `json:"auxPrice,omitempty"` // stop price
	TIF        string  `json:"tif,omitempty"`      // DAY, GTC, IOC, ...
	OutsideRTH bool    `json:"outsideRTH,omitempty"`
	COID       string  `json:"cOID,omitempty"` // client order id
	AcctID     string  `json:"acctId,omitempty"`

	Extra map[string]any `json:"-"`
}

// MarshalJSON merges Extra over the typed fields.
func (o OrderRequest) MarshalJSON() ([]byte, error) {
	type alias OrderRequest
	data, err := json.Marshal(alias(o))
	if err != nil {
		return nil, err
	}
	if len(o.Extra) == 0 {
		return data, nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for k, v := range o.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// OrdersBody wraps order requests the way the orders endpoints
// expect: {"orders": [ ... ]}.
func OrdersBody(reqs ...OrderRequest) map[string]any {
	list := make([]any, len(reqs))
	for i, r := range reqs {
		list[i] = r
	}
	return map[string]any{"orders": list}
}

// LiveOrders returns the session's working orders.
// GET /iserver/account/orders (IBind: live_orders — filters are
// comma-joined status values; force clears the server's cached
// filter state, which otherwise also suppresses the WS "sor" topic.)
func (c *Client) LiveOrders(ctx context.Context, accountID string, filters []string, force bool) (*Response, error) {
	q := url.Values{}
	setParam(q, "accountId", accountID)
	if len(filters) > 0 {
		q.Set("filters", strings.Join(filters, ","))
	}
	if force {
		q.Set("force", "true")
	}
	return c.Get(ctx, "/iserver/account/orders", q)
}

// OrderStatus returns one order's status.
// GET /iserver/account/order/status/{orderId}
func (c *Client) OrderStatus(ctx context.Context, orderID string) (*Response, error) {
	return c.Get(ctx, "/iserver/account/order/status/"+url.PathEscape(orderID), nil)
}

// Trades returns executions for the session (optionally N days back,
// optionally for one account). GET /iserver/account/trades
func (c *Client) Trades(ctx context.Context, days, accountID string) (*Response, error) {
	q := url.Values{}
	setParam(q, "days", days)
	setParam(q, "accountId", accountID)
	return c.Get(ctx, "/iserver/account/trades", q)
}

// SuppressMessages suppresses order-confirmation messages by id for
// the session. POST /iserver/questions/suppress (IBind:
// suppress_messages). Mutating: the CLI gates it behind --confirm.
func (c *Client) SuppressMessages(ctx context.Context, messageIDs []string) (*Response, error) {
	return c.Post(ctx, "/iserver/questions/suppress", map[string]any{"messageIds": messageIDs})
}

// ResetSuppressedMessages clears all suppressed message ids.
// POST /iserver/questions/suppress/reset (IBind:
// reset_suppressed_messages). Mutating: gated behind --confirm.
func (c *Client) ResetSuppressedMessages(ctx context.Context) (*Response, error) {
	return c.Post(ctx, "/iserver/questions/suppress/reset", nil)
}

// WhatIf previews an order: margin and commission impact, no order
// placed. POST /iserver/account/{accountId}/orders/whatif
// (Note: the official path is orders/whatif; the SPEC table's
// order/whatif was corrected at build time against IBind and the
// official reference.)
func (c *Client) WhatIf(ctx context.Context, accountID string, req OrderRequest) (*Response, error) {
	path := "/iserver/account/" + url.PathEscape(accountID) + "/orders/whatif"
	return c.Post(ctx, path, OrdersBody(req))
}

// PlaceOrder submits an order. POST /iserver/account/{accountId}/orders
// The response is parsed by ParseOrderOutcome: it may be a
// QuestionPending error instead of a placed order (SPEC §5.1.3).
func (c *Client) PlaceOrder(ctx context.Context, accountID string, req OrderRequest) (*OrderOutcome, *Response, error) {
	path := "/iserver/account/" + url.PathEscape(accountID) + "/orders"
	resp, err := c.Post(ctx, path, OrdersBody(req))
	if err != nil {
		return nil, resp, err
	}
	out, err := ParseOrderOutcome(resp.Body)
	return out, resp, err
}

// Reply answers exactly one pending order question.
// POST /iserver/reply/{replyId} with {"confirmed": bool}. The
// response is again an order outcome or the next question.
func (c *Client) Reply(ctx context.Context, replyID string, confirmed bool) (*OrderOutcome, *Response, error) {
	path := "/iserver/reply/" + url.PathEscape(replyID)
	resp, err := c.Post(ctx, path, map[string]any{"confirmed": confirmed})
	if err != nil {
		return nil, resp, err
	}
	out, err := ParseOrderOutcome(resp.Body)
	return out, resp, err
}

// ModifyOrder replaces a working order. Same gating as place
// (SPEC §5): POST /iserver/account/{accountId}/order/{orderId}
func (c *Client) ModifyOrder(ctx context.Context, accountID, orderID string, req OrderRequest) (*OrderOutcome, *Response, error) {
	path := "/iserver/account/" + url.PathEscape(accountID) + "/order/" + url.PathEscape(orderID)
	resp, err := c.Post(ctx, path, req)
	if err != nil {
		return nil, resp, err
	}
	out, err := ParseOrderOutcome(resp.Body)
	return out, resp, err
}

// CancelOrder cancels a working order.
// DELETE /iserver/account/{accountId}/order/{orderId}
func (c *Client) CancelOrder(ctx context.Context, accountID, orderID string) (*Response, error) {
	path := "/iserver/account/" + url.PathEscape(accountID) + "/order/" + url.PathEscape(orderID)
	return c.Delete(ctx, path)
}

// OrderOutcome is the terminal state of a place/reply chain: an
// order id and its status.
type OrderOutcome struct {
	OrderID string
	Status  string
	Raw     json.RawMessage
}

// orderElement is one element of the array the orders endpoints
// return: either a question ({id, message:[...]}), a placed order
// ({order_id, order_status}), or an error ({error}).
type orderElement struct {
	ID          string          `json:"id"`
	Message     []string        `json:"message"`
	OrderID     json.RawMessage `json:"order_id"`
	OrderStatus string          `json:"order_status"`
	Error       string          `json:"error"`
	Success     bool            `json:"success"`
}

// ParseOrderOutcome interprets a place/reply/modify response body.
// Precedence: an explicit error wins; a question element yields
// *apierr.QuestionPending carrying the verbatim question; otherwise
// the placed order is returned. A body that is a bare object (not an
// array) is treated as a one-element array.
func ParseOrderOutcome(body []byte) (*OrderOutcome, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, &apierr.APIError{Msg: "empty order response"}
	}
	var elems []orderElement
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(body, &elems); err != nil {
			return nil, &apierr.APIError{Msg: "unparseable order response: " + trimmed}
		}
	} else {
		var one orderElement
		if err := json.Unmarshal(body, &one); err != nil {
			return nil, &apierr.APIError{Msg: "unparseable order response: " + trimmed}
		}
		elems = []orderElement{one}
	}
	var questions []apierr.Question
	var outcome *OrderOutcome
	for _, el := range elems {
		if el.Error != "" {
			return nil, &apierr.APIError{Msg: "order error: " + el.Error}
		}
		if len(el.Message) > 0 && el.ID != "" {
			questions = append(questions, apierr.Question{ID: el.ID, Messages: el.Message})
			continue
		}
		if len(el.OrderID) > 0 {
			var idNum int64
			var idStr string
			if err := json.Unmarshal(el.OrderID, &idNum); err == nil {
				outcome = &OrderOutcome{OrderID: fmt.Sprint(idNum), Status: el.OrderStatus, Raw: body}
			} else if err := json.Unmarshal(el.OrderID, &idStr); err == nil {
				outcome = &OrderOutcome{OrderID: idStr, Status: el.OrderStatus, Raw: body}
			}
		}
	}
	if len(questions) > 0 {
		return nil, &apierr.QuestionPending{Questions: questions}
	}
	if outcome != nil {
		return outcome, nil
	}
	return nil, &apierr.APIError{Msg: "order response carried no order id: " + trimmed}
}

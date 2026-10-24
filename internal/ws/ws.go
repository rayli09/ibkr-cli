// Package ws is a bounded WebSocket client for the CPAPI streaming
// endpoint, porting IBind's IbkrWsClient handshake and subscription
// payloads. The CLI's streams are deliberately bounded (a duration
// and/or an event cap); this is not a daemon framework.
//
// Handshake (from IBind's ibkr_ws_client.py):
//   - URL: {base}/ws where base is the REST base with http→ws /
//     https→wss (gateway https://localhost:5000/v1/api →
//     wss://localhost:5000/v1/api/ws).
//   - The session cookie comes from a REST tickle: gateway mode
//     sends Cookie `api={"session": "<tickle session>"}` (JSON),
//     OAuth mode sends `api=<tickle session>` (plain).
//   - OAuth mode additionally passes ?oauth_token=<access token> and
//     header User-Agent: ClientPortalGW/1.
//   - Subscriptions are plain-text messages (smd+..., sor+..., ...);
//     incoming messages are JSON objects with a "topic" field.
package ws

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// DialOpts configures one WS connection.
type DialOpts struct {
	// BaseURL is the REST base URL of the active provider.
	BaseURL string
	// Cookie is the full Cookie header value (api=...).
	Cookie string
	// OAuthToken, when non-empty, is appended as ?oauth_token= and
	// selects the OAuth-mode handshake (plain cookie + UA header).
	OAuthToken string
	// TLSConfig is used for wss dials (gateway loopback self-signed).
	TLSConfig *tls.Config
}

// WSURL derives the streaming URL from a REST base URL.
func WSURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("cannot derive websocket URL from base %q", base)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/ws"
	return u.String(), nil
}

// GatewayCookie builds the gateway-mode session cookie from a tickle
// session id: api={"session": "<id>"} (IBind's format).
func GatewayCookie(session string) string {
	b, _ := json.Marshal(map[string]string{"session": session})
	return "api=" + string(b)
}

// OAuthCookie builds the OAuth-mode session cookie: api=<id> (plain).
func OAuthCookie(session string) string {
	return "api=" + session
}

// Conn is one live CPAPI WebSocket connection.
type Conn struct {
	ws *websocket.Conn
}

// Dial opens the connection.
func Dial(ctx context.Context, opts DialOpts) (*Conn, error) {
	raw, err := WSURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	hdr := http.Header{}
	if opts.Cookie != "" {
		hdr.Set("Cookie", opts.Cookie)
	}
	if opts.OAuthToken != "" {
		hdr.Set("User-Agent", "ClientPortalGW/1")
		sep := "?"
		if strings.Contains(raw, "?") {
			sep = "&"
		}
		raw += sep + "oauth_token=" + url.QueryEscape(opts.OAuthToken)
	}
	d := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		TLSClientConfig:  opts.TLSConfig,
	}
	c, _, err := d.DialContext(ctx, raw, hdr)
	if err != nil {
		return nil, fmt.Errorf("websocket dial %s: %w", raw, err)
	}
	return &Conn{ws: c}, nil
}

// Send writes one text message (a subscribe/unsubscribe payload).
func (c *Conn) Send(payload string) error {
	return c.ws.WriteMessage(websocket.TextMessage, []byte(payload))
}

// Close sends a protocol close and closes the socket.
func (c *Conn) Close() {
	_ = c.ws.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = c.ws.Close()
}

// TopicOf extracts the "topic" field of an incoming message (""
// when absent/unparseable).
func TopicOf(msg []byte) string {
	var head struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(msg, &head); err != nil {
		return ""
	}
	return head.Topic
}

// Run subscribes, then relays messages to emit until the context
// ends (duration) or maxEvents counted events have been seen (<=0
// means no cap). Heartbeat messages (topic "tic") are emitted but
// not counted. On exit it calls unsubscribe (a function because the
// market-history unsubscribe needs the serverId that only arrives
// inside the stream) and sends the payloads best-effort before
// closing. Returns the counted event total.
func Run(ctx context.Context, c *Conn, subscribe []string, unsubscribe func() []string, maxEvents int, emit func([]byte)) (int, error) {
	for _, p := range subscribe {
		if err := c.Send(p); err != nil {
			return 0, fmt.Errorf("subscribe %q: %w", p, err)
		}
	}
	defer func() {
		if unsubscribe != nil {
			for _, p := range unsubscribe() {
				_ = c.Send(p)
			}
		}
		c.Close()
	}()

	type result struct {
		msg []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		for {
			_, msg, err := c.ws.ReadMessage()
			ch <- result{msg, err}
			if err != nil {
				return
			}
		}
	}()

	count := 0
	for {
		select {
		case <-ctx.Done():
			return count, nil
		case r := <-ch:
			if r.err != nil {
				return count, fmt.Errorf("websocket read: %w", r.err)
			}
			emit(r.msg)
			if TopicOf(r.msg) != "tic" {
				count++
				if maxEvents > 0 && count >= maxEvents {
					return count, nil
				}
			}
		}
	}
}

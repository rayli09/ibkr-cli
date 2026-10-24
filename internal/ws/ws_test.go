package ws

import "testing"

// Payload formats are byte-compared against IBind's
// ibkr_subscriptions.py output for the same inputs.
func TestPayloadFormats(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{SMD(265598, []string{"31", "84", "86"}), `smd+265598+{"fields":["31","84","86"]}`},
		{UMD(265598), `umd+265598+{}`},
		{SMH(265598, "", "1d", "1min", nil, "", ""), `smh+265598+{"bar":"1min","period":"1d"}`},
		{UMH("srv123"), `umh+srv123`},
		{SBD("DU1", 265598, ""), `sbd+DU1+265598`},
		{SBD("DU1", 265598, "NASDAQ"), `sbd+DU1+265598+NASDAQ`},
		{UBD("DU1"), `ubd+DU1`},
		{SSD("DU1", nil, nil), `ssd+DU1+{}`},
		{SSD("DU1", []string{"NetLiquidation"}, nil), `ssd+DU1+{"keys":["NetLiquidation"]}`},
		{USD("DU1"), `usd+DU1`},
		{SLD("DU1", nil, nil), `sld+DU1+{}`},
		{ULD("DU1"), `uld+DU1`},
		{SPL(), `spl`},
		{UPL(), `upl`},
		{SOR(""), `sor+{}`},
		{SOR("filled"), `sor+{"filters":["filled"]}`},
		{UOR(), `uor+{}`},
		{STR(nil, nil), `str+{}`},
		{UTR(), `utr`},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("payload = %q, want %q", c.got, c.want)
		}
	}
	tru, days := true, 7
	if got := STR(&tru, &days); got != `str+{"days":7,"realtime_updates_only":true}` {
		t.Errorf("STR = %q", got)
	}
}

func TestWSURL(t *testing.T) {
	cases := map[string]string{
		"https://localhost:5000/v1/api": "wss://localhost:5000/v1/api/ws",
		"http://127.0.0.1:8080/v1/api/": "ws://127.0.0.1:8080/v1/api/ws",
		"https://api.ibkr.com/v1/api":   "wss://api.ibkr.com/v1/api/ws",
	}
	for in, want := range cases {
		got, err := WSURL(in)
		if err != nil || got != want {
			t.Errorf("WSURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestCookies(t *testing.T) {
	if got := GatewayCookie("abc"); got != `api={"session":"abc"}` {
		t.Errorf("gateway cookie = %q", got)
	}
	if got := OAuthCookie("abc"); got != `api=abc` {
		t.Errorf("oauth cookie = %q", got)
	}
}

func TestTopicOf(t *testing.T) {
	if got := TopicOf([]byte(`{"topic":"md","conid":1}`)); got != "md" {
		t.Errorf("TopicOf = %q", got)
	}
	if got := TopicOf([]byte(`not json`)); got != "" {
		t.Errorf("TopicOf(bad) = %q", got)
	}
}

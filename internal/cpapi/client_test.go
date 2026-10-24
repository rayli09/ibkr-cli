package cpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayli09/ibkr-cli/internal/apierr"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// Request construction: method, path, query encoding, JSON body.
func TestRequestConstruction(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotBody string
	c := New("https://example.test/v1/api", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotQuery = r.URL.RawQuery
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
		}
		return fakeResponse(200, `[]`), nil
	}))
	_, _, err := c.SecdefSearch(context.Background(), "AAPL", false, "STK")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/api/iserver/secdef/search" {
		t.Fatalf("request line = %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotQuery, "symbol=AAPL") || !strings.Contains(gotQuery, "secType=STK") {
		t.Fatalf("query = %s", gotQuery)
	}
	resp, err := c.Post(context.Background(), "/iserver/reply/abc", map[string]any{"confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.Contains(gotBody, `"confirmed":true`) {
		t.Fatalf("post body = %s", gotBody)
	}
}

// Error typing: 401/403 -> SessionError (exit 3); 500 -> APIError (exit 4).
func TestErrorTyping(t *testing.T) {
	for status, wantExit := range map[int]int{401: 3, 403: 3, 500: 4, 400: 4} {
		c := New("https://example.test/v1/api", roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return fakeResponse(status, `{"error":"boom"}`), nil
		}))
		_, err := c.Get(context.Background(), "/portfolio/accounts", nil)
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if got := apierr.ExitCode(err); got != wantExit {
			t.Fatalf("status %d: exit = %d, want %d (err %v)", status, got, wantExit, err)
		}
	}
}

// 429 handling: retries (with injected no-op sleep) succeed; a
// persistent 429 yields a RateLimitError.
func Test429RetryThenSuccessAndExhaustion(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tickle") {
			w.WriteHeader(200)
			return
		}
		n := hits.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1/api", nil)
	c.Sleep = func(time.Duration) {}
	resp, err := c.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate after retries: %v", err)
	}
	if resp.StatusCode != 200 || hits.Load() != 3 {
		t.Fatalf("hits = %d, want 3", hits.Load())
	}

	hits.Store(0)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tickle") {
			w.WriteHeader(200)
			return
		}
		hits.Add(1)
		w.WriteHeader(429)
	}))
	defer srv2.Close()
	c2 := New(srv2.URL+"/v1/api", nil)
	c2.Sleep = func(time.Duration) {}
	_, err = c2.Validate(context.Background())
	var rl *apierr.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("persistent 429: err = %v, want RateLimitError", err)
	}
	if apierr.ExitCode(err) != 4 {
		t.Fatalf("persistent 429 exit = %d, want 4", apierr.ExitCode(err))
	}
	if hits.Load() != 3 {
		t.Fatalf("persistent 429 hits = %d, want 3 attempts", hits.Load())
	}
}

// Rate limiter pacing: with a fake clock, the second call on a
// limited route sleeps exactly the remaining interval.
func TestRateLimiterPacing(t *testing.T) {
	rl := newRateLimiter()
	base := time.Unix(1_700_000_000, 0)
	now := base
	var slept []time.Duration
	rl.now = func() time.Time { return now }
	rl.sleep = func(d time.Duration) { slept = append(slept, d) }

	ctx := context.Background()
	release, err := rl.acquire(ctx, "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	release()
	now = base.Add(30 * time.Millisecond)
	release, err = rl.acquire(ctx, "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if len(slept) != 1 || slept[0] != 70*time.Millisecond {
		t.Fatalf("slept = %v, want [70ms]", slept)
	}
	// Unlimited routes never sleep.
	release, _ = rl.acquire(ctx, "default")
	release()
	release, _ = rl.acquire(ctx, "default")
	release()
	if len(slept) != 1 {
		t.Fatalf("default route slept: %v", slept)
	}
}

// History concurrency cap: the 6th concurrent acquire blocks until
// a slot is released.
func TestHistoryConcurrencyCap(t *testing.T) {
	rl := newRateLimiter()
	ctx := context.Background()
	releases := make([]func(), 0, historyConcurrency)
	for i := 0; i < historyConcurrency; i++ {
		rel, err := rl.acquire(ctx, "history")
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, rel)
	}
	acquired := make(chan func(), 1)
	go func() {
		rel, err := rl.acquire(ctx, "history")
		if err == nil {
			acquired <- rel
		}
	}()
	select {
	case <-acquired:
		t.Fatal("6th history acquire should block at the cap")
	case <-time.After(100 * time.Millisecond):
	}
	releases[0]()
	select {
	case rel := <-acquired:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("6th history acquire should proceed after a release")
	}
	for _, rel := range releases[1:] {
		rel()
	}
}

// Snapshot priming (SPEC §7.1): first response carries no market
// data -> the client re-requests once and returns filled entries.
func TestSnapshotPriming(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tickle") {
			w.WriteHeader(200)
			return
		}
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = w.Write([]byte(`[{"conid":265598},{"conid":265599}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"conid":265598,"31":"100.5","84":"100.4","86":"100.6"},` +
			`{"conid":265599,"31":"20.25","84":"20.2","86":"20.3"}]`))
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1/api", nil)
	c.Sleep = func(time.Duration) {}
	c.PrimeDelay = 0
	entries, missing, err := c.Snapshot(context.Background(),
		[]int64{265598, 265599}, []string{"31", "84", "86"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none after priming", missing)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if v, ok := entries[0].Float(FieldLast); !ok || v != 100.5 {
		t.Fatalf("last = %v %v", v, ok)
	}
	if calls.Load() != 2 {
		t.Fatalf("snapshot calls = %d, want 2 (prime + fill)", calls.Load())
	}
}

// A conid that never fills is reported as missing, not silently
// returned empty.
func TestSnapshotStillMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tickle") {
			w.WriteHeader(200)
			return
		}
		_, _ = w.Write([]byte(`[{"conid":42}]`))
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1/api", nil)
	c.Sleep = func(time.Duration) {}
	c.PrimeDelay = 0
	_, missing, err := c.Snapshot(context.Background(), []int64{42}, []string{"31"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != 42 {
		t.Fatalf("missing = %v, want [42]", missing)
	}
}

// Order-outcome parsing (SPEC §5.1.3).
func TestParseOrderOutcome(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantID   string
		wantExit int // 0 = outcome, else apierr exit code
	}{
		{"placed array", `[{"order_id":123,"order_status":"Submitted"}]`, "123", 0},
		{"placed string id", `[{"order_id":"DU-9","order_status":"Filled"}]`, "DU-9", 0},
		{"question", `[{"id":"m1","message":["Confirm?","Really?"]}]`, "", 5},
		{"error element", `[{"error":"bad order"}]`, "", 4},
		{"bare object outcome", `{"order_id":7,"order_status":"Submitted"}`, "7", 0},
		{"garbage", `not json`, "", 4},
		{"empty", ``, "", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ParseOrderOutcome([]byte(tc.body))
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if out.OrderID != tc.wantID {
					t.Fatalf("order id = %q, want %q", out.OrderID, tc.wantID)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error, got outcome %+v", out)
			}
			if got := apierr.ExitCode(err); got != tc.wantExit {
				t.Fatalf("exit = %d, want %d (err %v)", got, tc.wantExit, err)
			}
			if tc.wantExit == 5 {
				var qp *apierr.QuestionPending
				if !errors.As(err, &qp) || len(qp.Questions) != 1 ||
					qp.Questions[0].ID != "m1" || len(qp.Questions[0].Messages) != 2 {
					t.Fatalf("question payload wrong: %+v", err)
				}
			}
		})
	}
}

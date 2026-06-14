package huobi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.Rate = 0
	c.HTTP = &http.Client{Timeout: 5 * time.Second}
	_ = srv
	return c
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGet404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Error("expected error on 404, got nil")
	}
}

func TestGetTickerDecoding(t *testing.T) {
	payload := map[string]interface{}{
		"status": "ok",
		"ch":     "market.btcusdt.detail.merged",
		"ts":     1781452800000,
		"tick": map[string]interface{}{
			"open":   64024.6,
			"high":   64214.8,
			"low":    63683.9,
			"close":  63818.82,
			"amount": 5289.32,
			"vol":    62165450.47,
			"count":  123456,
			"bid":    []float64{63800, 0.5},
			"ask":    []float64{63801, 0.3},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/market/detail/merged" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("symbol") != "btcusdt" {
			t.Errorf("unexpected symbol: %s", r.URL.Query().Get("symbol"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.Get(context.Background(), srv.URL+"/market/detail/merged?symbol=btcusdt")
	if err != nil {
		t.Fatal(err)
	}
	var resp wireTickerResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" {
		t.Errorf("Status = %q, want ok", resp.Status)
	}
	if resp.Tick.Close != 63818.82 {
		t.Errorf("Close = %v, want 63818.82", resp.Tick.Close)
	}
	if len(resp.Tick.Bid) == 0 || resp.Tick.Bid[0] != 63800 {
		t.Errorf("Bid[0] = %v, want 63800", resp.Tick.Bid)
	}
	if len(resp.Tick.Ask) == 0 || resp.Tick.Ask[0] != 63801 {
		t.Errorf("Ask[0] = %v, want 63801", resp.Tick.Ask)
	}
}

func TestGetKlineDecoding(t *testing.T) {
	payload := map[string]interface{}{
		"status": "ok",
		"data": []map[string]interface{}{
			{
				"id":     1781395200,
				"open":   64450.4,
				"high":   64711.5,
				"low":    63685.5,
				"close":  63818.82,
				"amount": 5000.0,
				"vol":    320000000.0,
				"count":  98765,
			},
			{
				"id":     1781308800,
				"open":   63900.0,
				"high":   64500.0,
				"low":    63600.0,
				"close":  64450.4,
				"amount": 4800.0,
				"vol":    305000000.0,
				"count":  95000,
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/market/history/kline" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.Get(context.Background(), srv.URL+"/market/history/kline?symbol=btcusdt&period=1day&size=2")
	if err != nil {
		t.Fatal(err)
	}
	var resp wireKlineResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" {
		t.Errorf("Status = %q, want ok", resp.Status)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(Data) = %d, want 2", len(resp.Data))
	}
	if resp.Data[0].ID != 1781395200 {
		t.Errorf("Data[0].ID = %d, want 1781395200", resp.Data[0].ID)
	}
	if resp.Data[0].Open != 64450.4 {
		t.Errorf("Data[0].Open = %v, want 64450.4", resp.Data[0].Open)
	}
}

func TestGetSymbolsDecoding(t *testing.T) {
	payload := map[string]interface{}{
		"status": "ok",
		"data": []map[string]interface{}{
			{
				"base-currency":    "btc",
				"quote-currency":   "usdt",
				"symbol":           "btcusdt",
				"state":            "online",
				"price-precision":  2,
				"amount-precision": 6,
			},
			{
				"base-currency":    "eth",
				"quote-currency":   "usdt",
				"symbol":           "ethusdt",
				"state":            "online",
				"price-precision":  2,
				"amount-precision": 4,
			},
			{
				"base-currency":    "ltc",
				"quote-currency":   "usdt",
				"symbol":           "ltcusdt",
				"state":            "offline",
				"price-precision":  2,
				"amount-precision": 4,
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/common/symbols" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.Get(context.Background(), srv.URL+"/v1/common/symbols")
	if err != nil {
		t.Fatal(err)
	}
	var resp wireSymbolsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" {
		t.Errorf("Status = %q, want ok", resp.Status)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("len(Data) = %d, want 3", len(resp.Data))
	}
	if resp.Data[0].Symbol != "btcusdt" {
		t.Errorf("Data[0].Symbol = %q, want btcusdt", resp.Data[0].Symbol)
	}
	if resp.Data[2].State != "offline" {
		t.Errorf("Data[2].State = %q, want offline", resp.Data[2].State)
	}
}

func TestNormalizeSymbol(t *testing.T) {
	cases := []struct{ in, want string }{
		{"BTCUSDT", "btcusdt"},
		{"btcusdt", "btcusdt"},
		{"  ETHUSDT  ", "ethusdt"},
	}
	for _, tc := range cases {
		got := normalizeSymbol(tc.in)
		if got != tc.want {
			t.Errorf("normalizeSymbol(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTickerStruct(t *testing.T) {
	tk := &Ticker{
		Symbol: "btcusdt",
		Last:   63818.82,
		Bid:    63800,
		Ask:    63801,
		Open:   64024.6,
		High:   64214.8,
		Low:    63683.9,
		Vol:    62165450.47,
	}
	if tk.Symbol != "btcusdt" {
		t.Errorf("Symbol = %q, want btcusdt", tk.Symbol)
	}
	if tk.Last != 63818.82 {
		t.Errorf("Last = %v, want 63818.82", tk.Last)
	}
}

func TestCandleStruct(t *testing.T) {
	c := &Candle{
		ID:    1781395200,
		Open:  64450.4,
		High:  64711.5,
		Low:   63685.5,
		Close: 63818.82,
		Vol:   320000000.0,
	}
	if c.ID != 1781395200 {
		t.Errorf("ID = %d, want 1781395200", c.ID)
	}
	if c.Vol != 320000000.0 {
		t.Errorf("Vol = %v, want 320000000.0", c.Vol)
	}
}

func TestSymbolStruct(t *testing.T) {
	s := &Symbol{
		Symbol:         "btcusdt",
		Base:           "btc",
		Quote:          "usdt",
		State:          "online",
		PricePrecision: 2,
	}
	if s.Symbol != "btcusdt" {
		t.Errorf("Symbol = %q, want btcusdt", s.Symbol)
	}
	if s.PricePrecision != 2 {
		t.Errorf("PricePrecision = %d, want 2", s.PricePrecision)
	}
}

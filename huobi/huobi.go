// Package huobi is the library behind the huobi command line:
// the HTTP client, request shaping, and the typed data models for Huobi (HTX)
// public market data.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package huobi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to Huobi.
const DefaultUserAgent = "huobi-cli/0.1 (tamnd87@gmail.com)"

// Host is the API host this client talks to.
const Host = "api.huobi.pro"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// Client talks to the Huobi public API over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 15s timeout, a 200ms
// minimum gap between requests, and three retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   3,
	}
}

// Get fetches rawURL and returns the response body. It paces and retries
// according to the client's settings.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// normalizeSymbol lowercases a symbol for the Huobi API (requires lowercase).
func normalizeSymbol(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// --- data models ---

// Ticker holds current price and 24-hour stats for a symbol.
type Ticker struct {
	Symbol string  `kit:"id" json:"symbol"`
	Last   float64 `json:"last"` // close price
	Bid    float64 `json:"bid"`  // best bid price
	Ask    float64 `json:"ask"`  // best ask price
	Open   float64 `json:"open_24h"`
	High   float64 `json:"high_24h"`
	Low    float64 `json:"low_24h"`
	Vol    float64 `json:"vol_24h"` // quote volume
}

// Candle is one OHLCV candlestick bar.
type Candle struct {
	ID    int64   `kit:"id" json:"id"` // unix timestamp
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
	Vol   float64 `json:"vol"`
}

// Symbol describes a trading pair listed on Huobi.
type Symbol struct {
	Symbol         string `kit:"id" json:"symbol"`
	Base           string `json:"base"`
	Quote          string `json:"quote"`
	State          string `json:"state"`
	PricePrecision int    `json:"price_precision"`
}

// --- wire types (Huobi JSON shapes) ---

type apiResponse struct {
	Status string `json:"status"`
	ErrMsg string `json:"err-msg"`
	Ch     string `json:"ch"`
}

type wireTickerResponse struct {
	apiResponse
	Tick wireTick `json:"tick"`
}

type wireTick struct {
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Amount float64   `json:"amount"`
	Vol    float64   `json:"vol"`
	Count  int       `json:"count"`
	Bid    []float64 `json:"bid"` // [price, size]
	Ask    []float64 `json:"ask"` // [price, size]
}

type wireKlineResponse struct {
	apiResponse
	Data []wireCandle `json:"data"`
}

type wireCandle struct {
	ID     int64   `json:"id"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Amount float64 `json:"amount"`
	Vol    float64 `json:"vol"`
	Count  int     `json:"count"`
}

type wireSymbolsResponse struct {
	apiResponse
	Data []wireSymbol `json:"data"`
}

type wireSymbol struct {
	BaseCurrency    string `json:"base-currency"`
	QuoteCurrency   string `json:"quote-currency"`
	Symbol          string `json:"symbol"`
	State           string `json:"state"`
	PricePrecision  int    `json:"price-precision"`
	AmountPrecision int    `json:"amount-precision"`
}

// --- API methods ---

// GetTicker fetches current price and 24h stats for a single symbol.
func (c *Client) GetTicker(ctx context.Context, symbol string) (*Ticker, error) {
	symbol = normalizeSymbol(symbol)
	u := BaseURL + "/market/detail/merged?symbol=" + url.QueryEscape(symbol)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp wireTickerResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse ticker: %w", err)
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("huobi API error: %s", resp.ErrMsg)
	}
	t := &Ticker{
		Symbol: symbol,
		Last:   resp.Tick.Close,
		Open:   resp.Tick.Open,
		High:   resp.Tick.High,
		Low:    resp.Tick.Low,
		Vol:    resp.Tick.Vol,
	}
	if len(resp.Tick.Bid) > 0 {
		t.Bid = resp.Tick.Bid[0]
	}
	if len(resp.Tick.Ask) > 0 {
		t.Ask = resp.Tick.Ask[0]
	}
	return t, nil
}

// GetKlines fetches candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, period string, count int) ([]*Candle, error) {
	symbol = normalizeSymbol(symbol)
	u := fmt.Sprintf("%s/market/history/kline?symbol=%s&period=%s&size=%d",
		BaseURL, url.QueryEscape(symbol), url.QueryEscape(period), count)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp wireKlineResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse klines: %w", err)
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("huobi API error: %s", resp.ErrMsg)
	}
	out := make([]*Candle, len(resp.Data))
	for i, d := range resp.Data {
		out[i] = &Candle{
			ID:    d.ID,
			Open:  d.Open,
			High:  d.High,
			Low:   d.Low,
			Close: d.Close,
			Vol:   d.Vol,
		}
	}
	return out, nil
}

// GetSymbols fetches all trading pairs from the exchange, optionally filtered
// by state and limited in count. Both filters are applied client-side.
func (c *Client) GetSymbols(ctx context.Context, state string, limit int) ([]*Symbol, error) {
	body, err := c.Get(ctx, BaseURL+"/v1/common/symbols")
	if err != nil {
		return nil, err
	}
	var resp wireSymbolsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse symbols: %w", err)
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("huobi API error: %s", resp.ErrMsg)
	}
	out := make([]*Symbol, 0, len(resp.Data))
	for _, d := range resp.Data {
		if state != "" && d.State != state {
			continue
		}
		out = append(out, &Symbol{
			Symbol:         d.Symbol,
			Base:           d.BaseCurrency,
			Quote:          d.QuoteCurrency,
			State:          d.State,
			PricePrecision: d.PricePrecision,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

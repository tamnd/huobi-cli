package huobi

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes Huobi public market data as a kit Domain: a driver that a
// multi-domain host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/huobi-cli/huobi"
//
// The init below registers it; the host then dereferences huobi:// URIs by
// routing to the operations Register installs. The same Domain also builds the
// standalone huobi binary (see cli.NewApp), so the binary and a host share
// one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the Huobi driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "huobi",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "huobi",
			Short:  "A command line for Huobi (HTX) public market data.",
			Long: `A command line for Huobi (HTX) public market data.

huobi reads public Huobi API data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools.
No API key required.`,
			Site: "www.htx.com",
			Repo: "https://github.com/tamnd/huobi-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "ticker", Group: "market", Single: true,
		Summary: "Current price and 24-hour stats for a symbol", URIType: "symbol", Resolver: true,
		Args: []kit.Arg{{Name: "symbol", Help: "symbol e.g. btcusdt or BTCUSDT"}},
	}, getTicker)

	kit.Handle(app, kit.OpMeta{
		Name: "kline", Group: "market", List: true,
		Summary: "Candlestick (OHLCV) data for a symbol", URIType: "symbol",
		Args: []kit.Arg{{Name: "symbol", Help: "symbol e.g. btcusdt or BTCUSDT"}},
	}, getKline)

	kit.Handle(app, kit.OpMeta{
		Name: "symbols", Group: "market", List: true,
		Summary: "List trading symbols on Huobi", URIType: "symbol",
	}, getSymbols)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type tickerInput struct {
	Symbol string  `kit:"arg" help:"symbol e.g. btcusdt or BTCUSDT"`
	Client *Client `kit:"inject"`
}

type klineInput struct {
	Symbol string  `kit:"arg" help:"symbol e.g. btcusdt or BTCUSDT"`
	Period string  `kit:"flag" help:"1min|5min|15min|30min|60min|4hour|1day|1week|1mon" default:"1day"`
	Count  int     `kit:"flag" help:"number of candles" default:"30"`
	Client *Client `kit:"inject"`
}

type symbolsInput struct {
	Limit  int     `kit:"flag,inherit" help:"max results" default:"20"`
	State  string  `kit:"flag" help:"filter by state" default:"online"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getTicker(ctx context.Context, in tickerInput, emit func(*Ticker) error) error {
	t, err := in.Client.GetTicker(ctx, strings.ToLower(strings.TrimSpace(in.Symbol)))
	if err != nil {
		return err
	}
	return emit(t)
}

func getKline(ctx context.Context, in klineInput, emit func(*Candle) error) error {
	period := in.Period
	if period == "" {
		period = "1day"
	}
	count := in.Count
	if count <= 0 {
		count = 30
	}
	candles, err := in.Client.GetKlines(ctx, in.Symbol, period, count)
	if err != nil {
		return err
	}
	for _, c := range candles {
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

func getSymbols(ctx context.Context, in symbolsInput, emit func(*Symbol) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	syms, err := in.Client.GetSymbols(ctx, in.State, limit)
	if err != nil {
		return err
	}
	for _, s := range syms {
		if err := emit(s); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: URI-native string functions, pure and network-free ---

// Classify turns any accepted input into the canonical (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty huobi reference")
	}
	// Strip URL prefix if given
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		parts := strings.Split(strings.TrimRight(input, "/"), "/")
		last := parts[len(parts)-1]
		id = strings.ToLower(strings.ReplaceAll(last, "-", ""))
	} else {
		id = strings.ToLower(strings.TrimSpace(input))
	}
	if id == "" {
		return "", "", errs.Usage("unrecognized huobi reference: %q", input)
	}
	return "symbol", id, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "symbol" {
		return "", errs.Usage("huobi has no resource type %q", uriType)
	}
	return "https://www.htx.com/en-us/trade/" + id + "/", nil
}

// mapErr converts a library error into the kit error kind that carries the
// right exit code.
func mapErr(err error) error {
	return err
}

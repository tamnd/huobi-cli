package huobi

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The client's
// HTTP behaviour is covered in huobi_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "huobi" {
		t.Errorf("Scheme = %q, want huobi", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "huobi" {
		t.Errorf("Identity.Binary = %q, want huobi", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct{ in, typ, id string }{
		{"btcusdt", "symbol", "btcusdt"},
		{"BTCUSDT", "symbol", "btcusdt"},
		{"ethusdt", "symbol", "ethusdt"},
		{"https://www.htx.com/en-us/trade/btcusdt/", "symbol", "btcusdt"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("symbol", "btcusdt")
	want := "https://www.htx.com/en-us/trade/btcusdt/"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("page", "btcusdt")
	if err == nil {
		t.Error("expected error for unknown resource type, got nil")
	}
}

// TestHostWiring mounts the driver in a kit Host (the runtime ant drives) and
// checks the round trip: a record mints to its URI and a bare id resolves back
// to the same URI. The init in domain.go registers the domain, so kit.Open finds it.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	ticker := &Ticker{Symbol: "btcusdt", Last: 63818.82, Bid: 63800, Ask: 63801}
	u, err := h.Mint(ticker)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "huobi://symbol/btcusdt"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("huobi", "btcusdt")
	if err != nil || got.String() != "huobi://symbol/btcusdt" {
		t.Errorf("ResolveOn = (%q, %v), want huobi://symbol/btcusdt", got.String(), err)
	}
}

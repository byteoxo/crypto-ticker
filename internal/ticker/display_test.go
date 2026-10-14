package ticker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise real example configs through loading and rendering, including older
// configs that omit the option. Full symbols must remain usable after rendering.
func TestSymbolDisplayAcrossExchanges(t *testing.T) {
	for _, name := range []string{
		"BINANCE_API_KEY", "BINANCE_API_SECRET", "GATE_API_KEY", "GATE_API_SECRET",
		"OKX_API_KEY", "OKX_API_SECRET", "OKX_API_PASSPHRASE",
		"BITGET_API_KEY", "BITGET_API_SECRET", "BITGET_API_PASSPHRASE",
	} {
		t.Setenv(name, "")
	}
	for _, exchange := range []string{"binance", "gate", "okx", "bitget"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "configs", "config.example."+exchange+".toml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, setting := range []struct {
			name, line string
			show       bool
		}{
			{"omitted", "", true},
			{"full", "show_quote_currency = true", true},
			{"base", "show_quote_currency = false", false},
		} {
			t.Run(exchange+"/"+setting.name, func(t *testing.T) {
				dir := t.TempDir()
				content := strings.Replace(string(data), "show_quote_currency = true", setting.line, 1)
				if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				t.Chdir(dir)
				cfg, err := loadConfig()
				if err != nil {
					t.Fatal(err)
				}
				if cfg.ShowQuoteCurrency != setting.show {
					t.Fatalf("ShowQuoteCurrency = %v, want %v", cfg.ShowQuoteCurrency, setting.show)
				}
				cfg.NoColor = true
				state := newAppState(cfg)
				state.applyTicker(priceTicker{Symbol: cfg.Symbols[0], Price: "123"})
				ui := newUI(cfg, time.UTC, state, func(int) {}, func() {})
				ui.refresh()
				want := "BTC"
				if setting.show {
					want = cfg.Symbols[0]
				}
				if got := ui.table.GetCell(1, 0).Text; got != want {
					t.Errorf("market label = %q, want %q", got, want)
				}
				if !strings.Contains(ui.header.GetText(false), "futures: "+want+",") {
					t.Error("overview does not use configured symbol display")
				}
				if got := ui.chart.GetTitle(); got != "1H Chart - "+want {
					t.Errorf("chart title = %q", got)
				}
				ui.renderPositions([]positionState{{Symbol: cfg.Symbols[0]}}, true, "", time.Time{})
				if got := ui.positions.GetCell(1, 0).Text; got != want {
					t.Errorf("position label = %q, want %q", got, want)
				}
				ui.renderOrderBook(nil, nil, cfg.Symbols[0])
				if got := ui.orderBookFrame.GetTitle(); got != "Order Book - "+want {
					t.Errorf("order book title = %q", got)
				}
				order := openOrder{Symbol: cfg.Symbols[0], OrderID: 42}
				ui.renderOpenOrders([]openOrder{order}, nil)
				if got := ui.openOrdersTable.GetCell(ui.openOrdersDataOffset, 0).Text; got != want {
					t.Errorf("order label = %q, want %q", got, want)
				}
				selected, _, ok := ui.selectedOpenOrder()
				if !ok || selected.Symbol != order.Symbol {
					t.Error("order selection lost the full exchange symbol")
				}
				if row := state.rows[cfg.Symbols[0]]; row.Symbol != cfg.Symbols[0] || row.Price != "123" {
					t.Error("rendering changed market state or its exchange symbol")
				}
				ui.renderSpotTable([]rowState{{Symbol: "BTCUSDT"}})
				spotWant := "BTC"
				if setting.show {
					spotWant = "BTCUSDT"
				}
				if got := ui.table.GetCell(1, 0).Text; got != spotWant {
					t.Errorf("spot label = %q, want %q", got, spotWant)
				}
			})
		}
	}
}

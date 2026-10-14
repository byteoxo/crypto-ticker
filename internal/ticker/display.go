package ticker

import "strings"

// displaySymbol changes labels only; state and exchange requests retain full symbols.
func (cfg config) displaySymbol(symbol string) string {
	if cfg.ShowQuoteCurrency {
		return symbol
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	// Gate pairs and OKX instrument IDs include an explicit base/quote separator.
	if i := strings.IndexAny(symbol, "_-"); i > 0 {
		return symbol[:i]
	}
	// Compact pairs use the quote currencies supported by the exchange adapters.
	for _, quote := range []string{"USDT", "USDC", "USD"} {
		if strings.HasSuffix(symbol, quote) && len(symbol) > len(quote) {
			return strings.TrimSuffix(symbol, quote)
		}
	}
	return symbol
}

func (cfg config) displaySymbols(symbols []string) string {
	labels := make([]string, len(symbols))
	for i, symbol := range symbols {
		labels[i] = cfg.displaySymbol(symbol)
	}
	return strings.Join(labels, ",")
}

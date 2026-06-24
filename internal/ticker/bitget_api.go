package ticker

// Bitget REST API v2 (USDT-M mix + spot).
//
// Production: https://api.bitget.com
// Signing: Base64(HMAC-SHA256(secret, timestamp + method + requestPath + query + body))

import (
	"context"
	"crypto-ticker/internal/symbol"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	bitgetAPIPrefix          = "/api/v2"
	bitgetProductTypeFutures = "usdt-futures"
)

var bitgetGranularityMap = map[string]string{
	"1m":  "1m",
	"5m":  "5m",
	"15m": "15m",
	"30m": "30m",
	"1h":  "1H",
	"2h":  "2H",
	"4h":  "4H",
	"1d":  "1D",
	"3d":  "3D",
}

// bitgetWSGranularityMap maps chart intervals to WS candle channel suffixes (no candle2H on WS).
var bitgetWSGranularityMap = map[string]string{
	"1m":  "1m",
	"5m":  "5m",
	"15m": "15m",
	"30m": "30m",
	"1h":  "1H",
	"2h":  "1H",
	"4h":  "4H",
	"1d":  "1D",
	"3d":  "3D",
}

func bitgetGranularity(interval string) string {
	if interval == "" {
		return "1H"
	}
	if v, ok := bitgetGranularityMap[strings.ToLower(interval)]; ok {
		return v
	}
	return "1H"
}

func bitgetWSCandleChannel(interval string) string {
	suffix := "1H"
	if v, ok := bitgetWSGranularityMap[strings.ToLower(interval)]; ok {
		suffix = v
	}
	return "candle" + suffix
}

func isBitgetBaseURL(baseURL string) bool {
	return strings.Contains(strings.ToLower(baseURL), "bitget.com")
}

func bitgetRESTHost(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "https://api.bitget.com"
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	return strings.TrimRight(parsed.String(), "/")
}

func bitgetTimestampMS() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10)
}

func buildBitgetSign(secret, timestamp, method, requestPath, body string) string {
	prehash := timestamp + strings.ToUpper(method) + requestPath + body
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(prehash))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func decodeBitgetJSON(body []byte, into interface{}) error {
	var env struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decode bitget envelope: %w", err)
	}
	if env.Code != "00000" {
		return fmt.Errorf("bitget api error %s: %s", env.Code, strings.TrimSpace(env.Msg))
	}
	if into == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, into); err != nil {
		return fmt.Errorf("decode bitget data: %w", err)
	}
	return nil
}

func bitgetPrivateRequest(ctx context.Context, client *http.Client, cfg config, method, pathWithQuery, body string) ([]byte, error) {
	ts := bitgetTimestampMS()
	sign := buildBitgetSign(cfg.APISecret, ts, method, pathWithQuery, body)
	host := bitgetRESTHost(cfg.RESTBase)
	reqURL := host + pathWithQuery

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build bitget request: %w", err)
	}
	req.Header.Set("ACCESS-KEY", cfg.APIKey)
	req.Header.Set("ACCESS-SIGN", sign)
	req.Header.Set("ACCESS-TIMESTAMP", ts)
	req.Header.Set("ACCESS-PASSPHRASE", cfg.APIPassphrase)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("locale", "en-US")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bitget request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read bitget response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitget status %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func bitgetPublicGET(ctx context.Context, client *http.Client, baseURL, pathWithQuery string, into interface{}) error {
	reqURL := bitgetRESTHost(baseURL) + pathWithQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("build bitget request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("bitget request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read bitget response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bitget status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return decodeBitgetJSON(body, into)
}

func fetchKlinesBitget(ctx context.Context, client *http.Client, baseURL, symbol, interval string, limit int, panel panelMode) ([]klineCandle, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	gran := bitgetGranularity(interval)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	var path string
	if panel == panelFutures {
		path = bitgetAPIPrefix + "/mix/market/candles?" + url.Values{
			"symbol":      {symbol},
			"productType": {bitgetProductTypeFutures},
			"granularity": {gran},
			"limit":       {strconv.Itoa(limit)},
		}.Encode()
	} else {
		path = bitgetAPIPrefix + "/spot/market/candles?" + url.Values{
			"symbol":      {symbol},
			"granularity": {strings.ToLower(gran)},
			"limit":       {strconv.Itoa(limit)},
		}.Encode()
	}

	var raw [][]string
	if err := bitgetPublicGET(ctx, client, baseURL, path, &raw); err != nil {
		return nil, err
	}

	out := make([]klineCandle, 0, len(raw))
	for _, row := range raw {
		if len(row) < 6 {
			continue
		}
		openMs, err := strconv.ParseInt(row[0], 10, 64)
		if err != nil {
			continue
		}
		vol := row[5]
		candle, err := newKlineCandle(symbol, openMs, openMs, row[1], row[2], row[3], row[4], vol, true)
		if err != nil {
			return nil, err
		}
		out = append(out, candle)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bitget: empty candle response")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenTime < out[j].OpenTime })
	return out, nil
}

func fetchOrderBookBitget(ctx context.Context, baseURL, symbol string, panel panelMode) (orderBookResponse, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	var path string
	if panel == panelFutures {
		path = bitgetAPIPrefix + "/mix/market/merge-depth?" + url.Values{
			"symbol":      {symbol},
			"productType": {bitgetProductTypeFutures},
			"limit":       {strconv.Itoa(orderBookLimit)},
		}.Encode()
	} else {
		path = bitgetAPIPrefix + "/spot/market/orderbook?" + url.Values{
			"symbol": {symbol},
			"type":   {"step0"},
			"limit":  {strconv.Itoa(orderBookLimit)},
		}.Encode()
	}

	var payload struct {
		Asks [][]string `json:"asks"`
		Bids [][]string `json:"bids"`
		Ts   string     `json:"ts"`
	}
	client := &http.Client{Timeout: defaultTimeout}
	if err := bitgetPublicGET(ctx, client, baseURL, path, &payload); err != nil {
		return orderBookResponse{}, err
	}
	ts, _ := strconv.ParseInt(payload.Ts, 10, 64)
	return orderBookResponse{
		LastUpdateID: ts,
		Asks:         payload.Asks,
		Bids:         payload.Bids,
	}, nil
}

func runBitgetMarketStatsLoop(ctx context.Context, client *http.Client, cfg config, state *appState, notify func()) {
	fetch := func() {
		for _, sym := range cfg.Symbols {
			if ls, err := fetchBitgetLongShortRatio(ctx, client, cfg.RESTBase, sym); err == nil {
				state.setLongShortRatio(ls)
			}
		}
		notify()
	}
	fetch()
	ticker := time.NewTicker(marketStatsRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetch()
		}
	}
}

func fetchBitgetLongShortRatio(ctx context.Context, client *http.Client, baseURL, symbol string) (longShortRatioData, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	path := bitgetAPIPrefix + "/mix/market/account-long-short?" + url.Values{
		"symbol":      {symbol},
		"productType": {bitgetProductTypeFutures},
		"period":      {"1H"},
	}.Encode()

	var rows []struct {
		LongAccountRatio      string `json:"longAccountRatio"`
		ShortAccountRatio     string `json:"shortAccountRatio"`
		LongShortAccountRatio string `json:"longShortAccountRatio"`
		Ts                    string `json:"ts"`
	}
	if err := bitgetPublicGET(ctx, client, baseURL, path, &rows); err != nil {
		return longShortRatioData{}, err
	}
	if len(rows) == 0 {
		return longShortRatioData{}, fmt.Errorf("bitget: empty long/short ratio")
	}
	row := rows[len(rows)-1]
	ratio, _ := strconv.ParseFloat(row.LongShortAccountRatio, 64)
	longAcc, _ := strconv.ParseFloat(row.LongAccountRatio, 64)
	shortAcc, _ := strconv.ParseFloat(row.ShortAccountRatio, 64)
	ts, _ := strconv.ParseInt(row.Ts, 10, 64)
	return longShortRatioData{
		Symbol:       symbol,
		Ratio:        ratio,
		LongAccount:  longAcc,
		ShortAccount: shortAcc,
		Timestamp:    ts,
	}, nil
}

func runBitgetPositionsLoop(ctx context.Context, client *http.Client, cfg config, state *appState, notify func()) {
	const interval = 5 * time.Second
	fetch := func() {
		positions, err := fetchBitgetPositions(ctx, client, cfg)
		if err != nil {
			state.setAccountError(fmt.Sprintf("bitget positions: %v", err))
			notify()
			return
		}
		state.setPositions(positions)
		state.clearAccountError()
		notify()
	}
	fetch()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetch()
		}
	}
}

func fetchBitgetPositions(ctx context.Context, client *http.Client, cfg config) ([]positionState, error) {
	pathWithQuery := bitgetAPIPrefix + "/mix/position/all-position?" + url.Values{
		"productType": {bitgetProductTypeFutures},
		"marginCoin":  {"USDT"},
	}.Encode()
	body, err := bitgetPrivateRequest(ctx, client, cfg, http.MethodGet, pathWithQuery, "")
	if err != nil {
		return nil, err
	}

	var payload []struct {
		Symbol           string `json:"symbol"`
		HoldSide         string `json:"holdSide"`
		Total            string `json:"total"`
		OpenPriceAvg     string `json:"openPriceAvg"`
		MarkPrice        string `json:"markPrice"`
		UnrealizedPL     string `json:"unrealizedPL"`
		LiquidationPrice string `json:"liquidationPrice"`
		Leverage         string `json:"leverage"`
		MarginMode       string `json:"marginMode"`
		UTime            string `json:"uTime"`
	}
	if err := decodeBitgetJSON(body, &payload); err != nil {
		return nil, err
	}

	out := make([]positionState, 0)
	for _, item := range payload {
		size, _ := strconv.ParseFloat(item.Total, 64)
		if math.Abs(size) < 1e-12 {
			continue
		}
		entry, _ := strconv.ParseFloat(item.OpenPriceAvg, 64)
		mark, _ := strconv.ParseFloat(item.MarkPrice, 64)
		upl, _ := strconv.ParseFloat(item.UnrealizedPL, 64)
		liq, _ := strconv.ParseFloat(item.LiquidationPrice, 64)

		side := strings.ToUpper(strings.TrimSpace(item.HoldSide))
		switch strings.ToLower(item.HoldSide) {
		case "long":
			side = "LONG"
		case "short":
			side = "SHORT"
		}

		marginType := "CROSS"
		if strings.EqualFold(item.MarginMode, "isolated") {
			marginType = "ISOLATED"
		}

		ut, _ := strconv.ParseInt(item.UTime, 10, 64)
		sym := strings.ToUpper(strings.TrimSpace(item.Symbol))

		out = append(out, positionState{
			Symbol:           sym,
			Side:             side,
			Size:             math.Abs(size),
			EntryPrice:       entry,
			MarkPrice:        mark,
			UnrealizedPnL:    upl,
			LiquidationPrice: liq,
			MarginType:       marginType,
			Leverage:         strings.TrimSpace(item.Leverage),
			UpdateTime:       ut,
			PnLFromAPI:       true,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Symbol == out[j].Symbol {
			return out[i].Side < out[j].Side
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out, nil
}

func fetchBitgetSpotBalances(ctx context.Context, client *http.Client, cfg config) ([]spotBalance, error) {
	pathWithQuery := bitgetAPIPrefix + "/spot/account/assets"
	body, err := bitgetPrivateRequest(ctx, client, cfg, http.MethodGet, pathWithQuery, "")
	if err != nil {
		return nil, err
	}

	var payload []struct {
		Coin      string `json:"coin"`
		Available string `json:"available"`
		Frozen    string `json:"frozen"`
	}
	if err := decodeBitgetJSON(body, &payload); err != nil {
		return nil, err
	}

	allowed := symbol.AllowedSpotAssets(cfg.SpotSymbols)
	out := make([]spotBalance, 0)
	for _, item := range payload {
		asset := strings.ToUpper(strings.TrimSpace(item.Coin))
		if _, ok := allowed[asset]; !ok {
			continue
		}
		free, err := strconv.ParseFloat(strings.TrimSpace(item.Available), 64)
		if err != nil {
			continue
		}
		locked, err := strconv.ParseFloat(strings.TrimSpace(item.Frozen), 64)
		if err != nil {
			continue
		}
		total := free + locked
		if total <= 0 {
			continue
		}
		ps := symbol.SpotSymbolToTicker(asset)
		out = append(out, spotBalance{
			Asset:          asset,
			Free:           free,
			Locked:         locked,
			Total:          total,
			QuoteValueText: "-",
			PriceSymbol:    ps,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Asset < out[j].Asset })
	return out, nil
}

func fetchBitgetPendingOrders(ctx context.Context, client *http.Client, cfg config, panel panelMode) ([]openOrder, error) {
	var pathWithQuery string
	if panel == panelFutures {
		pathWithQuery = bitgetAPIPrefix + "/mix/order/orders-pending?" + url.Values{
			"productType": {bitgetProductTypeFutures},
		}.Encode()
	} else {
		pathWithQuery = bitgetAPIPrefix + "/spot/trade/unfilled-orders"
	}

	respBody, err := bitgetPrivateRequest(ctx, client, cfg, http.MethodGet, pathWithQuery, "")
	if err != nil {
		return nil, err
	}

	if panel == panelSpot {
		var spotList []struct {
			Symbol     string `json:"symbol"`
			OrderId    string `json:"orderId"`
			Side       string `json:"side"`
			OrderType  string `json:"orderType"`
			Price      string `json:"price"`
			Size       string `json:"size"`
			BaseVolume string `json:"baseVolume"`
			Status     string `json:"status"`
			Force      string `json:"force"`
			CTime      string `json:"cTime"`
		}
		if err := decodeBitgetJSON(respBody, &spotList); err != nil {
			return nil, err
		}
		return bitgetParseOpenOrders(spotListToGeneric(spotList)), nil
	}

	var wrapped struct {
		EntrustedList []struct {
			Symbol     string `json:"symbol"`
			Size       string `json:"size"`
			OrderId    string `json:"orderId"`
			BaseVolume string `json:"baseVolume"`
			Price      string `json:"price"`
			Status     string `json:"status"`
			Side       string `json:"side"`
			Force      string `json:"force"`
			OrderType  string `json:"orderType"`
			CTime      string `json:"cTime"`
		} `json:"entrustedList"`
	}
	if err := decodeBitgetJSON(respBody, &wrapped); err != nil {
		return nil, err
	}
	rows := make([]bitgetOrderRow, 0, len(wrapped.EntrustedList))
	for _, item := range wrapped.EntrustedList {
		rows = append(rows, bitgetOrderRow{
			Symbol:     item.Symbol,
			OrderID:    item.OrderId,
			Side:       item.Side,
			OrderType:  item.OrderType,
			Price:      item.Price,
			Size:       item.Size,
			BaseVolume: item.BaseVolume,
			Status:     item.Status,
			Force:      item.Force,
			CTime:      item.CTime,
		})
	}
	return bitgetParseOpenOrders(rows), nil
}

type bitgetOrderRow struct {
	Symbol     string
	OrderID    string
	Side       string
	OrderType  string
	Price      string
	Size       string
	BaseVolume string
	Status     string
	Force      string
	CTime      string
}

func spotListToGeneric(spotList []struct {
	Symbol     string `json:"symbol"`
	OrderId    string `json:"orderId"`
	Side       string `json:"side"`
	OrderType  string `json:"orderType"`
	Price      string `json:"price"`
	Size       string `json:"size"`
	BaseVolume string `json:"baseVolume"`
	Status     string `json:"status"`
	Force      string `json:"force"`
	CTime      string `json:"cTime"`
}) []bitgetOrderRow {
	rows := make([]bitgetOrderRow, 0, len(spotList))
	for _, item := range spotList {
		rows = append(rows, bitgetOrderRow{
			Symbol:     item.Symbol,
			OrderID:    item.OrderId,
			Side:       item.Side,
			OrderType:  item.OrderType,
			Price:      item.Price,
			Size:       item.Size,
			BaseVolume: item.BaseVolume,
			Status:     item.Status,
			Force:      item.Force,
			CTime:      item.CTime,
		})
	}
	return rows
}

func bitgetParseOpenOrders(rows []bitgetOrderRow) []openOrder {
	out := make([]openOrder, 0, len(rows))
	for _, item := range rows {
		price, _ := strconv.ParseFloat(item.Price, 64)
		sz, _ := strconv.ParseFloat(item.Size, 64)
		filled, _ := strconv.ParseFloat(item.BaseVolume, 64)
		tif := strings.ToUpper(strings.TrimSpace(item.Force))
		if tif == "" {
			tif = "GTC"
		}
		t64, _ := strconv.ParseInt(item.CTime, 10, 64)
		oid, _ := strconv.ParseInt(item.OrderID, 10, 64)
		sym := strings.ToUpper(strings.TrimSpace(item.Symbol))
		out = append(out, openOrder{
			Symbol:      sym,
			OrderID:     oid,
			Side:        strings.ToUpper(item.Side),
			Type:        strings.ToUpper(item.OrderType),
			Price:       price,
			OrigQty:     sz,
			FilledQty:   filled,
			Status:      strings.ToUpper(item.Status),
			TimeInForce: tif,
			Time:        t64,
		})
	}
	return out
}

func newBitgetClientOID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "ticker-" + hex.EncodeToString(b)
}

func placeBitgetFuturesOrder(ctx context.Context, client *http.Client, cfg config, symbol, side, priceStr, sizeStr string, market, reduceOnly bool) (openOrder, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	okSide := strings.ToLower(strings.TrimSpace(side))
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY":
		okSide = "buy"
	case "SELL":
		okSide = "sell"
	}
	if okSide != "buy" && okSide != "sell" {
		return openOrder{}, fmt.Errorf("invalid side %s", side)
	}
	orderType := "limit"
	if market {
		orderType = "market"
	}
	reduceOnlyVal := "NO"
	if reduceOnly {
		reduceOnlyVal = "YES"
	}
	bodyMap := map[string]string{
		"symbol":      symbol,
		"productType": bitgetProductTypeFutures,
		"marginMode":  "crossed",
		"marginCoin":  "USDT",
		"side":        okSide,
		"orderType":   orderType,
		"size":        sizeStr,
		"force":       "gtc",
		"reduceOnly":  reduceOnlyVal,
		"clientOid":   newBitgetClientOID(),
	}
	if !market {
		bodyMap["price"] = priceStr
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return openOrder{}, err
	}
	path := bitgetAPIPrefix + "/mix/order/place-order"
	respBody, err := bitgetPrivateRequest(ctx, client, cfg, http.MethodPost, path, string(bodyBytes))
	if err != nil {
		return openOrder{}, err
	}
	var data struct {
		OrderId string `json:"orderId"`
	}
	if err := decodeBitgetJSON(respBody, &data); err != nil {
		return openOrder{}, err
	}
	oid, _ := strconv.ParseInt(data.OrderId, 10, 64)
	price, _ := strconv.ParseFloat(priceStr, 64)
	sz, _ := strconv.ParseFloat(sizeStr, 64)
	return openOrder{
		Symbol:      symbol,
		OrderID:     oid,
		Side:        strings.ToUpper(okSide),
		Type:        strings.ToUpper(orderType),
		Price:       price,
		OrigQty:     sz,
		Status:      "LIVE",
		TimeInForce: "GTC",
		Time:        time.Now().UnixMilli(),
	}, nil
}

func cancelBitgetFuturesOrder(ctx context.Context, client *http.Client, cfg config, symbol string, orderID int64) error {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	bodyMap := map[string]string{
		"symbol":      symbol,
		"productType": bitgetProductTypeFutures,
		"marginCoin":  "USDT",
		"orderId":     strconv.FormatInt(orderID, 10),
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return err
	}
	path := bitgetAPIPrefix + "/mix/order/cancel-order"
	_, err = bitgetPrivateRequest(ctx, client, cfg, http.MethodPost, path, string(bodyBytes))
	return err
}

func amendBitgetFuturesOrderPrice(ctx context.Context, client *http.Client, cfg config, symbol string, orderID int64, newPrice string) error {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	bodyMap := map[string]string{
		"symbol":       symbol,
		"productType":  bitgetProductTypeFutures,
		"orderId":      strconv.FormatInt(orderID, 10),
		"newClientOid": newBitgetClientOID(),
		"newPrice":     newPrice,
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return err
	}
	path := bitgetAPIPrefix + "/mix/order/modify-order"
	_, err = bitgetPrivateRequest(ctx, client, cfg, http.MethodPost, path, string(bodyBytes))
	return err
}

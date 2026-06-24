package ticker

import (
	"context"
	"crypto-ticker/internal/format"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// openOrder represents a single open (unfilled) order.
type openOrder struct {
	Symbol       string
	OrderID      int64
	Side         string // BUY / SELL / buy / sell
	Type         string // LIMIT, MARKET, TRIGGER, etc.
	Price        float64
	OrigQty      float64
	FilledQty    float64
	Status       string
	TimeInForce  string
	Time         int64
	TriggerPrice float64 // Gate.io price-triggered order: trigger price
	TriggerRule  int     // Gate.io price-triggered order: 1 = >=, 2 = <=
}

// ── Binance Futures open orders ───────────────────────────────────────────────

func fetchBinanceFuturesOpenOrders(ctx context.Context, client *http.Client, cfg config) ([]openOrder, error) {
	query := url.Values{}
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("recvWindow", strconv.FormatInt(int64(cfg.Timeout/time.Millisecond), 10))

	endpoint, err := buildSignedURL(cfg.RESTBase, "/fapi/v1/openOrders", query, cfg.APISecret)
	if err != nil {
		return nil, fmt.Errorf("build binance futures open orders url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build binance futures open orders request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send binance futures open orders request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read binance futures open orders response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance futures open orders status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload []struct {
		Symbol      string `json:"symbol"`
		OrderID     int64  `json:"orderId"`
		Side        string `json:"side"`
		Type        string `json:"type"`
		Price       string `json:"price"`
		OrigQty     string `json:"origQty"`
		ExecutedQty string `json:"executedQty"`
		Status      string `json:"status"`
		TimeInForce string `json:"timeInForce"`
		Time        int64  `json:"time"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode binance futures open orders: %w", err)
	}

	orders := make([]openOrder, 0, len(payload))
	for _, item := range payload {
		price, _ := strconv.ParseFloat(item.Price, 64)
		origQty, _ := strconv.ParseFloat(item.OrigQty, 64)
		filledQty, _ := strconv.ParseFloat(item.ExecutedQty, 64)
		orders = append(orders, openOrder{
			Symbol:      item.Symbol,
			OrderID:     item.OrderID,
			Side:        item.Side,
			Type:        item.Type,
			Price:       price,
			OrigQty:     origQty,
			FilledQty:   filledQty,
			Status:      item.Status,
			TimeInForce: item.TimeInForce,
			Time:        item.Time,
		})
	}
	return orders, nil
}

// ── Binance Spot open orders ──────────────────────────────────────────────────

func fetchBinanceSpotOpenOrders(ctx context.Context, client *http.Client, cfg config) ([]openOrder, error) {
	query := url.Values{}
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("recvWindow", strconv.FormatInt(int64(cfg.Timeout/time.Millisecond), 10))

	endpoint, err := buildSignedURL(defaultSpotRESTBaseURL, "/api/v3/openOrders", query, cfg.APISecret)
	if err != nil {
		return nil, fmt.Errorf("build binance spot open orders url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build binance spot open orders request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send binance spot open orders request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read binance spot open orders response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance spot open orders status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload []struct {
		Symbol      string `json:"symbol"`
		OrderID     int64  `json:"orderId"`
		Side        string `json:"side"`
		Type        string `json:"type"`
		Price       string `json:"price"`
		OrigQty     string `json:"origQty"`
		ExecutedQty string `json:"executedQty"`
		Status      string `json:"status"`
		TimeInForce string `json:"timeInForce"`
		Time        int64  `json:"time"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode binance spot open orders: %w", err)
	}

	orders := make([]openOrder, 0, len(payload))
	for _, item := range payload {
		price, _ := strconv.ParseFloat(item.Price, 64)
		origQty, _ := strconv.ParseFloat(item.OrigQty, 64)
		filledQty, _ := strconv.ParseFloat(item.ExecutedQty, 64)
		orders = append(orders, openOrder{
			Symbol:      item.Symbol,
			OrderID:     item.OrderID,
			Side:        item.Side,
			Type:        item.Type,
			Price:       price,
			OrigQty:     origQty,
			FilledQty:   filledQty,
			Status:      item.Status,
			TimeInForce: item.TimeInForce,
			Time:        item.Time,
		})
	}
	return orders, nil
}

// ── Binance Futures Order Management ──────────────────────────────────────────

// placeBinanceFuturesOrder places a LIMIT or MARKET order on Binance futures.
func placeBinanceFuturesOrder(ctx context.Context, client *http.Client, cfg config, symbol, side, orderType, quantity, price string, reduceOnly bool) (openOrder, error) {
	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("side", strings.ToUpper(side))
	query.Set("type", strings.ToUpper(orderType))
	query.Set("quantity", quantity)
	if strings.EqualFold(orderType, "LIMIT") {
		query.Set("timeInForce", "GTC")
		query.Set("price", price)
	}
	if reduceOnly {
		query.Set("reduceOnly", "true")
	}
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("recvWindow", strconv.FormatInt(int64(cfg.Timeout/time.Millisecond), 10))

	endpoint, err := buildSignedURL(cfg.RESTBase, "/fapi/v1/order", query, cfg.APISecret)
	if err != nil {
		return openOrder{}, fmt.Errorf("build binance order url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return openOrder{}, fmt.Errorf("build binance order request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return openOrder{}, fmt.Errorf("binance order request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return openOrder{}, fmt.Errorf("read binance order response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return openOrder{}, fmt.Errorf("binance order status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Symbol      string `json:"symbol"`
		OrderID     int64  `json:"orderId"`
		Side        string `json:"side"`
		Type        string `json:"type"`
		Price       string `json:"price"`
		OrigQty     string `json:"origQty"`
		ExecutedQty string `json:"executedQty"`
		Status      string `json:"status"`
		TimeInForce string `json:"timeInForce"`
		UpdateTime  int64  `json:"updateTime"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return openOrder{}, fmt.Errorf("decode binance order response: %w", err)
	}

	p, _ := strconv.ParseFloat(payload.Price, 64)
	origQty, _ := strconv.ParseFloat(payload.OrigQty, 64)
	filledQty, _ := strconv.ParseFloat(payload.ExecutedQty, 64)
	return openOrder{
		Symbol:      payload.Symbol,
		OrderID:     payload.OrderID,
		Side:        payload.Side,
		Type:        payload.Type,
		Price:       p,
		OrigQty:     origQty,
		FilledQty:   filledQty,
		Status:      payload.Status,
		TimeInForce: payload.TimeInForce,
		Time:        payload.UpdateTime,
	}, nil
}

// cancelBinanceFuturesOrder cancels a single futures order on Binance.
func cancelBinanceFuturesOrder(ctx context.Context, client *http.Client, cfg config, symbol string, orderID int64) error {
	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("orderId", strconv.FormatInt(orderID, 10))
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("recvWindow", strconv.FormatInt(int64(cfg.Timeout/time.Millisecond), 10))

	endpoint, err := buildSignedURL(cfg.RESTBase, "/fapi/v1/order", query, cfg.APISecret)
	if err != nil {
		return fmt.Errorf("build binance cancel url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build binance cancel request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("binance cancel request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("binance cancel status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// amendBinanceFuturesOrder modifies the price and quantity of an existing futures order on Binance.
func amendBinanceFuturesOrder(ctx context.Context, client *http.Client, cfg config, symbol string, orderID int64, side, quantity, price string) error {
	query := url.Values{}
	query.Set("orderId", strconv.FormatInt(orderID, 10))
	query.Set("symbol", symbol)
	query.Set("side", strings.ToUpper(side))
	query.Set("quantity", quantity)
	query.Set("price", price)
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("recvWindow", strconv.FormatInt(int64(cfg.Timeout/time.Millisecond), 10))

	endpoint, err := buildSignedURL(cfg.RESTBase, "/fapi/v1/order", query, cfg.APISecret)
	if err != nil {
		return fmt.Errorf("build binance amend url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build binance amend request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("binance amend request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("binance amend status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// ── Binance Spot Order Management ─────────────────────────────────────────────

func placeBinanceSpotOrder(ctx context.Context, client *http.Client, cfg config, symbol, side, orderType, quantity, price string) (openOrder, error) {
	symbol = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(symbol), "_", ""))
	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("side", strings.ToUpper(side))
	query.Set("type", strings.ToUpper(orderType))
	query.Set("quantity", quantity)
	if strings.EqualFold(orderType, "LIMIT") {
		query.Set("timeInForce", "GTC")
		query.Set("price", price)
	}
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("recvWindow", strconv.FormatInt(int64(cfg.Timeout/time.Millisecond), 10))

	endpoint, err := buildSignedURL(defaultSpotRESTBaseURL, "/api/v3/order", query, cfg.APISecret)
	if err != nil {
		return openOrder{}, fmt.Errorf("build binance spot order url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return openOrder{}, fmt.Errorf("build binance spot order request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return openOrder{}, fmt.Errorf("binance spot order request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return openOrder{}, fmt.Errorf("read binance spot order response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return openOrder{}, fmt.Errorf("binance spot order status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Symbol      string `json:"symbol"`
		OrderID     int64  `json:"orderId"`
		Side        string `json:"side"`
		Type        string `json:"type"`
		Price       string `json:"price"`
		OrigQty     string `json:"origQty"`
		ExecutedQty string `json:"executedQty"`
		Status      string `json:"status"`
		TimeInForce string `json:"timeInForce"`
		TransactTime int64 `json:"transactTime"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return openOrder{}, fmt.Errorf("decode binance spot order response: %w", err)
	}

	p, _ := strconv.ParseFloat(payload.Price, 64)
	origQty, _ := strconv.ParseFloat(payload.OrigQty, 64)
	filledQty, _ := strconv.ParseFloat(payload.ExecutedQty, 64)
	return openOrder{
		Symbol:      payload.Symbol,
		OrderID:     payload.OrderID,
		Side:        payload.Side,
		Type:        payload.Type,
		Price:       p,
		OrigQty:     origQty,
		FilledQty:   filledQty,
		Status:      payload.Status,
		TimeInForce: payload.TimeInForce,
		Time:        payload.TransactTime,
	}, nil
}

func cancelBinanceSpotOrder(ctx context.Context, client *http.Client, cfg config, symbol string, orderID int64) error {
	symbol = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(symbol), "_", ""))
	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("orderId", strconv.FormatInt(orderID, 10))
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("recvWindow", strconv.FormatInt(int64(cfg.Timeout/time.Millisecond), 10))

	endpoint, err := buildSignedURL(defaultSpotRESTBaseURL, "/api/v3/order", query, cfg.APISecret)
	if err != nil {
		return fmt.Errorf("build binance spot cancel url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build binance spot cancel request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("binance spot cancel request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("binance spot cancel status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func amendBinanceSpotOrder(ctx context.Context, client *http.Client, cfg config, symbol string, orderID int64, side, quantity, newPrice string) error {
	symbol = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(symbol), "_", ""))
	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("cancelOrderId", strconv.FormatInt(orderID, 10))
	query.Set("side", strings.ToUpper(side))
	query.Set("type", "LIMIT")
	query.Set("quantity", quantity)
	query.Set("price", newPrice)
	query.Set("cancelReplaceMode", "STOP_ON_FAILURE")
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("recvWindow", strconv.FormatInt(int64(cfg.Timeout/time.Millisecond), 10))

	endpoint, err := buildSignedURL(defaultSpotRESTBaseURL, "/api/v3/order/cancelReplace", query, cfg.APISecret)
	if err != nil {
		return fmt.Errorf("build binance spot amend url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build binance spot amend request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("binance spot amend request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("binance spot amend status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func placeOrderForPanel(ctx context.Context, client *http.Client, cfg config, panel panelMode, sym, side, typeName, qty, price string, reduceOnly bool) (openOrder, error) {
	market := strings.EqualFold(typeName, "MARKET")
	if panel == panelSpot {
		switch {
		case cfg.isGate():
			return placeGateSpotOrder(ctx, client, cfg, sym, side, qty, price, market)
		case cfg.isOKX():
			return placeOKXSpotOrder(ctx, client, cfg, sym, side, price, qty, market)
		case cfg.isBitget():
			return placeBitgetSpotOrder(ctx, client, cfg, sym, side, price, qty, market)
		default:
			return placeBinanceSpotOrder(ctx, client, cfg, sym, side, typeName, qty, price)
		}
	}
	switch {
	case cfg.isGate():
		sizeVal, err := strconv.ParseInt(qty, 10, 64)
		if err != nil {
			return openOrder{}, fmt.Errorf("invalid gate futures size: %w", err)
		}
		if strings.EqualFold(side, "SELL") && sizeVal > 0 {
			sizeVal = -sizeVal
		}
		if strings.EqualFold(side, "BUY") && sizeVal < 0 {
			sizeVal = -sizeVal
		}
		return placeGateFuturesOrder(ctx, client, cfg, sym, sizeVal, price, market, reduceOnly)
	case cfg.isOKX():
		return placeOKXFuturesOrder(ctx, client, cfg, sym, side, price, qty, market, reduceOnly)
	case cfg.isBitget():
		return placeBitgetFuturesOrder(ctx, client, cfg, sym, side, price, qty, market, reduceOnly)
	default:
		return placeBinanceFuturesOrder(ctx, client, cfg, sym, side, typeName, qty, price, reduceOnly)
	}
}

func cancelOrderForPanel(ctx context.Context, client *http.Client, cfg config, panel panelMode, order openOrder) error {
	if panel == panelSpot {
		switch {
		case cfg.isGate():
			return cancelGateSpotOrder(ctx, client, cfg, order.OrderID)
		case cfg.isOKX():
			return cancelOKXSpotOrder(ctx, client, cfg, order.Symbol, order.OrderID)
		case cfg.isBitget():
			return cancelBitgetSpotOrder(ctx, client, cfg, order.Symbol, order.OrderID)
		default:
			return cancelBinanceSpotOrder(ctx, client, cfg, order.Symbol, order.OrderID)
		}
	}
	if cfg.isGate() {
		if order.Type == "TRIGGER" {
			return cancelGateFuturesPriceOrder(ctx, client, cfg, order.OrderID)
		}
		return cancelGateFuturesOrder(ctx, client, cfg, order.OrderID)
	}
	if cfg.isOKX() {
		return cancelOKXFuturesOrder(ctx, client, cfg, order.Symbol, order.OrderID)
	}
	if cfg.isBitget() {
		return cancelBitgetFuturesOrder(ctx, client, cfg, order.Symbol, order.OrderID)
	}
	return cancelBinanceFuturesOrder(ctx, client, cfg, order.Symbol, order.OrderID)
}

func amendOrderPriceForPanel(ctx context.Context, client *http.Client, cfg config, panel panelMode, order openOrder, newPrice, newQty string) error {
	if panel == panelSpot {
		switch {
		case cfg.isGate():
			return amendGateSpotOrderPrice(ctx, client, cfg, order.OrderID, newPrice)
		case cfg.isOKX():
			return amendOKXSpotOrderPrice(ctx, client, cfg, order.Symbol, order.OrderID, newPrice)
		case cfg.isBitget():
			return amendBitgetSpotOrderPrice(ctx, client, cfg, order.Symbol, order.OrderID, newPrice)
		default:
			return amendBinanceSpotOrder(ctx, client, cfg, order.Symbol, order.OrderID, order.Side, newQty, newPrice)
		}
	}
	if cfg.isGate() {
		return amendGateFuturesOrderPrice(ctx, client, cfg, order.OrderID, newPrice)
	}
	if cfg.isOKX() {
		return amendOKXFuturesOrderPrice(ctx, client, cfg, order.Symbol, order.OrderID, newPrice)
	}
	if cfg.isBitget() {
		return amendBitgetFuturesOrderPrice(ctx, client, cfg, order.Symbol, order.OrderID, newPrice)
	}
	return amendBinanceFuturesOrder(ctx, client, cfg, order.Symbol, order.OrderID, order.Side, newQty, newPrice)
}

// ── Gate.io Futures open orders ───────────────────────────────────────────────

func fetchGateOpenOrders(ctx context.Context, client *http.Client, cfg config) ([]openOrder, error) {
	const path = "/api/v4/futures/usdt/orders"
	ts := time.Now().Unix()

	query := url.Values{}
	query.Set("status", "open")
	queryStr := query.Encode()

	sig := buildGateSignature(cfg.APISecret, "GET", path, queryStr, "", ts)

	parsed, err := url.Parse(cfg.RESTBase)
	if err != nil {
		return nil, fmt.Errorf("parse gate rest base: %w", err)
	}
	parsed.Path = path
	parsed.RawQuery = queryStr

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build gate open orders request: %w", err)
	}
	req.Header.Set("KEY", cfg.APIKey)
	req.Header.Set("SIGN", sig)
	req.Header.Set("Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gate open orders request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gate open orders response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gate open orders status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload []struct {
		ID         int64   `json:"id"`
		Contract   string  `json:"contract"`
		Size       int64   `json:"size"` // positive=buy, negative=sell
		Price      string  `json:"price"`
		Left       int64   `json:"left"` // remaining unfilled size
		Type       string  `json:"type"` // "limit" or "market"
		Tif        string  `json:"tif"`  // time in force: gtc, ioc, poc, fok
		Status     string  `json:"status"`
		CreateTime float64 `json:"create_time"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode gate open orders: %w", err)
	}

	orders := make([]openOrder, 0, len(payload))
	for _, item := range payload {
		price, _ := strconv.ParseFloat(item.Price, 64)
		origQty := float64(item.Size)
		if origQty < 0 {
			origQty = -origQty
		}
		filled := origQty - float64(item.Left)
		if filled < 0 {
			filled = 0
		}
		side := "BUY"
		if item.Size < 0 {
			side = "SELL"
		}
		orders = append(orders, openOrder{
			Symbol:      item.Contract,
			OrderID:     item.ID,
			Side:        side,
			Type:        strings.ToUpper(item.Type),
			Price:       price,
			OrigQty:     origQty,
			FilledQty:   filled,
			Status:      strings.ToUpper(item.Status),
			TimeInForce: strings.ToUpper(item.Tif),
			Time:        int64(item.CreateTime * 1000),
		})
	}
	return orders, nil
}

// ── Gate.io Futures price-triggered (conditional) orders ──────────────────────

func fetchGatePriceTriggeredOrders(ctx context.Context, client *http.Client, cfg config) ([]openOrder, error) {
	const path = "/api/v4/futures/usdt/price_orders"
	ts := time.Now().Unix()

	query := url.Values{}
	query.Set("status", "open")
	queryStr := query.Encode()

	sig := buildGateSignature(cfg.APISecret, "GET", path, queryStr, "", ts)

	parsed, err := url.Parse(cfg.RESTBase)
	if err != nil {
		return nil, fmt.Errorf("parse gate rest base: %w", err)
	}
	parsed.Path = path
	parsed.RawQuery = queryStr

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build gate price orders request: %w", err)
	}
	req.Header.Set("KEY", cfg.APIKey)
	req.Header.Set("SIGN", sig)
	req.Header.Set("Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gate price orders request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gate price orders response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gate price orders status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload []struct {
		ID         int64   `json:"id"`
		Status     string  `json:"status"`
		CreateTime float64 `json:"create_time"`
		OrderType  string  `json:"order_type"`
		Trigger    struct {
			StrategyType int    `json:"strategy_type"`
			PriceType    int    `json:"price_type"`
			Price        string `json:"price"`
			Rule         int    `json:"rule"`
			Expiration   int    `json:"expiration"`
		} `json:"trigger"`
		Initial struct {
			Contract     string `json:"contract"`
			Size         int64  `json:"size"`
			Price        string `json:"price"`
			Tif          string `json:"tif"`
			IsClose      bool   `json:"is_close"`
			IsReduceOnly bool   `json:"is_reduce_only"`
		} `json:"initial"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode gate price orders: %w", err)
	}

	orders := make([]openOrder, 0, len(payload))
	for _, item := range payload {
		price, _ := strconv.ParseFloat(item.Initial.Price, 64)
		triggerPrice, _ := strconv.ParseFloat(item.Trigger.Price, 64)

		origQty := float64(item.Initial.Size)
		if origQty < 0 {
			origQty = -origQty
		}
		side := "BUY"
		if item.Initial.Size < 0 {
			side = "SELL"
		}
		if item.Initial.Size == 0 {
			switch item.OrderType {
			case "close-long-order", "close-long-position", "plan-close-long-position":
				side = "SELL"
			case "close-short-order", "close-short-position", "plan-close-short-position":
				side = "BUY"
			}
		}

		orders = append(orders, openOrder{
			Symbol:       item.Initial.Contract,
			OrderID:      item.ID,
			Side:         side,
			Type:         "TRIGGER",
			Price:        price,
			OrigQty:      origQty,
			FilledQty:    0,
			Status:       strings.ToUpper(item.Status),
			TimeInForce:  strings.ToUpper(item.Initial.Tif),
			Time:         int64(item.CreateTime * 1000),
			TriggerPrice: triggerPrice,
			TriggerRule:  item.Trigger.Rule,
		})
	}
	return orders, nil
}

// fetchOpenOrders routes to the correct exchange.
func fetchOpenOrders(ctx context.Context, client *http.Client, cfg config) (futures []openOrder, spot []openOrder, err error) {
	if !cfg.hasAccountAuth() {
		return nil, nil, nil
	}
	if cfg.isGate() {
		f, e := fetchGateOpenOrders(ctx, client, cfg)
		if e != nil {
			return nil, nil, e
		}
		triggered, _ := fetchGatePriceTriggeredOrders(ctx, client, cfg)
		f = append(f, triggered...)
		s, e2 := fetchGateSpotOpenOrders(ctx, client, cfg)
		return f, s, e2
	}
	if cfg.isOKX() {
		f, e := fetchOKXPendingOrders(ctx, client, cfg, "SWAP")
		if e != nil {
			return nil, nil, e
		}
		s, e2 := fetchOKXPendingOrders(ctx, client, cfg, "SPOT")
		return f, s, e2
	}
	if cfg.isBitget() {
		f, e := fetchBitgetPendingOrders(ctx, client, cfg, panelFutures)
		if e != nil {
			return nil, nil, e
		}
		s, e2 := fetchBitgetPendingOrders(ctx, client, cfg, panelSpot)
		return f, s, e2
	}
	f, e1 := fetchBinanceFuturesOpenOrders(ctx, client, cfg)
	if e1 != nil {
		return nil, nil, e1
	}
	s, e2 := fetchBinanceSpotOpenOrders(ctx, client, cfg)
	return f, s, e2
}

// ── Open Orders UI overlay ────────────────────────────────────────────────────

const openOrdersRefreshInterval = 5 * time.Second

func buildOpenOrdersUI() (tview.Primitive, *tview.Frame, *tview.Table, *tview.TextView) {
	ooTable := tview.NewTable().SetBorders(false).SetSelectable(false, false).SetFixed(1, 0)
	ooTable.SetBackgroundColor(tcell.ColorDefault)

	hint := tview.NewTextView().SetDynamicColors(true)
	hint.SetBackgroundColor(tcell.ColorDefault)
	hint.SetText("r refresh  Esc/u close")

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(ooTable, 0, 1, false).
		AddItem(hint, 1, 0, false)

	frame := tview.NewFrame(content)
	frame.SetBorders(1, 1, 1, 1, 2, 2)
	frame.SetBorder(true)
	frame.SetTitle("Open Orders")
	frame.SetBackgroundColor(tcell.ColorDefault)

	overlay := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(frame, 0, 3, false).
			AddItem(nil, 0, 1, false), 90, 0, true).
		AddItem(nil, 0, 1, false)

	return overlay, frame, ooTable, hint
}

func (ui *uiModel) showOpenOrders() {
	if !ui.cfg.hasAccountAuth() {
		ui.state.setModal("Account auth not configured (api_key / api_secret missing)")
		ui.refreshNow()
		return
	}
	ui.openOrdersOpen = true
	ui.pages.ShowPage("openorders")

	ui.openOrdersTable.SetSelectable(true, false)
	ui.openOrdersTable.SetFixed(2, 0)
	ui.openOrdersTable.SetSelectedStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).
		Background(tcell.ColorDarkBlue).
		Attributes(tcell.AttrBold))
	ui.resetOpenOrdersHint()
	ui.app.SetFocus(ui.openOrdersTable)

	ctx, cancel := context.WithCancel(context.Background())
	ui.openOrdersCancel = cancel
	go ui.runOpenOrdersLoop(ctx)
}

func (ui *uiModel) hideOpenOrders() {
	ui.openOrdersOpen = false
	if ui.orderFormOpen {
		ui.orderFormOpen = false
		ui.pages.RemovePage("orderform")
	}
	ui.pages.HidePage("openorders")
	ui.focusChartOrTable()
	if ui.openOrdersCancel != nil {
		ui.openOrdersCancel()
		ui.openOrdersCancel = nil
	}
}

func (ui *uiModel) runOpenOrdersLoop(ctx context.Context) {
	ui.doOpenOrdersFetch(ctx)

	ticker := time.NewTicker(openOrdersRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ui.doOpenOrdersFetch(ctx)
		}
	}
}

func (ui *uiModel) doOpenOrdersFetch(ctx context.Context) {
	client := &http.Client{Timeout: ui.cfg.Timeout}
	futures, spot, err := fetchOpenOrders(ctx, client, ui.cfg)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		ui.app.QueueUpdateDraw(func() {
			ui.renderOpenOrdersError(err.Error())
		})
		return
	}
	ui.app.QueueUpdateDraw(func() {
		ui.renderOpenOrders(futures, spot)
	})
}

func (ui *uiModel) renderOpenOrders(futures, spot []openOrder) {
	ui.openOrdersTable.Clear()
	ui.openOrdersData = nil
	ui.openOrdersSpotData = nil
	ui.openOrdersDataOffset = 0
	ui.openOrdersSpotDataOffset = 0

	row := 0
	headers := []string{"SYMBOL", "SIDE", "TYPE", "PRICE", "QTY", "FILLED", "TIF", "STATUS", "TIME"}

	setHeader := func(text string, col int) {
		cell := tview.NewTableCell(ui.ooCell("[::b][yellow]", text)).
			SetSelectable(false).
			SetBackgroundColor(tcell.ColorDefault).
			SetExpansion(1)
		ui.openOrdersTable.SetCell(row, col, cell)
	}

	setCell := func(text, color string, r, col int) {
		cell := tview.NewTableCell(ui.ooCell(color, text)).
			SetSelectable(true).
			SetBackgroundColor(tcell.ColorDefault).
			SetExpansion(1)
		ui.openOrdersTable.SetCell(r, col, cell)
	}

	renderSection := func(title string, orders []openOrder) int {
		sectionStart := row
		titleCell := tview.NewTableCell(ui.ooCell("[::b][white]", title)).
			SetSelectable(false).
			SetBackgroundColor(tcell.ColorDefault).
			SetExpansion(len(headers))
		ui.openOrdersTable.SetCell(row, 0, titleCell)
		row++

		for col, h := range headers {
			setHeader(h, col)
		}
		row++

		dataStart := row

		if len(orders) == 0 {
			cell := tview.NewTableCell(ui.ooCell("[gray]", "no open orders")).
				SetSelectable(false).
				SetBackgroundColor(tcell.ColorDefault).
				SetExpansion(len(headers))
			ui.openOrdersTable.SetCell(row, 0, cell)
			row++
			_ = sectionStart
			return dataStart
		}

		for _, o := range orders {
			sideColor := "[green]"
			if strings.ToUpper(o.Side) == "SELL" {
				sideColor = "[red]"
			}
			priceStr := format.CompactFloat(o.Price)
			if o.Price == 0 {
				priceStr = "MKT"
			}
			if o.TriggerPrice > 0 {
				rule := "≥"
				if o.TriggerRule == 2 {
					rule = "≤"
				}
				priceStr = fmt.Sprintf("%s (%s%s)", priceStr, rule, format.CompactFloat(o.TriggerPrice))
			}
			timeStr := time.Unix(o.Time/1000, 0).Format("01-02 15:04")

			setCell(o.Symbol, "[white]", row, 0)
			setCell(strings.ToUpper(o.Side), sideColor, row, 1)
			setCell(strings.ToUpper(o.Type), "[white]", row, 2)
			setCell(priceStr, "[white]", row, 3)
			setCell(format.CompactFloat(o.OrigQty), "[white]", row, 4)
			setCell(format.CompactFloat(o.FilledQty), "[gray]", row, 5)
			setCell(o.TimeInForce, "[gray]", row, 6)
			setCell(strings.ToUpper(o.Status), "[white]", row, 7)
			setCell(timeStr, "[gray]", row, 8)
			row++
		}
		return dataStart
	}

	exchangeLabel := "Binance"
	spotSectionTitle := "Binance Spot"
	switch {
	case ui.cfg.isGate():
		exchangeLabel = "Gate.io"
		spotSectionTitle = "Gate.io Spot"
	case ui.cfg.isOKX():
		exchangeLabel = "OKX"
		spotSectionTitle = "OKX Spot"
	case ui.cfg.isBitget():
		exchangeLabel = "Bitget"
		spotSectionTitle = "Bitget Spot"
	}
	dataStart := renderSection(exchangeLabel+" Futures", futures)
	ui.openOrdersData = futures
	ui.openOrdersDataOffset = dataStart
	spotStart := renderSection(spotSectionTitle, spot)
	ui.openOrdersSpotData = spot
	ui.openOrdersSpotDataOffset = spotStart
	if len(futures) > 0 {
		ui.openOrdersTable.Select(dataStart, 0)
	} else if len(spot) > 0 {
		ui.openOrdersTable.Select(spotStart, 0)
	}
}

func (ui *uiModel) renderOpenOrdersError(msg string) {
	ui.openOrdersTable.Clear()
	cell := tview.NewTableCell(ui.ooCell("[red]", "fetch error: "+msg)).
		SetSelectable(false).
		SetBackgroundColor(tcell.ColorDefault).
		SetExpansion(1)
	ui.openOrdersTable.SetCell(0, 0, cell)
}

func (ui *uiModel) ooCell(colorTag, text string) string {
	if ui.cfg.NoColor {
		return text
	}
	return colorTag + escapeTView(text) + "[-]"
}

// ── Gate.io order management helpers ──────────────────────────────────────────

func (ui *uiModel) resetOpenOrdersHint() {
	ui.openOrdersHint.SetText("n new (Tab panel)  d cancel  e edit  r refresh  Esc/u close")
}

func (ui *uiModel) selectedOpenOrder() (openOrder, panelMode, bool) {
	row, _ := ui.openOrdersTable.GetSelection()
	if len(ui.openOrdersData) > 0 && ui.openOrdersDataOffset > 0 {
		idx := row - ui.openOrdersDataOffset
		if idx >= 0 && idx < len(ui.openOrdersData) {
			return ui.openOrdersData[idx], panelFutures, true
		}
	}
	if len(ui.openOrdersSpotData) > 0 && ui.openOrdersSpotDataOffset > 0 {
		idx := row - ui.openOrdersSpotDataOffset
		if idx >= 0 && idx < len(ui.openOrdersSpotData) {
			return ui.openOrdersSpotData[idx], panelSpot, true
		}
	}
	return openOrder{}, panelFutures, false
}

func (ui *uiModel) closeOrderForm() {
	ui.orderFormOpen = false
	ui.pages.RemovePage("orderform")
	ui.app.SetFocus(ui.openOrdersTable)
}

func (ui *uiModel) showNewOrderForm() {
	_, _, _, _, _, chartSymbol, _, _, _, _, _, _, _, _, _, _, _, panel, _ := ui.state.snapshot()
	isSpot := panel == panelSpot

	selectedStyle := tcell.StyleDefault.
		Foreground(tcell.ColorWhite).
		Background(tcell.ColorDarkBlue).
		Attributes(tcell.AttrBold)
	unselectedStyle := tcell.StyleDefault.
		Foreground(tcell.ColorWhite).
		Background(tcell.ColorDefault)
	numFilter := func(_ string, ch rune) bool {
		return ch == '.' || (ch >= '0' && ch <= '9')
	}

	form := tview.NewForm()
	symbolLabel := "Symbol"
	if ui.cfg.isGate() {
		if isSpot {
			symbolLabel = "Pair"
		} else {
			symbolLabel = "Contract"
		}
	}
	form.AddInputField(symbolLabel, chartSymbol, 20, nil, nil)

	sideOptions := []string{"BUY (long)", "SELL (short)"}
	if isSpot {
		sideOptions = []string{"BUY", "SELL"}
	}

	formReady := false
	form.AddDropDown("Side", sideOptions, 0, nil)
	form.GetFormItem(1).(*tview.DropDown).SetListStyles(unselectedStyle, selectedStyle)

	form.AddDropDown("Type", []string{"LIMIT", "MARKET"}, 0, func(option string, _ int) {
		if !formReady {
			return
		}
		priceField := form.GetFormItem(3).(*tview.InputField)
		if option == "MARKET" {
			priceField.SetText("")
			priceField.SetDisabled(true)
		} else {
			priceField.SetDisabled(false)
		}
	})
	form.GetFormItem(2).(*tview.DropDown).SetListStyles(unselectedStyle, selectedStyle)

	form.AddInputField("Price", "", 20, numFilter, nil)

	sizeInputFilter := numFilter
	if ui.cfg.isGate() && !isSpot {
		sizeInputFilter = func(_ string, ch rune) bool {
			return ch >= '0' && ch <= '9'
		}
	}
	form.AddInputField("Size", "1", 15, sizeInputFilter, nil)

	sizeFieldIdx := 4
	reduceFieldIdx := -1
	if !isSpot {
		form.AddDropDown("Reduce Only", []string{"No", "Yes"}, 0, nil)
		reduceFieldIdx = 5
		form.GetFormItem(reduceFieldIdx).(*tview.DropDown).SetListStyles(unselectedStyle, selectedStyle)
	}
	formReady = true

	form.AddButton("Submit", func() {
		sym := strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText())
		if sym == "" {
			return
		}
		sideIdx, _ := form.GetFormItem(1).(*tview.DropDown).GetCurrentOption()
		_, typeName := form.GetFormItem(2).(*tview.DropDown).GetCurrentOption()
		priceStr := strings.TrimSpace(form.GetFormItem(3).(*tview.InputField).GetText())
		rawSize := strings.TrimSpace(form.GetFormItem(sizeFieldIdx).(*tview.InputField).GetText())
		reduceOnly := false
		if reduceFieldIdx >= 0 {
			reduceIdx, _ := form.GetFormItem(reduceFieldIdx).(*tview.DropDown).GetCurrentOption()
			reduceOnly = reduceIdx == 1
		}
		market := typeName == "MARKET"

		if !market {
			if _, err := strconv.ParseFloat(priceStr, 64); err != nil {
				return
			}
		}
		if ui.cfg.isGate() && !isSpot {
			if _, err := strconv.ParseInt(rawSize, 10, 64); err != nil {
				return
			}
		} else if _, err := strconv.ParseFloat(rawSize, 64); err != nil {
			return
		}

		side := "BUY"
		if sideIdx == 1 {
			side = "SELL"
		}

		ui.closeOrderForm()
		ui.openOrdersHint.SetText("Placing order...")
		go func() {
			client := &http.Client{Timeout: ui.cfg.Timeout}
			_, err := placeOrderForPanel(context.Background(), client, ui.cfg, panel, sym, side, typeName, rawSize, priceStr, reduceOnly)
			if err != nil {
				ui.app.QueueUpdateDraw(func() {
					ui.renderOpenOrdersError("place order: " + err.Error())
					ui.resetOpenOrdersHint()
				})
				return
			}
			ui.doOpenOrdersFetch(context.Background())
			ui.app.QueueUpdateDraw(func() { ui.resetOpenOrdersHint() })
		}()
	})

	form.AddButton("Cancel", func() { ui.closeOrderForm() })
	title := " New Futures Order "
	formHeight := 17
	if isSpot {
		title = " New Spot Order "
		formHeight = 15
	}
	form.SetBorder(true).SetTitle(title).SetTitleAlign(tview.AlignCenter)
	form.SetBackgroundColor(tcell.ColorDefault)

	hint := tview.NewTextView().SetDynamicColors(true)
	hint.SetBackgroundColor(tcell.ColorDefault)
	hintText := "[gray]Tab/Enter next field  Shift+Tab back  Esc cancel\nDropDown: Enter open, ↑↓ select, Enter confirm[-]"
	if isSpot {
		hintText = "[gray]Tab switches futures/spot panel before n  |  Tab/Enter next  Shift+Tab back  Esc cancel\nDropDown: Enter open, ↑↓ select, Enter confirm[-]"
	}
	hint.SetText(hintText)

	overlay := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, formHeight, 0, true).
			AddItem(hint, 2, 0, false).
			AddItem(nil, 0, 1, false), 50, 0, true).
		AddItem(nil, 0, 1, false)

	ui.orderFormOpen = true
	ui.pages.AddPage("orderform", overlay, true, true)
	ui.app.SetFocus(form)
}

func (ui *uiModel) showCancelOrderConfirm() {
	order, panel, ok := ui.selectedOpenOrder()
	if !ok {
		return
	}

	modal := tview.NewModal().
		SetText(fmt.Sprintf("Cancel order #%d?\n%s %s @ %s",
			order.OrderID, order.Side, order.Symbol, format.CompactFloat(order.Price))).
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(_ int, label string) {
			ui.closeOrderForm()
			if label != "Yes" {
				return
			}
			ui.openOrdersHint.SetText("Cancelling order...")
			go func() {
				client := &http.Client{Timeout: ui.cfg.Timeout}
				err := cancelOrderForPanel(context.Background(), client, ui.cfg, panel, order)
				if err != nil {
					ui.app.QueueUpdateDraw(func() {
						ui.renderOpenOrdersError("cancel order: " + err.Error())
						ui.resetOpenOrdersHint()
					})
					return
				}
				ui.doOpenOrdersFetch(context.Background())
				ui.app.QueueUpdateDraw(func() {
					ui.resetOpenOrdersHint()
				})
			}()
		})
	modal.SetBackgroundColor(tcell.ColorDefault)

	ui.orderFormOpen = true
	ui.pages.AddPage("orderform", modal, true, true)
	ui.app.SetFocus(modal)
}

func (ui *uiModel) showEditPriceForm() {
	order, panel, ok := ui.selectedOpenOrder()
	if !ok {
		return
	}
	if order.Type != "LIMIT" {
		return
	}

	numFilter := func(_ string, ch rune) bool {
		return ch == '.' || (ch >= '0' && ch <= '9')
	}

	form := tview.NewForm()
	form.AddInputField("New Price", format.CompactFloat(order.Price), 20, numFilter, nil)
	showQty := !ui.cfg.isGate() && !ui.cfg.isOKX() && !ui.cfg.isBitget()
	if showQty {
		form.AddInputField("New Quantity", format.CompactFloat(order.OrigQty), 20, numFilter, nil)
	}
	form.AddButton("Submit", func() {
		newPrice := strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText())
		if _, err := strconv.ParseFloat(newPrice, 64); err != nil {
			return
		}
		newQty := format.CompactFloat(order.OrigQty)
		if showQty {
			newQty = strings.TrimSpace(form.GetFormItem(1).(*tview.InputField).GetText())
			if _, err := strconv.ParseFloat(newQty, 64); err != nil {
				return
			}
		}

		ui.closeOrderForm()
		ui.openOrdersHint.SetText("Amending order...")

		go func() {
			client := &http.Client{Timeout: ui.cfg.Timeout}
			err := amendOrderPriceForPanel(context.Background(), client, ui.cfg, panel, order, newPrice, newQty)
			if err != nil {
				ui.app.QueueUpdateDraw(func() {
					ui.renderOpenOrdersError("amend order: " + err.Error())
					ui.resetOpenOrdersHint()
				})
				return
			}
			ui.doOpenOrdersFetch(context.Background())
			ui.app.QueueUpdateDraw(func() {
				ui.resetOpenOrdersHint()
			})
		}()
	})
	form.AddButton("Cancel", func() {
		ui.closeOrderForm()
	})
	form.SetBorder(true).
		SetTitle(fmt.Sprintf(" Edit #%d %s %s @ %s ",
			order.OrderID, order.Side, order.Symbol, format.CompactFloat(order.Price))).
		SetTitleAlign(tview.AlignCenter)
	form.SetBackgroundColor(tcell.ColorDefault)

	formHeight := 9
	if showQty {
		formHeight = 11
	}

	overlay := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, formHeight, 0, true).
			AddItem(nil, 0, 1, false), 50, 0, true).
		AddItem(nil, 0, 1, false)

	ui.orderFormOpen = true
	ui.pages.AddPage("orderform", overlay, true, true)
	ui.app.SetFocus(form)
}

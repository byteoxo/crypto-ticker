package main

// Bitget WebSocket API v2 public: wss://ws.bitget.com/v2/ws/public

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	bitgetInstTypeFutures = "USDT-FUTURES"
	bitgetInstTypeSpot    = "SPOT"
)

type bitgetWSPush struct {
	Action string          `json:"action"`
	Arg    json.RawMessage `json:"arg"`
	Data   json.RawMessage `json:"data"`
	Event  string          `json:"event"`
	Op     string          `json:"op"`
	Code   string          `json:"code"`
	Msg    string          `json:"msg"`
}

func prepareBitgetConn(conn *websocket.Conn, timeout time.Duration) {
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(2 * timeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(2 * timeout))
	})
}

func bitgetReadNextJSON(conn *websocket.Conn, timeout time.Duration) (json.RawMessage, error) {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * timeout))
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		if mt != websocket.TextMessage {
			continue
		}
		if len(msg) == 0 {
			continue
		}
		switch string(msg) {
		case "pong":
			continue
		case "ping":
			_ = conn.SetWriteDeadline(time.Now().Add(timeout))
			_ = conn.WriteMessage(websocket.TextMessage, []byte("pong"))
			continue
		}
		return json.RawMessage(msg), nil
	}
}

func bitgetSendPing(conn *websocket.Conn, timeout time.Duration) error {
	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	return conn.WriteMessage(websocket.TextMessage, []byte("ping"))
}

func bitgetSubscribeArgs(instType, instID, channel string) map[string]string {
	return map[string]string{
		"instType": instType,
		"channel":  channel,
		"instId":   instID,
	}
}

func runBitgetWSLoop(ctx context.Context, cfg config, state *appState, notify func(), getChartSymbol func() string, getChartInterval func() string, getTickerSymbols func() []string, isSpotChartSymbol func(string) bool) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		state.setError("connecting bitget websocket...")
		notify()

		err := consumeBitgetWS(ctx, cfg, state, notify, getChartSymbol, getChartInterval, getTickerSymbols, isSpotChartSymbol)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errResubscribe) {
			continue
		}

		state.setError(fmt.Sprintf("bitget websocket disconnected: %v | retry in %s", err, cfg.RetryDelay))
		notify()

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cfg.RetryDelay):
		}
	}
}

func consumeBitgetWS(ctx context.Context, cfg config, state *appState, notify func(), getChartSymbol func() string, getChartInterval func() string, getTickerSymbols func() []string, isSpotChartSymbol func(string) bool) error {
	chartSym := getChartSymbol()
	if isSpotChartSymbol(chartSym) {
		chartSym = ""
	}
	chartInterval := getChartInterval()

	dialer := newWSDialer(cfg.Timeout)
	conn, _, err := dialer.DialContext(ctx, cfg.WSBase, nil)
	if err != nil {
		return fmt.Errorf("dial bitget websocket: %w", err)
	}
	defer conn.Close()
	prepareBitgetConn(conn, cfg.Timeout)

	args := make([]map[string]string, 0)
	for _, sym := range getTickerSymbols() {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		args = append(args, bitgetSubscribeArgs(bitgetInstTypeFutures, sym, "ticker"))
	}
	if chartSym != "" {
		args = append(args, bitgetSubscribeArgs(bitgetInstTypeFutures, strings.ToUpper(chartSym), bitgetWSCandleChannel(chartInterval)))
	}
	if err := conn.WriteJSON(map[string]interface{}{"op": "subscribe", "args": args}); err != nil {
		return fmt.Errorf("bitget subscribe: %w", err)
	}

	state.clearError()
	notify()

	pingTicker := time.NewTicker(cfg.Timeout)
	defer pingTicker.Stop()
	resubscribeTicker := time.NewTicker(time.Second)
	defer resubscribeTicker.Stop()
	baselineSymbols := strings.Join(getTickerSymbols(), ",") + "|" + chartSym + "|" + chartInterval

	readErrCh := make(chan error, 1)
	go bitgetReadFuturesConn(conn, readErrCh, cfg, state, notify, getChartSymbol, cfg.Timeout)

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"), time.Now().Add(time.Second))
			return nil
		case err := <-readErrCh:
			if err == nil {
				return nil
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, netErrClosed) {
				return err
			}
			return err
		case <-pingTicker.C:
			if err := bitgetSendPing(conn, cfg.Timeout); err != nil {
				return fmt.Errorf("ping bitget websocket: %w", err)
			}
		case <-resubscribeTicker.C:
			curChart := getChartSymbol()
			if isSpotChartSymbol(curChart) {
				curChart = ""
			}
			cur := strings.Join(getTickerSymbols(), ",") + "|" + curChart + "|" + getChartInterval()
			if cur != baselineSymbols {
				state.setError("updating bitget market subscriptions...")
				notify()
				return errResubscribe
			}
		}
	}
}

func bitgetReadFuturesConn(conn *websocket.Conn, readErrCh chan<- error, cfg config, state *appState, notify func(), getChartSymbol func() string, timeout time.Duration) {
	for {
		raw, err := bitgetReadNextJSON(conn, timeout)
		if err != nil {
			readErrCh <- err
			return
		}
		var env bitgetWSPush
		if err := json.Unmarshal(raw, &env); err != nil {
			readErrCh <- fmt.Errorf("decode bitget ws message: %w", err)
			return
		}
		if env.Event != "" {
			if env.Event == "error" || (env.Code != "" && env.Code != "0" && env.Code != "00000") {
				readErrCh <- fmt.Errorf("bitget ws error event: %s %s", env.Code, env.Msg)
				return
			}
			continue
		}
		if env.Op == "pong" {
			continue
		}
		if len(env.Arg) == 0 {
			continue
		}
		var arg struct {
			Channel  string `json:"channel"`
			InstID   string `json:"instId"`
			InstType string `json:"instType"`
		}
		if err := json.Unmarshal(env.Arg, &arg); err != nil || arg.Channel == "" {
			continue
		}
		switch {
		case arg.Channel == "ticker":
			var rows []bitgetWSTickerRow
			if err := json.Unmarshal(env.Data, &rows); err != nil {
				readErrCh <- fmt.Errorf("decode bitget ticker: %w", err)
				return
			}
			for _, row := range rows {
				ticker := bitgetWSTickerToPrice(row)
				state.applyTicker(ticker)
				if fr, ok := bitgetFundingFromTickerRow(row); ok {
					state.setFundingRates([]fundingRate{fr})
				}
				if oi, ok := bitgetOIFromTickerRow(row); ok {
					state.setOpenInterest(oi)
				}
			}
			notify()
		case strings.HasPrefix(arg.Channel, "candle"):
			var rows [][]string
			if err := json.Unmarshal(env.Data, &rows); err != nil {
				readErrCh <- fmt.Errorf("decode bitget candles: %w", err)
				return
			}
			if len(rows) == 0 {
				continue
			}
			candle, err := parseBitgetWSCandle(arg.InstID, rows[len(rows)-1])
			if err != nil {
				readErrCh <- err
				return
			}
			if candle.Symbol == getChartSymbol() {
				state.applyChartCandle(panelFutures, candle, cfg.ChartLimit)
			}
			notify()
		}
	}
}

type bitgetWSTickerRow struct {
	Symbol          string `json:"symbol"`
	LastPr          string `json:"lastPr"`
	Open24h         string `json:"open24h"`
	High24h         string `json:"high24h"`
	Low24h          string `json:"low24h"`
	BaseVolume      string `json:"baseVolume"`
	QuoteVolume     string `json:"quoteVolume"`
	Change24h       string `json:"change24h"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
	MarkPrice       string `json:"markPrice"`
	IndexPrice      string `json:"indexPrice"`
	HoldingAmount   string `json:"holdingAmount"`
	Ts              string `json:"ts"`
}

func bitgetWSTickerToPrice(row bitgetWSTickerRow) priceTicker {
	last, _ := strconv.ParseFloat(row.LastPr, 64)
	open, _ := strconv.ParseFloat(row.Open24h, 64)
	high, _ := strconv.ParseFloat(row.High24h, 64)
	low, _ := strconv.ParseFloat(row.Low24h, 64)
	vol, _ := strconv.ParseFloat(row.BaseVolume, 64)
	if row.QuoteVolume != "" {
		if v, err := strconv.ParseFloat(row.QuoteVolume, 64); err == nil && v > 0 {
			vol = v
		}
	}
	chgPct, _ := strconv.ParseFloat(row.Change24h, 64)
	chgPct *= 100
	var chg float64
	if open > 1e-12 {
		chg = last - open
	}
	ts, _ := strconv.ParseInt(row.Ts, 10, 64)
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	sym := strings.ToUpper(strings.TrimSpace(row.Symbol))
	return priceTicker{
		Symbol:       sym,
		Price:        row.LastPr,
		Time:         ts,
		ChangePct24h: chgPct,
		Change24h:    chg,
		High24h:      high,
		Low24h:       low,
		Volume24h:    vol,
	}
}

func bitgetFundingFromTickerRow(row bitgetWSTickerRow) (fundingRate, bool) {
	if row.Symbol == "" {
		return fundingRate{}, false
	}
	rate, _ := strconv.ParseFloat(row.FundingRate, 64)
	mark, _ := strconv.ParseFloat(row.MarkPrice, 64)
	index, _ := strconv.ParseFloat(row.IndexPrice, 64)
	nextMs, _ := strconv.ParseInt(row.NextFundingTime, 10, 64)
	return fundingRate{
		Symbol:          strings.ToUpper(strings.TrimSpace(row.Symbol)),
		MarkPrice:       mark,
		IndexPrice:      index,
		LastFundingRate: rate,
		NextFundingTime: nextMs,
	}, true
}

func bitgetOIFromTickerRow(row bitgetWSTickerRow) (openInterestData, bool) {
	if row.Symbol == "" || row.HoldingAmount == "" {
		return openInterestData{}, false
	}
	oi, _ := strconv.ParseFloat(row.HoldingAmount, 64)
	ts, _ := strconv.ParseInt(row.Ts, 10, 64)
	return openInterestData{
		Symbol:       strings.ToUpper(strings.TrimSpace(row.Symbol)),
		OpenInterest: oi,
		Time:         ts,
	}, true
}

func parseBitgetWSCandle(instID string, row []string) (klineCandle, error) {
	if len(row) < 6 {
		return klineCandle{}, fmt.Errorf("bitget candle row too short")
	}
	openMs, err := strconv.ParseInt(row[0], 10, 64)
	if err != nil {
		return klineCandle{}, err
	}
	symbol := strings.ToUpper(strings.TrimSpace(instID))
	vol := row[5]
	candle, err := newKlineCandle(symbol, openMs, openMs, row[1], row[2], row[3], row[4], vol, true)
	if err != nil {
		return klineCandle{}, err
	}
	return candle, nil
}

func runBitgetSpotWSLoop(ctx context.Context, cfg config, state *appState, notify func(), getSpotTickerSymbols func() []string, spotWSBase string) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		state.setSpotError("connecting bitget spot websocket...")
		notify()

		err := consumeBitgetSpotWS(ctx, cfg, state, notify, getSpotTickerSymbols, getChartSymbolForActivePanel(state), state.getChartInterval, isSpotTickerSymbolFunc(cfg), spotWSBase)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errResubscribe) {
			continue
		}

		state.setSpotError(fmt.Sprintf("bitget spot websocket disconnected: %v | retry in %s", err, cfg.RetryDelay))
		notify()

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cfg.RetryDelay):
		}
	}
}

func consumeBitgetSpotWS(ctx context.Context, cfg config, state *appState, notify func(), getSpotTickerSymbols func() []string, getChartSymbol func() string, getChartInterval func() string, isSpotChartSymbol func(string) bool, wsBase string) error {
	chartSym := getChartSymbol()
	if !isSpotChartSymbol(chartSym) {
		chartSym = ""
	}
	chartInterval := getChartInterval()

	dialer := newWSDialer(cfg.Timeout)
	conn, _, err := dialer.DialContext(ctx, wsBase, nil)
	if err != nil {
		return fmt.Errorf("dial bitget spot websocket: %w", err)
	}
	defer conn.Close()
	prepareBitgetConn(conn, cfg.Timeout)

	args := make([]map[string]string, 0)
	for _, sym := range getSpotTickerSymbols() {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		args = append(args, bitgetSubscribeArgs(bitgetInstTypeSpot, sym, "ticker"))
	}
	if chartSym != "" {
		args = append(args, bitgetSubscribeArgs(bitgetInstTypeSpot, strings.ToUpper(chartSym), bitgetWSCandleChannel(chartInterval)))
	}
	if err := conn.WriteJSON(map[string]interface{}{"op": "subscribe", "args": args}); err != nil {
		return fmt.Errorf("bitget spot subscribe: %w", err)
	}

	state.clearSpotError()
	notify()

	pingTicker := time.NewTicker(cfg.Timeout)
	defer pingTicker.Stop()
	resubscribeTicker := time.NewTicker(time.Second)
	defer resubscribeTicker.Stop()
	baselineSymbols := strings.Join(getSpotTickerSymbols(), ",") + "|" + chartSym + "|" + chartInterval

	readErrCh := make(chan error, 1)
	go bitgetReadSpotConn(conn, readErrCh, cfg, state, notify, getChartSymbol, isSpotChartSymbol, cfg.Timeout)

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"), time.Now().Add(time.Second))
			return nil
		case err := <-readErrCh:
			if err == nil {
				return nil
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, netErrClosed) {
				return err
			}
			return err
		case <-pingTicker.C:
			if err := bitgetSendPing(conn, cfg.Timeout); err != nil {
				return fmt.Errorf("ping bitget spot websocket: %w", err)
			}
		case <-resubscribeTicker.C:
			curChart := getChartSymbol()
			if !isSpotChartSymbol(curChart) {
				curChart = ""
			}
			cur := strings.Join(getSpotTickerSymbols(), ",") + "|" + curChart + "|" + getChartInterval()
			if cur != baselineSymbols {
				state.setSpotError("updating bitget spot subscriptions...")
				notify()
				return errResubscribe
			}
		}
	}
}

func bitgetReadSpotConn(conn *websocket.Conn, readErrCh chan<- error, cfg config, state *appState, notify func(), getChartSymbol func() string, isSpotChartSymbol func(string) bool, timeout time.Duration) {
	for {
		raw, err := bitgetReadNextJSON(conn, timeout)
		if err != nil {
			readErrCh <- err
			return
		}
		var env bitgetWSPush
		if err := json.Unmarshal(raw, &env); err != nil {
			readErrCh <- fmt.Errorf("decode bitget spot ws message: %w", err)
			return
		}
		if env.Event != "" {
			if env.Event == "error" || (env.Code != "" && env.Code != "0" && env.Code != "00000") {
				readErrCh <- fmt.Errorf("bitget spot ws error event: %s %s", env.Code, env.Msg)
				return
			}
			continue
		}
		if env.Op == "pong" {
			continue
		}
		if len(env.Arg) == 0 {
			continue
		}
		var arg struct {
			Channel string `json:"channel"`
			InstID  string `json:"instId"`
		}
		if err := json.Unmarshal(env.Arg, &arg); err != nil || arg.Channel == "" {
			continue
		}
		switch {
		case arg.Channel == "ticker":
			var rows []bitgetWSTickerRow
			if err := json.Unmarshal(env.Data, &rows); err != nil {
				readErrCh <- fmt.Errorf("decode bitget spot ticker: %w", err)
				return
			}
			for _, row := range rows {
				state.applySpotTicker(bitgetWSTickerToPrice(row))
			}
			notify()
		case strings.HasPrefix(arg.Channel, "candle"):
			var rows [][]string
			if err := json.Unmarshal(env.Data, &rows); err != nil {
				readErrCh <- fmt.Errorf("decode bitget spot candles: %w", err)
				return
			}
			if len(rows) == 0 {
				continue
			}
			candle, err := parseBitgetWSCandle(arg.InstID, rows[len(rows)-1])
			if err != nil {
				readErrCh <- err
				return
			}
			if isSpotChartSymbol(candle.Symbol) && candle.Symbol == getChartSymbol() {
				state.applyChartCandle(panelSpot, candle, cfg.ChartLimit)
			}
			notify()
		}
	}
}

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build
go build -o crypto ./cmd/crypto-ticker

# Run (config: ./config.toml > ~/.config/crypto-ticker/config.toml > embedded default)
./crypto

# Lint
go vet ./...

# Tests (no test files currently; run with)
go test ./...
```

Releases are produced via GoReleaser triggered by pushing a `v*` tag. The CI workflow (`.github/workflows/release.yml`) builds cross-platform binaries and updates the Homebrew formula automatically.

## Configuration

The app reads TOML config only — no CLI flags. Config priority: `./config.toml`, `~/.config/crypto-ticker/config.toml`, else the Binance example embedded in the binary via `//go:embed` (`internal/ticker/defaults/config.example.binance.toml`). Copy an exchange example from `configs/` to `config.toml` to customize. All fields in `internal/ticker/config.go:required` slice must be present or the program exits.

## Project Layout

```
cmd/crypto-ticker/     # thin main entry (calls ticker.Run)
configs/               # example TOML configs per exchange
internal/
  format/              # display formatting helpers
  symbol/              # symbol normalization (incl. OKX inst IDs)
  ticker/              # application core: state, UI, WS, REST, charts
    defaults/          # embedded default config (go:embed)
```

## Architecture Overview

Application logic lives in `internal/ticker` (`package ticker`). Shared helpers are in `internal/format` and `internal/symbol`. All market state flows through one central `appState` struct.

### Data Flow

```
Exchange WebSocket ──► *_ws.go ──► appState (state.go)
Exchange REST API  ──► *_api.go ──► appState
                                         │
                                         ▼
                                      ui.go (tview TUI)
                                      chart.go + indicators.go
```

### Key Files (`internal/ticker/`)

| File | Role |
|------|------|
| `run.go` | `Run()` entry, goroutine wiring, `changeChart`/`changeInterval` callbacks, app-level constants |
| `state.go` | `appState` struct + all mutating methods; `snapshot()` is the single read path for the UI |
| `ui.go` | `tview` layout, keyboard bindings, table rendering, order book overlay, help panel |
| `market_ws.go` | Binance futures WebSocket loop with auto-reconnect; `errResubscribe` sentinel |
| `api.go` | Binance REST: klines, positions, spot balances, funding rates, OI, long/short ratio |
| `gate_*.go`, `okx_*.go`, `bitget_*.go` | Gate.io / OKX / Bitget REST + WebSocket |
| `userdata.go` | Binance user data stream (listen key keepalive, position + balance updates) |
| `chart.go` | Candlestick rendering; calls `buildIndicatorLine` and volume bars |
| `indicators.go` | EMA, RSI, Bollinger Bands, MACD — pure functions over `[]klineCandle` |
| `sparkline.go` | Unicode sparkline (`▁▂▃▄▅▆▇█`) from `[]float64` price history |
| `orderbook.go` | REST-polled order book overlay (20 levels) |
| `openorders.go` | Open orders overlay and order management UI |
| `config.go` | TOML parsing, validation, `config` struct |
| `types.go` | JSON payload types for WebSocket messages |

### `appState` and `snapshot()`

`appState` is guarded by `sync.RWMutex`. The UI reads state exclusively via `snapshot()`, which returns 19 values as value copies (no pointers into the map). Every call site must destructure all 19 values — use `_` for unused ones. Adding a new return value requires updating every call site in `run.go`, `ui.go`, `config.go`, and `orderbook.go`.

### WebSocket Resubscription Pattern

`consumeWS` records a `baselineSymbols` string (symbols + chart symbol + interval) at connection time. A 1-second ticker compares the current values; if changed, it returns `errResubscribe`. `runWSLoop` catches this sentinel and immediately reconnects with the new parameters — no sleep delay.

### Dual Panel (Futures / Spot)

- Futures: `cfg.WSBase` / `cfg.RESTBase` (exchange-specific defaults in `run.go`)
- Spot: per-exchange spot WS/REST URLs selected in `run.go`
- Chart state is kept separately as `futuresChart`/`spotChart` and `futuresChartSymbol`/`spotChartSymbol`; `snapshot()` returns the active panel's values

### Chart Intervals

Supported: `1h 2h 4h 1d 3d` (slice in `run.go:chartIntervals`). Interval is stored in `appState.chartInterval`. On change: both charts are set to `nil`, history is reloaded via REST, and WS resubscribes to the new kline stream.

### Signed REST Requests (Account)

`buildSignedURL` in `api.go` signs Binance requests with HMAC-SHA256 using `cfg.APISecret`. Gate/OKX/Bitget use their own signing in `gate_api.go`, `okx_api.go`, `bitget_api.go`. Account endpoints are only used when `cfg.hasAccountAuth()` is true.

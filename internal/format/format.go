package format

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	// Embed IANA timezone database so time.LoadLocation works on Windows
	// and other systems that do not ship zoneinfo files.
	_ "time/tzdata"
)

func CompactFloat(value float64) string {
	abs := math.Abs(value)
	precision := 6
	switch {
	case abs >= 1000:
		precision = 2
	case abs >= 1:
		precision = 4
	case abs >= 0.01:
		precision = 5
	}
	return trimTrailingZeros(strconv.FormatFloat(value, 'f', precision, 64))
}

func SignedCompactFloat(value float64) string {
	formatted := CompactFloat(math.Abs(value))
	switch {
	case value > 0:
		return "+" + formatted
	case value < 0:
		return "-" + formatted
	default:
		return formatted
	}
}

func OptionalCompactFloat(value float64) string {
	if math.Abs(value) < 1e-12 {
		return "-"
	}
	return CompactFloat(value)
}

func Epoch(timestampMS int64, loc *time.Location) string {
	if timestampMS <= 0 {
		return "-"
	}
	return time.UnixMilli(timestampMS).In(loc).Format("2006-01-02 15:04:05.000 MST")
}

func OptionalTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "-"
	}
	return Time(t, loc, true)
}

func Time(t time.Time, loc *time.Location, millis bool) string {
	if millis {
		return t.In(loc).Format("2006-01-02 15:04:05.000 MST")
	}
	return t.In(loc).Format("2006-01-02 15:04:05 MST")
}

func MustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		log.Fatalf("fatal: load timezone %q: %v", name, err)
	}
	return loc
}

// CompactNumber formats large numbers with K/M/B suffixes (e.g. 1234567 → "1.23M").
func CompactNumber(v float64) string {
	abs := math.Abs(v)
	switch {
	case abs >= 1e9:
		return trimTrailingZeros(fmt.Sprintf("%.2fB", v/1e9))
	case abs >= 1e6:
		return trimTrailingZeros(fmt.Sprintf("%.2fM", v/1e6))
	case abs >= 1e3:
		return trimTrailingZeros(fmt.Sprintf("%.2fK", v/1e3))
	default:
		return trimTrailingZeros(fmt.Sprintf("%.2f", v))
	}
}

func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func trimTrailingZeros(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if value == "-0" || value == "+0" || value == "" {
		return "0"
	}
	return value
}

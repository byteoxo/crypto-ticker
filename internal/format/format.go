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

// MustLoadLocation loads an IANA timezone (e.g. "Asia/Shanghai") or a fixed
// offset such as "UTC+8", "UTC+08:00", or "+08:00". It fatals on failure.
//
// IANA names require either system zoneinfo or the embedded database from
// importing time/tzdata (done in main and this package).
func MustLoadLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		log.Fatalf("fatal: load timezone: empty name")
	}

	loc, err := time.LoadLocation(name)
	if err == nil {
		return loc
	}

	// Fixed offsets work without zoneinfo files (useful when a binary was
	// built without time/tzdata on a system that also lacks /usr/share/zoneinfo).
	if fixed, ok := parseFixedOffsetLocation(name); ok {
		return fixed
	}

	log.Fatalf("fatal: load timezone %q: %v\n"+
		"hint: use an IANA name (Asia/Shanghai) or a fixed offset (UTC+8, +08:00).\n"+
		"if IANA names fail, rebuild with embedded tzdata: go build -tags timetzdata", name, err)
	return nil
}

// parseFixedOffsetLocation accepts UTC±H, UTC±HH:MM, GMT±…, or ±HH:MM / ±H.
func parseFixedOffsetLocation(name string) (*time.Location, bool) {
	s := strings.TrimSpace(name)
	if s == "" {
		return nil, false
	}

	upper := strings.ToUpper(s)
	switch {
	case strings.HasPrefix(upper, "UTC"):
		s = strings.TrimSpace(s[3:])
	case strings.HasPrefix(upper, "GMT"):
		s = strings.TrimSpace(s[3:])
	}
	if s == "" {
		// bare "UTC" / "GMT"
		if strings.EqualFold(name, "UTC") || strings.EqualFold(name, "GMT") {
			return time.UTC, true
		}
		return nil, false
	}

	sign := 1
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		sign = -1
		s = s[1:]
	default:
		return nil, false
	}
	if s == "" {
		return nil, false
	}

	var hours, mins int
	if i := strings.IndexByte(s, ':'); i >= 0 {
		h, err1 := strconv.Atoi(s[:i])
		m, err2 := strconv.Atoi(s[i+1:])
		if err1 != nil || err2 != nil || h > 23 || m > 59 {
			return nil, false
		}
		hours, mins = h, m
	} else {
		h, err := strconv.Atoi(s)
		if err != nil || h > 23 {
			return nil, false
		}
		hours = h
	}

	offset := sign * (hours*3600 + mins*60)
	label := name
	if strings.EqualFold(strings.TrimSpace(name), "UTC") || strings.EqualFold(strings.TrimSpace(name), "GMT") {
		label = "UTC"
	}
	return time.FixedZone(label, offset), true
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

package format

import (
	"testing"
	"time"
)

func TestParseFixedOffsetLocation(t *testing.T) {
	cases := []struct {
		in      string
		ok      bool
		offset  int
		wantUTC bool
	}{
		{"UTC+8", true, 8 * 3600, false},
		{"UTC+08:00", true, 8 * 3600, false},
		{"utc-5", true, -5 * 3600, false},
		{"GMT+9", true, 9 * 3600, false},
		{"+08:00", true, 8 * 3600, false},
		{"-05:30", true, -(5*3600 + 30*60), false},
		{"UTC", true, 0, true},
		{"Asia/Shanghai", false, 0, false},
		{"", false, 0, false},
		{"UTC+25", false, 0, false},
	}
	for _, tc := range cases {
		loc, ok := parseFixedOffsetLocation(tc.in)
		if ok != tc.ok {
			t.Fatalf("%q: ok=%v want %v", tc.in, ok, tc.ok)
		}
		if !tc.ok {
			continue
		}
		if tc.wantUTC {
			if loc != time.UTC {
				t.Fatalf("%q: want time.UTC, got %v", tc.in, loc)
			}
			continue
		}
		// Probe offset via a fixed instant.
		_, offset := time.Date(2024, 1, 1, 12, 0, 0, 0, loc).Zone()
		if offset != tc.offset {
			t.Fatalf("%q: offset=%d want %d", tc.in, offset, tc.offset)
		}
	}
}

func TestMustLoadLocationIANA(t *testing.T) {
	// Relies on time/tzdata blank import in this package (and/or system zoneinfo).
	loc := MustLoadLocation("Asia/Shanghai")
	if loc == nil {
		t.Fatal("nil location")
	}
	_, offset := time.Date(2024, 6, 1, 12, 0, 0, 0, loc).Zone()
	if offset != 8*3600 {
		t.Fatalf("Asia/Shanghai offset=%d want %d", offset, 8*3600)
	}
}

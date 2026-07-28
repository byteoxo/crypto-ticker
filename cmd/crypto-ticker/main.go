package main

import (
	"log"

	// Embed the IANA timezone database in the binary so config values like
	// tz = "Asia/Shanghai" work on Windows, Alpine, and other systems that
	// do not ship zoneinfo files. Prefer importing from main (Go recommendation).
	_ "time/tzdata"

	"crypto-ticker/internal/ticker"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("")

	if err := ticker.Run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

package main

import (
	"log"

	"crypto-ticker/internal/ticker"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("")

	if err := ticker.Run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

// Command migrate applies database migrations.
//
//	go run ./cmd/migrate up
//	go run ./cmd/migrate down
//	go run ./cmd/migrate status
package main

import (
	"fmt"
	"os"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/config"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/database"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: migrate <up|down|status|redo|reset|version> [args]")
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := database.Migrate(cfg.DatabaseURL, os.Args[1], os.Args[2:]...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

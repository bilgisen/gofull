// FILE: cmd/server/main.go
package main

import (
	"flag"
	"fmt"
	"os"

	"gofull/internal/app"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	cfg := app.DefaultConfig()
	// Allow overriding port via PORT env (useful for platforms)
	if p := os.Getenv("PORT"); p != "" {
		*addr = ":" + p
	}

	srv, err := app.NewServer(cfg)
	if err != nil {
		fmt.Printf("failed to initialize server: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Run(*addr); err != nil {
		fmt.Printf("server exited with error: %v\n", err)
		os.Exit(1)
	}
}

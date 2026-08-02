// Package main is the champs entry point: it sets the default slog
// handler, hands off to internal/cli, and exits with the code the CLI
// contract promises CI.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/donaldgifford/champs/internal/cli"
)

// Injected via -ldflags at release time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	os.Exit(cli.Execute(context.Background(), cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}))
}

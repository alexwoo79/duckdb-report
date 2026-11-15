package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/alexwoo79/duckdb-report/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	exitCode := cli.Execute(ctx, os.Args[1:], nil, os.Stdout, os.Stderr)
	stop()
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

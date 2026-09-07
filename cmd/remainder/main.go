package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/douglasjarquin/remainder/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr, version))
}

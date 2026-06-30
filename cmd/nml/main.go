package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/natelindev/no-mistakes-lite/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(app.Main(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

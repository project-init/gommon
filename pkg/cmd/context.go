package cmd

import (
	"context"
	"os/signal"
	"syscall"
)

func SetupMainContext() context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx
}

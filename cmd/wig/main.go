package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/subcommands"
)

func syntaxErr(msg string) subcommands.ExitStatus {
	log.Printf("invocation error: %s", msg)
	return subcommands.ExitUsageError
}

func fatalErr(err error) subcommands.ExitStatus {
	if err != nil {
		log.Printf("fatal error: %v", err)
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

func init() {
	subcommands.Register(subcommands.HelpCommand(), "documentation")
}

func withInterruptibleContext(ctx context.Context, f func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	go func() {
		<-sigCh
		log.Printf("terminating due to signal")
		cancel()
	}()
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	f(ctx)
}

func main() {
	log.SetFlags(0)
	flag.Parse()

	var status int
	withInterruptibleContext(
		context.Background(),
		func(ctx context.Context) {
			status = int(subcommands.Execute(ctx))
		},
	)
	os.Exit(status)
}

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"git.autistici.org/ai3/tools/wig/util"
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
	subcommands.ImportantFlag("config")
	subcommands.Register(subcommands.HelpCommand(), "documentation")
	subcommands.Register(subcommands.FlagsCommand(), "documentation")
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

var configFilePath = flag.String("config", "", "config file with flags (default: /etc/wig.conf, ~/.wig.conf if they exist)")

func main() {
	log.SetFlags(0)
	flag.Parse()

	if err := util.LoadFlagsFromConfig(*configFilePath); err != nil {
		log.Fatal(err)
	}

	var status int
	withInterruptibleContext(
		context.Background(),
		func(ctx context.Context) {
			status = int(subcommands.Execute(ctx))
		},
	)
	os.Exit(status)
}

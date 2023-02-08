package main

import (
	"context"
	"flag"

	"git.autistici.org/ai3/attic/wig/collector"
	"git.autistici.org/ai3/attic/wig/datastore/crudlog"
	"git.autistici.org/ai3/attic/wig/datastore/model"
	"git.autistici.org/ai3/attic/wig/gateway"
	"git.autistici.org/ai3/attic/wig/util"
	"github.com/google/subcommands"
)

type gwCommand struct {
	util.ClientCommand

	logURL urlFlag
}

func (c *gwCommand) Name() string     { return "gw" }
func (c *gwCommand) Synopsis() string { return "run the gateway node" }
func (c *gwCommand) Usage() string {
	return `gw
        Run the VPN gateway node.

`
}

func (c *gwCommand) SetFlags(f *flag.FlagSet) {
	f.Var(&c.logURL, "log-url", "`URL` for the log API")

	c.ClientCommand.SetFlags(f)
}

func (c *gwCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if c.logURL == "" {
		return syntaxErr("must specify --log-url")
	}

	return fatalErr(c.run(ctx))
}

func (c *gwCommand) run(ctx context.Context) error {
	client, err := c.HTTPClient()
	if err != nil {
		return err
	}

	rlog := crudlog.NewRemoteLogSource(string(c.logURL), model.Model.Encoding(), client)
	rstats := collector.NewStatsCollectorStub(string(c.logURL), client)

	gw, err := gateway.New(rstats)
	if err != nil {
		return err
	}
	defer gw.Close()

	if err := crudlog.Follow(ctx, rlog, gw); err != nil {
		return err
	}

	return nil
}

func init() {
	subcommands.Register(&gwCommand{}, "")
}

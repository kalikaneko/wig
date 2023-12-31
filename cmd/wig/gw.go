package main

import (
	"context"
	"flag"
	"net/http"

	"git.autistici.org/ai3/tools/wig/datastore/crudlog"
	"git.autistici.org/ai3/tools/wig/datastore/model"
	"git.autistici.org/ai3/tools/wig/datastore/sessions"
	"git.autistici.org/ai3/tools/wig/gateway"
	"git.autistici.org/ai3/tools/wig/util"
	"github.com/google/subcommands"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
)

type gwCommand struct {
	util.ClientCommand

	logURL    string
	statusURL string
	httpAddr  string
}

func (c *gwCommand) Name() string     { return "gateway" }
func (c *gwCommand) Synopsis() string { return "run the gateway node" }
func (c *gwCommand) Usage() string {
	return `gw
        Run the VPN gateway node.

`
}

func (c *gwCommand) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.logURL, "log-url", "", "`URL` for the log API")
	f.StringVar(&c.statusURL, "status-url", "", "`URL` for the status API (defaults to --log-url)")
	f.StringVar(&c.httpAddr, "metrics-addr", ":4007", "listen address for the metrics HTTP server")

	c.ClientCommand.SetFlags(f)
}

func (c *gwCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if c.logURL == "" {
		return syntaxErr("must specify --log-url")
	}
	if c.statusURL == "" {
		c.statusURL = c.logURL
	}

	return fatalErr(c.run(ctx))
}

func (c *gwCommand) run(ctx context.Context) error {
	client, err := c.HTTPClient()
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	rlog := crudlog.NewRemoteLogSource(c.logURL, model.Model.Encoding(), client)
	rstats := sessions.NewStatsCollectorStub(c.statusURL, client)

	gw, err := gateway.New(rstats)
	if err != nil {
		return err
	}
	defer gw.Close()

	prometheus.MustRegister(gw)

	g.Go(func() error {
		return crudlog.Follow(ctx, rlog, gw)
	})

	g.Go(func() error {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		server := makeHTTPServer(mux, c.httpAddr, nil)
		return runHTTPServerWithContext(ctx, server)
	})

	return g.Wait()
}

func init() {
	subcommands.Register(&gwCommand{}, "")
	subcommands.Register(subcommands.Alias("gw", &gwCommand{}), "")
}

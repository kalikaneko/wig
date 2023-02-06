package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"git.autistici.org/ai3/attic/wig/collector"
	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/crud"
	"git.autistici.org/ai3/attic/wig/datastore/crud/httpapi"
	"git.autistici.org/ai3/attic/wig/datastore/crud/httptransport"
	"git.autistici.org/ai3/attic/wig/datastore/crudlog"
	"git.autistici.org/ai3/attic/wig/datastore/model"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"git.autistici.org/ai3/attic/wig/util"
	"github.com/google/subcommands"
	"golang.org/x/sync/errgroup"
)

type apiCommand struct {
	util.ClientTLSFlags
	util.ServerTLSFlags

	addr      string
	dburi     string
	maxLogAge time.Duration
	logURL    urlFlag
}

func (c *apiCommand) Name() string     { return "api" }
func (c *apiCommand) Synopsis() string { return "run the datastore API server" }
func (c *apiCommand) Usage() string {
	return `api
        Run the datastore API server.

`
}

func (c *apiCommand) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.addr, "addr", ":5005", "`address` to listen on")
	f.StringVar(&c.dburi, "db", "", "`path` to the database file")
	f.DurationVar(&c.maxLogAge, "max-log-age", 120*24*time.Hour, "maximum age of log entries")
	f.Var(&c.logURL, "log-url", "`URL` for pull replication")

	c.ServerTLSFlags.SetFlags(f)
	c.ClientTLSFlags.SetFlags(f)
}

func (c *apiCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if c.dburi == "" {
		return syntaxErr("must specify a database path")
	}

	return fatalErr(c.run(ctx))
}

func (c *apiCommand) run(ctx context.Context) error {
	sql, err := sqlite.OpenDB(c.dburi, datastore.Migrations)
	if err != nil {
		return err
	}
	defer sql.Close()

	logdb := crudlog.Wrap(
		sql,
		model.Model,
		model.Model.Encoding(),
	)
	api := crud.Combine(
		crud.NewSQL(model.Model, sql),
		logdb,
	)
	//defer logdb.Close()

	stats, err := collector.NewStatsReceiver(sql)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	logURL := string(c.logURL)
	if logURL != "" {
		tlsConf, err := c.TLSClientConfig()
		if err != nil {
			return err
		}

		rlog := crudlog.NewRemoteLogSource(logURL, model.Model.Encoding(),
			httptransport.NewClient(tlsConf))

		//db.SetReadonly()
		g.Go(func() error {
			return crudlog.Follow(ctx, rlog, logdb)
		})
	}

	tlsConf, err := c.TLSServerConfig()
	if err != nil {
		return err
	}
	g.Go(func() error {
		httpAPI := httpapi.New()
		httpAPI.Add(model.Model.API(
			api,
			apiURLBase,
		))
		logH := crudlog.NewLogSourceHTTPHandler(
			logdb,
			model.Model.Encoding(),
		)
		defer logH.Close()
		httpAPI.Add(logH)
		if logURL == "" {
			httpAPI.Add(stats)
		}
		server := &http.Server{
			Addr:              c.addr,
			Handler:           httpAPI,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       900 * time.Second,
			TLSConfig:         tlsConf,
		}
		return runHTTPServerWithContext(ctx, server)
	})

	return g.Wait()
}

func init() {
	subcommands.Register(&apiCommand{}, "")
}

func runHTTPServerWithContext(ctx context.Context, server *http.Server) error {
	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Printf("shutting down HTTP server")
		if err := server.Shutdown(ctx); err != nil {
			server.Close() // nolint: errcheck
		}
	}()

	log.Printf("starting HTTP server on %s", server.Addr)
	return server.ListenAndServe()
}

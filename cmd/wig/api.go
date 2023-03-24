package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"log"
	"net/http"
	"time"

	"git.autistici.org/ai3/tools/wig/datastore"
	"git.autistici.org/ai3/tools/wig/datastore/crud"
	"git.autistici.org/ai3/tools/wig/datastore/crud/httpapi"
	"git.autistici.org/ai3/tools/wig/datastore/crudlog"
	"git.autistici.org/ai3/tools/wig/datastore/expire"
	"git.autistici.org/ai3/tools/wig/datastore/model"
	"git.autistici.org/ai3/tools/wig/datastore/registration"
	"git.autistici.org/ai3/tools/wig/datastore/sessions"
	"git.autistici.org/ai3/tools/wig/datastore/sqlite"
	"git.autistici.org/ai3/tools/wig/util"
	"github.com/google/subcommands"
	"github.com/jmoiron/sqlx"
	"golang.org/x/sync/errgroup"
)

var rbacRules = map[string][]string{
	"admin": []string{
		"write-peer", "read-peer",
		"write-interface", "read-interface",
		"write-token", "read-token",
		"write-sessions", "read-sessions",
		"read-log",
		"register-peer",
	},
	"follower": []string{
		"read-log",
	},
	"registrar": []string{
		"register-peer",
	},
}

type apiCommand struct {
	util.ServerTLSFlags
	util.ClientCommand

	addr            string
	dburi           string
	maxLogAge       time.Duration
	logURL          string
	authType        string
	authTLSRoleSpec string
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
	f.StringVar(&c.logURL, "log-url", "", "`URL` for pull replication")
	f.StringVar(&c.authType, "auth", "bearer", "authentication mechanism (bearer/mtls/none)")
	f.StringVar(&c.authTLSRoleSpec, "tls-roles", "", "TLS roles (cn=role1,role2;cn=...)")

	c.ServerTLSFlags.SetFlags(f)
	c.ClientCommand.SetFlags(f)
}

func (c *apiCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if c.dburi == "" {
		return syntaxErr("must specify a database path")
	}

	return fatalErr(c.run(ctx))
}

func (c *apiCommand) api(sql *sqlx.DB) (*httpapi.API, error) {
	authz := httpapi.NewRBAC(rbacRules)
	switch c.authType {
	case "none":
		return httpapi.New(httpapi.NilAuthn(), httpapi.NilAuthz()), nil
	case "bearer":
		return httpapi.New(
			httpapi.NewBearerTokenAuthn(sql),
			authz,
		), nil
	case "mtls", "tls":
		if c.authTLSRoleSpec == "" {
			return nil, errors.New("must specify --tls-roles")
		}
		authn, err := httpapi.NewTLSAuthn(c.authTLSRoleSpec)
		if err != nil {
			return nil, err
		}
		return httpapi.New(authn, authz), nil
	default:
		return nil, errors.New("unknown authentication type")
	}
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

	// If we're a follower, switch the API to read-only.
	var w crud.Writer
	if c.logURL != "" {
		w = crud.ReadOnlyWriter()
	} else {
		w = logdb
	}
	api := crud.Combine(crud.NewSQL(model.Model, sql), w)

	// On the primary datastore, expire peers periodically.
	if c.logURL == "" {
		expire.Expire(ctx, sql, logdb, 30*time.Minute)
	}

	g, ctx := errgroup.WithContext(ctx)

	// Start the follower.
	if c.logURL != "" {
		client, err := c.HTTPClient()
		if err != nil {
			return err
		}

		rlog := crudlog.NewRemoteLogSource(c.logURL, model.Model.Encoding(), client)

		//db.SetReadonly()
		g.Go(func() error {
			return crudlog.Follow(ctx, rlog, logdb)
		})
	}

	// Start the HTTP server.
	g.Go(func() error {
		tlsConf, err := c.TLSServerConfig()
		if err != nil {
			return err
		}
		httpAPI, err := c.api(sql)
		if err != nil {
			return err
		}
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

		// Optional components of the HTTP server, that should
		// only run on primary datastore nodes.
		if c.logURL == "" {
			stats, err := sessions.NewSessionManager(sql)
			if err != nil {
				return err
			}
			httpAPI.Add(stats)

			reg := registration.NewRegistrationAPI(sql)
			httpAPI.Add(reg)
		}

		server := makeHTTPServer(httpAPI, c.addr, tlsConf)
		return runHTTPServerWithContext(ctx, server)
	})

	return g.Wait()
}

func init() {
	subcommands.Register(&apiCommand{}, "")
}

func makeHTTPServer(h http.Handler, addr string, tlsConf *tls.Config) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       900 * time.Second,
		TLSConfig:         tlsConf,
	}
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

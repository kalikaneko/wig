package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"os"

	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/google/subcommands"
	"github.com/jmoiron/sqlx"
)

type initCommand struct {
	dburi string
}

func (c *initCommand) Name() string     { return "init" }
func (c *initCommand) Synopsis() string { return "initialize the database" }
func (c *initCommand) Usage() string {
	return `init
        Initialize the database.

        Its most important (and only) function is creating the initial
        admin authentication token.

`
}

func (c *initCommand) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.dburi, "db", "", "`path` to the database file")
}

func (c *initCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if c.dburi == "" {
		return syntaxErr("must specify a database path")
	}

	return fatalErr(c.run(ctx))
}

func (c *initCommand) run(ctx context.Context) error {
	sql, err := sqlite.OpenDB(c.dburi, datastore.Migrations)
	if err != nil {
		return err
	}
	defer sql.Close()

	id := generateRandomString()
	secret := generateRandomString()
	if err := sqlite.WithTx(sql, func(tx *sqlx.Tx) error {
		_, err := tx.Exec("INSERT INTO tokens (id, secret, roles) VALUES (?, ?, 'admin')", id, secret)
		return err
	}); err != nil {
		return err
	}

	out := struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}{id, secret}
	json.NewEncoder(os.Stdout).Encode(&out)
	return nil
}

func init() {
	subcommands.Register(&initCommand{}, "")
}

var (
	alphabet  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	secretLen = 32
)

func randomInt(n int) int {
	for {
		var b [1]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(err)
		}
		i := int(b[0])
		if i < n {
			return i
		}
	}
}

func generateRandomString() string {
	out := make([]byte, secretLen)
	for i := 0; i < secretLen; i++ {
		out[i] = alphabet[randomInt(len(alphabet))]
	}
	return string(out)
}

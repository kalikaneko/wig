package expire

import (
	"context"
	"log"
	"time"

	"git.autistici.org/ai3/tools/wig/datastore/crud"
	"git.autistici.org/ai3/tools/wig/datastore/model"
	"git.autistici.org/ai3/tools/wig/datastore/sqlite"
	"github.com/jmoiron/sqlx"
)

type expirer struct {
	sql   *sqlx.DB
	dbapi crud.Writer
}

// Find expired peers with a fast query. Returns public_keys.
func findExpiredPeers(tx *sqlx.Tx) []string {
	rows, err := tx.Queryx("SELECT public_key FROM peers WHERE expire IS NOT NULL AND expire < ?", time.Now())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil
		}
	}
	return out
}

func (e *expirer) expirePeers(ctx context.Context, publicKeys []string) (lastErr error) {
	for _, pkey := range publicKeys {
		log.Printf("expiring peer %s", pkey)
		peer := model.Peer{PublicKey: pkey}
		if err := e.dbapi.Delete(ctx, &peer); err != nil {
			lastErr = err
		}
	}
	return
}

func (e *expirer) expire(ctx context.Context) error {
	var toExpire []string

	// Make a list of expired peers with an optimized query.
	//
	// nolint: errcheck
	sqlite.WithTx(e.sql, func(tx *sqlx.Tx) error {
		toExpire = findExpiredPeers(tx)
		return sqlite.ErrRollback
	})

	// Run Delete operations through the crud.Writer (so they will
	// eventually propagate through the log).
	return e.expirePeers(ctx, toExpire)
}

func Expire(ctx context.Context, sql *sqlx.DB, dbapi crud.Writer, interval time.Duration) {
	e := &expirer{
		sql:   sql,
		dbapi: dbapi,
	}

	go func() {
		tick := time.NewTicker(interval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				ictx, cancel := context.WithTimeout(ctx, interval/2)
				err := e.expire(ictx)
				cancel()
				if err != nil {
					log.Printf("error while expiring peers: %v", err)
				}
			}
		}
	}()
}

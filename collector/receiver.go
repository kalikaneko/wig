package collector

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore/sessiondb"
	"git.autistici.org/ai3/attic/wig/gateway"
	"github.com/jmoiron/sqlx"
)

const apiURLReceive = "/api/v1/receive-stats"

type StatsReceiver struct {
	db *sqlx.DB
	sf *SessionFinder
}

func NewStatsReceiver(db *sqlx.DB) (*StatsReceiver, error) {
	sf, err := NewSessionFinder(db)
	if err != nil {
		return nil, err
	}
	return &StatsReceiver{
		db: db,
		sf: sf,
	}, nil
}

func (r *StatsReceiver) ReceivePeerStats(_ context.Context, dump gateway.StatsDump) error {
	now := time.Now()
	return sessiondb.WithTx(r.db, func(tx sessiondb.Tx) error {
		for _, s := range dump {
			sess := r.sf.Analyze(now, &s)
			if sess != nil {
				if err := tx.WriteCompletedSession(sess); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

type receiverHandler struct {
	rec  *StatsReceiver
	wrap http.Handler
}

func NewHandler(r *StatsReceiver, h http.Handler) http.Handler {
	return &receiverHandler{
		rec:  r,
		wrap: h,
	}
}

func (r *receiverHandler) handleReceive(w http.ResponseWriter, req *http.Request) {
	var dump gateway.StatsDump
	if err := json.NewDecoder(req.Body).Decode(&dump); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := r.rec.ReceivePeerStats(req.Context(), dump); err != nil {
		log.Printf("StatsReceiver error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(200)
}

func (r *receiverHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case apiURLReceive:
		r.handleReceive(w, req)
	default:
		if r.wrap != nil {
			r.wrap.ServeHTTP(w, req)
			return
		}
		http.NotFound(w, req)
	}
}

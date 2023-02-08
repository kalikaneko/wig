package sessiondb

import (
	"log"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore/model"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/jmoiron/sqlx"
)

type sessionTx struct {
	tx *sqlx.Tx
}

func (s *sessionTx) DumpActiveSessions(sessions []*model.Session) error {
	_, err := s.tx.Exec("DELETE FROM active_sessions")
	if err != nil {
		return err
	}

	if len(sessions) > 0 {
		_, err = s.tx.NamedExec("INSERT INTO active_sessions (peer_public_key, begin_timestamp, end_timestamp, src_as_num, src_as_org, src_country, active) VALUES (:peer_public_key, :begin_timestamp, :end_timestamp, :src_as_num, :src_as_org, :src_country, :active)", sessions)
	}
	return err
}

func (s *sessionTx) GetActiveSessions() map[string]*model.Session {
	rows, err := s.tx.Queryx("SELECT * FROM active_sessions")
	if err != nil {
		log.Printf("oops: %v", err)
		return nil
	}
	defer rows.Close()

	out := make(map[string]*model.Session)
	for rows.Next() {
		var sess model.Session
		if err := rows.StructScan(&sess); err != nil {
			log.Printf("oops: %v", err)
			break
		}
		out[sess.PeerPublicKey] = &sess
	}

	return out
}

func (s *sessionTx) GetLastHandshakeTimes() map[string]time.Time {
	rows, err := s.tx.Query("SELECT peer_public_key, last_handshake FROM active_sessions")
	if err != nil {
		log.Printf("oops: %v", err)
		return nil
	}
	defer rows.Close()

	out := make(map[string]time.Time)
	for rows.Next() {
		var pk string
		var ht time.Time
		if err := rows.Scan(&pk, &ht); err != nil {
			log.Printf("oops: %v", err)
			break
		}
		out[pk] = ht
	}
	return out
}

func (s *sessionTx) WriteCompletedSession(sess *model.Session) error {
	_, err := s.tx.NamedExec("INSERT INTO sessions (peer_public_key, begin_timestamp, end_timestamp, src_as_num, src_as_org, src_country) VALUES (:peer_public_key, :begin_timestamp, :end_timestamp, :src_as_num, :src_as_org, :src_country)", sess)
	return err
}

func (s *sessionTx) FindSessionsByPublicKey(pk string, limit int) []*model.Session {
	rows, err := s.tx.Queryx("SELECT * FROM sessions WHERE peer_public_key = ? ORDER BY begin_timestamp DESC LIMIT ?", pk, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*model.Session
	for rows.Next() {
		var sess model.Session
		if err := rows.StructScan(&sess); err != nil {
			continue
		}
	}
	return out
}

func (s *sessionTx) Tx() *sqlx.Tx { return s.tx }

type Tx interface {
	Tx() *sqlx.Tx

	GetActiveSessions() map[string]*model.Session
	GetLastHandshakeTimes() map[string]time.Time
	DumpActiveSessions([]*model.Session) error

	WriteCompletedSession(*model.Session) error
	FindSessionsByPublicKey(string, int) []*model.Session
}

func newTx(tx *sqlx.Tx) Tx {
	return &sessionTx{tx: tx}
}

func WithTx(db *sqlx.DB, f func(Tx) error) error {
	return sqlite.WithTx(db, func(tx *sqlx.Tx) error {
		return f(newTx(tx))
	})
}

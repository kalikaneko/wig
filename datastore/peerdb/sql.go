package peerdb

import (
	"log"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/jmoiron/sqlx"
)

type sqlLog struct {
	tx *sqlx.Tx
}

func newSQLLog(tx *sqlx.Tx) *sqlLog {
	return &sqlLog{tx: tx}
}

func (l *sqlLog) appendOp(op Op) {
	_, err := l.tx.NamedExec(`
		INSERT INTO log 
                  (seq, type, timestamp, peer_public_key, peer_ip, peer_ip6, peer_expire)
                VALUES
                  (:seq, :type, :timestamp, :peer.public_key, :peer.ip, :peer.ip6, :peer.expire)
`, &op)
	if err != nil {
		log.Printf("sql error: %v", err)
	}
}

func (l *sqlLog) since(seq Sequence) ([]Op, error) {
	rows, err := l.tx.Queryx(`
		SELECT
		   seq, type, timestamp, peer_public_key AS 'peer.public_key',
                   peer_ip AS 'peer.ip', peer_ip6 AS 'peer.ip6', peer_expire AS 'peer.expire'
                FROM log
                WHERE seq >= ? ORDER BY seq ASC
`, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Op
	for rows.Next() {
		var op Op
		if err := rows.StructScan(&op); err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

type sqlDB struct {
	tx *sqlx.Tx
}

func newSQLDB(tx *sqlx.Tx) *sqlDB {
	return &sqlDB{tx: tx}
}

func (d *sqlDB) Insert(peer *datastore.Peer) error {
	_, err := d.tx.NamedExec(
		"INSERT INTO peers (public_key, ip, ip6, expire) VALUES (:public_key, :ip, :ip6, :expire)",
		peer,
	)
	return err
}

func (d *sqlDB) Update(peer *datastore.Peer) error {
	_, err := d.tx.NamedExec(
		"UPDATE peers SET ip=:ip, ip6=:ip6, expire=:expire WHERE public_key=:public_key",
		peer,
	)
	return err
}

func (d *sqlDB) Delete(pk string) error {
	_, err := d.tx.Exec(
		"DELETE FROM peers WHERE public_key=?",
		pk,
	)
	return err
}

func (d *sqlDB) DropAll() {
	// nolint: errcheck
	d.tx.Exec("DELETE FROM peers")
}

func (d *sqlDB) Size() int {
	var sz int
	if err := d.tx.QueryRow("SELECT COUNT(*) FROM peers").Scan(&sz); err != nil {
		return 0
	}
	return sz
}

func (d *sqlDB) Each(f func(*datastore.Peer)) {
	rows, err := d.tx.Queryx("SELECT * FROM peers")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var peer datastore.Peer
		if err := rows.StructScan(&peer); err != nil {
			return
		}
		f(&peer)
	}
}

func (d *sqlDB) FindByPublicKey(pk string) (*datastore.Peer, bool) {
	var peer datastore.Peer
	if err := d.tx.Get(&peer, "SELECT * FROM peers WHERE public_key = ?", pk); err != nil {
		return nil, false
	}
	return &peer, true
}

type sqlSequencer struct {
	tx *sqlx.Tx
}

func newSQLSequencer(tx *sqlx.Tx) *sqlSequencer {
	return &sqlSequencer{tx: tx}
}

func (s *sqlSequencer) GetSequence() Sequence {
	var u uint64
	if err := s.tx.QueryRow("SELECT seq FROM sequence LIMIT 1").Scan(&u); err != nil {
		return 0
	}
	return Sequence(u)
}

func (s *sqlSequencer) updateSequence(seq Sequence) (err error) {
	_, err = s.tx.Exec("UPDATE sequence SET seq = ?", seq)
	if err != nil {
		_, err = s.tx.Exec("INSERT INTO sequence (seq) VALUES (?)", seq)
	}
	return
}

func (s *sqlSequencer) Inc() Sequence {
	cur := s.GetSequence()
	i := cur
	cur++
	if err := s.updateSequence(cur); err != nil {
		log.Printf("error updating sequence: %v", err)
	}
	return i
}

func (s *sqlSequencer) SetSequence(seq Sequence) error {
	cur := s.GetSequence()
	if seq < cur {
		return ErrOutOfSequence
	}
	return s.updateSequence(seq)
}

type sqlTransaction struct {
	*sqlSequencer
	*sqlLog
	*sqlDB

	tx *sqlx.Tx
}

func (t *sqlTransaction) commit() error {
	return t.tx.Commit()
}

func (t *sqlTransaction) rollback() {
	t.tx.Rollback() // nolint: errcheck
}

type sqlDatabase struct {
	*sqlx.DB

	maxLogAge time.Duration
	stop      chan struct{}
}

func (d *sqlDatabase) Close() {
	close(d.stop)
}

func (d *sqlDatabase) newTransaction() (transaction, error) {
	tx, err := d.DB.Beginx()
	if err != nil {
		return nil, err
	}

	return &sqlTransaction{
		tx:           tx,
		sqlSequencer: newSQLSequencer(tx),
		sqlLog:       newSQLLog(tx),
		sqlDB:        newSQLDB(tx),
	}, nil
}

func (d *sqlDatabase) cleanup() error {
	cutoff := time.Now().Add(-d.maxLogAge)
	return sqlite.WithTx(d.DB, func(tx *sqlx.Tx) error {
		_, err := tx.Exec("DELETE FROM log WHERE timestamp < ?", cutoff)
		return err
	})
}

func (d *sqlDatabase) cleanupLoop() {
	tick := time.NewTicker(1 * time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-tick.C:
			if err := d.cleanup(); err != nil {
				log.Printf("cleanup error: %v", err)
			}
		}
	}
}

func newSQLDatabaseImpl(db *sqlx.DB, maxLogAge time.Duration) *sqlDatabase {
	if maxLogAge == 0 {
		maxLogAge = 168 * time.Hour
	}
	d := &sqlDatabase{
		DB:        db,
		stop:      make(chan struct{}),
		maxLogAge: maxLogAge,
	}
	go d.cleanupLoop()
	return d
}

func NewSQLDatabase(db *sqlx.DB, maxLogAge time.Duration) Database {
	return newDatabase(newSQLDatabaseImpl(db, maxLogAge))
}

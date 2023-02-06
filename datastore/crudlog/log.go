package crudlog

import (
	"context"
	"sync"

	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/jmoiron/sqlx"
)

type dbAPI interface {
	WithTransaction(func(Transaction) error) error
	WithROTransaction(func(Transaction))
}

const chanBufSize = 1000

type crudDatabaseImpl struct {
	crud CRUD
}

func (d *crudDatabaseImpl) ApplyOp(tx Transaction, op Op) error {
	switch op.Type() {
	case OpCreate:
		return d.crud.Create(tx.Tx(), op.Value())
	case OpUpdate:
		return d.crud.Update(tx.Tx(), op.Value())
	case OpDelete:
		return d.crud.Delete(tx.Tx(), op.Value())
	default:
		return ErrInvalidOpType
	}
}

type crudLogSink struct {
	db   dbAPI
	impl LogImpl
	crud CRUD
}

func (s *crudLogSink) Apply(op Op, fromLog bool) error {
	return s.db.WithTransaction(func(tx Transaction) error {
		// If the op does not originate from the log, assign a
		// new sequence to it.
		if !fromLog {
			op = op.WithSequence(s.impl.GetNextSequence(tx))
		}

		// Apply the operation to the underlying storage layer
		// (no logging side effects here).
		if err := s.impl.ApplyOp(tx, op); err != nil {
			return err
		}

		// Append the entry to the log.
		if err := s.impl.AppendToLog(tx, op); err != nil {
			return err
		}

		// Advance the sequence counter.
		if err := s.impl.SetSequence(tx, op.Seq()); err != nil {
			return err
		}

		tx.Emit(op)
		return nil
	})
}

func (s *crudLogSink) LatestSequence() (seq Sequence) {
	s.db.WithROTransaction(func(tx Transaction) {
		seq = s.impl.GetSequence(tx)
	})
	return
}

func (s *crudLogSink) LoadSnapshot(snap Snapshot) error {
	return s.db.WithTransaction(func(tx Transaction) error {
		if err := s.crud.DeleteAll(tx.Tx()); err != nil {
			return err
		}
		if err := snap.Each(func(obj interface{}) error {
			return s.crud.Create(tx.Tx(), obj)
		}); err != nil {
			return err
		}
		return s.impl.SetSequence(tx, snap.Seq())
	})
}

type crudLogSource struct {
	db   dbAPI
	impl LogImpl
	crud CRUD
}

func (s *crudLogSource) Snapshot(_ context.Context) (snap Snapshot, err error) {
	s.db.WithROTransaction(func(tx Transaction) {
		items := make([]interface{}, 0, s.crud.Count(tx.Tx()))
		err = s.crud.Each(tx.Tx(), func(obj interface{}) error {
			items = append(items, obj)
			return nil
		})
		snap = &memSnapshot{
			SeqNum: s.impl.GetSequence(tx),
			Items:  items,
		}
	})
	return
}

// A Snapshot implementation that keeps its contents in memory.
type memSnapshot struct {
	SeqNum Sequence      `json:"seq"`
	Items  []interface{} `json:"items"`
}

func (s *memSnapshot) Seq() Sequence {
	return s.SeqNum
}

func (s *memSnapshot) Each(f func(interface{}) error) error {
	for _, obj := range s.Items {
		if err := f(obj); err != nil {
			return err
		}
	}
	return nil
}

func (s *crudLogSource) Subscribe(_ context.Context, start Sequence) (sub Subscription, err error) {
	err = s.db.WithTransaction(func(tx Transaction) error {
		preload, err := s.impl.QueryLogSince(tx, start)
		if err != nil {
			return err
		}

		ch := make(chan Op, chanBufSize)
		cleanup := tx.AddSubscriber(ch)

		sub = &subscription{
			preload: preload,
			ch:      ch,
			cleanup: cleanup,
		}
		return nil
	})
	return
}

type subscription struct {
	preload []Op
	ch      chan Op

	cleanup func()
}

func (s *subscription) Notify() <-chan Op {
	outCh := make(chan Op, chanBufSize)
	go func() {
		for _, op := range s.preload {
			outCh <- op
		}
		for op := range s.ch {
			outCh <- op
		}
	}()
	return outCh
}

func (s *subscription) Close() {
	s.cleanup()
}

// Maps a LogSink to a crud.Writer interface, hiding the transaction
// management behind the interface.
type crudLogWriter struct {
	sink  LogSink
	newOp func(OpType, interface{}) Op
}

func newCrudLogWriter(sink LogSink, f func(OpType, interface{}) Op) *crudLogWriter {
	return &crudLogWriter{
		sink:  sink,
		newOp: f,
	}
}

func (l *crudLogWriter) Create(_ context.Context, obj interface{}) error {
	return l.sink.Apply(l.newOp(OpCreate, obj), false)
}

func (l *crudLogWriter) Update(_ context.Context, obj interface{}) error {
	return l.sink.Apply(l.newOp(OpUpdate, obj), false)
}

func (l *crudLogWriter) Delete(_ context.Context, obj interface{}) error {
	return l.sink.Apply(l.newOp(OpDelete, obj), false)
}

type dbTx struct {
	*pubsub
	tx *sqlx.Tx
}

func (d *dbTx) Tx() *sqlx.Tx { return d.tx }

// The database layer that binds together SQL (sqlite) and our own
// in-process pubsub. Implements the dbAPI interface.
type dbImpl struct {
	db     *sqlx.DB
	pubsub *pubsub

	// The global database mutex.
	mx sync.RWMutex
}

func (d *dbImpl) newTx(tx *sqlx.Tx) Transaction {
	return &dbTx{
		pubsub: d.pubsub,
		tx:     tx,
	}
}

func (d *dbImpl) WithTransaction(f func(Transaction) error) error {
	d.mx.Lock()
	defer d.mx.Unlock()

	return sqlite.WithTx(d.db, func(tx *sqlx.Tx) error {
		return f(d.newTx(tx))
	})
}

func (d *dbImpl) WithROTransaction(f func(Transaction)) {
	d.mx.RLock()
	defer d.mx.RUnlock()

	// nolint: errcheck
	sqlite.WithTx(d.db, func(tx *sqlx.Tx) error {
		f(d.newTx(tx))
		return sqlite.ErrRollback
	})
}

type crudWithLogImpl struct {
	SnapshotImpl
	*sqlSequencer
	*sqlLogger
	*crudDatabaseImpl
}

func newSQLImpl(src CRUD, encoding Encoding) LogImpl {
	return &crudWithLogImpl{
		SnapshotImpl:     src,
		sqlSequencer:     &sqlSequencer{},
		sqlLogger:        &sqlLogger{encoding},
		crudDatabaseImpl: &crudDatabaseImpl{src},
	}
}

// Wraps a crud.API with LogSource/LogSink implementations.
type crudWithLog struct {
	*crudLogWriter
	*crudLogSource
	*crudLogSink
}

func Wrap(db *sqlx.DB, src CRUD, encoding Encoding) Log {
	api := &dbImpl{
		db:     db,
		pubsub: newPubSub(),
	}
	impl := newSQLImpl(src, encoding)

	source := &crudLogSource{
		db:   api,
		impl: impl,
		crud: src,
	}
	sink := &crudLogSink{
		db:   api,
		impl: impl,
		crud: src,
	}

	return &crudWithLog{
		crudLogSource: source,
		crudLogSink:   sink,
		crudLogWriter: newCrudLogWriter(sink, func(typ OpType, value interface{}) Op {
			return newOp(typ, value)
		}),
	}
}

package peerdb

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
)

const chanBufSize = 1000

var (
	ErrHorizon       = errors.New("sequence out of horizon")
	ErrOutOfSequence = errors.New("out of sequence (log rewind)")
	ErrReadOnly      = errors.New("readonly")
)

const (
	OpUnknown = iota
	OpAdd
	OpUpdate
	OpDelete
)

type OpType int

func (t OpType) String() string {
	switch t {
	case OpAdd:
		return "add"
	case OpUpdate:
		return "update"
	case OpDelete:
		return "delete"
	default:
		return "UNKNOWN"
	}
}

type Sequence uint64

func ParseSequence(s string) (Sequence, error) {
	u, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, err
	}
	return Sequence(u), nil
}

func (s Sequence) String() string {
	return strconv.FormatUint(uint64(s), 16)
}

type Op struct {
	Seq       Sequence       `json:"seq" db:"seq"`
	Type      OpType         `json:"type" db:"type"`
	Peer      datastore.Peer `json:"peer" db:"peer"`
	Timestamp time.Time      `json:"timestamp" db:"timestamp"`
}

type Snapshot interface {
	Seq() Sequence
	Each(func(*datastore.Peer) error) error
}

type Subscription interface {
	Notify() <-chan Op
	Close()
}

type Log interface {
	Snapshot(context.Context) (Snapshot, error)
	Subscribe(context.Context, Sequence) (Subscription, error)
}

type DatabaseAPI interface {
	Add(*datastore.Peer) error
	Update(*datastore.Peer) error
	Delete(*datastore.Peer) error
	FindByPublicKey(string) (*datastore.Peer, bool)
}

type LogReceiver interface {
	Apply(Op) error
	LatestSequence() Sequence
	LoadSnapshot(Snapshot) error
}

type Database interface {
	DatabaseAPI
	LogReceiver
	Log

	SetReadonly()
	Close()
}

// Internal interfaces used to split the implementation into
// manageable components.
//
// sequencer maintains a monotonic Sequence.
type sequencer interface {
	Inc() Sequence
	GetSequence() Sequence
	SetSequence(Sequence) error
}

type logStorage interface {
	appendOp(Op)
	since(Sequence) ([]Op, error)
}

type dataStorage interface {
	Insert(*datastore.Peer) error
	Update(*datastore.Peer) error
	Delete(string) error
	DropAll()
	FindByPublicKey(string) (*datastore.Peer, bool)
	Size() int
	Each(func(*datastore.Peer))
}

type transaction interface {
	sequencer
	logStorage
	dataStorage

	rollback()
	commit() error
}

type databaseImpl interface {
	newTransaction() (transaction, error)

	Close()
}

type database struct {
	readonly bool
	mx       sync.Mutex

	*pubsub
	databaseImpl
}

func newDatabase(impl databaseImpl) *database {
	return &database{
		databaseImpl: impl,
		pubsub:       newPubSub(),
	}
}

func (d *database) Close() {
	d.databaseImpl.Close()
}

func (d *database) SetReadonly() {
	d.readonly = true
}

func (d *database) withTransaction(f func(transaction) error) error {
	// The exclusive lock is necessary in order to wrap pubsub
	// together with database transactions.
	//
	// It is also required by the Snapshot code, given the level
	// of transaction isolation provided by sqlite: different
	// goroutines sharing the same connection might see data
	// written after the transaction was created, which breaks the
	// consistency between the sequence and the snapshot contents.
	d.mx.Lock()
	defer d.mx.Unlock()

	tx, err := d.databaseImpl.newTransaction()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.rollback()
		return err
	}
	return tx.commit()
}

func (d *database) LatestSequence() (seq Sequence) {
	// nolint: errcheck
	d.withTransaction(func(tx transaction) error {
		seq = tx.GetSequence()
		return sqlite.ErrRollback
	})
	return
}

func (d *database) LoadSnapshot(snap Snapshot) error {
	return d.withTransaction(func(tx transaction) error {
		tx.DropAll()
		if err := snap.Each(func(peer *datastore.Peer) error {
			return tx.Insert(peer)
		}); err != nil {
			return err
		}
		return tx.SetSequence(snap.Seq() /* + 1 */)
	})
}

func (d *database) Snapshot(_ context.Context) (snap Snapshot, err error) {
	// nolint: errcheck
	d.withTransaction(func(tx transaction) error {
		items := make([]*datastore.Peer, 0, tx.Size())
		tx.Each(func(peer *datastore.Peer) {
			items = append(items, peer)
		})
		snap = &memSnapshot{
			SeqNum: tx.GetSequence(), /* - 1 */
			Items:  items,
		}
		return sqlite.ErrRollback
	})
	return
}

func (d *database) Apply(op Op) error {
	return d.withTransaction(func(tx transaction) error {
		// Sequencer is only reset when applying external ops.
		if err := tx.SetSequence(op.Seq + 1); err != nil {
			return err
		}

		return d.applyInternal(tx, op)
	})
}

func (d *database) applyInternal(tx transaction, op Op) error {
	var err error
	switch op.Type {
	case OpAdd:
		err = tx.Insert(&op.Peer)
	case OpUpdate:
		err = tx.Update(&op.Peer)
	case OpDelete:
		err = tx.Delete(op.Peer.PublicKey)
	}
	if err != nil {
		return err
	}

	tx.appendOp(op)
	d.pubsub.publish(op)

	return nil
}

func (d *database) Subscribe(_ context.Context, start Sequence) (sub Subscription, err error) {
	err = d.withTransaction(func(tx transaction) error {
		preload, err := tx.since(start)
		if err != nil {
			return err
		}

		ch := make(chan Op, chanBufSize)
		el := d.pubsub.addSubscriber(ch)

		sub = &subscription{
			preload: preload,
			ch:      ch,
			p:       d.pubsub,
			el:      el,
		}
		return nil
	})
	return
}

func (d *database) Add(peer *datastore.Peer) error {
	if d.readonly {
		return ErrReadOnly
	}
	return d.withTransaction(func(tx transaction) error {
		return d.applyInternal(tx, newOp(tx.Inc(), OpAdd, peer))
	})
}

func (d *database) Update(peer *datastore.Peer) error {
	if d.readonly {
		return ErrReadOnly
	}
	return d.withTransaction(func(tx transaction) error {
		return d.applyInternal(tx, newOp(tx.Inc(), OpUpdate, peer))
	})
}

func (d *database) Delete(peer *datastore.Peer) error {
	if d.readonly {
		return ErrReadOnly
	}
	return d.withTransaction(func(tx transaction) error {
		return d.applyInternal(tx, newOp(tx.Inc(), OpDelete,
			&datastore.Peer{PublicKey: peer.PublicKey}))
	})
}

func (d *database) FindByPublicKey(pk string) (peer *datastore.Peer, ok bool) {
	// nolint: errcheck
	d.withTransaction(func(tx transaction) error {
		peer, ok = tx.FindByPublicKey(pk)
		return sqlite.ErrRollback
	})
	return
}

func newOp(seq Sequence, typ OpType, peer *datastore.Peer) Op {
	return Op{
		Seq:       seq,
		Type:      typ,
		Peer:      *peer,
		Timestamp: time.Now(),
	}
}

package crudlog

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"git.autistici.org/ai3/tools/wig/datastore/crud"
	"github.com/jmoiron/sqlx"
)

// Implementation of a simple database with pubsub change logging.
//
// An Op is just {sequence, type, value}.
//
// Everything must be scoped to an arbitrary Transaction (which
// includes a SQL tx but also perhaps a higher-level process lock).
//
// Instead of compositing Transactions we prefer using objects that do
// things and take Transactions as arguments, like Context.
//
//

type LogSource interface {
	Snapshot(context.Context) (Snapshot, error)
	Subscribe(context.Context, Sequence) (Subscription, error)
}

type Subscription interface {
	Notify() <-chan Op
	Close()
}

type LogSink interface {
	Apply(Op, bool) error
	LatestSequence() Sequence
	LoadSnapshot(Snapshot) error
}

type Snapshot interface {
	Seq() Sequence
	Each(func(interface{}) error) error
}

type Op interface {
	Seq() Sequence
	Type() OpType
	Value() interface{}
	Timestamp() time.Time
	WithSequence(Sequence) Op
	WithEncoding(Encoding) OpWithEncoding
}

type Encoding interface {
	MarshalValue(interface{}) ([]byte, error)
	UnmarshalValue([]byte) (interface{}, error)
}

type OpWithEncoding interface {
	Op() Op
	json.Marshaler
	json.Unmarshaler
}

const (
	OpUnknown = iota
	OpCreate
	OpUpdate
	OpDelete
)

var (
	ErrInvalidOpType = errors.New("invalid op type in log")
	ErrHorizon       = errors.New("sequence out of horizon")
	ErrOutOfSequence = errors.New("out of sequence (log rewind)")
)

type OpType int

func (t OpType) String() string {
	switch t {
	case OpCreate:
		return "create"
	case OpUpdate:
		return "update"
	case OpDelete:
		return "delete"
	default:
		return "UNKNOWN"
	}
}

// Sequence is a monotonically incrementing counter.
type Sequence uint64

// ParseSequence parses a Sequence from its string representation
// (hex).
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

// SnapshotImpl handles generic access to the database for making
// snapshots.
type SnapshotImpl interface {
	Count(*sqlx.Tx) int // for pre-allocation
	Each(*sqlx.Tx, func(interface{}) error) error
}

// CRUD is a low-level interface to a generic CRUD database that also
// offers the methods we need for taking snapshots.
type CRUD interface {
	Create(*sqlx.Tx, interface{}) error
	Update(*sqlx.Tx, interface{}) error
	Delete(*sqlx.Tx, interface{}) error
	DeleteAll(*sqlx.Tx) error

	SnapshotImpl
}

// Log extends a crud.Writer with LogSource/LogSink interfaces.
type Log interface {
	crud.Writer
	LogSource
	LogSink
}

// SequencerImpl maintains a monotonic Sequence (transaction-bound).
type SequencerImpl interface {
	GetSequence(Transaction) Sequence
	GetNextSequence(Transaction) Sequence
	SetSequence(Transaction, Sequence) error
}

// LoggerImpl manages low-level access to log storage.
type LoggerImpl interface {
	AppendToLog(Transaction, Op) error
	QueryLogSince(Transaction, Sequence) ([]Op, error)
}

// DatabaseImpl modifies the low-level database via an Op and it's
// used to apply entries from the log.
type DatabaseImpl interface {
	ApplyOp(Transaction, Op) error
}

// LogImpl decouples the generic log logic from its low-level database
// implementation.
type LogImpl interface {
	DatabaseImpl
	SequencerImpl
	SnapshotImpl
	LoggerImpl
}

// PubsubTx has pubsub-related methods that should be part of the same
// Transaction with database accesses.
type PubsubTx interface {
	Emit(Op)
	AddSubscriber(chan Op) func()
}

// Transaction binds together a SQL transaction and a Pubsub context
// (which operates entirely in memory). Together they require a shared
// lock for synchronization.
type Transaction interface {
	Tx() *sqlx.Tx
	PubsubTx
}

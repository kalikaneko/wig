package crud

import (
	"context"
	"errors"
	"log"
	"reflect"

	"git.autistici.org/ai3/attic/wig/datastore/crud/httptransport"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/jmoiron/sqlx"
)

var (
	ErrUnknownType = errors.New("unknown type")
	ErrReadonly    = errors.New("read-only")
)

// Writer is the write part of a generic CRUD client interface.
type Writer interface {
	Create(context.Context, interface{}) error
	Update(context.Context, interface{}) error
	Delete(context.Context, interface{}) error
}

// Reader is a read interface for a generic CRUD service. The Find
// method applies to an explicitly named type.
type Reader interface {
	Find(context.Context, string, map[string]string, func(interface{}) error) error
}

// API is the full CRUD API interface, combining a Reader and a
// Writer.
type API interface {
	Reader
	Writer
}

type splitAPI struct {
	Reader
	Writer
}

// Combine a Reader and a Writer into an API. Lets us override the
// Writer from a crudlog.CRUDLog with a standard (log-unaware) Reader.
func Combine(r Reader, w Writer) API {
	return &splitAPI{Reader: r, Writer: w}
}

type roWriter struct{}

func (roWriter) Create(_ context.Context, _ interface{}) error { return ErrReadonly }
func (roWriter) Update(_ context.Context, _ interface{}) error { return ErrReadonly }
func (roWriter) Delete(_ context.Context, _ interface{}) error { return ErrReadonly }

func ReadOnlyWriter() Writer { return new(roWriter) }

// TypeMeta is the descriptor interface for a data type (returns static metadata).
type TypeMeta interface {
	Name() string

	PrimaryKeyField() string
	Fields() []string

	NewInstance() interface{}
	NewInstanceFromValues(Values) (interface{}, error)
}

// Type is the implementation interface of a specific data type (backed by a SQL table).
type Type interface {
	TypeMeta

	Create(*sqlx.Tx, interface{}) error
	Update(*sqlx.Tx, interface{}) error
	Delete(*sqlx.Tx, interface{}) error
	DeleteAll(*sqlx.Tx) error
	Count(*sqlx.Tx) int
	Each(*sqlx.Tx, func(interface{}) error) error
	Find(*sqlx.Tx, map[string]string, func(interface{}) error) error
}

type registry struct {
	byType map[reflect.Type]Type
	byName map[string]Type
}

func newRegistry() *registry {
	return &registry{
		byType: make(map[reflect.Type]Type),
		byName: make(map[string]Type),
	}
}

func (r *registry) Register(m Type) {
	t := reflect.TypeOf(m.NewInstance())
	r.byType[t] = m
	r.byName[m.Name()] = m
}

func (r *registry) getType(obj interface{}) (Type, bool) {
	t := reflect.TypeOf(obj)
	m, ok := r.byType[t]
	return m, ok
}

func (r *registry) getTypeByName(name string) (Type, bool) {
	m, ok := r.byName[name]
	return m, ok
}

func (r *registry) each(f func(Type) error) error {
	for _, m := range r.byName {
		if err := f(m); err != nil {
			return err
		}
	}
	return nil
}

// The dispatcher routes CRD calls to the appropriate Type depending
// on the type of the object provided.
//
// Implements the crudlog.CRUD interface.
type dispatcher struct {
	*registry
}

func (c *dispatcher) Create(tx *sqlx.Tx, obj interface{}) error {
	m, ok := c.registry.getType(obj)
	if !ok {
		log.Printf("unknown type: %+v", obj)
		return ErrUnknownType
	}
	return m.Create(tx, obj)
}

func (c *dispatcher) Update(tx *sqlx.Tx, obj interface{}) error {
	m, ok := c.registry.getType(obj)
	if !ok {
		return ErrUnknownType
	}
	return m.Update(tx, obj)
}

func (c *dispatcher) Delete(tx *sqlx.Tx, obj interface{}) error {
	m, ok := c.registry.getType(obj)
	if !ok {
		return ErrUnknownType
	}
	return m.Delete(tx, obj)
}

func (c *dispatcher) DeleteAll(tx *sqlx.Tx) error {
	return c.registry.each(func(m Type) error {
		return c.DeleteAll(tx)
	})
}

func (c *dispatcher) Count(tx *sqlx.Tx) int {
	var count int
	// nolint: errcheck
	c.registry.each(func(m Type) error {
		count += m.Count(tx)
		return nil
	})
	return count
}

func (c *dispatcher) Each(tx *sqlx.Tx, f func(obj interface{}) error) error {
	return c.registry.each(func(m Type) error {
		return m.Each(tx, f)
	})
}

// A Model is a registry of data types, and it provides type routing
// for transaction-scoped CRUD methods that the crudlog.CRUD interface
// requires.
type Model struct {
	*registry
	*dispatcher
}

// New creates a new Model.
func New() *Model {
	r := newRegistry()
	return &Model{
		registry:   r,
		dispatcher: &dispatcher{r},
	}
}

// Encoding returns an Encoding implementation that is aware of the
// types recorded in this Model's registry.
func (m *Model) Encoding() *Encoding {
	return &Encoding{m.registry}
}

// SQLReader binds together a Model with a SQL database, providing its
// transactional context, to implement the Reader interface.
type SQLReader struct {
	m  *Model
	db *sqlx.DB
}

func NewSQL(m *Model, db *sqlx.DB) *SQLReader {
	return &SQLReader{m: m, db: db}
}

func (s *SQLReader) Find(_ context.Context, typ string, query map[string]string, f func(interface{}) error) error {
	t, ok := s.m.getTypeByName(typ)
	if !ok {
		return ErrUnknownType
	}
	return sqlite.WithTx(s.db, func(tx *sqlx.Tx) (err error) {
		return t.Find(tx, query, f)
	})
}

func init() {
	httptransport.RegisterError("unknown-type", ErrUnknownType)
	httptransport.RegisterError("readonly", ErrReadonly)
}

package crud

import (
	"errors"
	"testing"

	"github.com/jmoiron/sqlx"
)

type testObj struct {
	Foo string `db:"foo"`
}

type testType struct{}

func (t *testType) Name() string             { return "test" }
func (t *testType) PrimaryKeyField() string  { return "foo" }
func (t *testType) Fields() []string         { return nil }
func (t *testType) NewInstance() interface{} { return new(testObj) }
func (t *testType) NewInstanceFromValues(v Values) (interface{}, error) {
	return &testObj{Foo: v.Get("foo")}, nil
}

func (t *testType) Create(_ *sqlx.Tx, _ interface{}) error { return errors.New("not implemented") }
func (t *testType) Update(_ *sqlx.Tx, _ interface{}) error { return errors.New("not implemented") }
func (t *testType) Delete(_ *sqlx.Tx, _ interface{}) error { return errors.New("not implemented") }
func (t *testType) DeleteAll(_ *sqlx.Tx) error             { return errors.New("not implemented") }
func (t *testType) Count(_ *sqlx.Tx) int                   { return 42 }
func (t *testType) Each(_ *sqlx.Tx, _ func(interface{}) error) error {
	return errors.New("not implemented")
}
func (t *testType) Find(_ *sqlx.Tx, _ map[string]string, _ func(interface{}) error) error {
	return errors.New("not implemented")
}

func TestRegistry(t *testing.T) {
	m := New()
	m.Register(&testType{})

	if _, ok := m.getType(&testObj{}); !ok {
		t.Fatalf("registered type not returned by getType()")
	}
	if _, ok := m.byName["test"]; !ok {
		t.Fatalf("type not registered by its name")
	}

	if n := m.Count(nil); n != 42 {
		t.Fatalf("Count() returned %d, expected 42", n)
	}
}

package crud

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

type sqlTableAdapter struct {
	typename    string
	table       string
	allFields   []string
	newFn       func() interface{}
	newFnValues func(Values) (interface{}, error)
	insStmt     string
	updStmt     string
	delStmt     string
}

// NewSQLTableType creates a Type out of a SQL table and a link to the backing object type.
func NewSQLTableType(typename, table, primaryKey string, fields []string, newFn func() interface{}, newFnValues func(Values) (interface{}, error)) Type {
	return &sqlTableAdapter{
		typename:    typename,
		table:       table,
		newFn:       newFn,
		newFnValues: newFnValues,
		insStmt:     buildInsertStatement(table, primaryKey, fields),
		updStmt:     buildUpdateStatement(table, primaryKey, fields),
		delStmt:     buildDeleteStatement(table, primaryKey),
		allFields:   append([]string{primaryKey}, fields...),
	}
}

func (t *sqlTableAdapter) Name() string { return t.typename }

func (t *sqlTableAdapter) Fields() []string { return t.allFields }

func (t *sqlTableAdapter) PrimaryKeyField() string { return t.allFields[0] }

func (t *sqlTableAdapter) NewInstance() interface{} { return t.newFn() }

func (t *sqlTableAdapter) NewInstanceFromValues(v Values) (interface{}, error) {
	return t.newFnValues(v)
}

func (t *sqlTableAdapter) Create(tx *sqlx.Tx, obj interface{}) error {
	_, err := tx.NamedExec(t.insStmt, obj)
	return err
}

func (t *sqlTableAdapter) Update(tx *sqlx.Tx, obj interface{}) error {
	_, err := tx.NamedExec(t.updStmt, obj)
	return err
}

func (t *sqlTableAdapter) Delete(tx *sqlx.Tx, obj interface{}) error {
	_, err := tx.NamedExec(t.delStmt, obj)
	return err
}

func (t *sqlTableAdapter) DeleteAll(tx *sqlx.Tx) error {
	_, err := tx.Exec(fmt.Sprintf("DELETE FROM `%s`", t.table))
	return err
}

func (t *sqlTableAdapter) Count(tx *sqlx.Tx) int {
	var sz int
	if err := tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s`", t.table)).Scan(&sz); err != nil {
		return 0
	}
	return sz
}

func (t *sqlTableAdapter) Each(tx *sqlx.Tx, f func(interface{}) error) error {
	return t.Find(tx, nil, f)
}

func (t *sqlTableAdapter) Find(tx *sqlx.Tx, query map[string]string, f func(interface{}) error) error {
	rows, err := newQueryBuilder(t.table, query).exec(tx)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		obj := t.newFn()
		if err := rows.StructScan(obj); err != nil {
			return err
		}
		if err := f(obj); err != nil {
			return err
		}
	}
	return rows.Err()
}

func buildInsertStatement(table, primaryKeyField string, fields []string) string {
	fnames := append([]string{primaryKeyField}, fields...)
	flabels := []string{":" + primaryKeyField}
	for _, f := range fields {
		flabels = append(flabels, ":"+f)
	}
	return fmt.Sprintf(
		"INSERT INTO `%s` (%s) VALUES (%s)",
		table,
		strings.Join(fnames, ","),
		strings.Join(flabels, ","),
	)
}

func buildUpdateStatement(table, primaryKeyField string, fields []string) string {
	var tmp []string
	for _, f := range fields {
		tmp = append(tmp, fmt.Sprintf("%s=:%s", f, f))
	}
	return fmt.Sprintf(
		"UPDATE `%s` SET %s WHERE %s=:%s",
		table,
		strings.Join(tmp, ","),
		primaryKeyField,
		primaryKeyField,
	)
}

func buildDeleteStatement(table, primaryKeyField string) string {
	return fmt.Sprintf("DELETE FROM `%s` WHERE %s=:%s", table, primaryKeyField, primaryKeyField)
}

type queryBuilder struct {
	table   string
	clauses []string
	args    []interface{}
}

func newQueryBuilder(table string, query map[string]string) *queryBuilder {
	q := &queryBuilder{table: table}
	for k, v := range query {
		q.clauses = append(q.clauses, fmt.Sprintf("%s = ?", k))
		q.args = append(q.args, v)
	}
	return q
}

func (q *queryBuilder) exec(tx *sqlx.Tx) (*sqlx.Rows, error) {
	return tx.Queryx(
		fmt.Sprintf(
			"SELECT * FROM `%s` WHERE %s",
			q.table,
			strings.Join(q.clauses, " AND "),
		),
		q.args...)
}

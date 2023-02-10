package model

import (
	"database/sql/driver"
	"strings"

	"git.autistici.org/ai3/tools/wig/datastore/crud"
)

type Token struct {
	ID     string       `json:"id" db:"id"`
	Secret string       `json:"secret" db:"secret"`
	Roles  CommaSepList `json:"roles" db:"roles"`
}

var TokenType = crud.NewSQLTableType(
	"token",
	"tokens",
	"id",
	[]string{"secret", "roles"},
	func() interface{} {
		return new(Token)
	},
	func(values crud.Values) (interface{}, error) {
		var token Token

		token.ID = values.Get("id")
		token.Secret = values.Get("secret")
		token.Roles = strings.Split(values.Get("roles"), ",")
		return &token, nil
	},
)

type CommaSepList []string

func (l CommaSepList) Value() (driver.Value, error) {
	if len(l) == 0 {
		return nil, nil
	}
	return driver.Value(strings.Join(l, ",")), nil
}

func (l *CommaSepList) Scan(src interface{}) error {
	switch src := src.(type) {
	case string:
		*l = strings.Split(src, ",")
	default:
		*l = nil
	}
	return nil
}

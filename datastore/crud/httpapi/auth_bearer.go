package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/jmoiron/sqlx"
)

type bearerCredentials struct {
	tokenID string
	roles   []string
}

func (c *bearerCredentials) Identity() string { return c.tokenID }
func (c *bearerCredentials) Roles() []string  { return c.roles }

type authBearerToken struct {
	db *sqlx.DB
}

func NewBearerTokenAuthn(db *sqlx.DB) Authn {
	return &authBearerToken{db}
}

func (a *authBearerToken) CredentialsFromRequest(req *http.Request) (Credentials, error) {
	tokenID, tokenSecret, ok := req.BasicAuth()
	if !ok {
		return nil, errors.New("missing credentials")
	}

	var rolesStr, dbSecret string
	if err := sqlite.WithTx(a.db, func(tx *sqlx.Tx) error {
		return tx.QueryRow("SELECT roles, secret FROM tokens WHERE id=?", tokenID).Scan(&rolesStr, &dbSecret)
	}); err != nil {
		return nil, err
	}

	// Non constant-time comparison, no hashing, nothing.
	if tokenSecret != dbSecret {
		return nil, errors.New("bad secret")
	}

	return &bearerCredentials{
		tokenID: tokenID,
		roles:   strings.Split(rolesStr, ","),
	}, nil
}

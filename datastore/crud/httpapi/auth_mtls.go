package httpapi

import (
	"crypto/x509"
	"errors"
	"net/http"
	"strings"
)

type tlsCredentials struct {
	cert  *x509.Certificate
	roles []string
}

func (c *tlsCredentials) Identity() string { return c.cert.Subject.CommonName }
func (c *tlsCredentials) Roles() []string  { return c.roles }

type authTLS struct {
	rolemap map[string][]string
}

func NewTLSAuthn(s string) (Authn, error) {
	m := make(map[string][]string)
	for _, spec := range strings.Split(s, ";") {
		parts := strings.SplitN(spec, "=", 2)
		if len(parts) != 2 {
			return nil, errors.New("syntax error in mTLS auth spec")
		}
		m[parts[0]] = strings.Split(parts[1], ",")
	}
	return &authTLS{rolemap: m}, nil
}

func (t *authTLS) CredentialsFromRequest(req *http.Request) (Credentials, error) {
	if req.TLS == nil {
		return nil, errors.New("no TLS")
	}
	for _, cert := range req.TLS.PeerCertificates {
		roles, ok := t.rolemap[cert.Subject.CommonName]
		if ok {
			return &tlsCredentials{
				cert:  cert,
				roles: roles,
			}, nil
		}
	}
	return nil, errors.New("no known TLS credentials")
}

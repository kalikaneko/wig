package httptransport

import (
	"crypto/tls"
	"net/http"
	"time"
)

type basicAuthTransport struct {
	*http.Transport

	username, password string
}

func withBasicAuth(username, password string, t *http.Transport) *basicAuthTransport {
	return &basicAuthTransport{
		Transport: t,
		username:  username,
		password:  password,
	}
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.username != "" {
		req.SetBasicAuth(t.username, t.password)
	}
	return t.Transport.RoundTrip(req)
}

// NewClient creates a http.Client with sensible parameters and an
// optional tls.Config.
func NewClient(tlsConf *tls.Config, username, password string) *http.Client {
	return &http.Client{
		Transport: withBasicAuth(username, password, &http.Transport{
			IdleConnTimeout: 300 * time.Second,
			TLSClientConfig: tlsConf,
		}),
	}
}

package util

import (
	"flag"
	"net/http"

	"git.autistici.org/ai3/attic/wig/datastore/crud/httptransport"
)

type ClientCommand struct {
	ClientTLSFlags

	authToken  string
	authSecret string
}

func (c *ClientCommand) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.authToken, "auth-token", "", "token ID for authentication")
	f.StringVar(&c.authSecret, "auth-secret", "", "secret for authentication")
	c.ClientTLSFlags.SetFlags(f)
}

func (c *ClientCommand) HTTPClient() (*http.Client, error) {
	tlsConf, err := c.TLSClientConfig()
	if err != nil {
		return nil, err
	}

	return httptransport.NewClient(tlsConf, c.authToken, c.authSecret), nil
}

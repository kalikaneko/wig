package util

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"
)

func loadCA(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cas := x509.NewCertPool()
	if !cas.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no certificates could be parsed in %s", path)
	}
	return cas, nil
}

type ClientTLSFlags struct {
	cert string
	key  string
	ca   string
}

func (c *ClientTLSFlags) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.cert, "ssl-client-cert", FlagDefault("ssl-client-cert", ""), "SSL certificate `path` (client)")
	f.StringVar(&c.key, "ssl-client-key", FlagDefault("ssl-client-key", ""), "SSL private key `path` (client)")
	f.StringVar(&c.ca, "ssl-client-ca", FlagDefault("ssl-client-ca", ""), "SSL CA `path` (client)")
}

func (c *ClientTLSFlags) TLSClientConfig() (*tls.Config, error) {
	if c.cert == "" || c.key == "" || c.ca == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(c.cert, c.key)
	if err != nil {
		return nil, err
	}
	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	cas, err := loadCA(c.ca)
	if err != nil {
		return nil, err
	}
	tlsConf.RootCAs = cas

	return tlsConf, nil
}

type ServerTLSFlags struct {
	cert string
	key  string
	ca   string
}

func (c *ServerTLSFlags) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.cert, "ssl-cert", "", "SSL certificate `path`")
	f.StringVar(&c.key, "ssl-key", "", "SSL private key `path`")
	f.StringVar(&c.ca, "ssl-ca", "", "SSL CA `path` (server, enables client certificate validation)")
}

func (c *ServerTLSFlags) TLSServerConfig() (*tls.Config, error) {
	if c.cert == "" || c.key == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(c.cert, c.key)
	if err != nil {
		return nil, err
	}

	// Set some TLS-level parameters (cipher-related), assuming
	// we're using EC keys.
	tlsConf := &tls.Config{
		Certificates:             []tls.Certificate{cert},
		MinVersion:               tls.VersionTLS13,
		PreferServerCipherSuites: true,
		NextProtos:               []string{"h2", "http/1.1"},
	}

	// Require client certificates if a CA is specified.
	if c.ca != "" {
		pool, err := loadCA(c.ca)
		if err != nil {
			return nil, err
		}

		tlsConf.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConf.ClientCAs = pool
	}

	return tlsConf, nil
}

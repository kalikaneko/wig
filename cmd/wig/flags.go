package main

import (
	"bufio"
	"net/url"
	"os"
	"strings"

	"git.autistici.org/ai3/attic/wig/datastore"
)

type ipNetFlag struct {
	*datastore.CIDR
}

func (f ipNetFlag) String() string {
	return f.CIDR.String()
}

func (f *ipNetFlag) Set(s string) error {
	ipnet, err := datastore.ParseCIDR(s)
	if err != nil {
		return err
	}
	f.CIDR = ipnet
	return nil
}

type urlFlag string

func (f urlFlag) String() string {
	return string(f)
}

func (f *urlFlag) Set(s string) error {
	if _, err := url.Parse(s); err != nil {
		return err
	}
	*f = urlFlag(s)
	return nil
}

type privateKeyFlag string

func (f privateKeyFlag) String() string {
	return string(f)
}

func (f *privateKeyFlag) Set(s string) error {
	ff, err := os.Open(s)
	if err != nil {
		return err
	}
	defer ff.Close()

	scanner := bufio.NewScanner(ff)
	scanner.Scan()
	*f = privateKeyFlag(strings.TrimSpace(scanner.Text()))
	return nil
}

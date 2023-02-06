package main

import (
	"net/url"
)

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

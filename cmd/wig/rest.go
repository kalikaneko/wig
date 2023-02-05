package main

import (
	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/rest"
)

type peerRestHandler struct{}

func (_ *peerRestHandler) Name() string { return "peer" }

func (p *peerRestHandler) Fields() []string {
	return []string{"public-key", "ip", "ip6"}
}

func (_ *peerRestHandler) NewInstance() interface{} {
	return new(datastore.Peer)
}

func (_ *peerRestHandler) NewInstanceFromValues(values rest.Values) (interface{}, error) {
	var peer datastore.Peer

	if s := values.Get("public-key"); s != "" {
		peer.PublicKey = s
	}

	if s := values.Get("ip"); s != "" {
		ip, err := datastore.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		peer.IP = ip
	}

	if s := values.Get("ip6"); s != "" {
		ip, err := datastore.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		peer.IP6 = ip
	}

	return &peer, nil
}

func init() {
	rest.RegisterCommands(new(peerRestHandler))
}

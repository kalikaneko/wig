package model

import (
	"time"

	"git.autistici.org/ai3/attic/wig/datastore/crud"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Peer struct {
	PublicKey string    `json:"public_key" db:"public_key"`
	Interface string    `json:"interface" db:"interface"`
	IP        *CIDR     `json:"ip" db:"ip"`
	IP6       *CIDR     `json:"ip6" db:"ip6"`
	Expire    time.Time `json:"expire" db:"expire"`
}

var PeerType = crud.NewSQLTableType(
	"peer",
	"peers",
	"public_key",
	[]string{"ip", "ip6", "interface", "expire"},
	func() interface{} {
		return new(Peer)
	},
	func(values crud.Values) (interface{}, error) {
		var peer Peer

		if s := values.Get("public_key"); s != "" {
			// Confirm parse-ability.
			if _, err := wgtypes.ParseKey(s); err != nil {
				return nil, err
			}
			peer.PublicKey = s
		}

		if s := values.Get("ip"); s != "" {
			ip, err := ParseCIDR(s)
			if err != nil {
				return nil, err
			}
			peer.IP = ip
		}

		if s := values.Get("ip6"); s != "" {
			ip, err := ParseCIDR(s)
			if err != nil {
				return nil, err
			}
			peer.IP6 = ip
		}

		peer.Interface = values.Get("interface")

		return &peer, nil
	},
)

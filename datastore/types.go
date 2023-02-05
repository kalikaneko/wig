package datastore

import (
	"database/sql/driver"
	"encoding/json"
	"net"
	"time"
)

type Interface struct {
	Name       string `json:"name"`
	IP         *CIDR  `json:"ip"`
	IP6        *CIDR  `json:"ip6"`
	Fwmark     int    `json:"fwmark"`
	PrivateKey string `json:"-"`
	PublicKey  string `json:"public_key"`
}

type Peer struct {
	PublicKey string    `json:"public_key" db:"public_key"`
	IP        *CIDR     `json:"ip" db:"ip"`
	IP6       *CIDR     `json:"ip6" db:"ip6"`
	Expire    time.Time `json:"expire" db:"expire"`
}

type Session struct {
	PeerPublicKey string    `json:"peer_public_key" db:"peer_public_key"`
	SrcASNum      string    `json:"src_as_num" db:"src_as_num"`
	SrcASOrg      string    `json:"src_as_org" db:"src_as_org"`
	SrcCountry    string    `json:"src_country" db:"src_country"`
	Begin         time.Time `json:"begin" db:"begin_timestamp"`
	End           time.Time `json:"end" db:"end_timestamp"`
	Active        bool      `json:"active" db:"active"`
}

type CIDR struct {
	net.IPNet
}

func ParseCIDR(s string) (*CIDR, error) {
	c := new(CIDR)
	return c, c.parse(s)
}

func (c *CIDR) parse(s string) error {
	if s == "" {
		return nil
	}
	ip, net, err := net.ParseCIDR(s)
	if err != nil {
		return err
	}
	c.IPNet.IP = ip
	c.IPNet.Mask = net.Mask
	return nil
}

func (c *CIDR) String() string {
	if c == nil || c.IPNet.IP == nil {
		return ""
	}
	return c.IPNet.String()
}

func (c *CIDR) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

func (c *CIDR) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	return c.parse(s)
}

func (c *CIDR) Scan(src interface{}) error {
	switch src := src.(type) {
	case string:
		return c.parse(src)
	default:
		c.IPNet = net.IPNet{}
		return nil
	}
}

func (c *CIDR) Value() (driver.Value, error) {
	if c == nil || c.IPNet.IP == nil {
		return nil, nil
	}
	return driver.Value(c.String()), nil
}

package model

import (
	"database/sql/driver"
	"encoding/json"
	"net"
)

// CIDR is a net.IPNet augmented with serialization / deserialization
// methods for database/sql and JSON.
type CIDR struct {
	net.IPNet
}

func ParseCIDR(s string) (*CIDR, error) {
	c := new(CIDR)
	return c, c.parse(s)
}

func NewCIDR(ip net.IP, sz int) *CIDR {
	return &CIDR{
		IPNet: net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(sz, 8*len(ip)),
		},
	}
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

func (c *CIDR) IsNil() bool {
	return c == nil || c.IPNet.IP == nil
}

func (c *CIDR) String() string {
	if c.IsNil() {
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

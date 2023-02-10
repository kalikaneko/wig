package model

import (
	"strconv"

	"git.autistici.org/ai3/tools/wig/datastore/crud"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Interface struct {
	Name       string `json:"name" db:"name"`
	Port       int    `json:"port" db:"port"`
	IP         *CIDR  `json:"ip" db:"ip"`
	IP6        *CIDR  `json:"ip6" db:"ip6"`
	Fwmark     int    `json:"fwmark" db:"fwmark"`
	PrivateKey string `json:"private_key" db:"private_key"`
	PublicKey  string `json:"public_key" db:"public_key"`
}

var InterfaceType = crud.NewSQLTableType(
	"interface",
	"interfaces",
	"name",
	[]string{"ip", "ip6", "fwmark", "port", "private_key", "public_key"},
	func() interface{} {
		return new(Interface)
	},
	func(values crud.Values) (interface{}, error) {
		var intf Interface

		if s := values.Get("private_key"); s != "" {
			key, err := wgtypes.ParseKey(s)
			if err != nil {
				return nil, err
			}
			intf.PrivateKey = key.String()
			intf.PublicKey = key.PublicKey().String()
		}

		if s := values.Get("ip"); s != "" {
			ip, err := ParseCIDR(s)
			if err != nil {
				return nil, err
			}
			intf.IP = ip
		}

		if s := values.Get("ip6"); s != "" {
			ip, err := ParseCIDR(s)
			if err != nil {
				return nil, err
			}
			intf.IP6 = ip
		}

		intf.Name = values.Get("name")
		intf.Fwmark, _ = strconv.Atoi(values.Get("fwmark"))
		intf.Port, _ = strconv.Atoi(values.Get("port"))

		return &intf, nil
	},
)

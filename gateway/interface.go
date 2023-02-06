package gateway

import (
	"errors"
	"fmt"
	"os"

	"git.autistici.org/ai3/attic/wig/datastore/model"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type wgInterface struct {
	*model.Interface

	ctrl *wgctrl.Client
}

func newInterface(ctrl *wgctrl.Client, intf *model.Interface) (*wgInterface, error) {
	wgi := &wgInterface{
		Interface: intf,
		ctrl:      ctrl,
	}

	err := wgi.initialize()
	if errors.Is(err, os.ErrNotExist) {
		if err = wgi.startInterface(); err != nil {
			return nil, err
		}
		err = wgi.initialize()
	}
	if err != nil {
		return nil, err
	}

	return wgi, nil
}

func (i *wgInterface) configureWGDevice(cfg wgtypes.Config) error {
	return i.ctrl.ConfigureDevice(i.Name, cfg)
}

func (i *wgInterface) reconfigure(intf *model.Interface) error {
	if err := i.stopInterface(); err != nil {
		return err
	}

	i.Interface = intf

	if err := i.initialize(); err != nil {
		return err
	}
	return i.startInterface()
}

func (i *wgInterface) initialize() error {
	key, err := wgtypes.ParseKey(i.PrivateKey)
	if err != nil {
		return err
	}

	cfg := wgtypes.Config{
		PrivateKey: &key,
		ListenPort: &i.Port,
	}
	if i.Fwmark > 0 {
		cfg.FirewallMark = &i.Fwmark
	}
	return i.configureWGDevice(cfg)
}

func (i *wgInterface) chains() (string, string) {
	return fmt.Sprintf("wg-%s-in", i.Name),
		fmt.Sprintf("wg-%s-out", i.Name)
}

func (i *wgInterface) startInterface() error {
	chainIn, chainOut := i.chains()
	return runMany(
		ignoreErrs(
			newCmd("ip", "link", "set", i.Name, "down"),
			newCmd("ip", "link", "del", "dev", i.Name),
			newCmd("iptables", "-N", chainIn),
			newCmd("iptables", "-N", chainOut),
			newCmd("iptables", "-D", "FORWARD", "-i", i.Name, "-j", chainIn),
			newCmd("iptables", "-D", "FORWARD", "-o", i.Name, "-j", chainOut),
		),

		newCmd("ip", "link", "add", "dev", i.Name, "type", "wireguard"),
		newCmd("ip", "address", "add", "dev", i.Name, i.IP.String()),
		newCmd("ip", "link", "set", "mtu", "1420", "dev", i.Name),

		newCmd("iptables", "-F", chainIn),
		newCmd("iptables", "-F", chainOut),
		newCmd("iptables", "-A", chainOut, "-p", "tcp", "--dport", "25", "-j", "DROP"),
		newCmd("iptables", "-A", chainOut, "-j", "ACCEPT"),
		newCmd("iptables", "-A", chainIn, "-p", "tcp", "--dport", "25", "-j", "DROP"),
		newCmd("iptables", "-A", chainIn, "-j", "ACCEPT"),
		newCmd("iptables", "-A", "FORWARD", "-i", i.Name, "-j", chainIn),
		newCmd("iptables", "-A", "FORWARD", "-o", i.Name, "-j", chainOut),
		newCmd("ip", "link", "set", i.Name, "up"),
	)
}

func (i *wgInterface) stopInterface() error {
	chainIn, chainOut := i.chains()
	return runMany(
		newCmd("ip", "link", "set", i.Name, "down"),
		newCmd("ip", "link", "del", "dev", i.Name),

		newCmd("iptables", "-D", "FORWARD", "-i", i.Name, "-j", chainIn),
		newCmd("iptables", "-D", "FORWARD", "-o", i.Name, "-j", chainOut),
		newCmd("iptables", "-F", chainIn),
		newCmd("iptables", "-F", chainOut),
		newCmd("iptables", "-X", chainIn),
		newCmd("iptables", "-X", chainOut),
	)
}

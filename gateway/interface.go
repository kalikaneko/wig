package gateway

import (
	"fmt"
	"log"

	"git.autistici.org/ai3/tools/wig/datastore/model"
	"github.com/vishvananda/netlink"
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

	if err := wgi.startInterface(); err != nil {
		return nil, err
	}
	if err := wgi.initialize(); err != nil {
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

// netlink pkg still lacks Wireguard type.
type wireguard struct {
	netlink.LinkAttrs
}

func (wg *wireguard) Attrs() *netlink.LinkAttrs {
	return &wg.LinkAttrs
}

func (wg *wireguard) Type() string {
	return "wireguard"
}

func (i *wgInterface) startInterface() error {
	// Bring the device down if it's currently up.
	if lnk, err := netlink.LinkByName(i.Name); err == nil {
		if err := netlink.LinkSetDown(lnk); err != nil {
			return fmt.Errorf("ip link set %s down: %w", i.Name, err)
		}
		if err := netlink.LinkDel(lnk); err != nil {
			return fmt.Errorf("ip del %s: %w", i.Name, err)
		}
	}

	log.Printf("configuring network interface %s", i.Name)

	attrs := netlink.NewLinkAttrs()
	attrs.Name = i.Name
	attrs.MTU = 1420
	lnk := &wireguard{LinkAttrs: attrs}
	if err := netlink.LinkAdd(lnk); err != nil {
		return fmt.Errorf("ip link add %s: %w", i.Name, err)
	}
	if i.IP != nil {
		if err := netlink.AddrAdd(lnk, &netlink.Addr{IPNet: &i.IP.IPNet}); err != nil {
			return fmt.Errorf("ip addr add %s: %w", i.Name, err)
		}
	}
	if i.IP6 != nil {
		if err := netlink.AddrAdd(lnk, &netlink.Addr{IPNet: &i.IP6.IPNet}); err != nil {
			return fmt.Errorf("ip addr add %s: %w", i.Name, err)
		}
	}

	return netlink.LinkSetUp(lnk)
}

func (i *wgInterface) stopInterface() error {
	log.Printf("stopping network interface %s", i.Name)

	lnk, err := netlink.LinkByName(i.Name)
	if err != nil {
		return nil
	}

	if err := netlink.LinkSetDown(lnk); err != nil {
		return fmt.Errorf("ip link set %s down: %w", i.Name, err)
	}
	if err := netlink.LinkDel(lnk); err != nil {
		return fmt.Errorf("ip del %s: %w", i.Name, err)
	}
	return nil
}

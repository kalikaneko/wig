package gateway

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/peerdb"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Gateway struct {
	port  int
	intf  *datastore.Interface
	ctrl  *wgctrl.Client
	seq   peerdb.Sequence
	stats StatsCollector
}

func New(port int, intf *datastore.Interface, stats StatsCollector) (*Gateway, error) {
	ctrl, err := wgctrl.New()
	if err != nil {
		return nil, err
	}

	n := &Gateway{
		port:  port,
		intf:  intf,
		ctrl:  ctrl,
		stats: stats,
	}

	err = n.initialize()
	if errors.Is(err, os.ErrNotExist) {
		if err = n.startInterface(); err != nil {
			ctrl.Close()
			return nil, err
		}
		err = n.initialize()
	}
	if err != nil {
		ctrl.Close()
		return nil, err
	}

	go n.statsLoop()

	return n, nil
}

func (n *Gateway) configureWGDevice(cfg wgtypes.Config) error {
	return n.ctrl.ConfigureDevice(n.intf.Name, cfg)
}

func (n *Gateway) initialize() error {
	key, err := wgtypes.ParseKey(n.intf.PrivateKey)
	if err != nil {
		return err
	}

	cfg := wgtypes.Config{
		PrivateKey: &key,
		ListenPort: &n.port,
	}
	if n.intf.Fwmark > 0 {
		cfg.FirewallMark = &n.intf.Fwmark
	}
	if err := n.configureWGDevice(cfg); err != nil {
		return err
	}

	return n.raiseInterface()
}

func (n *Gateway) chains() (string, string) {
	return fmt.Sprintf("wg-%s-in", n.intf.Name),
		fmt.Sprintf("wg-%s-out", n.intf.Name)
}

func (n *Gateway) startInterface() error {
	chainIn, chainOut := n.chains()
	return runMany(
		newCmd("ip", "link", "add", "dev", n.intf.Name, "type", "wireguard"),
		newCmd("ip", "address", "add", "dev", n.intf.Name, n.intf.IP.String()),
		newCmd("ip", "link", "set", "mtu", "1420", "dev", n.intf.Name),

		ignoreErrs(
			newCmd("iptables", "-N", chainIn),
			newCmd("iptables", "-N", chainOut),
			newCmd("iptables", "-D", "FORWARD", "-i", n.intf.Name, "-j", chainIn),
			newCmd("iptables", "-D", "FORWARD", "-o", n.intf.Name, "-j", chainOut),
		),

		newCmd("iptables", "-F", chainIn),
		newCmd("iptables", "-F", chainOut),
		newCmd("iptables", "-A", chainOut, "-p", "tcp", "--dport", "25", "-j", "DROP"),
		newCmd("iptables", "-A", chainOut, "-j", "ACCEPT"),
		newCmd("iptables", "-A", chainIn, "-p", "tcp", "--dport", "25", "-j", "DROP"),
		newCmd("iptables", "-A", chainIn, "-j", "ACCEPT"),
		newCmd("iptables", "-A", "FORWARD", "-i", n.intf.Name, "-j", chainIn),
		newCmd("iptables", "-A", "FORWARD", "-o", n.intf.Name, "-j", chainOut),
	)
}

func (n *Gateway) raiseInterface() error {
	return runMany(
		newCmd("ip", "link", "set", n.intf.Name, "up"),
		newCmd("ip", "link", "show", "dev", n.intf.Name),
		newCmd("ip", "addr", "show", "dev", n.intf.Name),
	)
}

func (n *Gateway) stopInterface() error {
	chainIn, chainOut := n.chains()
	return runMany(
		newCmd("ip", "link", "set", n.intf.Name, "down"),
		newCmd("ip", "link", "del", "dev", n.intf.Name),

		newCmd("iptables", "-D", "FORWARD", "-i", n.intf.Name, "-j", chainIn),
		newCmd("iptables", "-D", "FORWARD", "-o", n.intf.Name, "-j", chainOut),
		newCmd("iptables", "-F", chainIn),
		newCmd("iptables", "-F", chainOut),
		newCmd("iptables", "-X", chainIn),
		newCmd("iptables", "-X", chainOut),
	)
}

func (n *Gateway) Close() {
	n.ctrl.Close()
	if err := n.stopInterface(); err != nil {
		log.Printf("error stopping interface %s: %v", n.intf.Name, err)
	}
}

func (n *Gateway) LatestSequence() peerdb.Sequence { return n.seq }

func (n *Gateway) Apply(op peerdb.Op) error {
	var cfg wgtypes.PeerConfig
	var err error

	switch op.Type {
	case peerdb.OpAdd:
		log.Printf("creating peer %+v", op.Peer)
		cfg, err = peerToConfig(&op.Peer, false, false)

		// DEBUG
		defer func() {
			runSysCmd("wg", "show")
		}()
	case peerdb.OpUpdate:
		log.Printf("updating peer %+v", op.Peer)
		cfg, err = peerToConfig(&op.Peer, true, false)
	case peerdb.OpDelete:
		log.Printf("deleting peer %s", op.Peer.PublicKey)
		cfg, err = peerToConfig(&op.Peer, false, true)
	}
	if err != nil {
		return err
	}

	n.seq = op.Seq + 1
	return n.configureWGDevice(wgtypes.Config{
		Peers: []wgtypes.PeerConfig{cfg},
	})
}

func (n *Gateway) LoadSnapshot(snap peerdb.Snapshot) error {
	var peers []wgtypes.PeerConfig
	if err := snap.Each(func(peer *datastore.Peer) error {
		cfg, err := peerToConfig(peer, false, false)
		if err != nil {
			return err
		}
		peers = append(peers, cfg)
		return nil
	}); err != nil {
		return err
	}

	if err := n.configureWGDevice(wgtypes.Config{
		ReplacePeers: true,
		Peers:        peers,
	}); err != nil {
		return err
	}

	n.seq = snap.Seq()
	return nil
}

type command struct {
	cmd  string
	args []string
}

func newCmd(cmd string, args ...string) *command {
	return &command{cmd, args}
}

func (c *command) run() error {
	return runSysCmd(c.cmd, c.args...)
}

type ignoreErrsRunnable struct {
	cmds []runnable
}

func ignoreErrs(cmds ...runnable) *ignoreErrsRunnable {
	return &ignoreErrsRunnable{cmds: cmds}
}

func (r *ignoreErrsRunnable) run() error {
	for _, cmd := range r.cmds {
		cmd.run() // nolint: errcheck
	}
	return nil
}

func runSysCmd(cmd string, args ...string) error {
	log.Printf("running command: %s %s", cmd, strings.Join(args, " "))
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	//out, err := exec.Command(cmd, args...).CombinedOutput()
	//if err != nil {
	//	log.Printf("command %s failed (%v):\n%s", cmd, err, out)
	//	return err
	//}
	return c.Run()
}

type runnable interface {
	run() error
}

func runMany(cmds ...runnable) error {
	for _, cmd := range cmds {
		if err := cmd.run(); err != nil {
			return err
		}
	}
	return nil
}

func peerToConfig(peer *datastore.Peer, update, remove bool) (wgtypes.PeerConfig, error) {
	key, err := wgtypes.ParseKey(peer.PublicKey)
	if err != nil {
		return wgtypes.PeerConfig{}, err
	}

	c := wgtypes.PeerConfig{
		PublicKey:    key,
		Remove:       remove,
		UpdateOnly:   update,
		PresharedKey: new(wgtypes.Key),
	}

	if !remove {
		var allowedIPs []net.IPNet
		if peer.IP != nil {
			allowedIPs = append(allowedIPs, peer.IP.IPNet)
		}
		if peer.IP6 != nil {
			allowedIPs = append(allowedIPs, peer.IP6.IPNet)
		}
		if len(allowedIPs) == 0 {
			return wgtypes.PeerConfig{}, errors.New("no IPs configured for peer")
		}

		c.AllowedIPs = allowedIPs
		c.ReplaceAllowedIPs = true
		c.PersistentKeepaliveInterval = &persistentKeepaliveInterval
	}

	return c, nil
}

var persistentKeepaliveInterval = 10 * time.Second

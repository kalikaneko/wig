package gateway

import (
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"git.autistici.org/ai3/tools/wig/datastore/crudlog"
	"git.autistici.org/ai3/tools/wig/datastore/model"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Gateway struct {
	ctrl *wgctrl.Client

	mx        sync.Mutex
	intfs     map[string]*wgInterface
	peerIndex map[string]string

	seq   crudlog.Sequence
	stats StatsCollector
}

func New(stats StatsCollector) (*Gateway, error) {
	ctrl, err := wgctrl.New()
	if err != nil {
		return nil, err
	}

	gw := &Gateway{
		intfs:     make(map[string]*wgInterface),
		peerIndex: make(map[string]string),
		ctrl:      ctrl,
		stats:     stats,
	}

	go gw.statsLoop()

	return gw, nil
}

func (n *Gateway) Close() {
	n.closeAllInterfaces()
	n.ctrl.Close()
}

func (n *Gateway) closeAllInterfaces() {
	for _, i := range n.intfs {
		if err := i.stopInterface(); err != nil {
			log.Printf("error stopping interface %s: %v", i.Name, err)
		}
	}
}

func (n *Gateway) LatestSequence() crudlog.Sequence {
	n.mx.Lock()
	defer n.mx.Unlock()
	return n.seq
}

func (n *Gateway) LoadSnapshot(snap crudlog.Snapshot) error {
	intfs, peers, err := splitSnapshot(snap)
	if err != nil {
		return err
	}

	n.mx.Lock()
	defer n.mx.Unlock()

	n.closeAllInterfaces()

	// Create all interfaces.
	tmpIntfs := make(map[string]*wgInterface)
	tmpPeerIndex := make(map[string]string)
	for _, intf := range intfs {
		wgi, err := newInterface(n.ctrl, intf)
		if err != nil {
			return err
		}
		tmpIntfs[intf.Name] = wgi

		var wgPeers []wgtypes.PeerConfig
		for _, peer := range peers[intf.Name] {
			cfg, err := peerToConfig(peer, false, false)
			if err != nil {
				return err
			}
			wgPeers = append(wgPeers, cfg)
			tmpPeerIndex[peer.PublicKey] = intf.Name
		}
		if err := wgi.configureWGDevice(wgtypes.Config{
			ReplacePeers: true,
			Peers:        wgPeers,
		}); err != nil {
			return err
		}
	}

	n.intfs = tmpIntfs
	n.peerIndex = tmpPeerIndex
	n.seq = snap.Seq()
	return nil
}

func splitSnapshot(snap crudlog.Snapshot) (intfs []*model.Interface, peers map[string][]*model.Peer, err error) {
	peers = make(map[string][]*model.Peer)
	err = snap.Each(func(obj interface{}) error {
		switch value := obj.(type) {
		case *model.Interface:
			intfs = append(intfs, value)
		case *model.Peer:
			peers[value.Interface] = append(peers[value.Interface], value)
		}
		return nil
	})
	return
}

func (n *Gateway) Apply(op crudlog.Op, fromLog bool) error {
	n.mx.Lock()
	defer n.mx.Unlock()

	var err error

	switch value := op.Value().(type) {
	case *model.Peer:
		err = n.applyPeer(op.Type(), value)
	case *model.Interface:
		err = n.applyInterface(op.Type(), value)
	}
	if err != nil {
		return err
	}

	n.seq = op.Seq()
	return nil
}

func (n *Gateway) applyInterface(opType crudlog.OpType, intf *model.Interface) error {
	switch opType {
	case crudlog.OpCreate:
		if _, ok := n.intfs[intf.Name]; ok {
			return errors.New("interface already exists")
		}
		log.Printf("creating interface %+v", intf)

		gwi, err := newInterface(n.ctrl, intf)
		if err != nil {
			return err
		}
		n.intfs[intf.Name] = gwi

	case crudlog.OpUpdate:
		wgi, ok := n.intfs[intf.Name]
		if !ok {
			return errors.New("interface does not exist")
		}
		return wgi.reconfigure(intf)

	case crudlog.OpDelete:
		if _, ok := n.intfs[intf.Name]; !ok {
			return errors.New("interface does not exist")
		}
		delete(n.intfs, intf.Name)
	}
	return nil
}

func (n *Gateway) applyPeer(opType crudlog.OpType, peer *model.Peer) error {
	var wgi *wgInterface
	var cfg wgtypes.PeerConfig
	var err error
	var ok bool

	switch opType {
	case crudlog.OpCreate:
		wgi, ok = n.intfs[peer.Interface]
		if !ok {
			return errors.New("interface does not exist")
		}
		log.Printf("creating peer %+v", peer)
		n.peerIndex[peer.PublicKey] = peer.Interface
		cfg, err = peerToConfig(peer, false, false)

	case crudlog.OpUpdate:
		wgi, ok = n.intfs[peer.Interface]
		if !ok {
			return errors.New("interface does not exist")
		}

		// If the update has changed interface, deconfigure
		// the peer from the previous interface.
		oldIntf := n.peerIndex[peer.PublicKey]
		if oldIntf != "" && oldIntf != peer.Interface {
			// nolint: errcheck
			delCfg, _ := peerToConfig(peer, false, true)
			// nolint: errcheck
			n.intfs[oldIntf].configureWGDevice(wgtypes.Config{
				Peers: []wgtypes.PeerConfig{delCfg},
			})
		}

		log.Printf("updating peer %+v", peer)
		n.peerIndex[peer.PublicKey] = peer.Interface
		cfg, err = peerToConfig(peer, true, false)

	case crudlog.OpDelete:
		wgi = n.intfs[n.peerIndex[peer.PublicKey]]
		log.Printf("deleting peer %s", peer.PublicKey)
		cfg, err = peerToConfig(peer, false, true)
	}
	if err != nil {
		return err
	}

	return wgi.configureWGDevice(wgtypes.Config{
		Peers: []wgtypes.PeerConfig{cfg},
	})
}

func peerToConfig(peer *model.Peer, update, remove bool) (wgtypes.PeerConfig, error) {
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

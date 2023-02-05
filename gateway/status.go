package gateway

import (
	"context"
	"log"
	"time"
)

var (
	statsInterval = 1 * time.Minute
	statsTimeout  = 20 * time.Second
)

type PeerStats struct {
	PublicKey         string    `json:"public_key"`
	LastHandshakeTime time.Time `json:"last_handshake_time"`
	RxBytes           int64     `json:"rx_bytes"`
	TxBytes           int64     `json:"tx_bytes"`
	Endpoint          string    `json:"endpoint"`
}

type StatsDump []PeerStats

func (n *Gateway) collectStats() (StatsDump, error) {
	dev, err := n.ctrl.Device(n.intf.Name)
	if err != nil {
		return nil, err
	}
	out := make([]PeerStats, 0, len(dev.Peers))
	for _, peer := range dev.Peers {
		s := PeerStats{
			PublicKey:         peer.PublicKey.String(),
			LastHandshakeTime: peer.LastHandshakeTime,
			RxBytes:           peer.ReceiveBytes,
			TxBytes:           peer.TransmitBytes,
		}
		if peer.Endpoint != nil {
			s.Endpoint = peer.Endpoint.IP.String()
		}
		out = append(out, s)
	}
	return StatsDump(out), nil
}

func (n *Gateway) updateStats(ctx context.Context) error {
	stats, err := n.collectStats()
	if err != nil {
		return err
	}
	return n.stats.ReceivePeerStats(ctx, stats)
}

func (n *Gateway) statsLoop() {
	for range time.NewTicker(statsInterval).C {
		ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
		err := n.updateStats(ctx)
		cancel()

		if err != nil {
			log.Printf("stats collection error: %v", err)
		}
	}
}

type StatsCollector interface {
	ReceivePeerStats(context.Context, StatsDump) error
}

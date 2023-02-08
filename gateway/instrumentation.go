package gateway

import "github.com/prometheus/client_golang/prometheus"

var (
	rxBytesDesc = prometheus.NewDesc(
		"wig_receive_bytes_total",
		"Total bytes received, by peer.",
		[]string{"peer"}, nil,
	)
	txBytesDesc = prometheus.NewDesc(
		"wig_transmit_bytes_total",
		"Total bytes transmitted, by peer.",
		[]string{"peer"}, nil,
	)
)

func (n *Gateway) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(n, ch)
}

func (n *Gateway) Collect(ch chan<- prometheus.Metric) {
	n.mx.Lock()
	defer n.mx.Unlock()

	for _, i := range n.intfs {
		stats, err := i.collectStats()
		if err != nil {
			continue
		}
		for _, ps := range stats {
			ch <- prometheus.MustNewConstMetric(
				rxBytesDesc,
				prometheus.CounterValue,
				float64(ps.RxBytes),
				ps.PublicKey)
			ch <- prometheus.MustNewConstMetric(
				txBytesDesc,
				prometheus.CounterValue,
				float64(ps.TxBytes),
				ps.PublicKey)
		}
	}
}

package collector

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"

	"git.autistici.org/ai3/attic/wig/gateway"
)

type statsCollectorStub struct {
	client *http.Client
	uri    string
}

func NewStatsCollectorStub(uri string, tlsConf *tls.Config) gateway.StatsCollector {
	return &statsCollectorStub{
		uri: uri,
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConf,
			},
		},
	}
}

func (c *statsCollectorStub) ReceivePeerStats(ctx context.Context, stats gateway.StatsDump) error {
	payload, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.uri+apiURLReceive, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	return nil
}

package sessions

import (
	"context"
	"net/http"

	"git.autistici.org/ai3/attic/wig/datastore/crud/httptransport"
	"git.autistici.org/ai3/attic/wig/datastore/model"
	"git.autistici.org/ai3/attic/wig/gateway"
)

type statsCollectorStub struct {
	client *http.Client
	uri    string
}

func NewStatsCollectorStub(uri string, client *http.Client) gateway.StatsCollector {
	return &statsCollectorStub{
		uri:    uri,
		client: client,
	}
}

func (c *statsCollectorStub) ReceivePeerStats(ctx context.Context, stats gateway.StatsDump) error {
	return httptransport.Do(ctx, c.client, "POST", c.uri+apiURLReceive, stats, nil)
}

func (c *statsCollectorStub) GetSessions(ctx context.Context, pkey string) (sessions []*model.Session, err error) {
	err = httptransport.Do(ctx, c.client, "GET", c.uri+apiURLGetSessions, nil, &sessions)
	return
}

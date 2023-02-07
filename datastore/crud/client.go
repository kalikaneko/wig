package crud

import (
	"context"
	"net/http"
	"net/url"
	"reflect"

	"git.autistici.org/ai3/attic/wig/datastore/crud/httptransport"
)

type typeClient struct {
	t Type

	uri    string
	client *http.Client
}

func newTypeClient(uri string, t Type, httpc *http.Client) *typeClient {
	return &typeClient{
		t:      t,
		uri:    uri,
		client: httpc,
	}
}

func (c *typeClient) verbURL(verb string) string {
	return httptransport.JoinURL(c.uri, c.t.Name(), verb)
}

func (c *typeClient) requestWithObj(ctx context.Context, method, verb string, obj interface{}) error {
	return httptransport.Do(ctx, c.client, method, c.verbURL(verb), obj, nil)
}

func (c *typeClient) Create(ctx context.Context, obj interface{}) error {
	return c.requestWithObj(ctx, "POST", "create", obj)
}

func (c *typeClient) Update(ctx context.Context, obj interface{}) error {
	return c.requestWithObj(ctx, "POST", "update", obj)
}

func (c *typeClient) Delete(ctx context.Context, obj interface{}) error {
	return c.requestWithObj(ctx, "POST", "delete", obj)
}

func (c *typeClient) Find(ctx context.Context, _ string, query map[string]string, f func(interface{}) error) error {
	values := make(url.Values)
	for k, v := range query {
		values.Set(k, v)
	}

	// Use reflect to build a list of model.NewInstance() types.
	l := reflect.MakeSlice(
		reflect.SliceOf(reflect.TypeOf(c.t.NewInstance())), 0, 4)

	if err := httptransport.Do(ctx, c.client, "GET", c.verbURL("find")+"?"+values.Encode(), nil, &l); err != nil {
		return err
	}

	tmpl := l.Interface().([]interface{})
	for _, obj := range tmpl {
		if err := f(obj); err != nil {
			return err
		}
	}

	return nil
}

type Client struct {
	registry *registry
	clients  map[string]*typeClient
}

func (m *Model) Client(uri string, httpc *http.Client) *Client {
	c := &Client{
		registry: m.registry,
		clients:  make(map[string]*typeClient),
	}

	// nolint: errcheck
	m.registry.each(func(t Type) error {
		c.clients[t.Name()] = newTypeClient(uri, t, httpc)
		return nil
	})
	return c
}

func (c *Client) Get(typ string) API {
	return c.clients[typ]
}

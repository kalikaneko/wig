package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

type Client interface {
	Add(context.Context, interface{}) error
	Update(context.Context, interface{}) error
	Delete(context.Context, string) error
	Find(context.Context, string, interface{}) error
}

type restClient struct {
	name string

	uri    string
	client *http.Client
}

func newClient(uri, name string, tlsConf *tls.Config) *restClient {
	return &restClient{
		uri:  joinURL(uri, apiURLBase),
		name: name,
		client: &http.Client{
			Transport: &http.Transport{
				IdleConnTimeout: 300 * time.Second,
				TLSClientConfig: tlsConf,
			},
		},
	}
}

func (r *restClient) requestWithObj(ctx context.Context, method, verb string, obj interface{}) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(obj); err != nil {
		return err
	}

	req, err := http.NewRequest(method, joinURL(r.uri, r.name, verb), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return UnwrapError(resp)
	}
	return nil
}

func (r *restClient) requestWithID(ctx context.Context, method, verb, id string, out interface{}) error {
	var v url.Values
	v.Set("id", id)
	req, err := http.NewRequest(method, joinURL(r.uri, r.name, verb)+"?"+v.Encode(), nil)
	if err != nil {
		return err
	}

	resp, err := r.client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return UnwrapError(resp)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}

	return nil
}

func (r *restClient) Add(ctx context.Context, obj interface{}) error {
	return r.requestWithObj(ctx, "POST", "add", obj)
}

func (r *restClient) Update(ctx context.Context, obj interface{}) error {
	return r.requestWithObj(ctx, "POST", "update", obj)
}

func (r *restClient) Delete(ctx context.Context, id string) error {
	return r.requestWithID(ctx, "POST", "delete", id, nil)
}

func (r *restClient) Find(ctx context.Context, id string, out interface{}) error {
	return r.requestWithID(ctx, "POST", "find", id, out)
}

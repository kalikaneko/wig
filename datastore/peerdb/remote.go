package peerdb

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore/rest"
	"github.com/cenkalti/backoff/v4"
)

type remotePubsubClient struct {
	uri    string
	client *http.Client
}

const (
	apiURLSnapshot  = "/api/v1/log/snapshot"
	apiURLSubscribe = "/api/v1/log/subscribe"
)

func joinURL(a, b string) string {
	out := a
	if !strings.HasSuffix(a, "/") {
		out += "/"
	}
	out += strings.TrimPrefix(b, "/")
	return out
}

func NewRemoteLog(uri string, tlsConf *tls.Config) Log {
	return newRemotePubsubClient(uri, tlsConf)
}

func newRemotePubsubClient(uri string, tlsConf *tls.Config) *remotePubsubClient {
	return &remotePubsubClient{
		uri: uri,
		client: &http.Client{
			Transport: &http.Transport{
				IdleConnTimeout: 300 * time.Second,
				TLSClientConfig: tlsConf,
			},
		},
	}
}

func (r *remotePubsubClient) Snapshot(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	err := backoff.Retry(
		func() (err error) {
			snap, err = r.doSnapshot(ctx)
			err = maybeTempError(err)
			return
		},
		backoff.WithContext(rest.RetryPolicy, ctx),
	)
	return snap, err
}

func (r *remotePubsubClient) doSnapshot(ctx context.Context) (Snapshot, error) {
	req, err := http.NewRequest("GET", joinURL(r.uri, apiURLSnapshot), nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, rest.UnwrapError(resp)
	}

	var snap memSnapshot
	if err := json.NewDecoder(bufio.NewReader(resp.Body)).Decode(&snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (r *remotePubsubClient) Subscribe(ctx context.Context, start Sequence) (Subscription, error) {
	var sub Subscription
	err := backoff.Retry(
		func() (err error) {
			sub, err = r.doSubscribe(ctx, start)
			err = maybeTempError(err)
			return
		},
		backoff.WithContext(rest.RetryPolicy, ctx),
	)
	return sub, err
}

func (r *remotePubsubClient) doSubscribe(ctx context.Context, start Sequence) (Subscription, error) {
	uri := joinURL(r.uri, apiURLSubscribe) + "?start=" + start.String()
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		return nil, rest.UnwrapError(resp)
	}
	return newRemoteSubscription(ctx, resp), nil
}

type remoteSubscription struct {
	ctx     context.Context
	resp    *http.Response
	scanner *bufio.Scanner
}

func newRemoteSubscription(ctx context.Context, resp *http.Response) *remoteSubscription {
	return &remoteSubscription{
		ctx:     ctx,
		resp:    resp,
		scanner: bufio.NewScanner(resp.Body),
	}
}

func (s *remoteSubscription) loop(ch chan Op) {
	defer close(ch)

	// TODO: If the outer context is canceled, close the response
	// to prevent a deadlock (should break out of scanner.Scan,
	// but IT DOES NOT).

	done := make(chan bool)
	go func() {
		defer close(done)
		for s.scanner.Scan() {
			b := s.scanner.Bytes()
			if len(b) == 0 {
				// Empty lines are a normal result of chunked
				// encoding.
				continue
			}
			var op Op
			if err := json.Unmarshal(b, &op); err != nil {
				log.Printf("error unmarshaling op: %v", err)
				return
			}
			ch <- op
		}
	}()

	select {
	case <-s.ctx.Done():
		if s.ctx.Err() != nil {
			s.resp.Body.Close()
		}
	case <-done:
	}
}

func (s *remoteSubscription) Notify() <-chan Op {
	ch := make(chan Op, chanBufSize)
	go s.loop(ch)
	return ch
}

func (s *remoteSubscription) Close() {
	s.resp.Body.Close()
}

type LogHTTPHandler struct {
	Log

	wrap http.Handler
	done chan struct{}
}

func NewLogHTTPHandler(l Log, h http.Handler) *LogHTTPHandler {
	return &LogHTTPHandler{
		Log:  l,
		wrap: h,
		done: make(chan struct{}),
	}
}

func (s *LogHTTPHandler) Close() {
	close(s.done)
}

func (s *LogHTTPHandler) handleSnapshot(w http.ResponseWriter, req *http.Request) {
	log.Printf("HTTPServer: Snapshot()")

	snap, err := s.Log.Snapshot(req.Context())
	if err != nil {
		log.Printf("Snapshot() error: %v", err)
		rest.HTTPError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	msnap := snap.(*memSnapshot)
	bw := bufio.NewWriter(w)
	if err := json.NewEncoder(bw).Encode(msnap); err != nil {
		log.Printf("Snapshot() write error: %v", err)
		return
	}
	bw.Flush()
}

func (s *LogHTTPHandler) handleSubscribe(w http.ResponseWriter, req *http.Request) {
	// Parse the 'start' parameter.
	start, err := ParseSequence(req.FormValue("start"))
	if err != nil {
		http.Error(w, "Bad start parameter", http.StatusBadRequest)
		return
	}

	log.Printf("HTTPServer: Subscribe(%s)", start)

	sub, err := s.Log.Subscribe(req.Context(), start)
	if err != nil {
		log.Printf("Subscribe() error: %v", err)
		rest.HTTPError(w, err)
		return
	}
	defer sub.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Transfer-Encoding", "chunked")

	ch := sub.Notify()
	for {
		select {
		case op := <-ch:
			if err := json.NewEncoder(w).Encode(&op); err != nil {
				log.Printf("Subscribe() write error: %v", err)
				return
			}
			if _, err := w.Write([]byte{'\n'}); err != nil {
				log.Printf("Subscribe() write error: %v", err)
				return
			}
			flusher.Flush()
		case <-s.done:
			return
		}
	}
}

func (s *LogHTTPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case apiURLSnapshot:
		s.handleSnapshot(w, req)
	case apiURLSubscribe:
		s.handleSubscribe(w, req)
	default:
		if s.wrap != nil {
			s.wrap.ServeHTTP(w, req)
			return
		}
		http.NotFound(w, req)
	}
}

func init() {
	rest.RegisterError("horizon", ErrHorizon)
	rest.RegisterError("out-of-sequence", ErrOutOfSequence)
	rest.RegisterError("readonly", ErrReadOnly)
}

func maybeTempError(err error) error {
	if err != nil {
		switch {
		case errors.Is(err, &url.Error{}):
		case errors.Is(err, &net.DNSError{}):
		default:
			return backoff.Permanent(err)
		}
	}
	return err
}

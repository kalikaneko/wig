package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
)

const apiURLBase = "/api/v1"

type Handler interface {
	Name() string
	NewInstance() interface{}
	Add(interface{}) error
	Update(interface{}) error
	Delete(string) error
	Find(string) (interface{}, bool)
}

type httpHandler struct {
	rest Handler
	wrap http.Handler

	addURL, deleteURL, updateURL, findURL string
}

func NewHandler(rest Handler, h http.Handler) http.Handler {
	urlPrefix := joinURL(apiURLBase, rest.Name())
	return &httpHandler{
		rest:      rest,
		addURL:    joinURL(urlPrefix, "add"),
		updateURL: joinURL(urlPrefix, "update"),
		deleteURL: joinURL(urlPrefix, "delete"),
		findURL:   joinURL(urlPrefix, "find"),
		wrap:      h,
	}
}

func (s *httpHandler) jsonOp(w http.ResponseWriter, req *http.Request, f func(interface{}) error) {
	if req.Method != "POST" {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	obj := s.rest.NewInstance()
	if err := json.NewDecoder(req.Body).Decode(obj); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := f(obj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(200)
}

func (s *httpHandler) opWithArg(w http.ResponseWriter, req *http.Request, method string, f func(string) error) {
	if req.Method != method {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	id := req.FormValue("id")

	err := f(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *httpHandler) handleAdd(w http.ResponseWriter, req *http.Request) {
	s.jsonOp(w, req, s.rest.Add)
}

func (s *httpHandler) handleUpdate(w http.ResponseWriter, req *http.Request) {
	s.jsonOp(w, req, s.rest.Update)
}

func (s *httpHandler) handleDelete(w http.ResponseWriter, req *http.Request) {
	s.opWithArg(w, req, "POST", s.rest.Delete)
}

func (s *httpHandler) handleFind(w http.ResponseWriter, req *http.Request) {
	s.opWithArg(w, req, "GET", func(id string) error {
		obj, ok := s.rest.Find(id)
		if !ok {
			http.NotFound(w, req)
			return nil
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(obj) // nolint: errcheck
		return nil
	})
}

func (s *httpHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case s.addURL:
		s.handleAdd(w, req)
	case s.updateURL:
		s.handleUpdate(w, req)
	case s.deleteURL:
		s.handleDelete(w, req)
	case s.findURL:
		s.handleFind(w, req)
	default:
		if s.wrap != nil {
			s.wrap.ServeHTTP(w, req)
			return
		}
		http.NotFound(w, req)
	}
}

func joinURL(parts ...string) string {
	out := parts[0]
	for _, part := range parts[1:] {
		if !strings.HasSuffix(out, "/") {
			out += "/"
		}
		out += strings.TrimPrefix(part, "/")
	}
	return out
}

type errResponse struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

type errorRegistryEntry struct {
	code string
	err  error
}

var errorRegistry []errorRegistryEntry

func RegisterError(code string, err error) {
	errorRegistry = append(errorRegistry, errorRegistryEntry{
		code: code,
		err:  err,
	})
}

func HTTPError(w http.ResponseWriter, err error) {
	var resp errResponse
	resp.Message = err.Error()
	status := http.StatusInternalServerError
	for _, merr := range errorRegistry {
		if errors.Is(err, merr.err) {
			status = http.StatusBadRequest
			resp.Code = merr.code
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(&resp) // nolint: errcheck, errchkjson
}

func UnwrapError(resp *http.Response) error {
	if resp.StatusCode == http.StatusBadRequest && resp.Header.Get("Content-Type") == "application/json" {
		var errResp errResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return errors.New("malformed remote error response")
		}
		for _, merr := range errorRegistry {
			if errResp.Code == merr.code {
				return backoff.Permanent(merr.err)
			}
		}
	}
	err := fmt.Errorf("HTTP status code %d", resp.StatusCode)
	if resp.StatusCode != 429 && resp.StatusCode < 500 {
		err = backoff.Permanent(err)
	}
	return err
}

var RetryPolicy = newPermanentRetryBackOff()

func newPermanentRetryBackOff() backoff.BackOff {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 200 * time.Millisecond
	exp.RandomizationFactor = 0.2
	exp.MaxInterval = 60 * time.Second
	exp.MaxElapsedTime = 0
	return exp
}

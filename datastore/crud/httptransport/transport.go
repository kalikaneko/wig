package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ServeJSON handles a JSON request, calling 'f' and encoding its response.
// The caller must provide storage for the JSON request object if necessary.
func ServeJSON(w http.ResponseWriter, req *http.Request, reqObj interface{}, f func() (interface{}, error)) {
	if reqObj != nil {
		if err := json.NewDecoder(req.Body).Decode(reqObj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	respObj, err := f()
	if err != nil {
		HTTPError(w, err)
		return
	}
	if respObj != nil {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(respObj); err != nil {
			HTTPError(w, err)
			return
		}
	}

	// Empty response.
	w.WriteHeader(200)
}

// Do performs a JSON request to the specified uri. Request and response objects can be nil.
func Do(ctx context.Context, client *http.Client, method, uri string, reqObj, respObj interface{}) error {
	var input io.Reader
	if reqObj != nil {
		payload, err := json.Marshal(reqObj)
		if err != nil {
			return err
		}
		input = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, uri, input)
	if err != nil {
		return err
	}
	if reqObj != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return UnwrapError(resp)
	}

	if respObj != nil {
		return json.NewDecoder(resp.Body).Decode(respObj)
	}
	return nil
}

// JoinURL concatenates multiple URL segments into one.
func JoinURL(parts ...string) string {
	out := parts[0]
	for _, part := range parts[1:] {
		if !strings.HasSuffix(out, "/") {
			out += "/"
		}
		out += strings.TrimPrefix(part, "/")
	}
	return out
}

package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/cenkalti/backoff/v4"
)

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

	log.Printf("http error: %v", err)

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

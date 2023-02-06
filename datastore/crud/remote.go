package crud

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"git.autistici.org/ai3/attic/wig/datastore/crud/httpapi"
	"git.autistici.org/ai3/attic/wig/datastore/crud/httptransport"
)

type typeHandler struct {
	t   Type
	api API
}

func newTypeHTTPHandler(t Type, api API, urlPrefix string) http.Handler {
	h := &typeHandler{
		t:   t,
		api: api,
	}

	mux := http.NewServeMux()
	log.Printf("installing HTTP handler at %s",
		httptransport.JoinURL(urlPrefix, "{create|update|delete|find}"))
	mux.HandleFunc(httptransport.JoinURL(urlPrefix, "create"), h.handleCreate)
	mux.HandleFunc(httptransport.JoinURL(urlPrefix, "update"), h.handleUpdate)
	mux.HandleFunc(httptransport.JoinURL(urlPrefix, "delete"), h.handleDelete)
	mux.HandleFunc(httptransport.JoinURL(urlPrefix, "find"), h.handleFind)
	return mux
}

func (h *typeHandler) handleCreate(w http.ResponseWriter, req *http.Request) {
	obj := h.t.NewInstance()
	httptransport.ServeJSON(w, req, obj, func() (interface{}, error) {
		log.Printf("Create: %+v", obj)
		return nil, h.api.Create(req.Context(), obj)
	})
}

func (h *typeHandler) handleUpdate(w http.ResponseWriter, req *http.Request) {
	obj := h.t.NewInstance()
	httptransport.ServeJSON(w, req, obj, func() (interface{}, error) {
		log.Printf("Update: %+v", obj)
		return nil, h.api.Update(req.Context(), obj)
	})
}

func (h *typeHandler) handleDelete(w http.ResponseWriter, req *http.Request) {
	obj := h.t.NewInstance()
	httptransport.ServeJSON(w, req, obj, func() (interface{}, error) {
		log.Printf("Delete: %+v", obj)
		return nil, h.api.Delete(req.Context(), obj)
	})
}

func (h *typeHandler) handleFind(w http.ResponseWriter, req *http.Request) {
	// Transform the request query args to a query map.
	queryArgs := make(map[string]string)
	for k, vv := range req.URL.Query() {
		if len(vv) < 1 {
			continue
		}
		queryArgs[k] = vv[0]
	}

	w.Header().Set("Content-Type", "application/json")

	// Write the JSON content on-the-fly.
	i := 0
	err := h.api.Find(req.Context(), h.t.Name(), queryArgs, func(obj interface{}) error {
		if i == 0 {
			io.WriteString(w, "[") // nolint: errcheck
		} else {
			io.WriteString(w, ",") // nolint: errcheck
		}
		i++
		return json.NewEncoder(w).Encode(obj)
	})
	if err != nil {
		// Might be too late to return an error.
		log.Printf("query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	io.WriteString(w, "]") // nolint: errcheck
}

func (m *Model) API(api API, urlPrefix string) httpapi.Builder {
	return httpapi.BuilderFunc(func(hapi *httpapi.API) {
		// nolint: errcheck
		m.registry.each(func(t Type) error {
			pfx := httptransport.JoinURL(urlPrefix, t.Name()) + "/"
			hapi.Handle(pfx, newTypeHTTPHandler(t, api, pfx))
			return nil
		})
	})
}
